//go:build blackbox

package blackbox

import (
	"path/filepath"
	"testing"
	"time"

	"miren.dev/runtime/blackbox/harness"
)

func TestValkeyAddonDeployWithAppToml(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.UniqueAppName(t, "bun-valkey")

	hostDir := filepath.Join(c.TestdataDir, "bun-valkey")
	containerDir := m.ContainerPath(hostDir)

	t.Cleanup(func() {
		t.Logf("cleaning up app %s", name)
		m.Run("app", "delete", name, "-f")
	})

	// Deploy without waiting for healthy — the app depends on VALKEY_URL
	// which is only injected after addon provisioning completes.
	m.MustRun("deploy", "-a", name, "-d", containerDir, "-f")

	// Wait for addon provisioning to complete.
	harness.WaitForAddonReady(t, m, name, "miren-valkey", 5*time.Minute)
	harness.WaitForEnvVar(t, m, name, "VALKEY_URL", 5*time.Minute)

	// Now wait for the app to become healthy
	harness.WaitForAppReady(t, m, name, 3*time.Minute)

	// Verify addon is listed
	r := m.MustRun("addon", "list", "-a", name, "--format", "json")
	r.RequireContains(t, "miren-valkey")

	// Set a route and verify the app responds with valkey connectivity
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

	// Verify the root endpoint works (exercises valkey AUTH + INCR)
	code, body, err := harness.HTTPGet(m, host, "/")
	if err != nil {
		t.Fatalf("HTTP GET / failed: %v", err)
	}
	if code != 200 {
		t.Fatalf("expected status 200, got %d: %s", code, body)
	}
}
