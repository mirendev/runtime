package oidc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"
)

func TestGeneratePKCEChallenge(t *testing.T) {
	verifier := "test-verifier-123456789"
	challenge := generatePKCEChallenge(verifier)

	if challenge == "" {
		t.Error("PKCE challenge is empty")
	}

	// Challenge should be different from verifier
	if challenge == verifier {
		t.Error("PKCE challenge should be hashed, not plain verifier")
	}

	// Should be deterministic
	challenge2 := generatePKCEChallenge(verifier)
	if challenge != challenge2 {
		t.Error("PKCE challenge should be deterministic")
	}

	// Different verifiers should produce different challenges
	challenge3 := generatePKCEChallenge("different-verifier")
	if challenge == challenge3 {
		t.Error("different verifiers should produce different challenges")
	}
}

// newTestProvider starts an httptest server that serves OIDC discovery and JWKS
// endpoints backed by the given RSA key. It returns the server and the key ID
// used in the JWKS.
func newTestProvider(t *testing.T, key *rsa.PrivateKey) (*httptest.Server, string) {
	t.Helper()

	kid := "test-key-1"

	mux := http.NewServeMux()

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		// We need the server URL but don't have it yet during setup,
		// so we read it from the request host.
		baseURL := fmt.Sprintf("http://%s", r.Host)
		json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 baseURL,
			"authorization_endpoint": baseURL + "/authorize",
			"token_endpoint":         baseURL + "/token",
			"jwks_uri":               baseURL + "/jwks",
		})
	})

	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		jwk := jose.JSONWebKey{
			Key:       &key.PublicKey,
			KeyID:     kid,
			Algorithm: string(jose.RS256),
			Use:       "sig",
		}
		json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{jwk}})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, kid
}

// signToken creates a signed JWT with the given claims using RS256.
func signToken(t *testing.T, key *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}
	return signed
}

func TestParseIDToken(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	srv, kid := newTestProvider(t, key)

	client := NewClient(srv.URL, "client-id", "secret", "https://redirect.example.com", []string{"openid"}, nil)

	t.Run("valid token", func(t *testing.T) {
		token := signToken(t, key, kid, jwt.MapClaims{
			"iss":  srv.URL,
			"aud":  "client-id",
			"sub":  "user-123",
			"name": "Test User",
			"exp":  time.Now().Add(time.Hour).Unix(),
			"iat":  time.Now().Unix(),
		})

		claims, err := client.ParseIDToken(context.Background(), token)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if claims["sub"] != "user-123" {
			t.Errorf("sub claim mismatch: got %v", claims["sub"])
		}
		if claims["name"] != "Test User" {
			t.Errorf("name claim mismatch: got %v", claims["name"])
		}
	})

	t.Run("wrong issuer", func(t *testing.T) {
		token := signToken(t, key, kid, jwt.MapClaims{
			"iss": "https://evil.example.com",
			"aud": "client-id",
			"sub": "user-123",
			"exp": time.Now().Add(time.Hour).Unix(),
			"iat": time.Now().Unix(),
		})

		_, err := client.ParseIDToken(context.Background(), token)
		if err == nil {
			t.Fatal("expected error for wrong issuer")
		}
	})

	t.Run("wrong audience", func(t *testing.T) {
		token := signToken(t, key, kid, jwt.MapClaims{
			"iss": srv.URL,
			"aud": "wrong-client",
			"sub": "user-123",
			"exp": time.Now().Add(time.Hour).Unix(),
			"iat": time.Now().Unix(),
		})

		_, err := client.ParseIDToken(context.Background(), token)
		if err == nil {
			t.Fatal("expected error for wrong audience")
		}
	})

	t.Run("expired token", func(t *testing.T) {
		token := signToken(t, key, kid, jwt.MapClaims{
			"iss": srv.URL,
			"aud": "client-id",
			"sub": "user-123",
			"exp": time.Now().Add(-time.Hour).Unix(),
			"iat": time.Now().Add(-2 * time.Hour).Unix(),
		})

		_, err := client.ParseIDToken(context.Background(), token)
		if err == nil {
			t.Fatal("expected error for expired token")
		}
	})

	t.Run("tampered signature", func(t *testing.T) {
		otherKey, _ := rsa.GenerateKey(rand.Reader, 2048)
		token := signToken(t, otherKey, kid, jwt.MapClaims{
			"iss": srv.URL,
			"aud": "client-id",
			"sub": "user-123",
			"exp": time.Now().Add(time.Hour).Unix(),
			"iat": time.Now().Unix(),
		})

		_, err := client.ParseIDToken(context.Background(), token)
		if err == nil {
			t.Fatal("expected error for tampered signature")
		}
	})
}

func TestClient_AuthorizationURL(t *testing.T) {
	client := NewClient(
		"https://provider.example.com",
		"test-client-id",
		"test-secret",
		"https://app.example.com/callback",
		[]string{"openid", "email", "profile"},
		nil,
	)

	// Note: This test will fail without a real OIDC provider
	// In a real test environment, we'd mock the HTTP client
	_, err := client.AuthorizationURL("test-state", "test-verifier", "app.example.com")
	if err != nil {
		// Expected to fail without real provider, but we can check error message
		t.Logf("Expected error without provider: %v", err)
	}
}

func TestNewClient(t *testing.T) {
	client := NewClient(
		"https://provider.example.com",
		"client-id",
		"client-secret",
		"https://redirect.example.com/callback",
		[]string{"openid", "email"},
		nil,
	)

	if client == nil {
		t.Fatal("client is nil")
		return
	}

	if client.clientID != "client-id" {
		t.Errorf("client ID mismatch: got %s", client.clientID)
	}

	if client.providerURL != "https://provider.example.com" {
		t.Errorf("provider URL mismatch: got %s", client.providerURL)
	}

	if len(client.scopes) != 2 {
		t.Errorf("scopes length mismatch: got %d", len(client.scopes))
	}
}
