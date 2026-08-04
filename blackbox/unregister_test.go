//go:build blackbox

package blackbox

import (
	"strings"
	"testing"

	"miren.dev/runtime/blackbox/harness"
)

// TestServerUnregister exercises `miren server unregister` against a real
// cloud: the cluster asks to be removed using its own service account key,
// then clears the registration files it authenticated with.
//
// The interesting property is that the cloud side genuinely takes effect.
// Deleting a cluster only soft-deletes its row and the service account
// outlives it, so the test stashes the credentials before unregistering and
// replays them afterwards to confirm they no longer authenticate.
func TestServerUnregister(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)
	harness.NewCloudEnv(t, m)

	const serverDir = "/var/lib/miren/server"
	const stashDir = "/tmp/bb-unregister-stash"

	// Files that belong to something other than cloud registration. Unregister
	// must leave every one of them alone: the first four secure CLI-to-cluster
	// auth, and the last two sign end-user OIDC sessions and the cluster's own
	// workload identities.
	bystanders := []string{"ca.crt", "ca.key", "api.crt", "api.key", "oidc-signing.key", "workload-identity.key"}
	for _, name := range bystanders {
		m.RunCmdAsRoot("bash", "-c", "test -f "+serverDir+"/"+name+" || touch "+serverDir+"/"+name)
	}

	// The replay below is only meaningful if the stash actually happened. A
	// silently failed copy would leave nothing to restore, and the replay would
	// then "fail" for want of credentials rather than because they were revoked.
	stash := m.RunCmdAsRoot("bash", "-c", "mkdir -p "+stashDir+" && cp "+serverDir+"/registration.json "+serverDir+"/service-account.key "+stashDir+"/")
	if !stash.Success() {
		t.Fatalf("failed to stash registration credentials (exit %d):\n%s\n%s", stash.ExitCode, stash.Stdout, stash.Stderr)
	}
	t.Cleanup(func() {
		m.RunCmdAsRoot("rm", "-rf", stashDir)
	})

	r := m.RunCmdAsRoot("m", "server", "unregister", "--force")
	if !r.Success() {
		t.Fatalf("unregister failed (exit %d):\n%s\n%s", r.ExitCode, r.Stdout, r.Stderr)
	}

	status := m.RunCmdAsRoot("m", "server", "register", "status")
	if !strings.Contains(status.Stdout, "No cluster registration found") {
		t.Errorf("expected no registration after unregister, got:\n%s", status.Stdout)
	}

	for _, name := range []string{"registration.json", "service-account.key"} {
		check := m.RunCmdAsRoot("bash", "-c", "test -e "+serverDir+"/"+name+" && echo present || echo gone")
		if !strings.Contains(check.Stdout, "gone") {
			t.Errorf("%s should have been removed by unregister", name)
		}
	}

	for _, name := range bystanders {
		check := m.RunCmdAsRoot("bash", "-c", "test -e "+serverDir+"/"+name+" && echo present || echo gone")
		if !strings.Contains(check.Stdout, "present") {
			t.Errorf("%s is not cloud registration material and should have survived unregister", name)
		}
	}

	// Put the credentials back and try again. The cluster is gone from the
	// organization and its service account is revoked, so the key that worked a
	// moment ago must no longer get a token.
	m.RunCmdAsRoot("bash", "-c", "cp "+stashDir+"/registration.json "+stashDir+"/service-account.key "+serverDir+"/")

	replay := m.RunCmdAsRoot("m", "server", "unregister", "--force")
	if replay.Success() {
		t.Errorf("revoked credentials still authenticated to cloud:\n%s", replay.Stdout)
	}

	// Local state is untouched by the failed attempt, so the operator can still
	// recover with --local-only.
	local := m.RunCmdAsRoot("m", "server", "unregister", "--force", "--local-only")
	if !local.Success() {
		t.Fatalf("--local-only unregister failed (exit %d):\n%s\n%s", local.ExitCode, local.Stdout, local.Stderr)
	}

	for _, name := range []string{"registration.json", "service-account.key"} {
		check := m.RunCmdAsRoot("bash", "-c", "test -e "+serverDir+"/"+name+" && echo present || echo gone")
		if !strings.Contains(check.Stdout, "gone") {
			t.Errorf("%s should have been removed by --local-only unregister", name)
		}
	}
}
