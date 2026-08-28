//go:build blackbox

package blackbox

import (
	"fmt"
	"testing"
	"time"

	"miren.dev/runtime/blackbox/harness"
)

func TestRouteSetService(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.DeployApp(t, m, harness.AppOptions{Testdata: "tcp-echo"})
	host := name + ".service.test.local"

	m.MustRun("route", "set", host, name, "--service", "echo")

	shown := m.MustRun("route", "show", host, "--format", "json")
	shown.RequireContains(t, `"service":"echo"`)

	listed := m.MustRun("route", "list", "--format", "json")
	listed.RequireContains(t, `"service":"echo"`)

	textList := m.MustRun("route", "list")
	textList.RequireContains(t, "SERVICE")
	textList.RequireContains(t, "echo")

	harness.Poll(t, "named HTTP service responds via route", 30*time.Second, 2*time.Second, func() (bool, string) {
		code, body, err := harness.HTTPGet(m, host, "/")
		if err != nil {
			return false, err.Error()
		}
		if code != 200 || body != "ok" {
			return false, fmt.Sprintf("HTTP %d: %q", code, body)
		}
		return true, ""
	})

	missing := m.Run("route", "set", "missing."+host, name, "--service", "missing")
	if missing.Success() {
		t.Fatalf("expected unknown service to be rejected\nstdout: %s\nstderr: %s", missing.Stdout, missing.Stderr)
	}
	missing.RequireContains(t, "does not exist in the active configuration")
}
