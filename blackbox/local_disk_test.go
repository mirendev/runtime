//go:build blackbox

package blackbox

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"miren.dev/runtime/blackbox/harness"
)

func TestLocalDiskPersistence(t *testing.T) {
	t.Parallel()
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.DeployApp(t, m, harness.AppOptions{
		Testdata: "local-disk-app",
	})

	// Set a route so we can reach the app via HTTP ingress
	host := name + ".test.local"
	m.MustRun("route", "set", host, name)
	t.Cleanup(func() {
		m.Run("route", "remove", host)
	})

	// Wait for HTTP to be reachable
	harness.Poll(t, "HTTP reachable", 30*time.Second, 2*time.Second, func() (bool, string) {
		code, _, err := harness.HTTPGet(m, host, "/health")
		if err != nil {
			return false, fmt.Sprintf("HTTP error: %v", err)
		}
		if code != 200 {
			return false, fmt.Sprintf("HTTP status %d", code)
		}
		return true, ""
	})

	// Write data to the local disk via the app
	r := m.RunCmd("curl", "-sk", "-X", "POST", "-d", "hello-from-disk",
		"-H", fmt.Sprintf("Host: %s", host),
		"-w", "\n%{http_code}",
		fmt.Sprintf("https://localhost:443/data"))
	r.RequireSuccess(t)
	r.RequireContains(t, "201")

	// Read it back to confirm the write worked
	code, body, err := harness.HTTPGet(m, host, "/data")
	if err != nil {
		t.Fatalf("failed to read data: %v", err)
	}
	if code != 200 || body != "hello-from-disk" {
		t.Fatalf("expected 200/hello-from-disk, got %d/%q", code, body)
	}

	// Record the current version
	versionBefore := getAppVersion(t, m, name)
	t.Logf("version before redeploy: %s", versionBefore)

	// Trigger a redeploy by setting an env var
	m.MustRun("env", "set", "-a", name, "-e", "REDEPLOY_MARKER=v2")
	harness.WaitForAppReady(t, m, name, 2*time.Minute)

	// Verify we got a new version
	versionAfter := getAppVersion(t, m, name)
	t.Logf("version after redeploy: %s", versionAfter)
	if versionBefore == versionAfter {
		t.Fatalf("expected version to change after redeploy, still %s", versionBefore)
	}

	// Wait for the new version to be serving HTTP
	harness.Poll(t, "new version HTTP reachable", 30*time.Second, 2*time.Second, func() (bool, string) {
		code, _, err := harness.HTTPGet(m, host, "/health")
		if err != nil {
			return false, fmt.Sprintf("HTTP error: %v", err)
		}
		if code != 200 {
			return false, fmt.Sprintf("HTTP status %d", code)
		}
		return true, ""
	})

	// Read data back — it should have survived the redeploy
	code, body, err = harness.HTTPGet(m, host, "/data")
	if err != nil {
		t.Fatalf("failed to read data after redeploy: %v", err)
	}
	if code != 200 {
		t.Fatalf("expected 200 after redeploy, got %d (body: %q)", code, body)
	}
	if body != "hello-from-disk" {
		t.Fatalf("data did not survive redeploy: expected %q, got %q", "hello-from-disk", body)
	}
}

func getAppVersion(t *testing.T, m *harness.Miren, appName string) string {
	t.Helper()
	r := m.MustRun("app", "list", "--format", "json")
	var apps []struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &apps); err != nil {
		t.Fatalf("failed to parse app list JSON: %v", err)
	}
	for _, app := range apps {
		if app.Name == appName {
			return app.Version
		}
	}
	t.Fatalf("app %s not found in app list", appName)
	return ""
}
