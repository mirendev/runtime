package auth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
)

func TestJWTValidatorWithEdDSA(t *testing.T) {
	// Generate Ed25519 key pair for testing
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate Ed25519 key: %v", err)
	}

	// Create a mock JWKS server
	jwks := JWKS{
		Keys: []JWK{
			{
				Kty: "OKP",
				Kid: "test-key-1",
				Use: "sig",
				Alg: "EdDSA",
				Crv: "Ed25519",
				X:   base64.RawURLEncoding.EncodeToString(publicKey),
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/jwks.json" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwks)
	}))
	defer server.Close()

	// Create validator
	validator := NewJWTValidator(server.URL)

	// Create test claims
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "test-service-account",
			Issuer:    "miren.cloud",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		OrganizationID: "org-123",
		GroupIDs:       []string{"group-1", "group-2"},
	}

	// Create and sign token with Ed25519
	token := jwt.NewWithClaims(&jwt.SigningMethodEd25519{}, claims)
	token.Header["kid"] = "test-key-1"
	
	tokenString, err := token.SignedString(privateKey)
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}

	// Validate the token
	ctx := context.Background()
	validatedClaims, err := validator.ValidateToken(ctx, tokenString)
	if err != nil {
		t.Fatalf("failed to validate token: %v", err)
	}

	// Verify claims
	if validatedClaims.RegisteredClaims.Subject != "test-service-account" {
		t.Errorf("expected subject 'test-service-account', got %s", validatedClaims.RegisteredClaims.Subject)
	}
	if validatedClaims.OrganizationID != "org-123" {
		t.Errorf("expected org_id 'org-123', got %s", validatedClaims.OrganizationID)
	}
	if len(validatedClaims.GroupIDs) != 2 {
		t.Errorf("expected 2 groups, got %d", len(validatedClaims.GroupIDs))
	}
}

func TestJWTValidatorWithMultipleKeys(t *testing.T) {
	// Generate multiple Ed25519 key pairs
	publicKey1, privateKey1, _ := ed25519.GenerateKey(rand.Reader)
	publicKey2, privateKey2, _ := ed25519.GenerateKey(rand.Reader)

	// Create a mock JWKS server with multiple keys
	jwks := JWKS{
		Keys: []JWK{
			{
				Kty: "OKP",
				Kid: "key-1",
				Use: "sig",
				Alg: "EdDSA",
				Crv: "Ed25519",
				X:   base64.RawURLEncoding.EncodeToString(publicKey1),
			},
			{
				Kty: "OKP",
				Kid: "key-2",
				Use: "sig",
				Alg: "EdDSA",
				Crv: "Ed25519",
				X:   base64.RawURLEncoding.EncodeToString(publicKey2),
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/jwks.json" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwks)
	}))
	defer server.Close()

	validator := NewJWTValidator(server.URL)
	ctx := context.Background()

	// Test with first key
	t.Run("validate with key-1", func(t *testing.T) {
		claims := &Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   "sa-1",
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			},
		}
		token := jwt.NewWithClaims(&jwt.SigningMethodEd25519{}, claims)
		token.Header["kid"] = "key-1"
		
		tokenString, _ := token.SignedString(privateKey1)
		
		validatedClaims, err := validator.ValidateToken(ctx, tokenString)
		if err != nil {
			t.Fatalf("failed to validate token with key-1: %v", err)
		}
		if validatedClaims.RegisteredClaims.Subject != "sa-1" {
			t.Errorf("expected subject 'sa-1', got %s", validatedClaims.RegisteredClaims.Subject)
		}
	})

	// Test with second key
	t.Run("validate with key-2", func(t *testing.T) {
		claims := &Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   "sa-2",
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			},
		}
		token := jwt.NewWithClaims(&jwt.SigningMethodEd25519{}, claims)
		token.Header["kid"] = "key-2"
		
		tokenString, _ := token.SignedString(privateKey2)
		
		validatedClaims, err := validator.ValidateToken(ctx, tokenString)
		if err != nil {
			t.Fatalf("failed to validate token with key-2: %v", err)
		}
		if validatedClaims.RegisteredClaims.Subject != "sa-2" {
			t.Errorf("expected subject 'sa-2', got %s", validatedClaims.RegisteredClaims.Subject)
		}
	})

	// Test with wrong key ID
	t.Run("invalid key ID", func(t *testing.T) {
		claims := &Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   "sa-3",
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			},
		}
		token := jwt.NewWithClaims(&jwt.SigningMethodEd25519{}, claims)
		token.Header["kid"] = "non-existent-key"
		
		tokenString, _ := token.SignedString(privateKey1)
		
		_, err := validator.ValidateToken(ctx, tokenString)
		if err == nil {
			t.Error("expected error for non-existent key ID")
		}
	})
}

func TestJWKSCaching(t *testing.T) {
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	
	requestCount := 0
	jwks := JWKS{
		Keys: []JWK{
			{
				Kty: "OKP",
				Kid: "test-key",
				Use: "sig",
				Alg: "EdDSA",
				Crv: "Ed25519",
				X:   base64.RawURLEncoding.EncodeToString(publicKey),
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if r.URL.Path != "/.well-known/jwks.json" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwks)
	}))
	defer server.Close()

	validator := NewJWTValidator(server.URL)
	ctx := context.Background()

	// Create a valid token
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "test",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
		},
	}
	token := jwt.NewWithClaims(&jwt.SigningMethodEd25519{}, claims)
	token.Header["kid"] = "test-key"
	tokenString, _ := token.SignedString(privateKey)

	// First validation should fetch JWKS
	_, err := validator.ValidateToken(ctx, tokenString)
	if err != nil {
		t.Fatalf("first validation failed: %v", err)
	}
	if requestCount != 1 {
		t.Errorf("expected 1 JWKS request, got %d", requestCount)
	}

	// Second validation should use cached keys
	_, err = validator.ValidateToken(ctx, tokenString)
	if err != nil {
		t.Fatalf("second validation failed: %v", err)
	}
	if requestCount != 1 {
		t.Errorf("expected 1 JWKS request (cached), got %d", requestCount)
	}

	// Third validation should still use cached keys
	_, err = validator.ValidateToken(ctx, tokenString)
	if err != nil {
		t.Fatalf("third validation failed: %v", err)
	}
	if requestCount != 1 {
		t.Errorf("expected 1 JWKS request (still cached), got %d", requestCount)
	}
}

func TestInvalidTokens(t *testing.T) {
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	
	jwks := JWKS{
		Keys: []JWK{
			{
				Kty: "OKP",
				Kid: "test-key",
				Use: "sig",
				Alg: "EdDSA",
				Crv: "Ed25519",
				X:   base64.RawURLEncoding.EncodeToString(publicKey),
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/jwks.json" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwks)
	}))
	defer server.Close()

	validator := NewJWTValidator(server.URL)
	ctx := context.Background()

	t.Run("expired token", func(t *testing.T) {
		claims := &Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   "test",
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)), // Expired
			},
		}
		token := jwt.NewWithClaims(&jwt.SigningMethodEd25519{}, claims)
		token.Header["kid"] = "test-key"
		tokenString, _ := token.SignedString(privateKey)

		_, err := validator.ValidateToken(ctx, tokenString)
		if err == nil {
			t.Error("expected error for expired token")
		}
	})

	t.Run("missing subject", func(t *testing.T) {
		claims := &Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   "", // Missing subject
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			},
		}
		token := jwt.NewWithClaims(&jwt.SigningMethodEd25519{}, claims)
		token.Header["kid"] = "test-key"
		tokenString, _ := token.SignedString(privateKey)

		_, err := validator.ValidateToken(ctx, tokenString)
		if err == nil {
			t.Error("expected error for missing subject")
		}
	})

	t.Run("invalid signature", func(t *testing.T) {
		claims := &Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   "test",
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			},
		}
		token := jwt.NewWithClaims(&jwt.SigningMethodEd25519{}, claims)
		token.Header["kid"] = "test-key"
		
		// Sign with a different key
		_, wrongKey, _ := ed25519.GenerateKey(rand.Reader)
		tokenString, _ := token.SignedString(wrongKey)

		_, err := validator.ValidateToken(ctx, tokenString)
		if err == nil {
			t.Error("expected error for invalid signature")
		}
	})

	t.Run("missing kid header", func(t *testing.T) {
		claims := &Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   "test",
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			},
		}
		token := jwt.NewWithClaims(&jwt.SigningMethodEd25519{}, claims)
		// Don't set kid header
		tokenString, _ := token.SignedString(privateKey)

		_, err := validator.ValidateToken(ctx, tokenString)
		if err == nil {
			t.Error("expected error for missing kid header")
		}
	})
}