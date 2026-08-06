package oidcauth

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

type testOIDCServer struct {
	server     *httptest.Server
	privateKey *rsa.PrivateKey
	kid        string
}

func newTestOIDCServer(t *testing.T) *testOIDCServer {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	kid := "test-key-1"

	ts := &testOIDCServer{
		privateKey: privateKey,
		kid:        kid,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		// Use the actual server URL once set
		json.NewEncoder(w).Encode(map[string]any{
			"issuer":   ts.server.URL,
			"jwks_uri": ts.server.URL + "/jwks",
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		jwk := jose.JSONWebKey{
			Key:       &privateKey.PublicKey,
			KeyID:     kid,
			Algorithm: "RS256",
			Use:       "sig",
		}
		jwks := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{jwk}}
		json.NewEncoder(w).Encode(jwks)
	})

	ts.server = httptest.NewServer(mux)
	return ts
}

func (ts *testOIDCServer) Close() {
	ts.server.Close()
}

func (ts *testOIDCServer) URL() string {
	return ts.server.URL
}

func (ts *testOIDCServer) SignToken(claims jwt.MapClaims) string {
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = ts.kid
	tokenString, err := token.SignedString(ts.privateKey)
	if err != nil {
		panic(fmt.Sprintf("failed to sign token: %v", err))
	}
	return tokenString
}

func TestValidateToken_Valid(t *testing.T) {
	ts := newTestOIDCServer(t)
	defer ts.Close()

	tokenString := ts.SignToken(jwt.MapClaims{
		"iss":        ts.URL(),
		"sub":        "repo:acme/web-app:ref:refs/heads/main",
		"aud":        "miren-test",
		"exp":        time.Now().Add(10 * time.Minute).Unix(),
		"iat":        time.Now().Unix(),
		"event_name": "push",
	})

	v := NewValidator()
	claims, err := v.ValidateToken(context.Background(), tokenString, ts.URL(), "miren-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if claims.Issuer != ts.URL() {
		t.Errorf("issuer = %q, want %q", claims.Issuer, ts.URL())
	}
	if claims.Subject != "repo:acme/web-app:ref:refs/heads/main" {
		t.Errorf("subject = %q, want %q", claims.Subject, "repo:acme/web-app:ref:refs/heads/main")
	}
	if claims.Extra["event_name"] != "push" {
		t.Errorf("event_name = %v, want push", claims.Extra["event_name"])
	}
}

func TestValidateToken_Expired(t *testing.T) {
	ts := newTestOIDCServer(t)
	defer ts.Close()

	tokenString := ts.SignToken(jwt.MapClaims{
		"iss": ts.URL(),
		"sub": "repo:acme/web-app:ref:refs/heads/main",
		"aud": "miren-test",
		"exp": time.Now().Add(-10 * time.Minute).Unix(),
		"iat": time.Now().Add(-20 * time.Minute).Unix(),
	})

	v := NewValidator()
	_, err := v.ValidateToken(context.Background(), tokenString, ts.URL(), "miren-test")
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestValidateToken_WrongIssuer(t *testing.T) {
	ts := newTestOIDCServer(t)
	defer ts.Close()

	tokenString := ts.SignToken(jwt.MapClaims{
		"iss": ts.URL(),
		"sub": "repo:acme/web-app:ref:refs/heads/main",
		"aud": "miren-test",
		"exp": time.Now().Add(10 * time.Minute).Unix(),
		"iat": time.Now().Unix(),
	})

	v := NewValidator()
	_, err := v.ValidateToken(context.Background(), tokenString, "https://wrong-issuer.example.com", "miren-test")
	if err == nil {
		t.Fatal("expected error for wrong issuer")
	}
}

func TestValidateToken_WrongAudience(t *testing.T) {
	ts := newTestOIDCServer(t)
	defer ts.Close()

	tokenString := ts.SignToken(jwt.MapClaims{
		"iss": ts.URL(),
		"sub": "repo:acme/web-app:ref:refs/heads/main",
		"aud": "wrong-audience",
		"exp": time.Now().Add(10 * time.Minute).Unix(),
		"iat": time.Now().Unix(),
	})

	v := NewValidator()
	_, err := v.ValidateToken(context.Background(), tokenString, ts.URL(), "miren-test")
	if err == nil {
		t.Fatal("expected error for wrong audience")
	}
}

func TestValidateToken_JWKSRefresh(t *testing.T) {
	// Start with a different key in JWKS to force a refresh
	privateKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	signingKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	kid := "test-key-1"
	requestCount := 0

	mux := http.NewServeMux()
	var server *httptest.Server

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"issuer":   server.URL,
			"jwks_uri": server.URL + "/jwks",
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		var key *rsa.PublicKey
		if requestCount == 1 {
			// First request returns wrong key
			key = &privateKey.PublicKey
		} else {
			// Second request returns correct key
			key = &signingKey.PublicKey
		}
		jwk := jose.JSONWebKey{
			Key:       key,
			KeyID:     kid,
			Algorithm: "RS256",
			Use:       "sig",
		}
		jwks := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{jwk}}
		json.NewEncoder(w).Encode(jwks)
	})

	server = httptest.NewServer(mux)
	defer server.Close()

	// Sign with the signing key
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss": server.URL,
		"sub": "test-subject",
		"aud": "miren-test",
		"exp": time.Now().Add(10 * time.Minute).Unix(),
		"iat": time.Now().Unix(),
	})
	token.Header["kid"] = kid
	tokenString, _ := token.SignedString(signingKey)

	v := NewValidator()
	claims, err := v.ValidateToken(context.Background(), tokenString, server.URL, "miren-test")
	if err != nil {
		t.Fatalf("expected JWKS refresh to succeed: %v", err)
	}

	if claims.Subject != "test-subject" {
		t.Errorf("subject = %q, want test-subject", claims.Subject)
	}

	if requestCount < 2 {
		t.Errorf("expected at least 2 JWKS requests (initial + refresh), got %d", requestCount)
	}
}

func TestClaimsMatchesSubjectPattern(t *testing.T) {
	tests := []struct {
		subject string
		pattern string
		want    bool
	}{
		{"repo:acme/web-app:ref:refs/heads/main", "repo:acme/web-app:*", true},
		{"repo:acme/web-app:ref:refs/heads/main", "repo:acme/other:*", false},
		{"repo:acme/web-app:ref:refs/heads/main", "", true},
		{"repo:acme/web-app:ref:refs/heads/main", "repo:acme/web-app:ref:refs/heads/main", true},
	}

	for _, tt := range tests {
		c := &Claims{Subject: tt.subject}
		got := c.MatchesSubjectPattern(tt.pattern)
		if got != tt.want {
			t.Errorf("MatchesSubjectPattern(%q, %q) = %v, want %v", tt.subject, tt.pattern, got, tt.want)
		}
	}
}

func TestClaimsMatchesClaimConditions(t *testing.T) {
	claims := &Claims{
		Extra: map[string]any{
			"event_name": "push",
			"ref":        "refs/heads/main",
		},
	}

	tests := []struct {
		name       string
		conditions []ClaimCondition
		want       bool
	}{
		{
			name:       "empty conditions",
			conditions: nil,
			want:       true,
		},
		{
			name: "single match",
			conditions: []ClaimCondition{
				{Key: "event_name", Pattern: "push"},
			},
			want: true,
		},
		{
			name: "comma alternatives",
			conditions: []ClaimCondition{
				{Key: "event_name", Pattern: "push,workflow_dispatch"},
			},
			want: true,
		},
		{
			name: "no match",
			conditions: []ClaimCondition{
				{Key: "event_name", Pattern: "pull_request"},
			},
			want: false,
		},
		{
			name: "missing claim",
			conditions: []ClaimCondition{
				{Key: "nonexistent", Pattern: "value"},
			},
			want: false,
		},
		{
			name: "multiple conditions all match",
			conditions: []ClaimCondition{
				{Key: "event_name", Pattern: "push"},
				{Key: "ref", Pattern: "refs/heads/*"},
			},
			want: true,
		},
		{
			name: "multiple conditions one fails",
			conditions: []ClaimCondition{
				{Key: "event_name", Pattern: "push"},
				{Key: "ref", Pattern: "refs/tags/*"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := claims.MatchesClaimConditions(tt.conditions)
			if got != tt.want {
				t.Errorf("MatchesClaimConditions() = %v, want %v", got, tt.want)
			}
		})
	}
}
