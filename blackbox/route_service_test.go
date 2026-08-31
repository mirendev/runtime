//go:build blackbox

package blackbox

import (
	"encoding/json"
	"fmt"
	"strings"
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
	var shownRoute struct {
		Service string `json:"service"`
	}
	if err := json.Unmarshal([]byte(shown.Stdout), &shownRoute); err != nil {
		t.Fatalf("decode route show JSON: %v", err)
	}
	if shownRoute.Service != "echo" {
		t.Fatalf("route show service = %q, want echo", shownRoute.Service)
	}

	listed := m.MustRun("route", "list", "--format", "json")
	var listedRoutes []struct {
		Host    string `json:"host"`
		Service string `json:"service"`
	}
	if err := json.Unmarshal([]byte(listed.Stdout), &listedRoutes); err != nil {
		t.Fatalf("decode route list JSON: %v", err)
	}
	foundRoute := false
	for _, route := range listedRoutes {
		if route.Host == host && route.Service == "echo" {
			foundRoute = true
			break
		}
	}
	if !foundRoute {
		t.Fatalf("route list has no route for %q with service echo: %s", host, listed.Stdout)
	}

	textList := m.MustRun("route", "list")
	assertRouteListService(t, textList.Stdout, host, "echo")

	harness.Poll(t, "named HTTP service responds via route", 30*time.Second, 2*time.Second, func() (bool, string) {
		code, body, err := harness.HTTPGet(m, host, "/")
		if err != nil {
			return false, err.Error()
		}
		if code != 200 || strings.TrimSpace(body) != "ok" {
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

// assertRouteListService finds one route-list row and checks the service column.
// The harness uses TERM=dumb, so text output has no terminal styling and each
// cell is separated by whitespace. Route hosts and service names cannot contain
// spaces, making Fields a stable way to assert the rendered row.
func assertRouteListService(t *testing.T, output, host, service string) {
	t.Helper()

	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != host {
			continue
		}
		if len(fields) < 3 {
			t.Fatalf("route list row for %q has %d fields, want at least 3: %q", host, len(fields), line)
		}
		if fields[2] != service {
			t.Fatalf("route list service for %q = %q, want %q: %q", host, fields[2], service, line)
		}
		return
	}

	t.Fatalf("route list has no row for %q: %s", host, output)
}
