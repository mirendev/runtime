package rpc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"net/http"
	"testing"
)

// TestNoOpAuthenticator verifies that NoOpAuthenticator always returns an anonymous identity
func TestNoOpAuthenticator(t *testing.T) {
	auth := &NoOpAuthenticator{}

	tests := []struct {
		name       string
		authHeader string
	}{
		{
			name:       "no auth header",
			authHeader: "",
		},
		{
			name:       "with bearer token",
			authHeader: "Bearer token123",
		},
		{
			name:       "with basic auth",
			authHeader: "Basic dXNlcjpwYXNz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest("POST", "/_rpc/call/test/method", nil)
			if err != nil {
				t.Fatal(err)
			}

			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			identity, err := auth.Authenticate(context.Background(), req)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if identity == nil {
				t.Error("expected non-nil identity")
				return
			}
			if identity.Subject != "anonymous" {
				t.Errorf("expected subject=anonymous, got %q", identity.Subject)
			}
			if identity.Method != AuthMethodAnonymous {
				t.Errorf("expected method=%v, got %v", AuthMethodAnonymous, identity.Method)
			}
		})
	}
}

// TestLocalOnlyAuthenticator verifies the LocalOnlyAuthenticator behavior
func TestLocalOnlyAuthenticator(t *testing.T) {
	mockCert := &x509.Certificate{
		Subject: pkix.Name{
			CommonName: "test-client",
		},
	}

	tests := []struct {
		name           string
		hasCert        bool
		verifiedChain  bool
		expectIdentity bool
		expectSubject  string
		expectMethod   AuthMethod
	}{
		{
			name:           "verified certificate returns identity",
			hasCert:        true,
			verifiedChain:  true,
			expectIdentity: true,
			expectSubject:  "test-client",
			expectMethod:   AuthMethodCert,
		},
		{
			// Regression test for the RPC auth bypass: a client cert that was
			// presented but not verified against the cluster CA (empty
			// VerifiedChains) must NOT yield a cert identity, otherwise a
			// self-signed forgery would be granted superuser access.
			name:           "unverified certificate returns nil",
			hasCert:        true,
			verifiedChain:  false,
			expectIdentity: false,
		},
		{
			name:           "without certificate returns nil",
			hasCert:        false,
			expectIdentity: false,
		},
		{
			name:           "TLS without peer certs returns nil",
			hasCert:        false,
			expectIdentity: false,
		},
	}

	auth := &LocalOnlyAuthenticator{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest("POST", "/_rpc/call/test/method", nil)
			if err != nil {
				t.Fatal(err)
			}

			if tt.hasCert {
				req.TLS = &tls.ConnectionState{
					PeerCertificates: []*x509.Certificate{mockCert},
				}
				if tt.verifiedChain {
					req.TLS.VerifiedChains = [][]*x509.Certificate{{mockCert}}
				}
			} else if tt.name == "TLS without peer certs returns nil" {
				// TLS connection but no peer certificates
				req.TLS = &tls.ConnectionState{}
			}

			identity, err := auth.Authenticate(context.Background(), req)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if tt.expectIdentity {
				if identity == nil {
					t.Error("expected non-nil identity")
					return
				}
				if identity.Subject != tt.expectSubject {
					t.Errorf("expected subject=%q, got %q", tt.expectSubject, identity.Subject)
				}
				if identity.Method != tt.expectMethod {
					t.Errorf("expected method=%v, got %v", tt.expectMethod, identity.Method)
				}
			} else {
				if identity != nil {
					t.Errorf("expected nil identity, got %+v", identity)
				}
			}
		})
	}
}

// TestIdentityContext verifies the context helpers for identity propagation
func TestIdentityContext(t *testing.T) {
	t.Run("stores and retrieves identity", func(t *testing.T) {
		identity := &Identity{
			Subject: "test-user",
			Groups:  []string{"admin", "users"},
			Method:  AuthMethodJWT,
			Metadata: map[string]any{
				"organization_id": "org-123",
			},
		}

		ctx := ContextWithIdentity(context.Background(), identity)
		retrieved := IdentityFromContext(ctx)

		if retrieved == nil {
			t.Fatal("expected non-nil identity from context")
			return
		}
		if retrieved.Subject != identity.Subject {
			t.Errorf("expected subject=%q, got %q", identity.Subject, retrieved.Subject)
		}
		if len(retrieved.Groups) != len(identity.Groups) {
			t.Errorf("expected %d groups, got %d", len(identity.Groups), len(retrieved.Groups))
		}
		if retrieved.Method != identity.Method {
			t.Errorf("expected method=%v, got %v", identity.Method, retrieved.Method)
		}
		if retrieved.Metadata["organization_id"] != identity.Metadata["organization_id"] {
			t.Errorf("expected org_id=%v, got %v",
				identity.Metadata["organization_id"],
				retrieved.Metadata["organization_id"])
		}
	})

	t.Run("returns nil for empty context", func(t *testing.T) {
		retrieved := IdentityFromContext(context.Background())
		if retrieved != nil {
			t.Errorf("expected nil identity, got %+v", retrieved)
		}
	})
}

func ctxWith(identity *Identity) context.Context {
	return ContextWithIdentity(context.Background(), identity)
}

// TestBoundApp covers the registry of app-scoped auth methods. A method that
// stops reporting its binding here silently disables every AllowApp call site
// for it.
func TestBoundApp(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
		want string
	}{
		{
			name: "oidc reports its binding",
			ctx:  ctxWith(&Identity{Method: AuthMethodOIDC, Metadata: map[string]any{"bound_app": "myapp"}}),
			want: "myapp",
		},
		{
			name: "workload reports its app claim",
			ctx:  ctxWith(&Identity{Method: AuthMethodWorkload, Metadata: map[string]any{"app": "myapp"}}),
			want: "myapp",
		},
		{
			name: "workload does not read the oidc key",
			ctx:  ctxWith(&Identity{Method: AuthMethodWorkload, Metadata: map[string]any{"bound_app": "myapp"}}),
			want: "",
		},
		{
			name: "oidc does not read the workload key",
			ctx:  ctxWith(&Identity{Method: AuthMethodOIDC, Metadata: map[string]any{"app": "myapp"}}),
			want: "",
		},
		{
			name: "cert is not app-scoped",
			ctx:  ctxWith(&Identity{Method: AuthMethodCert, Metadata: map[string]any{"app": "myapp"}}),
			want: "",
		},
		{
			name: "jwt is not app-scoped",
			ctx:  ctxWith(&Identity{Method: AuthMethodJWT, Metadata: map[string]any{"app": "myapp"}}),
			want: "",
		},
		{
			name: "anonymous is not app-scoped",
			ctx:  ctxWith(&Identity{Method: AuthMethodAnonymous}),
			want: "",
		},
		{
			name: "token is not app-scoped",
			ctx:  ctxWith(&Identity{Method: AuthMethodToken}),
			want: "",
		},
		{
			name: "nil metadata",
			ctx:  ctxWith(&Identity{Method: AuthMethodWorkload}),
			want: "",
		},
		{
			name: "non-string metadata",
			ctx:  ctxWith(&Identity{Method: AuthMethodWorkload, Metadata: map[string]any{"app": 42}}),
			want: "",
		},
		{
			name: "no identity",
			ctx:  context.Background(),
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BoundApp(tt.ctx); got != tt.want {
				t.Errorf("BoundApp() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAllowApp(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
		app  string
		want bool
	}{
		{
			name: "workload allowed on its own app",
			ctx:  ctxWith(&Identity{Method: AuthMethodWorkload, Metadata: map[string]any{"app": "myapp"}}),
			app:  "myapp",
			want: true,
		},
		{
			name: "workload denied on another app",
			ctx:  ctxWith(&Identity{Method: AuthMethodWorkload, Metadata: map[string]any{"app": "myapp"}}),
			app:  "otherapp",
			want: false,
		},
		{
			name: "oidc allowed on its bound app",
			ctx:  ctxWith(&Identity{Method: AuthMethodOIDC, Metadata: map[string]any{"bound_app": "myapp"}}),
			app:  "myapp",
			want: true,
		},
		{
			name: "oidc denied on another app",
			ctx:  ctxWith(&Identity{Method: AuthMethodOIDC, Metadata: map[string]any{"bound_app": "myapp"}}),
			app:  "otherapp",
			want: false,
		},
		{
			name: "cert is unscoped",
			ctx:  ctxWith(&Identity{Method: AuthMethodCert, Subject: "runner-1"}),
			app:  "anyapp",
			want: true,
		},
		{
			name: "jwt is unscoped",
			ctx:  ctxWith(&Identity{Method: AuthMethodJWT}),
			app:  "anyapp",
			want: true,
		},
		{
			name: "anonymous is unscoped",
			ctx:  ctxWith(&Identity{Method: AuthMethodAnonymous}),
			app:  "anyapp",
			want: true,
		},
		{
			name: "no identity is unscoped",
			ctx:  context.Background(),
			app:  "anyapp",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AllowApp(tt.ctx, tt.app); got != tt.want {
				t.Errorf("AllowApp(%q) = %v, want %v", tt.app, got, tt.want)
			}
		})
	}
}

// A workload identity must never reach an app other than its own. This is the
// regression guard for the default-allow that AllowApp used to apply to every
// non-OIDC method: before BoundApp became method-driven, a workload token
// passed every AllowApp call site in the tree.
func TestAllowApp_WorkloadCannotCrossApps(t *testing.T) {
	ctx := ctxWith(&Identity{
		Subject:  "org:org-1:app:foo:sandbox:sb-1",
		Method:   AuthMethodWorkload,
		Metadata: map[string]any{"app": "foo"},
	})

	if !AllowApp(ctx, "foo") {
		t.Error("workload should reach its own app")
	}
	if AllowApp(ctx, "bar") {
		t.Error("workload reached another app: app scoping is not being enforced")
	}
	if err := AppAccessError(ctx, "bar"); err == nil || !errors.Is(err, ErrUnauthorized) {
		t.Errorf("expected an ErrUnauthorized denial, got %v", err)
	}
}

// TestServerTLSVerifiesClientCertsWhenCAConfigured is a configuration tripwire
// for the RPC client-cert auth bypass. When a cluster CA is configured, the
// listener must verify presented client certs against it. tls.RequestClientCert
// requests a client cert but never verifies it -- the exact misconfiguration
// behind the historical bypass -- so it must never be selected here. This is a
// cheap guard that fails the moment someone flips the mode back.
func TestServerTLSVerifiesClientCertsWhenCAConfigured(t *testing.T) {
	// A non-nil CA is all that's needed to drive the ClientAuth selection; the
	// bytes need not be a parseable cert for this branch.
	dummyCA := []byte("-----BEGIN CERTIFICATE-----\n-----END CERTIFICATE-----\n")

	tests := []struct {
		name     string
		opts     []StateOption
		wantAuth tls.ClientAuthType
	}{
		{
			name:     "CA configured verifies presented certs",
			opts:     []StateOption{WithCertificateVerification(dummyCA)},
			wantAuth: tls.VerifyClientCertIfGiven,
		},
		{
			name:     "CA configured with required client certs",
			opts:     []StateOption{WithCertificateVerification(dummyCA), WithRequireClientCerts},
			wantAuth: tls.RequireAndVerifyClientCert,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := NewState(t.Context(), tt.opts...)
			if err != nil {
				t.Fatalf("NewState: %v", err)
			}
			defer s.Close()

			got := s.serverTlsCfg.ClientAuth
			if got == tls.RequestClientCert {
				t.Fatal("listener uses tls.RequestClientCert with a CA configured; " +
					"presented client certs are not verified against the CA (auth bypass)")
			}
			if got != tt.wantAuth {
				t.Fatalf("ClientAuth = %v, want %v", got, tt.wantAuth)
			}
		})
	}
}
