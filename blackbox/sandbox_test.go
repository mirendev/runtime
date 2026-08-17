//go:build blackbox

package blackbox

import (
	"strings"
	"testing"

	"miren.dev/runtime/blackbox/harness"
)

func TestSandboxList(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.DeployApp(t, m, harness.AppOptions{
		Testdata: "go-server",
	})

	// List sandboxes — our app's sandbox should appear in the output.
	// Use JSON format since the table view shows short IDs, not app names.
	r := m.MustRun("sandbox", "list", "--format", "json")
	r.RequireContains(t, name)

	t.Run("app-filter", func(t *testing.T) {
		r := m.MustRun("sandbox", "list", "--app", name)
		r.RequireContains(t, name)
	})

	// The default blackbox mode shares one dev cluster, so a filter that
	// matched nothing would still look like a pass above. This is the half
	// that proves it excludes.
	t.Run("app-filter-excludes-others", func(t *testing.T) {
		r := m.MustRun("sandbox", "list", "--app", name+"-nope")
		if !strings.Contains(r.Stdout, "No sandboxes found") {
			t.Fatalf("expected an empty listing for an unknown app\nstdout: %s", r.Stdout)
		}
		if strings.Contains(r.Stdout, name+" ") {
			t.Fatalf("filter leaked another app's sandboxes\nstdout: %s", r.Stdout)
		}
	})

	t.Run("service-filter", func(t *testing.T) {
		r := m.MustRun("sandbox", "list", "--app", name, "--service", "web")
		r.RequireContains(t, name)
	})
}

// requireSandboxOf asserts that a `hostname` run landed in a sandbox belonging
// to app. It checks stdout alone, unlike Result.RequireContains: stderr carries
// miren's own chatter, and an app name appearing there would pass this without
// the exec ever having reached the right sandbox.
func requireSandboxOf(t *testing.T, r *harness.Result, app string) {
	t.Helper()

	if !strings.Contains(r.Stdout, app) {
		t.Fatalf("expected a sandbox of app %q, got hostname %q\nstderr: %s",
			app, strings.TrimSpace(r.Stdout), r.Stderr)
	}
}

func TestSandboxExec(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.DeployApp(t, m, harness.AppOptions{
		Testdata: "go-server",
	})

	sandboxID := harness.GetSandboxID(t, m, name)

	t.Run("positional", func(t *testing.T) {
		r := m.MustRun("sandbox", "exec", sandboxID, "--", "echo", "hello-from-sandbox")
		r.RequireContains(t, "hello-from-sandbox")
	})

	t.Run("id-flag", func(t *testing.T) {
		r := m.MustRun("sandbox", "exec", "-i", sandboxID, "--", "echo", "hello-from-sandbox")
		r.RequireContains(t, "hello-from-sandbox")
	})

	// These ask for the hostname rather than echoing a fixed string, because
	// the claim under test is *which* sandbox we reached, not that we reached
	// one. A sandbox's hostname is its entity name, which embeds the app name
	// (the same property harness.GetSandboxID leans on), so this fails if
	// selection wanders into another app — and other apps really are present,
	// since the default blackbox mode shares one dev cluster.
	//
	// The "which one did it pick" notice is deliberately not asserted: it only
	// fires on a terminal, and the harness runs with pipes.
	t.Run("app-flag", func(t *testing.T) {
		r := m.MustRun("sandbox", "exec", "-a", name, "--", "hostname")
		requireSandboxOf(t, r, name)
	})

	t.Run("app-flag-with-service", func(t *testing.T) {
		r := m.MustRun("sandbox", "exec", "-a", name, "--service", "web", "--", "hostname")
		requireSandboxOf(t, r, name)
	})

	t.Run("app-and-id-conflict", func(t *testing.T) {
		r := m.Run("sandbox", "exec", "-a", name, "-i", sandboxID, "--", "echo", "nope")
		if r.Success() {
			t.Fatalf("expected --app with --id to be rejected\nstdout: %s\nstderr: %s", r.Stdout, r.Stderr)
		}
		r.RequireContains(t, "--app and --id")
	})
}
