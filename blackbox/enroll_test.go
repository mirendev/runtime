//go:build blackbox

package blackbox

import (
	"encoding/json"
	"strings"
	"testing"

	"miren.dev/runtime/blackbox/harness"
)

// TestServerEnrollWithToken drives the runtime's own registration command
// against a real cloud on the unattended path: an org admin mints an enroll
// token, and `miren server register --enroll-token` turns it into an approved
// registration in one shot — no browser, no polling.
//
// This is the first blackbox test that exercises the runtime's registration
// CLI end to end. The others hand-write registration.json; here the runtime
// produces it, so the whole initiate handshake is under test.
func TestServerEnrollWithToken(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)
	cloud := harness.NewCloudControlPlane(t, m)

	token, orgID := cloud.MintEnrollToken(t, map[string]string{"env": "bb"})

	const serverDir = "/var/lib/miren/server"
	const regFile = serverDir + "/registration.json"

	// Start from a clean slate so we register rather than trip the
	// already-registered guard, and clean up after ourselves.
	m.RunCmdAsRoot("rm", "-f", regFile, serverDir+"/service-account.key")
	t.Cleanup(func() {
		m.RunCmdAsRoot("rm", "-f", regFile, serverDir+"/service-account.key")
	})

	r := m.RunCmdAsRoot("m", "server", "register",
		"--name", "bb-enroll", "--url", cloud.CloudURL, "--enroll-token", token)
	if !r.Success() {
		t.Fatalf("enroll register failed (exit %d):\n%s\n%s", r.ExitCode, r.Stdout, r.Stderr)
	}

	// The runtime should report an approved registration with real cloud IDs.
	status := m.RunCmdAsRoot("m", "server", "register", "status")
	if !strings.Contains(status.Stdout, "Status: approved") {
		t.Errorf("expected an approved registration, got:\n%s", status.Stdout)
	}

	raw := m.RunCmdAsRoot("cat", regFile)
	if !raw.Success() {
		t.Fatalf("registration.json was not written:\n%s", raw.Stderr)
	}
	var reg struct {
		Status           string `json:"status"`
		ClusterID        string `json:"cluster_id"`
		OrganizationID   string `json:"organization_id"`
		ServiceAccountID string `json:"service_account_id"`
		PrivateKey       string `json:"private_key"`
	}
	if err := json.Unmarshal([]byte(raw.Stdout), &reg); err != nil {
		// Deliberately not logging raw.Stdout: registration.json holds the
		// service-account private key, and this failure output lands in CI logs.
		t.Fatalf("failed to parse registration.json: %v", err)
	}
	if reg.Status != "approved" {
		t.Errorf("registration status = %q, want approved", reg.Status)
	}
	if reg.ClusterID == "" {
		t.Errorf("registration has no cluster_id: status=%q organization_id=%q service_account_id=%q",
			reg.Status, reg.OrganizationID, reg.ServiceAccountID)
	}
	// The token was minted for a specific org; the cluster must land there and
	// not in some other org the token had no authority over.
	if reg.OrganizationID != orgID {
		t.Errorf("registration organization = %q, want %q (the token's org)", reg.OrganizationID, orgID)
	}
	if reg.ServiceAccountID == "" {
		t.Errorf("registration has no service_account_id: status=%q cluster_id=%q organization_id=%q",
			reg.Status, reg.ClusterID, reg.OrganizationID)
	}
	if reg.PrivateKey == "" {
		t.Errorf("registration has no private key")
	}

	// The token is single use. Clearing local state and presenting it again
	// gives a fresh keypair, so cloud cannot replay for the same key — it must
	// reject the spent token, and the runtime must surface that as an error
	// rather than falling back to an interactive flow no one is watching.
	m.RunCmdAsRoot("rm", "-f", regFile, serverDir+"/service-account.key")
	replay := m.RunCmdAsRoot("m", "server", "register",
		"--name", "bb-enroll", "--url", cloud.CloudURL, "--enroll-token", token)
	if replay.Success() {
		t.Errorf("a spent enroll token was accepted a second time:\n%s", replay.Stdout)
	}
	// And it must fail for the right reason: cloud reporting the token spent, not
	// some incidental network error or a fall back to the interactive flow.
	if !strings.Contains(replay.Stderr, "already been used") {
		t.Errorf("expected a spent-token rejection, got exit %d:\n%s", replay.ExitCode, replay.Stderr)
	}
}
