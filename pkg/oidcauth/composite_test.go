package oidcauth

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/workloadroles"
)

// mockAuthenticator is a configurable authenticator for testing.
type mockAuthenticator struct {
	identity *rpc.Identity
	err      error
}

func (m *mockAuthenticator) Authenticate(ctx context.Context, creds *rpc.Credentials) (*rpc.Identity, error) {
	return m.identity, m.err
}

// mockAuthorizer is a configurable authorizer for testing.
type mockAuthorizer struct {
	err error
}

func (m *mockAuthorizer) Authorize(ctx context.Context, identity *rpc.Identity, resource, action string) error {
	return m.err
}

func TestCompositeAuthenticator_PrimarySucceeds(t *testing.T) {
	primary := &mockAuthenticator{
		identity: &rpc.Identity{
			Subject: "user@example.com",
			Method:  rpc.AuthMethodJWT,
		},
	}
	oidcAuth := NewOIDCAuthenticator(testutils.TestLogger(t))
	comp := NewCompositeAuthenticator(primary, oidcAuth)

	req := httptest.NewRequest("GET", "/", nil)
	identity, err := comp.Authenticate(context.Background(), rpc.CredentialsFromRequest(req))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if identity == nil {
		t.Fatal("expected identity from primary")
		return
	}
	if identity.Method != rpc.AuthMethodJWT {
		t.Errorf("method = %q, want %q", identity.Method, rpc.AuthMethodJWT)
	}
}

func TestCompositeAuthenticator_PrimaryErrorOIDCNil(t *testing.T) {
	// When primary errors and OIDC returns nil, the primary error propagates.
	primary := &mockAuthenticator{
		err: fmt.Errorf("auth server unavailable"),
	}
	oidcAuth := NewOIDCAuthenticator(testutils.TestLogger(t))
	comp := NewCompositeAuthenticator(primary, oidcAuth)

	req := httptest.NewRequest("GET", "/", nil)
	_, err := comp.Authenticate(context.Background(), rpc.CredentialsFromRequest(req))
	if err == nil {
		t.Fatal("expected error to propagate from primary")
	}
}

func TestCompositeAuthenticator_BindingMismatchBeatsPrimaryError(t *testing.T) {
	// A CI token makes the primary fail with something unhelpful, since the
	// token was never meant for it. OIDC verified the token and knows exactly
	// why it was rejected, so that's the error the caller should get.
	primary := &mockAuthenticator{
		err: fmt.Errorf("key with ID xyz not found"),
	}
	oidcStub := &mockAuthenticator{
		err: &BindingMismatchError{
			Issuer:     "https://token.actions.githubusercontent.com",
			Subject:    "repo:acme@277133432/app@1316584243:ref:refs/heads/main",
			Repository: "acme/app",
		},
	}

	comp := NewCompositeAuthenticatorChain(primary, oidcStub)

	req := httptest.NewRequest("GET", "/", nil)
	identity, err := comp.Authenticate(context.Background(), rpc.CredentialsFromRequest(req))
	if identity != nil {
		t.Error("expected nil identity when neither authenticator succeeds")
	}

	mismatch, ok := errors.AsType[*BindingMismatchError](err)
	if !ok {
		t.Fatalf("expected the binding mismatch to win, got %v", err)
	}
	if mismatch.Repository != "acme/app" {
		t.Errorf("Repository = %q, want acme/app", mismatch.Repository)
	}
}

func TestCompositeAuthenticator_PrimaryErrorOIDCSucceeds(t *testing.T) {
	// When primary errors on a token it doesn't recognize, but OIDC succeeds,
	// the OIDC identity should be returned (not the primary error).
	primary := &mockAuthenticator{
		err: fmt.Errorf("key with ID xyz not found"),
	}
	oidcStub := &mockAuthenticator{
		identity: &rpc.Identity{
			Subject: "repo:acme/app:ref:refs/heads/main",
			Method:  rpc.AuthMethodOIDC,
		},
	}

	comp := NewCompositeAuthenticatorChain(primary, oidcStub)

	req := httptest.NewRequest("GET", "/", nil)
	identity, err := comp.Authenticate(context.Background(), rpc.CredentialsFromRequest(req))
	if err != nil {
		t.Fatalf("expected no error when OIDC succeeds, got: %v", err)
	}
	if identity == nil {
		t.Fatal("expected OIDC identity, got nil")
		return
	}
	if identity.Subject != "repo:acme/app:ref:refs/heads/main" {
		t.Errorf("subject = %q, want %q", identity.Subject, "repo:acme/app:ref:refs/heads/main")
	}
	if identity.Method != rpc.AuthMethodOIDC {
		t.Errorf("method = %q, want %q", identity.Method, rpc.AuthMethodOIDC)
	}
}

func TestCompositeAuthenticator_FallbackToOIDC(t *testing.T) {
	// Primary returns nil (no credentials recognized)
	primary := &mockAuthenticator{identity: nil, err: nil}
	oidcAuth := NewOIDCAuthenticator(testutils.TestLogger(t)) // No EAC set, so OIDC will also return nil
	comp := NewCompositeAuthenticator(primary, oidcAuth)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer some-token")

	identity, err := comp.Authenticate(context.Background(), rpc.CredentialsFromRequest(req))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if identity != nil {
		t.Error("expected nil identity when both authenticators fail to match")
	}
}

// spyAuthenticator records whether it was consulted.
type spyAuthenticator struct {
	called   bool
	identity *rpc.Identity
}

func (s *spyAuthenticator) Authenticate(ctx context.Context, creds *rpc.Credentials) (*rpc.Identity, error) {
	s.called = true
	return s.identity, nil
}

// Once a link claims a token, later links are never consulted. This is what
// keeps workload identity tokens away from the OIDC authenticator: an
// oidc_binding registered against our own issuer could otherwise reinterpret
// one under an attacker-chosen bound_app.
func TestCompositeAuthenticator_StopsAtFirstMatch(t *testing.T) {
	primary := &mockAuthenticator{identity: nil}
	workload := &spyAuthenticator{identity: &rpc.Identity{
		Subject:  "org:org-1:app:myapp:sandbox:sb-1",
		Method:   rpc.AuthMethodWorkload,
		Metadata: map[string]any{"app": "myapp"},
	}}
	oidcSpy := &spyAuthenticator{}

	comp := NewCompositeAuthenticatorChain(primary, workload, oidcSpy)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer workload-token")

	identity, err := comp.Authenticate(context.Background(), rpc.CredentialsFromRequest(req))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if identity == nil || identity.Method != rpc.AuthMethodWorkload {
		t.Fatalf("expected the workload identity, got %+v", identity)
	}
	if !workload.called {
		t.Error("workload authenticator was not consulted")
	}
	if oidcSpy.called {
		t.Error("OIDC was consulted for a token workload identity already claimed")
	}
}

// The chain is built from optional components; a nil one is skipped rather than
// panicking on a nil receiver. Covers a plain nil interface and a typed nil.
func TestCompositeAuthenticator_SkipsNilLinks(t *testing.T) {
	var typedNil *OIDCAuthenticator
	primary := &mockAuthenticator{identity: &rpc.Identity{Method: rpc.AuthMethodJWT}}

	comp := NewCompositeAuthenticatorChain(nil, primary, typedNil)

	req := httptest.NewRequest("GET", "/", nil)
	identity, err := comp.Authenticate(context.Background(), rpc.CredentialsFromRequest(req))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if identity == nil || identity.Method != rpc.AuthMethodJWT {
		t.Fatalf("expected the primary identity, got %+v", identity)
	}
}

func TestCompositeAuthorizer_CertBypass(t *testing.T) {
	comp := NewCompositeAuthorizer(nil)

	identity := &rpc.Identity{
		Subject: "local-client",
		Method:  rpc.AuthMethodCert,
	}

	// A cert-method identity bypasses all RBAC checks. This is only safe
	// because an AuthMethodCert identity is produced upstream ONLY for a client
	// cert that the TLS layer verified against the cluster CA -- see the
	// VerifiedChains gate in Authenticate (pkg/rpc/authenticator.go,
	// pkg/cloudauth/rpc_authenticator.go) and the verifying listener config
	// (pkg/rpc/state.go). Weakening either of those turns this blanket bypass
	// back into the client-cert auth bypass. Do not treat "cert = superuser" as
	// harmless in isolation.
	err := comp.Authorize(context.Background(), identity, "anything", "anything")
	if err != nil {
		t.Errorf("cert auth should bypass all checks: %v", err)
	}
}

func TestCompositeAuthorizer_OIDCAllowed(t *testing.T) {
	comp := NewCompositeAuthorizer(nil)

	identity := &rpc.Identity{
		Subject: "repo:acme/app:ref:refs/heads/main",
		Method:  rpc.AuthMethodOIDC,
	}

	allowed := []struct {
		resource string
		action   string
	}{
		{"deployment", "deployversion"},
		{"deployment", "createdeployment"},
		{"logs", "applogs"},
		{"logs", "streamlogs"},
		{"logs", "streamlogchunks"},
		{"crud", "list"},
		{"crud", "getconfiguration"},
		{"builder", "buildfromtar"},
		{"builder", "analyzeapp"},
		{"telemetry", "reportspans"},
		{"appstatus", "appinfo"},
	}

	for _, tc := range allowed {
		err := comp.Authorize(context.Background(), identity, tc.resource, tc.action)
		if err != nil {
			t.Errorf("Authorize(%q, %q) should be allowed for OIDC: %v", tc.resource, tc.action, err)
		}
	}
}

func TestCompositeAuthorizer_OIDCDenied(t *testing.T) {
	comp := NewCompositeAuthorizer(nil)

	identity := &rpc.Identity{
		Subject: "repo:acme/app:ref:refs/heads/main",
		Method:  rpc.AuthMethodOIDC,
	}

	denied := []struct {
		resource string
		action   string
	}{
		{"admin", "runcommand"},
		{"oidcbindings", "add"},
		{"oidcbindings", "remove"},
		{"deployment", "unknownmethod"},
		{"sandbox", "stop"},
		{"sandbox", "delete"},
		{"entities", "delete"},
	}

	for _, tc := range denied {
		err := comp.Authorize(context.Background(), identity, tc.resource, tc.action)
		if err == nil {
			t.Errorf("Authorize(%q, %q) should be denied for OIDC", tc.resource, tc.action)
		}
	}
}

func TestCompositeAuthorizer_JWTDelegatesToPrimary(t *testing.T) {
	primary := &mockAuthorizer{err: nil}
	comp := NewCompositeAuthorizer(primary)

	identity := &rpc.Identity{
		Subject: "user@example.com",
		Method:  rpc.AuthMethodJWT,
	}

	err := comp.Authorize(context.Background(), identity, "deployment", "deployversion")
	if err != nil {
		t.Errorf("JWT auth should delegate to primary: %v", err)
	}

	// Now make primary deny
	primary.err = fmt.Errorf("access denied by RBAC")
	err = comp.Authorize(context.Background(), identity, "deployment", "deployversion")
	if err == nil {
		t.Error("JWT auth should propagate primary denial")
	}
}

func TestCompositeAuthorizer_NilPrimary(t *testing.T) {
	comp := NewCompositeAuthorizer(nil)

	identity := &rpc.Identity{
		Subject: "user@example.com",
		Method:  rpc.AuthMethodJWT,
	}

	// With nil primary, JWT auth should allow (no-op)
	err := comp.Authorize(context.Background(), identity, "deployment", "deployversion")
	if err != nil {
		t.Errorf("nil primary should allow all for JWT: %v", err)
	}
}

func TestAuthorizeOIDC(t *testing.T) {
	tests := []struct {
		resource string
		action   string
		allowed  bool
	}{
		{"deployment", "deployversion", true},
		{"deployment", "canceldeployment", true},
		{"logs", "applogs", true},
		{"crud", "list", true},
		{"crud", "getconfiguration", true},
		{"crud", "delete", false},
		{"builder", "buildfromtar", true},
		{"builder", "analyzeapp", true},
		{"telemetry", "reportspans", true},
		{"unknown", "anything", false},
		{"deployment", "unknown", false},
		{"", "", false},
	}

	for _, tt := range tests {
		err := authorizeRole("OIDC", oidcDeployRole, tt.resource, tt.action)
		if tt.allowed && err != nil {
			t.Errorf("authorizeRole(OIDC, %q, %q) should be allowed: %v", tt.resource, tt.action, err)
		}
		if !tt.allowed && err == nil {
			t.Errorf("authorizeRole(OIDC, %q, %q) should be denied", tt.resource, tt.action)
		}
	}
}

func workloadIdentity(app, role string) *rpc.Identity {
	return &rpc.Identity{
		Subject:  fmt.Sprintf("org:org-1:app:%s:sandbox:sb-1", app),
		Method:   rpc.AuthMethodWorkload,
		Metadata: map[string]any{"app": app, "role": role, "sandbox_id": "sb-1"},
	}
}

// The authorizer resolves the role named in the token and enforces its perms.
// A spot-check per role — the exhaustive membership is tested in
// pkg/workloadroles; here we only prove Authorize wires role name → perms.
func TestCompositeAuthorizer_WorkloadRoles(t *testing.T) {
	comp := NewCompositeAuthorizer(nil)

	cases := []struct {
		role            string
		granted, denied [2]string
	}{
		{workloadroles.RoleAppReadonly, [2]string{"logs", "applogs"}, [2]string{"crud", "setenvvar"}},
		{workloadroles.RoleAppAdmin, [2]string{"crud", "setenvvar"}, [2]string{"crud", "list"}},
		{workloadroles.RoleAppDebugger, [2]string{"sandboxexec", "exec"}, [2]string{"deployment", "deployversion"}},
		{workloadroles.RoleClusterReadonly, [2]string{"crud", "list"}, [2]string{"crud", "setenvvar"}},
		{workloadroles.RoleClusterAdmin, [2]string{"sandboxexec", "exec"}, [2]string{"entityaccess", "delete"}},
	}

	for _, c := range cases {
		id := workloadIdentity("myapp", c.role)
		if err := comp.Authorize(context.Background(), id, c.granted[0], c.granted[1]); err != nil {
			t.Errorf("%s: %s.%s should be allowed: %v", c.role, c.granted[0], c.granted[1], err)
		}
		if err := comp.Authorize(context.Background(), id, c.denied[0], c.denied[1]); err == nil {
			t.Errorf("%s: %s.%s should be denied", c.role, c.denied[0], c.denied[1])
		}
	}
}

// The carved-out methods are denied even to cluster-admin, the broadest role.
func TestCompositeAuthorizer_ClusterAdminCarveOuts(t *testing.T) {
	comp := NewCompositeAuthorizer(nil)
	id := workloadIdentity("myapp", workloadroles.RoleClusterAdmin)

	for _, m := range [][2]string{
		{"entityaccess", "delete"},
		{"runnerregistration", "issueworkloadtoken"},
		{"netdb", "releaseall"},
		{"oidcbindings", "add"},
	} {
		if err := comp.Authorize(context.Background(), id, m[0], m[1]); err == nil {
			t.Errorf("cluster-admin must not reach %s.%s", m[0], m[1])
		}
	}
}

// Setting an app's workload role is operator-only: it is absent from the OIDC
// deploy role and from every workload role, so neither an app-scoped OIDC
// identity nor an in-sandbox workload (even cluster-admin) can call it. Only
// cert/JWT-with-RBAC reach it. This is what stops an app owner self-granting a
// cluster role.
func TestCompositeAuthorizer_SetWorkloadRoleIsOperatorOnly(t *testing.T) {
	comp := NewCompositeAuthorizer(nil)

	oidc := &rpc.Identity{Method: rpc.AuthMethodOIDC, Metadata: map[string]any{"bound_app": "myapp"}}
	if err := comp.Authorize(context.Background(), oidc, "crud", "setworkloadrole"); err == nil {
		t.Error("OIDC deploy identity must not set workload roles")
	}

	for _, role := range []string{workloadroles.RoleAppAdmin, workloadroles.RoleClusterAdmin} {
		id := workloadIdentity("myapp", role)
		if err := comp.Authorize(context.Background(), id, "crud", "setworkloadrole"); err == nil {
			t.Errorf("workload role %q must not set workload roles", role)
		}
	}
}

// An unknown or missing role name grants nothing (fail closed).
func TestCompositeAuthorizer_WorkloadUnknownRoleDenied(t *testing.T) {
	comp := NewCompositeAuthorizer(nil)

	for _, role := range []string{"", "no-such-role"} {
		id := workloadIdentity("myapp", role)
		if err := comp.Authorize(context.Background(), id, "logs", "applogs"); err == nil {
			t.Errorf("role %q must be denied everything", role)
		}
	}
}

// A workload is authorized by its role alone and never delegates to cloud RBAC,
// which would evaluate it as a group-less identity.
func TestCompositeAuthorizer_WorkloadIgnoresPrimary(t *testing.T) {
	primary := &mockAuthorizer{err: fmt.Errorf("cloud RBAC denies group-less identities")}
	comp := NewCompositeAuthorizer(primary)

	err := comp.Authorize(context.Background(), workloadIdentity("myapp", workloadroles.RoleAppReadonly), "appstatus", "appinfo")
	if err != nil {
		t.Errorf("workload authorization should not consult primary: %v", err)
	}
}

// The regression guard for the default-allow trap: an auth method the authorizer
// doesn't know must be denied, not handed to primary-or-allow. With a nil
// primary -- the local-only configuration -- that branch used to return nil,
// silently granting every new AuthMethod full access to the cluster.
func TestCompositeAuthorizer_UnknownMethodDenied(t *testing.T) {
	for _, primary := range []rpc.Authorizer{nil, &mockAuthorizer{err: nil}} {
		comp := NewCompositeAuthorizer(primary)

		identity := &rpc.Identity{
			Subject: "whoever",
			Method:  rpc.AuthMethod("some-future-method"),
		}

		err := comp.Authorize(context.Background(), identity, "admin", "runcommand")
		if err == nil {
			t.Errorf("unknown auth method must be denied (primary=%v)", primary)
		}
	}
}
