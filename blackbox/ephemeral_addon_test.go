//go:build blackbox

package blackbox

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"miren.dev/runtime/blackbox/harness"
	"miren.dev/runtime/pkg/model"
)

func ephemeralCloneConfigDir(t *testing.T, m *harness.Miren, source, addonName string) string {
	t.Helper()
	r := m.RunCmd("mktemp", "-d")
	r.RequireSuccess(t)
	dir := strings.TrimSpace(r.Stdout)
	t.Cleanup(func() { m.RunCmd("rm", "-rf", dir).RequireSuccess(t) })
	m.RunCmd("cp", "-a", source+"/.", dir).RequireSuccess(t)
	m.RunCmd("sed", "-i", `/^\[addons\.`+addonName+`\]/a clone = true`, dir+"/.miren/app.toml").RequireSuccess(t)
	return dir
}

func TestEphemeralPostgresqlCloneShared(t *testing.T) {
	testEphemeralPostgresqlClone(t, "bun-postgres")
}

func TestEphemeralPostgresqlCloneVersioned(t *testing.T) {
	testEphemeralPostgresqlClone(t, "bun-postgres-versioned")
}

func TestEphemeralPostgresqlCloneDedicatedToShared(t *testing.T) {
	testEphemeralPostgresqlClone(t, "dedicated-to-shared")
}

func TestEphemeralPostgresqlCloneDedicatedToNewShared(t *testing.T) {
	testEphemeralPostgresqlClone(t, "dedicated-to-new-shared")
}

// Keep each fixture independently shardable: expiry and GC wait on real time.
func testEphemeralPostgresqlClone(t *testing.T, fixture string) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)
	var name, dir string
	overrideVariant := strings.HasPrefix(fixture, "dedicated-to-")
	if overrideVariant {
		if fixture == "dedicated-to-shared" {
			harness.DeployApp(t, m, harness.AppOptions{Testdata: "bun-postgres"})
		}
		dir = ephemeralCloneConfigDir(t, m, m.ContainerPath(filepath.Join(c.TestdataDir, "bun-postgres-versioned")), "miren-postgresql")
		m.RunCmd("sed", "-i", `/^version = /d`, dir+"/.miren/app.toml").RequireSuccess(t)
		m.RunCmd("sed", "-i", `/^clone = true/a clone_variant = "shared"`, dir+"/.miren/app.toml").RequireSuccess(t)
		name = harness.UniqueAppName(t, "clone-variant")
		t.Cleanup(func() { m.MustRun("app", "delete", name, "-f") })
	} else {
		name = harness.DeployApp(t, m, harness.AppOptions{Testdata: fixture})
		dir = ephemeralCloneConfigDir(t, m, m.ContainerPath(filepath.Join(c.TestdataDir, fixture)), "miren-postgresql")
	}
	if fixture == "bun-postgres" {
		// Add an unsupported clone provider with cloning omitted. The web
		// fixture proxies a cache endpoint to the existing Valkey fixture.
		m.RunCmd("cp", m.ContainerPath(filepath.Join(c.TestdataDir, "bun-valkey", "index.ts")), dir+"/cache.ts").RequireSuccess(t)
		m.RunCmd("sed", "-i", "s/process.env.PORT || 3000/3001/", dir+"/cache.ts").RequireSuccess(t)
		m.RunCmd("sed", "-i", `1i import "./cache.ts";`, dir+"/index.ts").RequireSuccess(t)
		m.RunCmd("sed", "-i", `/const url = new URL(req.url);/a if (url.pathname === "/cache") return fetch("http://127.0.0.1:3001/");`, dir+"/index.ts").RequireSuccess(t)
		m.RunCmd("sh", "-c", `printf '\n[addons.miren-valkey]\nvariant = "small"\n' >> "$1/.miren/app.toml"`, "sh", dir).RequireSuccess(t)
	}
	m.MustRun("deploy", "-a", name, "-d", dir, "-f")
	activeVersion := getEphemeralAppVersion(t, m, name)
	host := name + ".test.local"
	previewHost := "clone-test." + host
	m.MustRun("route", "set", host, name)
	t.Cleanup(func() { m.Run("route", "remove", host) })

	ready := func(host string) {
		t.Helper()
		harness.Poll(t, host+" database ready", 5*time.Minute, 2*time.Second, func() (bool, string) {
			code, body, err := harness.HTTPGet(m, host, "/health")
			return err == nil && code == 200, fmt.Sprintf("status=%d body=%s err=%v", code, body, err)
		})
	}
	visit := func(host string) int {
		t.Helper()
		code, body, err := harness.HTTPGet(m, host, "/")
		if err != nil || code != 200 {
			t.Fatalf("visit %s: status=%d body=%s err=%v", host, code, body, err)
		}
		var count int
		if _, err := fmt.Sscanf(body, "Hello! This page has been visited %d times.", &count); err != nil {
			t.Fatalf("parsing visit count: %v: %s", err, body)
		}
		return count
	}
	checkVisit := func(host string, want int) {
		t.Helper()
		if got := visit(host); got != want {
			t.Fatalf("%s visits = %d, want %d (clone must copy data and isolate writes)", host, got, want)
		}
	}
	deployPreview := func(ttl string, fromVersion bool) string {
		t.Helper()
		args := []string{"deploy", "-a", name, "--ephemeral", "clone-test", "--ttl", ttl}
		if fromVersion {
			args = append(args, "--version", activeVersion)
		} else {
			args = append(args, "-d", dir, "-f")
		}
		m.MustRun(args...).
			RequireContains(t, "Ephemeral version")
		if got := getEphemeralAppVersion(t, m, name); got != activeVersion {
			t.Fatalf("ephemeral deploy changed active version: got %s, want %s", got, activeVersion)
		}
		var versions []struct {
			ID string `json:"id"`
		}
		r := m.MustRun("app", "versions", "-a", name, "--ephemeral", "--format", "json")
		if err := json.Unmarshal([]byte(r.Stdout), &versions); err != nil || len(versions) != 1 {
			t.Fatalf("expected exactly one preview: %s (err=%v)", r.Stdout, err)
		}
		ready(previewHost)
		return versions[0].ID
	}
	cloneCount := func(version string) int {
		t.Helper()
		r := m.MustRun("debug", "entity", "list", "-a", "dev.miren.addon/addon_association.app_version", "-V", version, "--format", "json")
		var clones []model.Document
		if err := json.Unmarshal([]byte(r.Stdout), &clones); err != nil {
			t.Fatalf("parsing clone associations: %v", err)
		}
		if overrideVariant && len(clones) > 0 {
			for _, clone := range clones {
				var variant, server string
				for _, facet := range clone.Facets {
					for _, field := range facet.Fields {
						if field.Name == "variant" {
							variant, _ = field.Value.(string)
						}
					}
				}
				for _, field := range clone.Unclaimed {
					if field.Name == "dev.miren.addon/postgresql_shared_data.postgres_server" {
						server, _ = field.Value.(string)
					}
				}
				if variant != "shared" || server != "postgres_server/pg-shared" {
					t.Fatalf("preview must use the cluster shared server: variant=%q server=%q", variant, server)
				}
			}
		}
		return len(clones)
	}

	ready(host)
	cacheVisits := 0
	cacheVisit := func(host string, want int) {
		t.Helper()
		code, body, err := harness.HTTPGet(m, host, "/cache")
		var got int
		_, parseErr := fmt.Sscanf(body, "Hello! Visit count: %d", &got)
		if err != nil || code != 200 || parseErr != nil || got != want {
			t.Fatalf("shared cache visit: status=%d body=%s err=%v, want count=%d", code, body, err, want)
		}
	}
	if fixture == "bun-postgres" {
		harness.Poll(t, "cache ready", time.Minute, time.Second, func() (bool, string) {
			code, body, err := harness.HTTPGet(m, host, "/cache")
			_, parseErr := fmt.Sscanf(body, "Hello! Visit count: %d", &cacheVisits)
			return err == nil && code == 200 && parseErr == nil, body
		})
	}
	visit(host)
	visit(host)
	seed := visit(host)
	first := deployPreview("1h", false)
	if got := cloneCount(first); got != 1 {
		t.Fatalf("clone association count = %d, want 1", got)
	}
	// Write to the primary before reading the preview: shared bindings
	// would produce seed+2 instead of the copied snapshot's seed+1.
	checkVisit(host, seed+1)
	checkVisit(previewHost, seed+1)
	checkVisit(previewHost, seed+2)
	checkVisit(previewHost, seed+3)
	checkVisit(host, seed+2)
	if fixture == "bun-postgres" {
		cacheVisit(previewHost, cacheVisits+1)
		cacheVisit(host, cacheVisits+2)
	}

	second := deployPreview("3m", true)
	if second == first {
		t.Fatal("replacement reused the old version")
	}
	checkVisit(previewHost, seed+3)
	checkVisit(host, seed+3)
	if fixture == "bun-postgres" {
		cacheVisit(previewHost, cacheVisits+3)
		cacheVisit(host, cacheVisits+4)
	}
	harness.Poll(t, "replaced clone removed", 5*time.Minute, 3*time.Second, func() (bool, string) {
		count := cloneCount(first)
		return count == 0, fmt.Sprintf("%d associations remain", count)
	})
	harness.Poll(t, "expired preview returns 404", 4*time.Minute, 3*time.Second, func() (bool, string) {
		code, body, err := harness.HTTPGet(m, previewHost, "/health")
		return err == nil && code == 404, fmt.Sprintf("status=%d body=%s err=%v", code, body, err)
	})
	harness.Poll(t, "expired clone removed by GC", 7*time.Minute, 5*time.Second, func() (bool, string) {
		count := cloneCount(second)
		return count == 0, fmt.Sprintf("%d associations remain", count)
	})
	ready(host)
	checkVisit(host, seed+4)
	m.MustRun("addon", "list", "-a", name, "--format", "json").RequireContains(t, `"status": "active"`)
	if overrideVariant {
		m.MustRun("addon", "list", "-a", name, "--format", "json").RequireContains(t, `"variant": "small"`)
	}
}

func TestEphemeralPostgresqlCloneVariantMismatch(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)
	harness.DeployApp(t, m, harness.AppOptions{Testdata: "bun-postgres"})
	name := harness.DeployApp(t, m, harness.AppOptions{Testdata: "bun-postgres-versioned"})
	active := getEphemeralAppVersion(t, m, name)
	dir := ephemeralCloneConfigDir(t, m, m.ContainerPath(filepath.Join(c.TestdataDir, "bun-postgres-versioned")), "miren-postgresql")
	m.RunCmd("sed", "-i", `/^clone = true/a clone_variant = "shared"`, dir+"/.miren/app.toml").RequireSuccess(t)
	m.RunCmd("sed", "-i", `s/^\[addons\.miren-postgresql\]/[addons."miren-postgresql:small"]/`, dir+"/.miren/app.toml").RequireSuccess(t)
	r := m.Run("deploy", "-a", name, "-d", dir, "--ephemeral", "mismatch", "--ttl", "1h", "-f")
	if r.Success() {
		t.Fatal("preview must not silently change PostgreSQL versions to use the shared server")
	}
	r.RequireContains(t, "shared servers do not support mixed versions")
	if got := getEphemeralAppVersion(t, m, name); got != active {
		t.Fatalf("failed preview changed primary version: got %s, want %s", got, active)
	}
	r = m.MustRun("app", "versions", "-a", name, "--ephemeral", "--format", "json")
	var previews []json.RawMessage
	if err := json.Unmarshal([]byte(r.Stdout), &previews); err != nil || len(previews) != 0 {
		t.Fatalf("failed preview must be cleaned up without a restart: %s (err=%v)", r.Stdout, err)
	}
	harness.WaitForAppReady(t, m, name, time.Minute)
}

func TestEphemeralAddonsSharedByDefault(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)
	name := harness.DeployApp(t, m, harness.AppOptions{Testdata: "bun-postgres"})
	activeVersion := getEphemeralAppVersion(t, m, name)
	host := name + ".test.local"
	m.MustRun("route", "set", host, name)
	t.Cleanup(func() { m.Run("route", "remove", host) })
	visit := func(host string) int {
		t.Helper()
		code, body, err := harness.HTTPGet(m, host, "/")
		if err != nil || code != 200 {
			t.Fatalf("visit %s: status=%d body=%s err=%v", host, code, body, err)
		}
		var count int
		if _, err := fmt.Sscanf(body, "Hello! This page has been visited %d times.", &count); err != nil {
			t.Fatalf("parsing visit count: %v: %s", err, body)
		}
		return count
	}
	for _, args := range [][]string{
		{"-d", m.ContainerPath(filepath.Join(c.TestdataDir, "bun-postgres")), "-f"},
		{"--version", activeVersion},
	} {
		m.MustRun(append([]string{"deploy", "-a", name, "--ephemeral", "shared", "--ttl", "1h"}, args...)...)
		harness.Poll(t, "shared preview ready", 3*time.Minute, 2*time.Second, func() (bool, string) {
			code, body, err := harness.HTTPGet(m, "shared."+host, "/health")
			return err == nil && code == 200, fmt.Sprintf("status=%d body=%s err=%v", code, body, err)
		})
		seed := visit(host)
		if got := visit("shared." + host); got != seed+1 {
			t.Fatalf("preview count=%d, want %d from shared primary", got, seed+1)
		}
		if got := visit(host); got != seed+2 {
			t.Fatalf("primary count=%d, want %d after preview write", got, seed+2)
		}
		var versions []struct {
			ID string `json:"id"`
		}
		r := m.MustRun("app", "versions", "-a", name, "--ephemeral", "--format", "json")
		if err := json.Unmarshal([]byte(r.Stdout), &versions); err != nil || len(versions) != 1 {
			t.Fatalf("expected one preview: %s (err=%v)", r.Stdout, err)
		}
		r = m.MustRun("debug", "entity", "list", "-a", "dev.miren.addon/addon_association.app_version", "-V", versions[0].ID, "--format", "json")
		var clones []json.RawMessage
		if err := json.Unmarshal([]byte(r.Stdout), &clones); err != nil || len(clones) != 0 {
			t.Fatalf("default preview created clones: %s (err=%v)", r.Stdout, err)
		}
		if got := getEphemeralAppVersion(t, m, name); got != activeVersion {
			t.Fatalf("preview changed active version: %s", got)
		}
	}
}

func TestEphemeralUnsupportedAddonFailsClosed(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)
	name := harness.DeployApp(t, m, harness.AppOptions{Testdata: "bun-valkey"})
	activeVersion := getEphemeralAppVersion(t, m, name)
	host := name + ".test.local"
	m.MustRun("route", "set", host, name)
	t.Cleanup(func() { m.Run("route", "remove", host) })
	// An unsupported clone provider must still work when cloning is omitted.
	m.MustRun("deploy", "-a", name, "--version", activeVersion, "--ephemeral", "shared", "--ttl", "1h")
	harness.Poll(t, "unsupported provider shared preview ready", 3*time.Minute, 2*time.Second, func() (bool, string) {
		code, body, err := harness.HTTPGet(m, "shared."+host, "/health")
		return err == nil && code == 200, fmt.Sprintf("status=%d body=%s err=%v", code, body, err)
	})
	dir := ephemeralCloneConfigDir(t, m, m.ContainerPath(filepath.Join(c.TestdataDir, "bun-valkey")), "miren-valkey")
	m.MustRun("deploy", "-a", name, "-d", dir, "-f")
	activeVersion = getEphemeralAppVersion(t, m, name)
	for _, args := range [][]string{{"-d", dir, "-f"}, {"--version", activeVersion}} {
		r := m.Run(append([]string{"deploy", "-a", name, "--ephemeral", "unsupported", "--ttl", "1h"}, args...)...)
		if r.Success() {
			t.Fatal("preview must fail instead of sharing an addon that cannot be cloned")
		}
		r.RequireContains(t, "does not support cloning")
		if r.OutputContains("addon destroy") {
			t.Fatal("clone failure must not recommend destroying the primary addon")
		}
		if got := getEphemeralAppVersion(t, m, name); got != activeVersion {
			t.Fatalf("failed preview changed active version: got %s, want %s", got, activeVersion)
		}
		code, body, err := harness.HTTPGet(m, "unsupported."+host, "/health")
		if err != nil || code == 200 {
			t.Fatalf("failed preview must not serve with primary credentials: status=%d body=%s err=%v", code, body, err)
		}
		r = m.MustRun("app", "versions", "-a", name, "--ephemeral", "--format", "json")
		var previews []struct {
			Label string `json:"ephemeral_label"`
		}
		if err := json.Unmarshal([]byte(r.Stdout), &previews); err != nil || len(previews) != 1 || previews[0].Label != "shared" {
			t.Fatalf("only the successful shared preview should remain: %s (err=%v)", r.Stdout, err)
		}
	}
	harness.Poll(t, "primary addon still usable", 30*time.Second, 2*time.Second, func() (bool, string) {
		code, body, err := harness.HTTPGet(m, host, "/health")
		return err == nil && code == 200, fmt.Sprintf("status=%d body=%s err=%v", code, body, err)
	})
}
