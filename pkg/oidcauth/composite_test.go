package oidcauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
)

// mockAuthenticator is a configurable authenticator for testing.
type mockAuthenticator struct {
	identity *rpc.Identity
	err      error
}

func (m *mockAuthenticator) Authenticate(ctx context.Context, r *http.Request) (*rpc.Identity, error) {
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
	identity, err := comp.Authenticate(context.Background(), req)
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
	_, err := comp.Authenticate(context.Background(), req)
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

	comp := &CompositeAuthenticator{primary: primary, oidc: oidcStub}

	req := httptest.NewRequest("GET", "/", nil)
	identity, err := comp.Authenticate(context.Background(), req)
	if identity != nil {
		t.Error("expected nil identity when neither authenticator succeeds")
	}

	var mismatch *BindingMismatchError
	if !errors.As(err, &mismatch) {
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

	comp := &CompositeAuthenticator{primary: primary, oidc: oidcStub}

	req := httptest.NewRequest("GET", "/", nil)
	identity, err := comp.Authenticate(context.Background(), req)
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

	identity, err := comp.Authenticate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if identity != nil {
		t.Error("expected nil identity when both authenticators fail to match")
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
		err := authorizeOIDC(tt.resource, tt.action)
		if tt.allowed && err != nil {
			t.Errorf("authorizeOIDC(%q, %q) should be allowed: %v", tt.resource, tt.action, err)
		}
		if !tt.allowed && err == nil {
			t.Errorf("authorizeOIDC(%q, %q) should be denied", tt.resource, tt.action)
		}
	}
}
