//go:build blackbox

package blackbox

import (
	"strings"
	"testing"
	"time"

	"miren.dev/runtime/blackbox/harness"
)

// attrValues extracts every value for a fully-qualified attribute id from the
// output of `debug entity list -k <kind>`. The output pairs an `- id: <attr>`
// line with a following `value: <v>` line.
func attrValues(out, attrID string) []string {
	lines := strings.Split(out, "\n")
	var vals []string
	for i, ln := range lines {
		if strings.TrimSpace(ln) != "- id: "+attrID {
			continue
		}
		for j := i + 1; j < len(lines) && j <= i+2; j++ {
			if idx := strings.Index(lines[j], "value:"); idx >= 0 {
				vals = append(vals, strings.TrimSpace(lines[j][idx+len("value:"):]))
				break
			}
		}
	}
	return vals
}

// scopedSpecField returns a spec field's value from the entity block whose
// header contains headerMatch. Dedicated addons (valkey) create one server per
// app, so a global list can hold several; this picks the one for a given app.
func scopedSpecField(t *testing.T, m *harness.Miren, kind, headerMatch, field string) string {
	t.Helper()
	r := m.MustRun("debug", "entity", "list", "-k", kind)
	for _, b := range strings.Split(r.Stdout, "\nid: ") {
		if !strings.Contains(b, headerMatch) {
			continue
		}
		for _, ln := range strings.Split(b, "\n") {
			ln = strings.TrimSpace(ln)
			if strings.HasPrefix(ln, field+":") {
				return strings.TrimSpace(strings.SplitN(ln, ":", 2)[1])
			}
		}
	}
	return ""
}

// waitForRotationsSettled polls until no rotation_request is still pending or
// rotating, and fails if any settled in the "error" state. Blackbox runs are
// serial (-p 1), so the only in-flight rotation is the test's own.
func waitForRotationsSettled(t *testing.T, m *harness.Miren, timeout time.Duration) {
	t.Helper()
	harness.Poll(t, "rotation requests settled", timeout, 3*time.Second, func() (bool, string) {
		r := m.MustRun("debug", "entity", "list", "-k", "rotation_request")
		statuses := attrValues(r.Stdout, "dev.miren.addon/rotation_request.status")
		for _, s := range statuses {
			if s == "error" {
				return false, "a rotation request is in error state"
			}
			if s == "pending" || s == "rotating" {
				return false, "rotation still in progress (" + s + ")"
			}
		}
		return true, ""
	})
}

// TestAddonRotateValkey rotates a dedicated Valkey password and asserts the
// backing server credential and pool are both replaced, and the app stays up.
func TestAddonRotateValkey(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.DeployApp(t, m, harness.AppOptions{Testdata: "go-server"})

	m.MustRun("addon", "create", "miren-valkey:small", "-a", name)
	harness.WaitForAddonReady(t, m, name, "miren-valkey", 30*time.Second)
	harness.WaitForEnvVar(t, m, name, "VALKEY_PASSWORD", 5*time.Minute)

	// Scope to this app's dedicated server (named vk-<app>-s<id>).
	scope := "vk-" + name + "-"
	pwBefore := scopedSpecField(t, m, "valkey_server", scope, "password")
	poolBefore := scopedSpecField(t, m, "valkey_server", scope, "sandbox_pool")
	if pwBefore == "" || poolBefore == "" {
		t.Fatalf("could not find valkey server for app %s (pw=%q pool=%q)", name, pwBefore, poolBefore)
	}

	m.MustRun("addon", "rotate", "miren-valkey", "-a", name, "--force")
	waitForRotationsSettled(t, m, 3*time.Minute)

	pwAfter := scopedSpecField(t, m, "valkey_server", scope, "password")
	poolAfter := scopedSpecField(t, m, "valkey_server", scope, "sandbox_pool")

	if pwBefore == pwAfter {
		t.Errorf("expected valkey password to change, still %q", pwAfter)
	}
	if poolBefore == poolAfter {
		t.Errorf("expected valkey pool to be re-launched, still %q", poolAfter)
	}

	harness.WaitForAppReady(t, m, name, 3*time.Minute)
}

// TestAddonRotateSharedPostgresSuperuser rotates the shared server's superuser
// password and guards the disk-naming decoupling: the password must change while
// disk_name stays put (a naive change used to move the data disk).
func TestAddonRotateSharedPostgresSuperuser(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.DeployApp(t, m, harness.AppOptions{Testdata: "go-server"})

	m.MustRun("addon", "create", "miren-postgresql:shared", "-a", name)
	harness.WaitForAddonReady(t, m, name, "miren-postgresql", 30*time.Second)
	harness.WaitForEnvVar(t, m, name, "DATABASE_URL", 5*time.Minute)

	// The shared server is the singleton named "pg-shared"; scope to it so
	// dedicated postgres_server entities from other tests can't interfere.
	const shared = "postgres_server/pg-shared"
	suBefore := scopedSpecField(t, m, "postgres_server", shared, "superuser_password")
	diskBefore := scopedSpecField(t, m, "postgres_server", shared, "disk_name")
	if suBefore == "" || diskBefore == "" {
		t.Fatalf("expected superuser password and disk_name before rotation (su=%q disk=%q)", suBefore, diskBefore)
	}

	m.MustRun("addon", "rotate", "miren-postgresql", "-a", name, "--credential", "superuser", "--force")
	waitForRotationsSettled(t, m, 3*time.Minute)

	suAfter := scopedSpecField(t, m, "postgres_server", shared, "superuser_password")
	diskAfter := scopedSpecField(t, m, "postgres_server", shared, "disk_name")

	if suBefore == suAfter {
		t.Errorf("expected superuser password to change, still %q", suAfter)
	}
	if diskBefore != diskAfter {
		t.Errorf("disk_name must stay stable across superuser rotation: %q -> %q", diskBefore, diskAfter)
	}
}

// TestAddonRotateSharedPostgresUser rotates the per-app database user and
// asserts the rotation completes and the consuming app is redeployed healthy.
func TestAddonRotateSharedPostgresUser(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.DeployApp(t, m, harness.AppOptions{Testdata: "go-server"})

	m.MustRun("addon", "create", "miren-postgresql:shared", "-a", name)
	harness.WaitForAddonReady(t, m, name, "miren-postgresql", 30*time.Second)
	harness.WaitForEnvVar(t, m, name, "DATABASE_URL", 5*time.Minute)

	r := m.MustRun("addon", "rotate", "miren-postgresql", "-a", name, "--force")
	r.RequireContains(t, "Rotation requested")
	waitForRotationsSettled(t, m, 3*time.Minute)

	harness.WaitForAppReady(t, m, name, 3*time.Minute)
}
