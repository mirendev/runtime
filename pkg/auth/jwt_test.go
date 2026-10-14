package auth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	validator := NewJWTValidator(server.URL, slog.Default())

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
	if validatedClaims.Subject != "test-service-account" {
		t.Errorf("expected subject 'test-service-account', got %s", validatedClaims.Subject)
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

	validator := NewJWTValidator(server.URL, slog.Default())
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
		if validatedClaims.Subject != "sa-1" {
			t.Errorf("expected subject 'sa-1', got %s", validatedClaims.Subject)
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
		if validatedClaims.Subject != "sa-2" {
			t.Errorf("expected subject 'sa-2', got %s", validatedClaims.Subject)
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

	validator := NewJWTValidator(server.URL, slog.Default())
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

	validator := NewJWTValidator(server.URL, slog.Default())
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

// Cloud stamps user tokens with the user's email and name, and may add more
// claims later. The validator must pick up the ones it knows and ignore the
// rest, so a runtime never rejects a token for carrying a claim it predates.
func TestJWTValidatorProfileAndUnknownClaims(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(JWKS{Keys: []JWK{{
			Kty: "OKP", Kid: "test-key-1", Use: "sig", Alg: "EdDSA", Crv: "Ed25519",
			X: base64.RawURLEncoding.EncodeToString(publicKey),
		}}})
	}))
	defer server.Close()

	token := jwt.NewWithClaims(&jwt.SigningMethodEd25519{}, jwt.MapClaims{
		"sub":             "usr-ada",
		"iss":             "miren.cloud",
		"exp":             time.Now().Add(time.Hour).Unix(),
		"iat":             time.Now().Unix(),
		"group_ids":       []string{"grp-1"},
		"email":           "ada@example.com",
		"name":            "Ada Lovelace",
		"some_future_bit": map[string]any{"nested": true},
	})
	token.Header["kid"] = "test-key-1"
	tokenString, err := token.SignedString(privateKey)
	require.NoError(t, err)

	claims, err := NewJWTValidator(server.URL, slog.Default()).ValidateToken(context.Background(), tokenString)
	require.NoError(t, err)
	assert.Equal(t, "usr-ada", claims.Subject)
	assert.Equal(t, "ada@example.com", claims.Email)
	assert.Equal(t, "Ada Lovelace", claims.Name)
}

// jwksServer serves Ed25519 keys under the given kids and can be made to fail,
// counting the fetches it sees.
type jwksServer struct {
	*httptest.Server
	mu       sync.Mutex
	keys     map[string]ed25519.PrivateKey
	down     bool
	requests atomic.Int64
}

func newJWKSServer(t *testing.T, kids ...string) *jwksServer {
	s := &jwksServer{keys: map[string]ed25519.PrivateKey{}}
	for _, kid := range kids {
		s.addKey(t, kid)
	}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		s.requests.Add(1)
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.down {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		var jwks JWKS
		for kid, key := range s.keys {
			jwks.Keys = append(jwks.Keys, JWK{Kty: "OKP", Crv: "Ed25519", Kid: kid, X: base64.RawURLEncoding.EncodeToString(key.Public().(ed25519.PublicKey))})
		}
		_ = json.NewEncoder(w).Encode(jwks)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *jwksServer) addKey(t *testing.T, kid string) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	s.mu.Lock()
	s.keys[kid] = key
	s.mu.Unlock()
}

func (s *jwksServer) setDown(down bool) {
	s.mu.Lock()
	s.down = down
	s.mu.Unlock()
}

func (s *jwksServer) token(t *testing.T, kid string) string {
	s.mu.Lock()
	key := s.keys[kid]
	s.mu.Unlock()
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, jwt.MapClaims{"sub": "usr-test", "exp": time.Now().Add(time.Hour).Unix()})
	token.Header["kid"] = kid
	raw, err := token.SignedString(key)
	require.NoError(t, err)
	return raw
}

func TestJWTValidatorRefresherKeepsKeysWhileCloudIsUnreachable(t *testing.T) {
	cloud := newJWKSServer(t, "cloud-key")
	v := NewJWTValidator(cloud.URL, slog.Default())
	v.refreshInterval = 20 * time.Millisecond
	v.retryInterval = 10 * time.Millisecond
	v.Start(t.Context())
	require.Eventually(t, func() bool { return cloud.requests.Load() >= 1 }, 3*time.Second, time.Millisecond,
		"the refresher fetches at start, before any token needs a key")
	_, err := v.ValidateToken(t.Context(), cloud.token(t, "cloud-key"))
	require.NoError(t, err)

	cloud.setDown(true)
	failedFrom := cloud.requests.Load()
	require.Eventually(t, func() bool { return cloud.requests.Load() >= failedFrom+3 }, 3*time.Second, time.Millisecond,
		"the refresher keeps retrying on its own")
	for range 100 {
		_, err = v.ValidateToken(t.Context(), cloud.token(t, "cloud-key"))
		require.NoError(t, err, "known keys stay trusted while cloud is down")
	}

	cloud.addKey(t, "rotated-key")
	cloud.setDown(false)
	require.Eventually(t, func() bool {
		v.mu.Lock()
		defer v.mu.Unlock()
		_, ok := v.keys["rotated-key"]
		return ok && !v.failing
	}, 3*time.Second, time.Millisecond, "the refresher recovers once cloud is back")
}

func TestJWTValidatorRequestsDoNotRefreshKnownKeys(t *testing.T) {
	cloud := newJWKSServer(t, "cloud-key")
	v := NewJWTValidator(cloud.URL, slog.Default())
	_, err := v.ValidateToken(t.Context(), cloud.token(t, "cloud-key"))
	require.NoError(t, err)
	v.mu.Lock()
	v.fetchedAt = time.Now().Add(-2 * jwksRefreshInterval)
	v.lastAttempt = time.Now().Add(-2 * jwksRetryInterval)
	v.mu.Unlock()
	for range 100 {
		_, err = v.ValidateToken(t.Context(), cloud.token(t, "cloud-key"))
		require.NoError(t, err)
	}
	require.Equal(t, int64(1), cloud.requests.Load(), "refreshing known keys is the refresher's job alone")
}

func TestJWTValidatorRefetchesForUnknownKey(t *testing.T) {
	cloud := newJWKSServer(t, "old-key")
	v := NewJWTValidator(cloud.URL, slog.Default())
	_, err := v.ValidateToken(t.Context(), cloud.token(t, "old-key"))
	require.NoError(t, err)

	cloud.addKey(t, "new-key")
	_, err = v.ValidateToken(t.Context(), cloud.token(t, "new-key"))
	require.ErrorContains(t, err, "key with ID new-key not found", "a fetch within the retry limit is not repeated")
	require.Equal(t, int64(1), cloud.requests.Load())

	v.mu.Lock()
	v.lastAttempt = time.Now().Add(-2 * jwksRetryInterval)
	v.mu.Unlock()
	_, err = v.ValidateToken(t.Context(), cloud.token(t, "new-key"))
	require.NoError(t, err, "a key cloud published since the last fetch is picked up before the refresh is due")
	_, err = v.ValidateToken(t.Context(), cloud.token(t, "old-key"))
	require.NoError(t, err)
}

func TestJWTValidatorWithoutKeysFailsFastWhileCloudIsDown(t *testing.T) {
	cloud := newJWKSServer(t, "cloud-key")
	cloud.setDown(true)
	v := NewJWTValidator(cloud.URL, slog.Default())
	for range 5 {
		_, err := v.ValidateToken(t.Context(), cloud.token(t, "cloud-key"))
		require.ErrorContains(t, err, "failed to get public keys")
	}
	require.Equal(t, int64(1), cloud.requests.Load())
}

func TestJWTValidatorKeyCacheSurvivesRestart(t *testing.T) {
	cloud := newJWKSServer(t, "cloud-key")
	path := filepath.Join(t.TempDir(), "cloud-jwks.json")
	v := NewJWTValidator(cloud.URL, slog.Default())
	v.UseKeyCache(path)
	_, err := v.ValidateToken(t.Context(), cloud.token(t, "cloud-key"))
	require.NoError(t, err)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())

	cloud.setDown(true)
	restarted := NewJWTValidator(cloud.URL, slog.Default())
	restarted.UseKeyCache(path)
	_, err = restarted.ValidateToken(t.Context(), cloud.token(t, "cloud-key"))
	require.NoError(t, err)

	other := NewJWTValidator("https://other-cloud.invalid", slog.Default())
	other.UseKeyCache(path)
	other.mu.Lock()
	require.Nil(t, other.keys, "keys saved from one cloud are not trusted for another")
	other.mu.Unlock()

	require.NoError(t, os.WriteFile(path, []byte("{"), 0600))
	corrupt := NewJWTValidator(cloud.URL, slog.Default())
	corrupt.UseKeyCache(path)
	_, err = corrupt.ValidateToken(t.Context(), cloud.token(t, "cloud-key"))
	require.ErrorContains(t, err, "failed to get public keys")
}

func TestJWTValidatorCancelledRequestDoesNotBlockKeyFetch(t *testing.T) {
	cloud := newJWKSServer(t, "cloud-key")
	v := NewJWTValidator(cloud.URL, slog.Default())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, _ = v.ValidateToken(ctx, cloud.token(t, "cloud-key"))
	_, err := v.ValidateToken(t.Context(), cloud.token(t, "cloud-key"))
	require.NoError(t, err, "one caller giving up must not fail the fetch every caller shares")
}
