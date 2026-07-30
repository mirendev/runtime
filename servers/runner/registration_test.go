package runner

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"net"
	"testing"
	"time"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/runner/runner_v1alpha"
	"miren.dev/runtime/pkg/caauth"
	"miren.dev/runtime/pkg/enrolltoken"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/workloadidentity"
)

type testEnv struct {
	client *runner_v1alpha.RunnerRegistrationClient
	ec     *entityserver.Client
	store  *entity.MockStore
	server *RegistrationServer
	ca     *caauth.Authority
}

func newTestServer(t *testing.T) (*testEnv, func()) {
	t.Helper()

	es, cleanup := testutils.NewInMemEntityServer(t)

	ca, err := caauth.New(caauth.Options{
		CommonName:   "test-ca",
		Organization: "test",
		ValidFor:     24 * time.Hour,
	})
	if err != nil {
		cleanup()
		t.Fatalf("failed to create CA: %v", err)
	}

	regServer := NewRegistrationServer(RegistrationServerConfig{
		Log:             testutils.TestLogger(t),
		Authority:       ca,
		EAC:             es.EAC,
		CoordinatorAddr: "127.0.0.1:8443",
	})

	localClient := rpc.LocalClient(runner_v1alpha.AdaptRunnerRegistration(regServer))
	client := runner_v1alpha.NewRunnerRegistrationClient(localClient)

	return &testEnv{client: client, ec: es.Client, store: es.Store, server: regServer, ca: ca}, cleanup
}

// issueLeafCert issues a certificate from the given authority and returns the
// parsed leaf certificate (the first PEM block; IssueCertificate appends the CA
// cert after it).
func issueLeafCert(t *testing.T, ca *caauth.Authority, commonName, org string, ip string) *x509.Certificate {
	t.Helper()

	opts := caauth.Options{
		CommonName:   commonName,
		Organization: org,
		ValidFor:     time.Hour,
	}
	if ip != "" {
		opts.IPs = []net.IP{net.ParseIP(ip)}
	}

	cc, err := ca.IssueCertificate(opts)
	if err != nil {
		t.Fatalf("failed to issue cert: %v", err)
	}

	block, _ := pem.Decode(cc.CertPEM)
	if block == nil {
		t.Fatal("failed to decode issued cert PEM")
		return nil
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("failed to parse issued cert: %v", err)
	}
	return cert
}

// createInviteAndDecode creates a one-time invite and returns the secret.
func (e *testEnv) createInviteAndDecode(t *testing.T, ctx context.Context) string {
	t.Helper()

	res, err := e.client.CreateInvite(ctx, nil, 1, "", false, 0, "")
	if err != nil {
		t.Fatalf("CreateInvite failed: %v", err)
	}

	token := res.Code()
	if !enrolltoken.IsToken(token) {
		t.Fatalf("CreateInvite returned non-token code: %q", token)
	}

	_, secret, err := enrolltoken.Decode(token)
	if err != nil {
		t.Fatalf("failed to decode token: %v", err)
	}

	return secret
}

// findInviteEntityID finds a runner_invite entity in the mock store by its
// code hash. The entity ID doesn't survive the CBOR round-trip in the local
// RPC client's ListInvites response, so we look it up directly.
func (e *testEnv) findInviteEntityID(t *testing.T, secret string) string {
	t.Helper()
	hash := enrolltoken.Hash(secret)
	for id, ent := range e.store.Entities {
		// Check if this entity is a runner_invite by looking for the code_hash attr
		if attr, ok := ent.Get(runner_v1alpha.RunnerInviteCodeHashId); ok {
			if attr.Value.String() == hash {
				return string(id)
			}
		}
	}
	t.Fatal("invite entity not found in store")
	return ""
}

func TestCreateInviteReturnsToken(t *testing.T) {
	ctx := context.Background()
	env, cleanup := newTestServer(t)
	defer cleanup()

	res, err := env.client.CreateInvite(ctx, nil, 1, "", false, 0, "")
	if err != nil {
		t.Fatalf("CreateInvite failed: %v", err)
	}

	token := res.Code()
	if !enrolltoken.IsToken(token) {
		t.Fatalf("expected mren_ token, got %q", token)
	}

	addr, secret, err := enrolltoken.Decode(token)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if addr != "127.0.0.1:8443" {
		t.Errorf("token addr = %q, want %q", addr, "127.0.0.1:8443")
	}

	if !enrolltoken.IsHexSecret(secret) {
		t.Errorf("token secret is not valid hex: %q", secret)
	}
}

func TestCreateInviteCoordinatorAddrOverride(t *testing.T) {
	ctx := context.Background()
	env, cleanup := newTestServer(t)
	defer cleanup()

	res, err := env.client.CreateInvite(ctx, nil, 1, "", false, 0, "10.0.0.5:8443")
	if err != nil {
		t.Fatalf("CreateInvite failed: %v", err)
	}

	addr, _, err := enrolltoken.Decode(res.Code())
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if addr != "10.0.0.5:8443" {
		t.Errorf("token addr = %q, want overridden %q", addr, "10.0.0.5:8443")
	}
}

func TestJoinCreatesNodeEntity(t *testing.T) {
	ctx := context.Background()
	env, cleanup := newTestServer(t)
	defer cleanup()

	secret := env.createInviteAndDecode(t, ctx)

	joinResult, err := env.client.Join(ctx, secret, "", "10.0.0.1:8443", "test-version", nil, "test-runner")
	if err != nil {
		t.Fatalf("Join RPC failed: %v", err)
	}

	if joinResult.HasError() {
		t.Fatalf("Join returned error: %s", joinResult.Error())
	}

	if joinResult.RunnerId() == "" {
		t.Error("Join did not return a runner ID")
	}

	if len(joinResult.CertPem()) == 0 {
		t.Error("Join did not return a certificate")
	}

	if joinResult.CoordinatorAddr() != "127.0.0.1:8443" {
		t.Errorf("Join returned coordinator addr %q, want %q", joinResult.CoordinatorAddr(), "127.0.0.1:8443")
	}

	// Verify the issued certificate includes proper IP SANs
	block, _ := pem.Decode(joinResult.CertPem())
	if block == nil {
		t.Fatal("failed to decode cert PEM")
		return
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("failed to parse certificate: %v", err)
	}

	wantIPs := []net.IP{
		net.ParseIP("127.0.0.1"),
		net.ParseIP("::1"),
		net.ParseIP("10.0.0.1"),
	}
	for _, wantIP := range wantIPs {
		found := false
		for _, gotIP := range cert.IPAddresses {
			if gotIP.Equal(wantIP) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("certificate missing IP SAN %s, got %v", wantIP, cert.IPAddresses)
		}
	}

	foundLocalhost := false
	for _, name := range cert.DNSNames {
		if name == "localhost" {
			foundLocalhost = true
			break
		}
	}
	if !foundLocalhost {
		t.Errorf("certificate missing DNS SAN 'localhost', got %v", cert.DNSNames)
	}
}

func TestOneTimeInviteConsumedOnUse(t *testing.T) {
	ctx := context.Background()
	env, cleanup := newTestServer(t)
	defer cleanup()

	secret := env.createInviteAndDecode(t, ctx)

	// First join should succeed
	res, err := env.client.Join(ctx, secret, "", "10.0.0.1:8443", "v1", nil, "runner-1")
	if err != nil {
		t.Fatalf("Join failed: %v", err)
	}
	if res.HasError() {
		t.Fatalf("first join failed: %s", res.Error())
	}

	// Second join with same secret should fail (invite consumed)
	res2, err := env.client.Join(ctx, secret, "", "10.0.0.2:8443", "v1", nil, "runner-2")
	if err != nil {
		t.Fatalf("Join RPC failed: %v", err)
	}
	if res2.Error() == "" {
		t.Error("expected error on second join with consumed invite")
	}
}

func TestReusableInviteMultipleJoins(t *testing.T) {
	ctx := context.Background()
	env, cleanup := newTestServer(t)
	defer cleanup()

	res, err := env.client.CreateInvite(ctx, nil, 1, "test-token", true, 0, "")
	if err != nil {
		t.Fatalf("CreateInvite failed: %v", err)
	}

	_, secret, err := enrolltoken.Decode(res.Code())
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	// Join 3 times, all should succeed
	for i := range 3 {
		joinRes, err := env.client.Join(ctx, secret, "", "10.0.0.1:8443", "v1", nil, "")
		if err != nil {
			t.Fatalf("Join %d failed: %v", i, err)
		}
		if joinRes.HasError() {
			t.Fatalf("Join %d returned error: %s", i, joinRes.Error())
		}
		if joinRes.RunnerId() == "" {
			t.Fatalf("Join %d did not return a runner ID", i)
		}
	}

	// Verify enrollment count via list
	listRes, err := env.client.ListInvites(ctx)
	if err != nil {
		t.Fatalf("ListInvites failed: %v", err)
	}

	invites := listRes.Invites()
	if len(invites) != 1 {
		t.Fatalf("expected 1 invite, got %d", len(invites))
	}

	inv := invites[0]
	if inv.EnrollmentCount() != 3 {
		t.Errorf("enrollment_count = %d, want 3", inv.EnrollmentCount())
	}
	if inv.Name() != "test-token" {
		t.Errorf("name = %q, want %q", inv.Name(), "test-token")
	}
	if !inv.Reusable() {
		t.Error("invite should be marked reusable")
	}
	if inv.Status() != "status.pending" {
		t.Errorf("reusable invite should stay pending, got %q", inv.Status())
	}
}

// TestJoinRejectsDuplicateRunnerId is the MIR-1225 regression: a caller holding
// a reusable invite must not be able to join as a runner_id that already backs a
// live runner. Because runner_id becomes the mTLS cert CommonName that authorizes
// per-runner actions (minting workload identity tokens for a sandbox), letting a
// second join reuse a live id would let it impersonate that runner. The second
// join must be rejected before any certificate is issued.
func TestJoinRejectsDuplicateRunnerId(t *testing.T) {
	ctx := context.Background()
	env, cleanup := newTestServer(t)
	defer cleanup()

	// A reusable invite is the exposure: it is never consumed on use, so nothing
	// else gates repeated joins.
	res, err := env.client.CreateInvite(ctx, nil, 1, "reusable", true, 0, "")
	if err != nil {
		t.Fatalf("CreateInvite failed: %v", err)
	}
	_, secret, err := enrolltoken.Decode(res.Code())
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	const runnerID = "11111111-1111-1111-1111-111111111111"

	// First join with the chosen id succeeds and mints a cert.
	first, err := env.client.Join(ctx, secret, runnerID, "10.0.0.1:8443", "v1", nil, "victim")
	if err != nil {
		t.Fatalf("first Join RPC failed: %v", err)
	}
	if first.HasError() {
		t.Fatalf("first join returned error: %s", first.Error())
	}
	if len(first.CertPem()) == 0 {
		t.Fatal("first join did not return a certificate")
	}

	// A second join reusing the same id must be rejected, and must not return a
	// certificate for the impersonated identity.
	second, err := env.client.Join(ctx, secret, runnerID, "10.0.0.2:8443", "v1", nil, "impostor")
	if err != nil {
		t.Fatalf("second Join RPC failed: %v", err)
	}
	if !second.HasError() || second.Error() == "" {
		t.Fatal("expected second join with a duplicate runner_id to be rejected")
	}
	if len(second.CertPem()) != 0 {
		t.Error("rejected duplicate join must not return a certificate")
	}

	// The original node entity must be untouched: still exactly one node, with
	// the victim's identity.
	list, err := env.client.ListRunners(ctx)
	if err != nil {
		t.Fatalf("ListRunners failed: %v", err)
	}
	runners := list.Runners()
	if len(runners) != 1 {
		t.Fatalf("expected exactly 1 registered runner, got %d", len(runners))
	}
	if runners[0].Name() != "victim" {
		t.Errorf("existing runner should be the original 'victim', got %q", runners[0].Name())
	}
}

// TestJoinDuplicateCheckMatchesRunnerIdNotName guards the uniqueness check
// against false positives: it must key strictly on runner_id, not on a node's
// human name. A runner may be named with a string that happens to be another
// runner's UUID; that must not block a legitimate join using that UUID as a
// runner_id.
func TestJoinDuplicateCheckMatchesRunnerIdNotName(t *testing.T) {
	ctx := context.Background()
	env, cleanup := newTestServer(t)
	defer cleanup()

	res, err := env.client.CreateInvite(ctx, nil, 1, "reusable", true, 0, "")
	if err != nil {
		t.Fatalf("CreateInvite failed: %v", err)
	}
	_, secret, err := enrolltoken.Decode(res.Code())
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	// uuidA is used as one runner's *name* and, separately, as another runner's
	// *id*. Only the id should count for uniqueness.
	const uuidA = "22222222-2222-2222-2222-222222222222"

	// First runner: a distinct id, but named with uuidA.
	first, err := env.client.Join(ctx, secret, "33333333-3333-3333-3333-333333333333", "10.0.0.1:8443", "v1", nil, uuidA)
	if err != nil {
		t.Fatalf("first Join RPC failed: %v", err)
	}
	if first.HasError() {
		t.Fatalf("first join returned error: %s", first.Error())
	}

	// Second runner joins using uuidA as its runner_id. No node has that
	// runner_id (uuidA is only a name), so this must succeed.
	second, err := env.client.Join(ctx, secret, uuidA, "10.0.0.2:8443", "v1", nil, "second")
	if err != nil {
		t.Fatalf("second Join RPC failed: %v", err)
	}
	if second.HasError() {
		t.Fatalf("join using a UUID that is only another runner's name should not be rejected, got: %s", second.Error())
	}
	if len(second.CertPem()) == 0 {
		t.Error("second join should have returned a certificate")
	}
}

func TestReusableInviteRevoke(t *testing.T) {
	ctx := context.Background()
	env, cleanup := newTestServer(t)
	defer cleanup()

	res, err := env.client.CreateInvite(ctx, nil, 1, "revoke-me", true, 0, "")
	if err != nil {
		t.Fatalf("CreateInvite failed: %v", err)
	}

	_, secret, err := enrolltoken.Decode(res.Code())
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	// Join once to confirm it works
	joinRes, err := env.client.Join(ctx, secret, "", "10.0.0.1:8443", "v1", nil, "runner-1")
	if err != nil {
		t.Fatalf("Join failed: %v", err)
	}
	if joinRes.HasError() {
		t.Fatalf("Join returned error: %s", joinRes.Error())
	}

	// Look up the invite entity ID directly from the mock store. The mock
	// store doesn't generate entity IDs like the real etcd store does, so
	// we revoke by directly calling the server's RevokeInvite with the
	// entity key from the store. When the mock store assigns an empty key,
	// we fall back to finding it via ListInvites on the EAC.
	inviteID := env.findInviteEntityID(t, secret)

	// Revoke it
	revokeRes, err := env.client.RevokeInvite(ctx, inviteID)
	if err != nil {
		t.Fatalf("RevokeInvite failed: %v", err)
	}
	if !revokeRes.Success() {
		t.Fatalf("RevokeInvite failed: %s", revokeRes.Error())
	}

	// Subsequent join should fail
	joinRes2, err := env.client.Join(ctx, secret, "", "10.0.0.2:8443", "v1", nil, "runner-2")
	if err != nil {
		t.Fatalf("Join RPC failed: %v", err)
	}
	if joinRes2.Error() == "" {
		t.Error("expected error joining with revoked token")
	}
}

func TestRemoveRunner(t *testing.T) {
	ctx := context.Background()
	env, cleanup := newTestServer(t)
	defer cleanup()

	secret := env.createInviteAndDecode(t, ctx)

	joinResult, err := env.client.Join(ctx, secret, "", "10.0.0.1:8443", "test-version", nil, "test-runner")
	if err != nil {
		t.Fatalf("Join failed: %v", err)
	}
	if joinResult.HasError() {
		t.Fatalf("Join returned error: %s", joinResult.Error())
	}

	listResult, err := env.client.ListRunners(ctx)
	if err != nil {
		t.Fatalf("ListRunners failed: %v", err)
	}
	if len(listResult.Runners()) != 1 {
		t.Fatalf("expected 1 runner, got %d", len(listResult.Runners()))
	}

	removeResult, err := env.client.RemoveRunner(ctx, "test-runner", false)
	if err != nil {
		t.Fatalf("RemoveRunner failed: %v", err)
	}
	if removeResult.Error() != "" {
		t.Fatalf("RemoveRunner returned error: %s", removeResult.Error())
	}
	if removeResult.Name() != "test-runner" {
		t.Errorf("RemoveRunner returned name %q, want %q", removeResult.Name(), "test-runner")
	}
	if removeResult.RunnerId() != joinResult.RunnerId() {
		t.Errorf("RemoveRunner returned runner_id %q, want %q", removeResult.RunnerId(), joinResult.RunnerId())
	}

	listResult, err = env.client.ListRunners(ctx)
	if err != nil {
		t.Fatalf("ListRunners after remove failed: %v", err)
	}
	if len(listResult.Runners()) != 0 {
		t.Errorf("expected 0 runners after remove, got %d", len(listResult.Runners()))
	}
}

// TestCordonAndUncordonRunner exercises the full cordon -> uncordon round trip
// through the real in-memory store, verifying the persistent scheduling enum
// flips to cordoned and back to schedulable.
func TestCordonAndUncordonRunner(t *testing.T) {
	ctx := context.Background()
	env, cleanup := newTestServer(t)
	defer cleanup()

	secret := env.createInviteAndDecode(t, ctx)
	joinResult, err := env.client.Join(ctx, secret, "", "10.0.0.1:8443", "test-version", nil, "cordon-runner")
	if err != nil {
		t.Fatalf("Join failed: %v", err)
	}
	if joinResult.HasError() {
		t.Fatalf("Join returned error: %s", joinResult.Error())
	}

	cordonResult, err := env.client.CordonRunner(ctx, "cordon-runner", "cert rotation")
	if err != nil {
		t.Fatalf("CordonRunner failed: %v", err)
	}
	if cordonResult.Error() != "" {
		t.Fatalf("CordonRunner returned error: %s", cordonResult.Error())
	}
	if cordonResult.Name() != "cordon-runner" {
		t.Errorf("CordonRunner returned name %q, want %q", cordonResult.Name(), "cordon-runner")
	}

	listResult, err := env.client.ListRunners(ctx)
	if err != nil {
		t.Fatalf("ListRunners failed: %v", err)
	}
	if len(listResult.Runners()) != 1 {
		t.Fatalf("expected 1 runner, got %d", len(listResult.Runners()))
	}
	info := listResult.Runners()[0]
	if info.Scheduling() != "cordoned" {
		t.Errorf("scheduling = %q, want %q after cordon", info.Scheduling(), "cordoned")
	}

	uncordonResult, err := env.client.UncordonRunner(ctx, "cordon-runner")
	if err != nil {
		t.Fatalf("UncordonRunner failed: %v", err)
	}
	if uncordonResult.Error() != "" {
		t.Fatalf("UncordonRunner returned error: %s", uncordonResult.Error())
	}

	listResult, err = env.client.ListRunners(ctx)
	if err != nil {
		t.Fatalf("ListRunners after uncordon failed: %v", err)
	}
	info = listResult.Runners()[0]
	if info.Scheduling() != "schedulable" {
		t.Errorf("scheduling = %q, want %q after uncordon", info.Scheduling(), "schedulable")
	}
}

func TestCordonRunnerNotFound(t *testing.T) {
	ctx := context.Background()
	env, cleanup := newTestServer(t)
	defer cleanup()

	res, err := env.client.CordonRunner(ctx, "nonexistent", "")
	if err != nil {
		t.Fatalf("CordonRunner failed: %v", err)
	}
	if res.Error() == "" {
		t.Errorf("expected error for nonexistent runner")
	}
}

// TestDrainRunnerEvictsSandboxes verifies that draining a runner stops the
// sandboxes scheduled to it (marking them STOPPED so the pool controllers
// recreate them elsewhere) and cordons the node, while leaving both the stopped
// sandbox entities and the node entity intact.
func TestDrainRunnerEvictsSandboxes(t *testing.T) {
	ctx := context.Background()
	env, cleanup := newTestServer(t)
	defer cleanup()

	secret := env.createInviteAndDecode(t, ctx)
	joinResult, err := env.client.Join(ctx, secret, "", "10.0.0.1:8443", "v1", nil, "drain-runner")
	if err != nil {
		t.Fatalf("Join failed: %v", err)
	}
	if joinResult.HasError() {
		t.Fatalf("Join returned error: %s", joinResult.Error())
	}

	listResult, err := env.client.ListRunners(ctx)
	if err != nil {
		t.Fatalf("ListRunners failed: %v", err)
	}
	nodeID := entity.Id(listResult.Runners()[0].Id())

	// Create a sandbox scheduled to the node so drain has something to evict.
	sbID, err := env.ec.Create(ctx, "drain-sandbox", &compute_v1alpha.Sandbox{
		Status: compute_v1alpha.RUNNING,
	})
	if err != nil {
		t.Fatalf("failed to create sandbox: %v", err)
	}
	err = env.ec.UpdateAttrs(ctx, sbID, (&compute_v1alpha.Schedule{
		Key: compute_v1alpha.Key{Kind: compute_v1alpha.KindSandbox, Node: nodeID},
	}).Encode)
	if err != nil {
		t.Fatalf("failed to schedule sandbox to node: %v", err)
	}

	drainResult, err := env.client.DrainRunner(ctx, "drain-runner", "maintenance", 0)
	if err != nil {
		t.Fatalf("DrainRunner failed: %v", err)
	}
	if drainResult.Error() != "" {
		t.Fatalf("DrainRunner returned error: %s", drainResult.Error())
	}
	if drainResult.EvictedCount() != 1 {
		t.Errorf("EvictedCount = %d, want 1", drainResult.EvictedCount())
	}
	if drainResult.TimedOut() {
		t.Errorf("drain unexpectedly timed out")
	}

	// The sandbox should be vacated (no live sandboxes) but NOT deleted: it
	// stays in the node's schedule index as a terminal (STOPPED) entity, so the
	// sandbox controller and pool manager drive teardown/reschedule.
	live, err := env.server.countLiveNodeSandboxes(ctx, nodeID)
	if err != nil {
		t.Fatalf("countLiveNodeSandboxes failed: %v", err)
	}
	if live != 0 {
		t.Errorf("expected 0 live sandboxes on node after drain, got %d", live)
	}
	total, err := env.server.countNodeSchedules(ctx, nodeID)
	if err != nil {
		t.Fatalf("countNodeSchedules failed: %v", err)
	}
	if total != 1 {
		t.Errorf("expected the stopped sandbox entity to remain (not deleted), got %d scheduled", total)
	}

	// The node itself must still exist (drain does not remove the runner) and
	// must be cordoned.
	listResult, err = env.client.ListRunners(ctx)
	if err != nil {
		t.Fatalf("ListRunners after drain failed: %v", err)
	}
	if len(listResult.Runners()) != 1 {
		t.Fatalf("expected node to survive drain, got %d runners", len(listResult.Runners()))
	}
	if listResult.Runners()[0].Scheduling() != "cordoned" {
		t.Errorf("expected node to be cordoned after drain, got scheduling %q", listResult.Runners()[0].Scheduling())
	}
}

func TestRemoveRunnerNotFound(t *testing.T) {
	ctx := context.Background()
	env, cleanup := newTestServer(t)
	defer cleanup()

	removeResult, err := env.client.RemoveRunner(ctx, "nonexistent", false)
	if err != nil {
		t.Fatalf("RemoveRunner failed: %v", err)
	}
	if removeResult.Error() == "" {
		t.Error("expected error for nonexistent runner, got none")
	}
}

func TestRemoveRunnerByShortId(t *testing.T) {
	ctx := context.Background()
	env, cleanup := newTestServer(t)
	defer cleanup()

	secret := env.createInviteAndDecode(t, ctx)

	joinResult, err := env.client.Join(ctx, secret, "", "10.0.0.3:8443", "v1", nil, "runner-short")
	if err != nil {
		t.Fatalf("Join failed: %v", err)
	}
	if joinResult.HasError() {
		t.Fatalf("Join returned error: %s", joinResult.Error())
	}

	listResult, err := env.client.ListRunners(ctx)
	if err != nil {
		t.Fatalf("ListRunners failed: %v", err)
	}
	if len(listResult.Runners()) != 1 {
		t.Fatalf("expected 1 runner, got %d", len(listResult.Runners()))
	}
	shortId := listResult.Runners()[0].ShortId()
	if shortId == "" {
		t.Fatalf("expected runner to have a short id assigned")
	}

	removeResult, err := env.client.RemoveRunner(ctx, shortId, false)
	if err != nil {
		t.Fatalf("RemoveRunner failed: %v", err)
	}
	if removeResult.Error() != "" {
		t.Fatalf("RemoveRunner returned error: %s", removeResult.Error())
	}
	if removeResult.Name() != "runner-short" {
		t.Errorf("RemoveRunner returned name %q, want %q", removeResult.Name(), "runner-short")
	}
}

func TestRemoveRunnerByRunnerId(t *testing.T) {
	ctx := context.Background()
	env, cleanup := newTestServer(t)
	defer cleanup()

	secret := env.createInviteAndDecode(t, ctx)

	joinResult, err := env.client.Join(ctx, secret, "", "10.0.0.2:8443", "v1", nil, "runner-two")
	if err != nil {
		t.Fatalf("Join failed: %v", err)
	}
	if joinResult.HasError() {
		t.Fatalf("Join returned error: %s", joinResult.Error())
	}

	removeResult, err := env.client.RemoveRunner(ctx, joinResult.RunnerId(), false)
	if err != nil {
		t.Fatalf("RemoveRunner failed: %v", err)
	}
	if removeResult.Error() != "" {
		t.Fatalf("RemoveRunner returned error: %s", removeResult.Error())
	}
	if removeResult.Name() != "runner-two" {
		t.Errorf("RemoveRunner returned name %q, want %q", removeResult.Name(), "runner-two")
	}

	listResult, err := env.client.ListRunners(ctx)
	if err != nil {
		t.Fatalf("ListRunners failed: %v", err)
	}
	if len(listResult.Runners()) != 0 {
		t.Errorf("expected 0 runners, got %d", len(listResult.Runners()))
	}
}

func TestBuildRunnerSANs(t *testing.T) {
	t.Run("IP listen address adds IP SAN", func(t *testing.T) {
		ips, dnsNames := buildRunnerSANs("10.0.0.7:8444")

		wantIPs := []string{"127.0.0.1", "::1", "10.0.0.7"}
		for _, want := range wantIPs {
			if !containsIP(ips, want) {
				t.Errorf("missing IP SAN %s, got %v", want, ips)
			}
		}
		if !containsStr(dnsNames, "localhost") {
			t.Errorf("missing DNS SAN localhost, got %v", dnsNames)
		}
		if containsStr(dnsNames, "10.0.0.7") {
			t.Errorf("IP should not appear as a DNS SAN, got %v", dnsNames)
		}
	})

	t.Run("hostname listen address adds DNS SAN", func(t *testing.T) {
		ips, dnsNames := buildRunnerSANs("runner.example.com:8444")

		if !containsStr(dnsNames, "runner.example.com") {
			t.Errorf("missing DNS SAN runner.example.com, got %v", dnsNames)
		}
		if !containsIP(ips, "127.0.0.1") || !containsIP(ips, "::1") {
			t.Errorf("missing loopback IP SANs, got %v", ips)
		}
	})

	t.Run("empty listen address yields only loopback", func(t *testing.T) {
		ips, dnsNames := buildRunnerSANs("")
		if len(ips) != 2 {
			t.Errorf("expected only loopback IPs, got %v", ips)
		}
		if len(dnsNames) != 1 || dnsNames[0] != "localhost" {
			t.Errorf("expected only localhost DNS, got %v", dnsNames)
		}
	})
}

// joinRunner performs a Join and returns the issued leaf certificate and the
// assigned runner ID, so tests can exercise refresh as a real, registered runner.
func (e *testEnv) joinRunner(t *testing.T, ctx context.Context, listenAddr, name string) (*x509.Certificate, string) {
	t.Helper()

	secret := e.createInviteAndDecode(t, ctx)
	res, err := e.client.Join(ctx, secret, "", listenAddr, "v1", nil, name)
	if err != nil {
		t.Fatalf("Join failed: %v", err)
	}
	if res.HasError() {
		t.Fatalf("Join returned error: %s", res.Error())
	}

	block, _ := pem.Decode(res.CertPem())
	if block == nil {
		t.Fatal("failed to decode join cert PEM")
		return nil, ""
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("failed to parse join cert: %v", err)
	}
	return cert, res.RunnerId()
}

func TestReissueRunnerCertificate(t *testing.T) {
	ctx := context.Background()
	env, cleanup := newTestServer(t)
	defer cleanup()

	t.Run("happy path re-issues with new IP and preserves CN", func(t *testing.T) {
		peer, _ := env.joinRunner(t, ctx, "10.0.0.1:8444", "happy-runner")

		cc, err := env.server.reissueRunnerCertificate(ctx, peer, "10.0.0.2:8444")
		if err != nil {
			t.Fatalf("reissueRunnerCertificate failed: %v", err)
		}

		// The re-issued cert must be signed by the cluster CA.
		if err := env.ca.VerifyCertificate(cc.CertPEM); err != nil {
			t.Fatalf("re-issued cert not signed by CA: %v", err)
		}

		block, _ := pem.Decode(cc.CertPEM)
		if block == nil {
			t.Fatal("failed to decode re-issued cert PEM")
			return
		}
		newCert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			t.Fatalf("failed to parse re-issued cert: %v", err)
		}

		if newCert.Subject.CommonName != peer.Subject.CommonName {
			t.Errorf("CommonName = %q, want preserved %q", newCert.Subject.CommonName, peer.Subject.CommonName)
		}

		var foundIPs []string
		for _, ip := range newCert.IPAddresses {
			foundIPs = append(foundIPs, ip.String())
		}
		if !containsIP(newCert.IPAddresses, "10.0.0.2") {
			t.Errorf("re-issued cert missing new IP SAN 10.0.0.2, got %v", foundIPs)
		}
	})

	t.Run("nil peer is rejected", func(t *testing.T) {
		_, err := env.server.reissueRunnerCertificate(ctx, nil, "10.0.0.2:8444")
		if err == nil {
			t.Fatal("expected error for nil peer certificate")
		}
	})

	t.Run("cert from another CA is rejected", func(t *testing.T) {
		otherCA, err := caauth.New(caauth.Options{CommonName: "other-ca", Organization: "other", ValidFor: 24 * time.Hour})
		if err != nil {
			t.Fatalf("failed to create other CA: %v", err)
		}
		peer := issueLeafCert(t, otherCA, "runner-abc12345", "miren", "10.0.0.1")

		_, err = env.server.reissueRunnerCertificate(ctx, peer, "10.0.0.2:8444")
		if err == nil {
			t.Fatal("expected error for cert signed by a different CA")
		}
	})

	t.Run("non-runner CN is rejected", func(t *testing.T) {
		peer := issueLeafCert(t, env.ca, "operator-abc", "miren", "10.0.0.1")

		_, err := env.server.reissueRunnerCertificate(ctx, peer, "10.0.0.2:8444")
		if err == nil {
			t.Fatal("expected error for non-runner CommonName")
		}
	})

	t.Run("wrong organization is rejected", func(t *testing.T) {
		peer := issueLeafCert(t, env.ca, "runner-abc12345", "intruder", "10.0.0.1")

		_, err := env.server.reissueRunnerCertificate(ctx, peer, "10.0.0.2:8444")
		if err == nil {
			t.Fatal("expected error for cert with wrong organization")
		}
	})

	t.Run("unregistered runner cert is rejected", func(t *testing.T) {
		// A genuine CA-signed runner cert, but no matching Node exists (e.g. the
		// runner was never registered or has been removed). The identity comes
		// from the cert, so a caller cannot substitute another runner's ID.
		peer := issueLeafCert(t, env.ca, "runner-deadbeef", "miren", "10.0.0.1")

		_, err := env.server.reissueRunnerCertificate(ctx, peer, "10.0.0.2:8444")
		if err == nil {
			t.Fatal("expected error for a runner cert with no registered node")
		}
	})

	t.Run("removed runner cannot refresh", func(t *testing.T) {
		peer, runnerID := env.joinRunner(t, ctx, "10.0.0.1:8444", "removed-runner")

		// Remove the runner, deleting its Node entity.
		removeRes, err := env.client.RemoveRunner(ctx, runnerID, false)
		if err != nil {
			t.Fatalf("RemoveRunner failed: %v", err)
		}
		if removeRes.Error() != "" {
			t.Fatalf("RemoveRunner returned error: %s", removeRes.Error())
		}

		// Its certificate is still cryptographically valid, but it is no longer
		// registered, so refresh must be rejected.
		_, err = env.server.reissueRunnerCertificate(ctx, peer, "10.0.0.2:8444")
		if err == nil {
			t.Fatal("expected error refreshing a removed runner's certificate")
		}
	})

}

func TestRefreshCertificateRequiresClientCert(t *testing.T) {
	ctx := context.Background()
	env, cleanup := newTestServer(t)
	defer cleanup()

	// The local RPC client does not carry a TLS peer certificate, so the
	// handler must reject the request rather than minting a cert.
	res, err := env.client.RefreshCertificate(ctx, "10.0.0.2:8444")
	if err != nil {
		t.Fatalf("RefreshCertificate RPC failed: %v", err)
	}
	if res.Error() == "" {
		t.Fatal("expected RefreshCertificate to reject a call without a client certificate")
	}
	if len(res.CertPem()) != 0 {
		t.Error("expected no certificate when the call is rejected")
	}
}

func containsIP(ips []net.IP, want string) bool {
	w := net.ParseIP(want)
	for _, ip := range ips {
		if ip.Equal(w) {
			return true
		}
	}
	return false
}

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// TestAuthorizeSystemWorkloadRequest covers the authorization guarding system
// workload identity minting. The caller must be a runner that is still
// registered, and it may only ask for a workload it actually runs.
func TestAuthorizeSystemWorkloadRequest(t *testing.T) {
	ctx := context.Background()
	env, cleanup := newTestServer(t)
	defer cleanup()

	secret := env.createInviteAndDecode(t, ctx)
	joinResult, err := env.client.Join(ctx, secret, "", "10.0.0.1:8443", "test-version", nil, "test-runner")
	if err != nil {
		t.Fatalf("Join RPC failed: %v", err)
	}
	if joinResult.HasError() {
		t.Fatalf("Join returned error: %s", joinResult.Error())
	}
	runnerID := joinResult.RunnerId()

	certIdentity := func(cn string) *rpc.Identity {
		return &rpc.Identity{Subject: cn, Method: rpc.AuthMethodCert}
	}

	tests := []struct {
		name     string
		identity *rpc.Identity
		workload workloadidentity.SystemWorkload
		wantErr  bool
	}{
		{
			name:     "registered runner requesting an allowed system workload",
			identity: certIdentity(runnerCertName(runnerID)),
			workload: workloadidentity.SystemWorkloadSandboxController,
		},
		{
			name:     "registered runner requesting coordinator-only buildkit workload",
			identity: certIdentity(runnerCertName(runnerID)),
			workload: workloadidentity.SystemWorkloadBuildKit,
			wantErr:  true,
		},
		{
			name:     "unknown system workload",
			identity: certIdentity(runnerCertName(runnerID)),
			workload: workloadidentity.SystemWorkload("notathing"),
			wantErr:  true,
		},
		{
			// caauth has no revocation, so a removed runner keeps a valid
			// certificate. The registration check is what cuts its access.
			name:     "certificate for a runner that is not registered",
			identity: certIdentity(runnerCertName("00000000-0000-0000-0000-000000000000")),
			workload: workloadidentity.SystemWorkloadSandboxController,
			wantErr:  true,
		},
		{
			name:     "certificate that is not a runner certificate",
			identity: certIdentity("miren-server"),
			workload: workloadidentity.SystemWorkloadSandboxController,
			wantErr:  true,
		},
		{
			name:     "non-certificate authentication",
			identity: &rpc.Identity{Subject: "user@example.com", Method: rpc.AuthMethodJWT},
			workload: workloadidentity.SystemWorkloadSandboxController,
			wantErr:  true,
		},
		{
			name:     "no caller identity",
			identity: nil,
			workload: workloadidentity.SystemWorkloadSandboxController,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callCtx := ctx
			if tt.identity != nil {
				callCtx = rpc.ContextWithIdentity(ctx, tt.identity)
			}

			err := env.server.authorizeSystemWorkloadRequest(callCtx, tt.workload)
			if tt.wantErr && err == nil {
				t.Errorf("expected authorization to fail, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected authorization to succeed, got %v", err)
			}
		})
	}
}

// TestAuthorizeSystemWorkloadRequestAnonymousKeepsAllowlist pins the shape of the
// no-authentication path. Issuance is allowed because there is no identity to
// check against (NoAuth is test-only), but the allowlist still applies, so even
// an unauthenticated coordinator cannot mint an arbitrary system identity.
func TestAuthorizeSystemWorkloadRequestAnonymousKeepsAllowlist(t *testing.T) {
	ctx := context.Background()
	env, cleanup := newTestServer(t)
	defer cleanup()

	anon := rpc.ContextWithIdentity(ctx, &rpc.Identity{
		Subject: "anonymous",
		Method:  rpc.AuthMethodAnonymous,
	})

	if err := env.server.authorizeSystemWorkloadRequest(anon, workloadidentity.SystemWorkloadSandboxController); err != nil {
		t.Errorf("expected allowlisted system workload to be permitted, got %v", err)
	}

	if err := env.server.authorizeSystemWorkloadRequest(anon, workloadidentity.SystemWorkload("coordinator")); err == nil {
		t.Error("expected a system workload off the allowlist to be refused")
	}
}
