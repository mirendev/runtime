//go:build blackbox

package blackbox

import (
	"encoding/json"
	"testing"

	"miren.dev/runtime/blackbox/harness"
)

func TestRouteTimeoutSetShowClear(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.DeployApp(t, m, harness.AppOptions{
		Testdata: "go-server",
	})

	host := name + ".timeout.test.local"

	m.MustRun("route", "set", host, name).RequireSuccess(t)

	// No override to begin with
	r := m.MustRun("route", "timeout", host)
	r.RequireSuccess(t)
	r.RequireContains(t, "server default")

	// Set an override
	r = m.MustRun("route", "timeout", host, "10m")
	r.RequireSuccess(t)
	r.RequireContains(t, "10m")

	// It shows up on the route
	r = m.MustRun("route", "show", host)
	r.RequireSuccess(t)
	r.RequireContains(t, "10m")

	r = m.MustRun("route", "show", host, "--format", "json")
	r.RequireSuccess(t)

	var shown struct {
		RequestTimeout string `json:"request_timeout"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &shown); err != nil {
		t.Fatalf("failed to parse route show JSON: %v (stdout: %s)", err, r.Stdout)
	}
	if shown.RequestTimeout != "10m" {
		t.Errorf("expected request_timeout 10m, got %q", shown.RequestTimeout)
	}

	// And in the list
	r = m.MustRun("route", "list")
	r.RequireSuccess(t)
	r.RequireContains(t, "TIMEOUT")

	// Invalid values are rejected without disturbing the stored one
	r = m.Run("route", "timeout", host, "10")
	if r.Success() {
		t.Errorf("expected a bare number to be rejected\nstdout: %s\nstderr: %s", r.Stdout, r.Stderr)
	}

	r = m.MustRun("route", "timeout", host, "--format", "json")
	r.RequireSuccess(t)
	if err := json.Unmarshal([]byte(r.Stdout), &shown); err != nil {
		t.Fatalf("failed to parse route timeout JSON: %v (stdout: %s)", err, r.Stdout)
	}
	if shown.RequestTimeout != "10m" {
		t.Errorf("expected request_timeout to stay 10m, got %q", shown.RequestTimeout)
	}

	// Clearing puts the route back on the server default
	r = m.MustRun("route", "timeout", host, "--clear")
	r.RequireSuccess(t)
	r.RequireContains(t, "server default")

	r = m.MustRun("route", "timeout", host, "--format", "json")
	r.RequireSuccess(t)
	// Unmarshal into a fresh value: request_timeout is omitted once cleared, so
	// reusing `shown` would leave the previous value in place.
	var cleared struct {
		RequestTimeout string `json:"request_timeout"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &cleared); err != nil {
		t.Fatalf("failed to parse route timeout JSON: %v (stdout: %s)", err, r.Stdout)
	}
	if cleared.RequestTimeout != "" {
		t.Errorf("expected request_timeout to be cleared, got %q", cleared.RequestTimeout)
	}

	// Clearing again is a no-op
	r = m.MustRun("route", "timeout", host, "--clear")
	r.RequireSuccess(t)
	r.RequireContains(t, "No request timeout override")
}
