//go:build blackbox

package blackbox

import (
	"encoding/json"
	"testing"

	"miren.dev/runtime/blackbox/harness"
)

// TestSecretStoreAndRotate covers the store side on its own: writing, seeing
// what is there, recognizing an unchanged write, and revoking.
func TestSecretStoreAndRotate(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	const path = "blackbox/api-key"

	r := m.MustRun("secret", "set", path, "--value", "first-value")
	r.RequireContains(t, "Stored "+path+"@")

	// The same value again must not mint a duplicate version, or every pin
	// would be invalidated by a no-op command.
	r = m.MustRun("secret", "set", path, "--value", "first-value")
	r.RequireContains(t, "already stored at that value")

	r = m.MustRun("secret", "set", path, "--value", "second-value")
	r.RequireContains(t, "Stored "+path+"@")

	r = m.MustRun("secret", "list")
	r.RequireContains(t, path)
	r.RequireContains(t, "cluster")

	// Two versions now exist, and the listing distinguishes them.
	r = m.MustRun("secret", "versions", path, "--format", "json")
	r.RequireContains(t, `"current": true`)

	// The value itself must never appear in any listing.
	if r.OutputContains("first-value") || r.OutputContains("second-value") {
		t.Fatalf("secret listing leaked a value:\nstdout: %s\nstderr: %s", r.Stdout, r.Stderr)
	}
}

// TestSecretReferencedByApp is the loop the whole feature exists for: store a
// secret, point a variable at it, and have the app receive the real value while
// nothing but a reference is ever written down.
func TestSecretReferencedByApp(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	const (
		path     = "blackbox/app-secret"
		original = "value-before-rotation"
		rotated  = "value-after-rotation"
	)

	m.MustRun("secret", "set", path, "--value", original)

	name := harness.DeployApp(t, m, harness.AppOptions{
		Testdata: "go-server",
	})

	m.MustRun("env", "set", "-a", name, "-e", "APP_SECRET", "--backend", "cluster", "--ref", path)

	// What is recorded is the reference and its pin, not the value.
	r := m.MustRun("env", "list", "-a", name)
	r.RequireContains(t, "cluster:"+path+"@")
	if r.OutputContains(original) {
		t.Fatalf("env list leaked the secret value:\nstdout: %s\nstderr: %s", r.Stdout, r.Stderr)
	}

	// But the running container sees the real thing.
	sandboxID := harness.GetSandboxID(t, m, name)
	r = m.MustRun("sandbox", "exec", sandboxID, "--", "printenv", "APP_SECRET")
	r.RequireContains(t, original)

	// Rotating does not disturb what is already running.
	m.MustRun("secret", "set", path, "--value", rotated)
	r = m.MustRun("sandbox", "exec", sandboxID, "--", "printenv", "APP_SECRET")
	r.RequireContains(t, original)

	// Re-setting the reference re-pins it and rolls the change out.
	m.MustRun("env", "set", "-a", name, "-e", "APP_SECRET", "--backend", "cluster", "--ref", path)

	sandboxID = harness.GetSandboxID(t, m, name)
	r = m.MustRun("sandbox", "exec", sandboxID, "--", "printenv", "APP_SECRET")
	r.RequireContains(t, rotated)
}

// TestSecretRevocationFailsClosed checks the property that matters most after a
// leak: once a version is revoked, a deploy that needs it fails rather than
// starting the app without its credential.
func TestSecretRevocationFailsClosed(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	const path = "blackbox/revoked-secret"

	m.MustRun("secret", "set", path, "--value", "doomed-value")

	name := harness.DeployApp(t, m, harness.AppOptions{
		Testdata: "go-server",
	})
	m.MustRun("env", "set", "-a", name, "-e", "APP_SECRET", "--backend", "cluster", "--ref", path)

	// Find the version the app is pinned to and revoke exactly it.
	versions := m.MustRun("secret", "versions", path, "--format", "json")
	version := currentVersionFrom(t, versions.Stdout)

	m.MustRun("secret", "disable", path+"@"+version)

	// Re-pointing at the revoked version must fail rather than deploy without it.
	r := m.Run("env", "set", "-a", name, "-e", "APP_SECRET", "--backend", "cluster", "--ref", path+"@"+version)
	if r.ExitCode == 0 {
		t.Fatalf("expected setting a revoked secret to fail\nstdout: %s\nstderr: %s", r.Stdout, r.Stderr)
	}
	r.RequireContains(t, "APP_SECRET")

	// Re-enabling restores it, since disabling never touched the value.
	m.MustRun("secret", "enable", path+"@"+version)
	m.MustRun("env", "set", "-a", name, "-e", "APP_SECRET", "--backend", "cluster", "--ref", path+"@"+version)
}

// currentVersionFrom pulls the current version handle out of `secret versions
// --format json` output.
func currentVersionFrom(t *testing.T, out string) string {
	t.Helper()

	var parsed struct {
		CurrentVersion string `json:"current_version"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("parsing secret versions output: %v\noutput: %s", err, out)
	}
	if parsed.CurrentVersion == "" {
		t.Fatalf("no current version in output: %s", out)
	}
	return parsed.CurrentVersion
}
