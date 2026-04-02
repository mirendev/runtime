//go:build blackbox

package blackbox

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"miren.dev/runtime/blackbox/harness"
)

func TestCrashLoop(t *testing.T) {
	t.Parallel()
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.UniqueAppName(t, "crash-loop")
	t.Cleanup(func() {
		m.Run("app", "delete", name, "-f")
	})

	m.MustRun("deploy", "-a", name, "-d", m.ContainerPath(c.TestdataDir+"/crash-loop"), "-f")

	waitForAppCrashed(t, m, name)
}

func TestBadCommand(t *testing.T) {
	t.Parallel()
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.UniqueAppName(t, "bad-cmd")
	t.Cleanup(func() {
		m.Run("app", "delete", name, "-f")
	})

	m.MustRun("deploy", "-a", name, "-d", m.ContainerPath(c.TestdataDir+"/bad-command"), "-f")

	waitForAppCrashed(t, m, name)
}

// waitForAppCrashed polls app list until the named app reports "crashed" health.
func waitForAppCrashed(t *testing.T, m *harness.Miren, name string) {
	t.Helper()

	harness.Poll(t, "app crashed", 2*time.Minute, 3*time.Second, func() (bool, string) {
		r := m.Run("app", "list", "--format", "json")
		if !r.Success() {
			return false, "app list failed"
		}

		var apps []struct {
			Name   string `json:"name"`
			Health string `json:"health"`
		}
		if err := json.Unmarshal([]byte(r.Stdout), &apps); err != nil {
			return false, fmt.Sprintf("parse error: %v", err)
		}

		for _, app := range apps {
			if app.Name == name {
				if app.Health == "crashed" {
					return true, ""
				}
				return false, fmt.Sprintf("health: %s", app.Health)
			}
		}
		return false, "app not found"
	})
}
