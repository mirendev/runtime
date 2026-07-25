package rpc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
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

			identity, err := auth.Authenticate(context.Background(), CredentialsFromRequest(req))
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

			identity, err := auth.Authenticate(context.Background(), CredentialsFromRequest(req))
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
