package httpingress

import (
	"log/slog"
	"net/http/httptest"
	"testing"

	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/oidc"
)

func TestInjectClaims(t *testing.T) {
	tests := []struct {
		name            string
		claimMappings   []ingress_v1alpha.ClaimMappings
		claims          map[string]any
		existingHeaders map[string]string
		wantHeaders     map[string]string
		wantAbsent      []string
	}{
		{
			name: "basic string claims",
			claimMappings: []ingress_v1alpha.ClaimMappings{
				{Claim: "email", Header: "X-User-Email"},
				{Claim: "sub", Header: "X-User-ID"},
			},
			claims: map[string]any{
				"email": "alice@example.com",
				"sub":   "user-123",
			},
			wantHeaders: map[string]string{
				"X-User-Email": "alice@example.com",
				"X-User-ID":    "user-123",
			},
		},
		{
			name: "missing claim is skipped",
			claimMappings: []ingress_v1alpha.ClaimMappings{
				{Claim: "email", Header: "X-User-Email"},
				{Claim: "groups", Header: "X-User-Groups"},
			},
			claims: map[string]any{
				"email": "alice@example.com",
			},
			wantHeaders: map[string]string{
				"X-User-Email": "alice@example.com",
			},
			wantAbsent: []string{"X-User-Groups"},
		},
		{
			name: "spoofed header is stripped when claim is missing",
			claimMappings: []ingress_v1alpha.ClaimMappings{
				{Claim: "email", Header: "X-User-Email"},
				{Claim: "groups", Header: "X-User-Groups"},
			},
			claims: map[string]any{
				"email": "alice@example.com",
				// no "groups" claim
			},
			existingHeaders: map[string]string{
				"X-User-Groups": "admin",
			},
			wantHeaders: map[string]string{
				"X-User-Email": "alice@example.com",
			},
			wantAbsent: []string{"X-User-Groups"},
		},
		{
			name: "spoofed header is overwritten when claim is present",
			claimMappings: []ingress_v1alpha.ClaimMappings{
				{Claim: "email", Header: "X-User-Email"},
			},
			claims: map[string]any{
				"email": "alice@example.com",
			},
			existingHeaders: map[string]string{
				"X-User-Email": "evil@attacker.com",
			},
			wantHeaders: map[string]string{
				"X-User-Email": "alice@example.com",
			},
		},
		{
			name: "numeric claim",
			claimMappings: []ingress_v1alpha.ClaimMappings{
				{Claim: "iat", Header: "X-Token-Issued"},
			},
			claims: map[string]any{
				"iat": float64(1700000000),
			},
			wantHeaders: map[string]string{
				"X-Token-Issued": "1.7e+09",
			},
		},
		{
			name: "boolean claim",
			claimMappings: []ingress_v1alpha.ClaimMappings{
				{Claim: "email_verified", Header: "X-Email-Verified"},
			},
			claims: map[string]any{
				"email_verified": true,
			},
			wantHeaders: map[string]string{
				"X-Email-Verified": "true",
			},
		},
		{
			name: "array claim is JSON-encoded",
			claimMappings: []ingress_v1alpha.ClaimMappings{
				{Claim: "groups", Header: "X-User-Groups"},
			},
			claims: map[string]any{
				"groups": []any{"engineering", "platform"},
			},
			wantHeaders: map[string]string{
				"X-User-Groups": `["engineering","platform"]`,
			},
		},
		{
			name: "object claim is JSON-encoded",
			claimMappings: []ingress_v1alpha.ClaimMappings{
				{Claim: "address", Header: "X-User-Address"},
			},
			claims: map[string]any{
				"address": map[string]any{"city": "Portland"},
			},
			wantHeaders: map[string]string{
				"X-User-Address": `{"city":"Portland"}`,
			},
		},
		{
			name: "empty claim or header in mapping is skipped",
			claimMappings: []ingress_v1alpha.ClaimMappings{
				{Claim: "", Header: "X-User-Email"},
				{Claim: "email", Header: ""},
				{Claim: "sub", Header: "X-User-ID"},
			},
			claims: map[string]any{
				"email": "alice@example.com",
				"sub":   "user-123",
			},
			wantHeaders: map[string]string{
				"X-User-ID": "user-123",
			},
			wantAbsent: []string{"X-User-Email"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			for k, v := range tt.existingHeaders {
				req.Header.Set(k, v)
			}

			h := &oidcHandler{
				route: &ingress_v1alpha.HttpRoute{
					ClaimMappings: tt.claimMappings,
				},
			}

			h.injectClaims(req, tt.claims)

			for header, want := range tt.wantHeaders {
				got := req.Header.Get(header)
				if got != want {
					t.Errorf("header %s = %q, want %q", header, got, want)
				}
			}

			for _, header := range tt.wantAbsent {
				if got := req.Header.Get(header); got != "" {
					t.Errorf("header %s should be absent, got %q", header, got)
				}
			}
		})
	}
}

func TestOidcProviderMatches(t *testing.T) {
	base := &ingress_v1alpha.OidcProvider{
		ID:           "provider-1",
		ClientId:     "client-AAA",
		ClientSecret: "secret-AAA",
		ProviderUrl:  "https://auth.example.com",
		Scopes:       "openid email",
	}

	handler := &oidcHandler{provider: base}

	t.Run("identical provider matches", func(t *testing.T) {
		same := &ingress_v1alpha.OidcProvider{
			ID:           "provider-1",
			ClientId:     "client-AAA",
			ClientSecret: "secret-AAA",
			ProviderUrl:  "https://auth.example.com",
			Scopes:       "openid email",
		}
		if !oidcProviderMatches(handler, same) {
			t.Error("expected match for identical provider")
		}
	})

	t.Run("different client_id does not match", func(t *testing.T) {
		different := &ingress_v1alpha.OidcProvider{
			ID:           "provider-1",
			ClientId:     "client-BBB",
			ClientSecret: "secret-AAA",
			ProviderUrl:  "https://auth.example.com",
			Scopes:       "openid email",
		}
		if oidcProviderMatches(handler, different) {
			t.Error("expected mismatch for different client_id")
		}
	})

	t.Run("different provider ID does not match", func(t *testing.T) {
		different := &ingress_v1alpha.OidcProvider{
			ID:           "provider-2",
			ClientId:     "client-AAA",
			ClientSecret: "secret-AAA",
			ProviderUrl:  "https://auth.example.com",
			Scopes:       "openid email",
		}
		if oidcProviderMatches(handler, different) {
			t.Error("expected mismatch for different provider ID")
		}
	})

	t.Run("different secret does not match", func(t *testing.T) {
		different := &ingress_v1alpha.OidcProvider{
			ID:           "provider-1",
			ClientId:     "client-AAA",
			ClientSecret: "secret-BBB",
			ProviderUrl:  "https://auth.example.com",
			Scopes:       "openid email",
		}
		if oidcProviderMatches(handler, different) {
			t.Error("expected mismatch for different client_secret")
		}
	})
}

func makeOIDCProviderEntity(ident, clientID, clientSecret, providerURL, scopes string) *entity.Entity {
	return entity.New(
		entity.DBId, entity.Id(ident),
		entity.Ref(entity.EntityKind, ingress_v1alpha.KindOidcProvider),
		entity.String(ingress_v1alpha.OidcProviderClientIdId, clientID),
		entity.String(ingress_v1alpha.OidcProviderClientSecretId, clientSecret),
		entity.String(ingress_v1alpha.OidcProviderProviderUrlId, providerURL),
		entity.String(ingress_v1alpha.OidcProviderScopesId, scopes),
	)
}

func TestGetOrCreateOIDCHandlerCacheInvalidation(t *testing.T) {
	signingKey := make([]byte, 32)

	srv := &Server{
		Log:                slog.Default(),
		oidcSessionManager: oidc.NewSessionManager(false, "", signingKey),
		oidcHandlers:       make(map[string]*oidcHandler),
	}

	providerIdent := "test/oidc-provider"

	route := &ingress_v1alpha.HttpRoute{
		Host:         "socials.example.com",
		AuthProvider: entity.Id(providerIdent),
	}

	entA := makeOIDCProviderEntity(providerIdent,
		"client-AAA", "secret-AAA", "https://auth.example.com", "openid email")

	// First call: creates and caches a handler
	h1, err := srv.getOrCreateOIDCHandler(route, "https://socials.example.com", entA)
	if err != nil {
		t.Fatalf("first getOrCreateOIDCHandler: %v", err)
	}
	if h1.provider.ClientId != "client-AAA" {
		t.Fatalf("expected client_id=client-AAA, got %s", h1.provider.ClientId)
	}

	// Second call with same entity: should return cached handler
	h2, err := srv.getOrCreateOIDCHandler(route, "https://socials.example.com", entA)
	if err != nil {
		t.Fatalf("second getOrCreateOIDCHandler: %v", err)
	}
	if h1 != h2 {
		t.Error("expected same handler instance on cache hit")
	}

	// Pass a new entity with updated client_id
	entB := makeOIDCProviderEntity(providerIdent,
		"client-BBB", "secret-AAA", "https://auth.example.com", "openid email")

	h3, err := srv.getOrCreateOIDCHandler(route, "https://socials.example.com", entB)
	if err != nil {
		t.Fatalf("third getOrCreateOIDCHandler: %v", err)
	}
	if h3.provider.ClientId != "client-BBB" {
		t.Fatalf("expected client_id=client-BBB after update, got %s", h3.provider.ClientId)
	}
	if h1 == h3 {
		t.Error("expected different handler instance after provider change")
	}
}
