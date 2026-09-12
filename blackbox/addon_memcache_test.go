//go:build blackbox

package blackbox

import (
	"path/filepath"
	"testing"
	"time"

	"miren.dev/runtime/blackbox/harness"
)

func TestMemcacheAddonCreateListDestroy(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.DeployApp(t, m, harness.AppOptions{
		Testdata: "go-server",
	})

	// Create a small (dedicated) Memcache addon on the app
	m.MustRun("addon", "create", "miren-memcache:small", "-a", name)

	// Wait for addon to appear and provisioning to complete.
	harness.WaitForAddonReady(t, m, name, "miren-memcache", 5*time.Minute)
	harness.WaitForEnvVar(t, m, name, "MEMCACHE_URL", 5*time.Minute)

	// Verify Memcache-specific env vars are injected
	harness.WaitForEnvVar(t, m, name, "MEMCACHE_HOST", 30*time.Second)
	harness.WaitForEnvVar(t, m, name, "MEMCACHE_PORT", 30*time.Second)

	// Destroy the addon and verify full async cleanup completes.
	m.MustRun("addon", "destroy", "miren-memcache", "-a", name, "--force")
	harness.WaitForAddonRemoved(t, m, name, "miren-memcache", 5*time.Minute)
	harness.WaitForEnvVarRemoved(t, m, name, "MEMCACHE_URL", 5*time.Minute)
}

func TestMemcacheAddonDeployWithAppToml(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.UniqueAppName(t, "bun-memcache")

	hostDir := filepath.Join(c.TestdataDir, "bun-memcache")
	containerDir := m.ContainerPath(hostDir)

	t.Cleanup(func() {
		t.Logf("cleaning up app %s", name)
		m.Run("app", "delete", name, "-f")
	})

	// Deploy without waiting for healthy — the app depends on MEMCACHE_HOST
	// which is only injected after addon provisioning completes.
	m.MustRun("deploy", "-a", name, "-d", containerDir, "-f")

	// Wait for addon provisioning to complete.
	harness.WaitForAddonReady(t, m, name, "miren-memcache", 5*time.Minute)
	harness.WaitForEnvVar(t, m, name, "MEMCACHE_HOST", 5*time.Minute)

	// Now wait for the app to become healthy
	harness.WaitForAppReady(t, m, name, 3*time.Minute)

	// Verify addon is listed
	r := m.MustRun("addon", "list", "-a", name, "--format", "json")
	r.RequireContains(t, "miren-memcache")

	// Set a route and verify the app responds with memcache connectivity
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

	// Verify the root endpoint works (exercises memcache writes/reads)
	code, body, err := harness.HTTPGet(m, host, "/")
	if err != nil {
		t.Fatalf("HTTP GET / failed: %v", err)
	}
	if code != 200 {
		t.Fatalf("expected status 200, got %d: %s", code, body)
	}
}
