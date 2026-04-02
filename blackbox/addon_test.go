//go:build blackbox

package blackbox

import (
	"path/filepath"
	"testing"
	"time"

	"miren.dev/runtime/blackbox/harness"
)

func TestAddonListAvailable(t *testing.T) {
	t.Parallel()
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	r := m.MustRun("addon", "list-available")
	r.RequireContains(t, "miren-postgresql")
	r.RequireContains(t, "miren-mysql")
	r.RequireContains(t, "miren-valkey")
}

func TestAddonVariants(t *testing.T) {
	t.Parallel()
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	r := m.MustRun("addon", "variants", "miren-postgresql")
	r.RequireContains(t, "small")
	r.RequireContains(t, "shared")

	r = m.MustRun("addon", "variants", "miren-mysql")
	r.RequireContains(t, "small")
	r.RequireContains(t, "shared")

	r = m.MustRun("addon", "variants", "miren-valkey")
	r.RequireContains(t, "small")
}

func TestAddonCreateListDestroy(t *testing.T) {
	t.Parallel()
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.DeployApp(t, m, harness.AppOptions{
		Testdata: "go-server",
	})

	// Create a dedicated addon on the app (small variant gets its own PG server
	// per app, avoiding shared-server state issues between test runs)
	m.MustRun("addon", "create", "miren-postgresql:small", "-a", name)

	// Wait for addon to appear in the list, then for provisioning to complete.
	// The addon shows up in "addon list" immediately (status=pending), but env
	// vars aren't injected until the provisioning saga finishes.
	harness.WaitForAddonReady(t, m, name, "miren-postgresql", 30*time.Second)
	harness.WaitForEnvVar(t, m, name, "DATABASE_URL", 5*time.Minute)

	// Destroy the addon. The CLI sets the association status to "deprovisioning"
	// and the addon controller asynchronously tears down infrastructure and
	// removes env vars. We verify the command was accepted; full async cleanup
	// (env var removal, entity deletion) depends on the deprovision saga
	// completing, which requires infrastructure teardown.
	m.MustRun("addon", "destroy", "miren-postgresql", "-a", name, "--force")
}

func TestAddonDeployWithAppToml(t *testing.T) {
	t.Parallel()
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.UniqueAppName(t, "bun-postgres")

	hostDir := filepath.Join(c.TestdataDir, "bun-postgres")
	containerDir := m.ContainerPath(hostDir)

	t.Cleanup(func() {
		t.Logf("cleaning up app %s", name)
		m.Run("app", "delete", name, "-f")
	})

	// Deploy without waiting for healthy — the app depends on DATABASE_URL
	// which is only injected after addon provisioning completes. The launcher
	// defers pool creation until addons are ready, but we don't want
	// WaitForAppReady to fatal on transient "crashed" health during provisioning.
	m.MustRun("deploy", "-a", name, "-d", containerDir, "-f")

	// Wait for addon to appear in the list, then for provisioning to complete.
	// Env vars are the true readiness signal — they're injected only after the
	// full provisioning saga finishes (which may include creating the shared
	// PG server from scratch if a previous run's cleanup is still settling).
	harness.WaitForAddonReady(t, m, name, "miren-postgresql", 30*time.Second)
	harness.WaitForEnvVar(t, m, name, "DATABASE_URL", 5*time.Minute)

	// Now wait for the app to become healthy
	harness.WaitForAppReady(t, m, name, 3*time.Minute)

	// Verify addon is listed
	r := m.MustRun("addon", "list", "-a", name, "--format", "json")
	r.RequireContains(t, "miren-postgresql")

	// Set a route and verify the app responds with DB connectivity
	host := name + ".test.local"
	m.MustRun("route", "set", host, name)

	harness.Poll(t, "app responds via route", 30*time.Second, 2*time.Second, func() (bool, string) {
		code, body, err := harness.HTTPGet(m, host, "/health")
		if err != nil {
			return false, err.Error()
		}
		if code != 200 {
			return false, "status " + body
		}
		return true, ""
	})

	// Verify the root endpoint works (exercises DB writes/reads)
	code, body, err := harness.HTTPGet(m, host, "/")
	if err != nil {
		t.Fatalf("HTTP GET / failed: %v", err)
	}
	if code != 200 {
		t.Fatalf("expected status 200, got %d: %s", code, body)
	}
}

func TestMysqlAddonDeployWithAppToml(t *testing.T) {
	t.Parallel()
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.UniqueAppName(t, "bun-mysql")

	hostDir := filepath.Join(c.TestdataDir, "bun-mysql")
	containerDir := m.ContainerPath(hostDir)

	t.Cleanup(func() {
		t.Logf("cleaning up app %s", name)
		m.Run("app", "delete", name, "-f")
	})

	// Deploy without waiting for healthy — the app depends on DATABASE_URL
	// which is only injected after addon provisioning completes.
	m.MustRun("deploy", "-a", name, "-d", containerDir, "-f")

	// Wait for addon provisioning to complete.
	harness.WaitForAddonReady(t, m, name, "miren-mysql", 30*time.Second)
	harness.WaitForEnvVar(t, m, name, "DATABASE_URL", 5*time.Minute)

	// Now wait for the app to become healthy
	harness.WaitForAppReady(t, m, name, 3*time.Minute)

	// Verify addon is listed
	r := m.MustRun("addon", "list", "-a", name, "--format", "json")
	r.RequireContains(t, "miren-mysql")

	// Set a route and verify the app responds with DB connectivity
	host := name + ".test.local"
	m.MustRun("route", "set", host, name)

	harness.Poll(t, "app responds via route", 30*time.Second, 2*time.Second, func() (bool, string) {
		code, body, err := harness.HTTPGet(m, host, "/health")
		if err != nil {
			return false, err.Error()
		}
		if code != 200 {
			return false, "status " + body
		}
		return true, ""
	})

	// Verify the root endpoint works (exercises DB writes/reads)
	code, body, err := harness.HTTPGet(m, host, "/")
	if err != nil {
		t.Fatalf("HTTP GET / failed: %v", err)
	}
	if code != 200 {
		t.Fatalf("expected status 200, got %d: %s", code, body)
	}
}

func TestMysqlAddonCreateListDestroy(t *testing.T) {
	t.Parallel()
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.DeployApp(t, m, harness.AppOptions{
		Testdata: "go-server",
	})

	// Create a small (dedicated) MySQL addon on the app
	m.MustRun("addon", "create", "miren-mysql:small", "-a", name)

	// Wait for addon to appear and provisioning to complete.
	harness.WaitForAddonReady(t, m, name, "miren-mysql", 30*time.Second)
	harness.WaitForEnvVar(t, m, name, "DATABASE_URL", 5*time.Minute)

	// Verify MySQL-specific env vars are injected
	harness.WaitForEnvVar(t, m, name, "MYSQL_HOST", 30*time.Second)
	harness.WaitForEnvVar(t, m, name, "MYSQL_DATABASE", 30*time.Second)

	// Destroy the addon
	m.MustRun("addon", "destroy", "miren-mysql", "-a", name, "--force")
}

func TestValkeyAddonCreateListDestroy(t *testing.T) {
	t.Parallel()
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.DeployApp(t, m, harness.AppOptions{
		Testdata: "go-server",
	})

	// Create a small (dedicated) Valkey addon on the app
	m.MustRun("addon", "create", "miren-valkey:small", "-a", name)

	// Wait for addon to appear and provisioning to complete.
	harness.WaitForAddonReady(t, m, name, "miren-valkey", 30*time.Second)
	harness.WaitForEnvVar(t, m, name, "VALKEY_URL", 5*time.Minute)

	// Verify Valkey-specific env vars are injected
	harness.WaitForEnvVar(t, m, name, "VALKEY_HOST", 30*time.Second)
	harness.WaitForEnvVar(t, m, name, "VALKEY_PORT", 30*time.Second)

	// Verify REDIS_* aliases are also injected
	harness.WaitForEnvVar(t, m, name, "REDIS_URL", 30*time.Second)

	// Destroy the addon
	m.MustRun("addon", "destroy", "miren-valkey", "-a", name, "--force")
}

func TestAddonUnknownAddon(t *testing.T) {
	t.Parallel()
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.DeployApp(t, m, harness.AppOptions{
		Testdata: "go-server",
	})

	r := m.Run("addon", "create", "unknown-addon:small", "-a", name)
	if r.Success() {
		t.Fatal("expected addon create to fail for unknown addon")
	}
}
