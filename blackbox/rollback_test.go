//go:build blackbox

package blackbox

import (
	"encoding/json"
	"testing"
	"time"

	"miren.dev/runtime/blackbox/harness"
)

func TestDeployAndRollback(t *testing.T) {
	t.Parallel()
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.UniqueAppName(t, "go-server")
	t.Cleanup(func() {
		m.Run("app", "delete", name, "-f")
	})

	// Deploy v1
	m.MustRun("deploy", "-a", name, "-d", m.ContainerPath(c.TestdataDir+"/go-server"), "-f")
	harness.WaitForAppReady(t, m, name, 2*time.Minute)

	// Capture v1 version ID
	r := m.MustRun("app", "list", "--format", "json")
	v1Version := extractVersion(t, r.Stdout, name)
	t.Logf("v1 version: %s", v1Version)

	// Deploy v2 (same source, new version ID created)
	m.MustRun("deploy", "-a", name, "-d", m.ContainerPath(c.TestdataDir+"/go-server"), "-f")
	harness.WaitForAppReady(t, m, name, 2*time.Minute)

	// Capture v2 version ID — should be different
	r = m.MustRun("app", "list", "--format", "json")
	v2Version := extractVersion(t, r.Stdout, name)
	t.Logf("v2 version: %s", v2Version)

	if v1Version == v2Version {
		t.Fatal("v1 and v2 versions should differ")
	}

	// Roll back to v1 by deploying the previous version
	m.MustRun("deploy", "-a", name, "-V", v1Version, "-f")
	harness.WaitForAppReady(t, m, name, 2*time.Minute)

	// Verify the active version is back to v1
	r = m.MustRun("app", "list", "--format", "json")
	activeVersion := extractVersion(t, r.Stdout, name)
	if activeVersion != v1Version {
		t.Fatalf("expected active version %s after rollback, got %s", v1Version, activeVersion)
	}
}

func extractVersion(t *testing.T, jsonOutput, appName string) string {
	t.Helper()
	var apps []struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(jsonOutput), &apps); err != nil {
		t.Fatalf("failed to parse app list: %v", err)
	}
	for _, app := range apps {
		if app.Name == appName {
			return app.Version
		}
	}
	t.Fatalf("app %s not found in app list", appName)
	return ""
}
