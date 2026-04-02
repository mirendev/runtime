//go:build blackbox

package blackbox

import (
	"testing"

	"miren.dev/runtime/blackbox/harness"
)

func TestRouteSetListRemove(t *testing.T) {
	t.Parallel()
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.DeployApp(t, m, harness.AppOptions{
		Testdata: "go-server",
	})

	host := name + ".test.local"

	// Set a route
	m.MustRun("route", "set", host, name)

	// List routes — should include our host
	r := m.MustRun("route", "list")
	r.RequireContains(t, host)

	// Remove the route
	m.MustRun("route", "remove", host)

	// Verify it's gone
	r = m.MustRun("route", "list")
	if r.OutputContains(host) {
		t.Fatalf("route %s still present after removal", host)
	}
}
