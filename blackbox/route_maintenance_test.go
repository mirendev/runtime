//go:build blackbox

package blackbox

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"miren.dev/runtime/blackbox/harness"
)

func TestRouteMaintenance(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.DeployApp(t, m, harness.AppOptions{
		Testdata: "go-server",
	})

	host := name + ".test.local"
	m.MustRun("route", "set", host, name)

	harness.Poll(t, "app responds via route", 30*time.Second, 2*time.Second, func() (bool, string) {
		code, _, err := harness.HTTPGet(m, host, "/")
		if err != nil {
			return false, err.Error()
		}
		if code != 200 {
			return false, fmt.Sprintf("HTTP %d", code)
		}
		return true, ""
	})

	m.MustRun("route", "down", host, "--reason", "Upgrading the database", "--back-at", "30m")

	harness.Poll(t, "holding page served", 15*time.Second, 1*time.Second, func() (bool, string) {
		code, body, err := harness.HTTPGet(m, host, "/")
		if err != nil {
			return false, err.Error()
		}
		if code != 503 {
			return false, fmt.Sprintf("HTTP %d", code)
		}
		if !strings.Contains(body, "Upgrading the database") {
			return false, "response does not carry the maintenance reason"
		}
		// The heading names the site the visitor opened, not the app behind it.
		if !strings.Contains(body, host+" is down for maintenance") {
			return false, "holding page does not name the host that was visited"
		}
		if strings.Contains(body, name+" is down") {
			return false, "holding page leaks the internal app name"
		}
		return true, ""
	})

	// The window is only useful if the app itself is still reachable for
	// migrations, so the headers and the app's own liveness both matter.
	headers := httpHeadersDuringMaintenance(t, m, host)
	if !strings.Contains(headers, "retry-after:") {
		t.Fatalf("expected a Retry-After header during maintenance, got:\n%s", headers)
	}
	if !strings.Contains(headers, "cache-control: no-store") {
		t.Fatalf("expected Cache-Control: no-store during maintenance, got:\n%s", headers)
	}

	jsonBody := httpJSONDuringMaintenance(t, m, host)
	if !strings.Contains(jsonBody, `"error":"maintenance"`) && !strings.Contains(jsonBody, `"error": "maintenance"`) {
		t.Fatalf("expected a JSON maintenance body for an Accept: application/json client, got:\n%s", jsonBody)
	}

	// The window exists so migrations can run against a quiet app, so the app
	// staying reachable through `app run` is the behavior, not a side effect.
	r := m.MustRun("app", "run", "-a", name, "--", "echo", "migration-ran")
	r.RequireContains(t, "migration-ran")

	r = m.MustRun("route", "list")
	r.RequireContains(t, "maintenance")

	r = m.MustRun("route", "show", host)
	r.RequireContains(t, "Upgrading the database")

	r = m.MustRun("app", "status", "-a", name)
	r.RequireContains(t, "Maintenance")

	m.MustRun("route", "up", host)

	harness.Poll(t, "route serves the app again", 15*time.Second, 1*time.Second, func() (bool, string) {
		code, body, err := harness.HTTPGet(m, host, "/")
		if err != nil {
			return false, err.Error()
		}
		if code != 200 {
			return false, fmt.Sprintf("HTTP %d", code)
		}
		if strings.Contains(body, "Upgrading the database") {
			return false, "still serving the holding page"
		}
		return true, ""
	})

	// Bringing a route up twice is not an error; the reversal path shouldn't
	// make an operator stop and work out whether they already ran it.
	m.MustRun("route", "up", host)
}

// TestRouteDownDefaultNeedsConfirmation covers the guardrail on the widest
// command available: the default route catches every hostname without a route
// of its own. JSON output can't carry a prompt, so it must not be a way around
// one either.
func TestRouteDownDefaultNeedsConfirmation(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	r := m.Run("route", "down", "--default", "--json", "--reason", "Cluster upgrade")
	if r.ExitCode == 0 {
		t.Fatalf("expected --default --json without --yes to be refused, got exit 0: %s", r.Stdout)
	}
	r.RequireContains(t, "--yes")
}

func httpHeadersDuringMaintenance(t *testing.T, m *harness.Miren, host string) string {
	t.Helper()

	r := m.RunCmd("curl", "-sSkI",
		"-H", fmt.Sprintf("Host: %s", host),
		"--max-time", "10",
		"https://localhost:443/")

	if !r.Success() {
		t.Fatalf("curl for maintenance headers failed (exit %d): %s", r.ExitCode, r.Stderr)
	}

	return strings.ToLower(r.Stdout)
}

func httpJSONDuringMaintenance(t *testing.T, m *harness.Miren, host string) string {
	t.Helper()

	r := m.RunCmd("curl", "-sSk",
		"-H", fmt.Sprintf("Host: %s", host),
		"-H", "Accept: application/json",
		"--max-time", "10",
		"https://localhost:443/")

	if !r.Success() {
		t.Fatalf("curl for maintenance JSON failed (exit %d): %s", r.ExitCode, r.Stderr)
	}

	return r.Stdout
}
