//go:build blackbox

package blackbox

import (
	"testing"
	"time"

	"miren.dev/runtime/blackbox/harness"
)

func TestDeployGoServer(t *testing.T) {
	t.Parallel()
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.DeployApp(t, m, harness.AppOptions{
		Testdata: "go-server",
	})

	// Verify it shows up in app list
	r := m.MustRun("app", "list", "--format", "json")
	r.RequireContains(t, name)

	// Verify logs are flowing
	harness.Poll(t, "logs available", 30*time.Second, 2*time.Second,
		func() (bool, string) {
			r := m.Run("logs", "-a", name)
			if r.OutputContains("starting on port") || r.OutputContains("Server starting") {
				return true, ""
			}
			return false, "no startup log yet"
		},
	)
}
