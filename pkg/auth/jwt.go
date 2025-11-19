package auth

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
)

// JWTValidator validates JWT tokens from miren.cloud using EdDSA signatures.
// It fetches public keys from the JWKS endpoint at {cloudURL}/.well-known/jwks.json
// and caches them for efficient validation. Only Ed25519 keys are supported.
type JWTValidator struct {
	cloudURL   string
	httpClient *http.Client
	logger     *slog.Logger

	mu        sync.RWMutex
	keys      map[string]crypto.PublicKey // Map of kid -> public key
	keyExpiry time.Time
}

// NewJWTValidator creates a new JWT validator
func NewJWTValidator(cloudURL string, logger *slog.Logger) *JWTValidator {
	return &JWTValidator{
		cloudURL: cloudURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: logger.With("model", "jwt"),
	}
}

// Claims represents the JWT claims from miren.cloud
type Claims struct {
	jwt.RegisteredClaims
	OrganizationID string   `json:"organization_id,omitempty"`
	GroupIDs       []string `json:"group_ids,omitempty"`
}

// ValidateToken validates a JWT token and returns the claims
func (v *JWTValidator) ValidateToken(ctx context.Context, tokenString string) (*Claims, error) {
	// Parse and validate the token
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (any, error) {
		// Get the key ID from the token header
		kid, ok := token.Header["kid"].(string)
		if !ok {
			return nil, fmt.Errorf("key ID not found in token header")
		}

		// Get the public keys
		keys, err := v.getPublicKeys(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get public keys: %w", err)
		}

		// Find the key by ID
		publicKey, ok := keys[kid]
		if !ok {
			return nil, fmt.Errorf("key with ID %s not found", kid)
		}

		// Verify the signing method matches the key type (only Ed25519 supported)
		key, ok := publicKey.(ed25519.PublicKey)
		if !ok {
			return nil, fmt.Errorf("unsupported key type: %T (only Ed25519 supported)", publicKey)
		}

		if _, ok := token.Method.(*jwt.SigningMethodEd25519); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v (expected EdDSA)", token.Header["alg"])
		}

		return key, nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	if !token.Valid {
		return nil, fmt.Errorf("token is invalid")
	}

	claims, ok := token.Claims.(*Claims)
	if !ok {
		return nil, fmt.Errorf("failed to extract claims")
	}

	// Additional validation
	if claims.Subject == "" {
		return nil, fmt.Errorf("subject missing in token")
	}

	return claims, nil
}

// getPublicKeys fetches the public keys from miren.cloud JWKS, with caching
func (v *JWTValidator) getPublicKeys(ctx context.Context) (map[string]crypto.PublicKey, error) {
	v.mu.RLock()
	if v.keys != nil && time.Now().Before(v.keyExpiry) {
		keys := v.keys
		v.mu.RUnlock()
		return keys, nil
	}
	v.mu.RUnlock()

	// Need to fetch new keys
	v.mu.Lock()
	defer v.mu.Unlock()

	// Double-check after acquiring write lock
	if v.keys != nil && time.Now().Before(v.keyExpiry) {
		return v.keys, nil
	}

	// Fetch JWKS from well-known endpoint
	jwksURL := fmt.Sprintf("%s/.well-known/jwks.json", v.cloudURL)
	req, err := http.NewRequestWithContext(ctx, "GET", jwksURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create JWKS request: %w", err)
	}

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch JWKS: status %d", resp.StatusCode)
	}

	// Parse JWKS response
	var jwks JWKS
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return nil, fmt.Errorf("failed to decode JWKS: %w", err)
	}

	if len(jwks.Keys) == 0 {
		return nil, fmt.Errorf("no keys found in JWKS")
	}

	// Parse all keys
	keys := make(map[string]crypto.PublicKey)
	for _, jwk := range jwks.Keys {
		if jwk.Kid == "" {
			continue // Skip keys without ID
		}

		// Only support Ed25519 keys
		if jwk.Kty != "OKP" || jwk.Crv != "Ed25519" {
			continue // Skip non-Ed25519 keys
		}

		publicKey, err := parseEd25519Key(jwk)
		if err != nil {
			// Log error but continue with other keys
			v.logger.Error("failed to parse Ed25519 key",
				"kid", jwk.Kid,
				"url", jwksURL,
				"error", err,
			)
			continue
		}

		keys[jwk.Kid] = publicKey
	}

	if len(keys) == 0 {
		return nil, fmt.Errorf("no valid keys found in JWKS")
	}

	// Cache for 1 hour
	v.keys = keys
	v.keyExpiry = time.Now().Add(1 * time.Hour)

	return keys, nil
}

// JWKS represents a JSON Web Key Set
type JWKS struct {
	Keys []JWK `json:"keys"`
}

// JWK represents a JSON Web Key
type JWK struct {
	Kty string `json:"kty"` // Key type (OKP for Ed25519)
	Kid string `json:"kid"` // Key ID
	Use string `json:"use"` // Key use (sig)
	Alg string `json:"alg"` // Algorithm (EdDSA)
	Crv string `json:"crv"` // Curve (Ed25519)
	X   string `json:"x"`   // X coordinate (public key for Ed25519)
}

// parseEd25519Key parses an Ed25519 public key from a JWK
func parseEd25519Key(jwk JWK) (ed25519.PublicKey, error) {
	if jwk.Kty != "OKP" {
		return nil, fmt.Errorf("invalid key type for Ed25519: %s", jwk.Kty)
	}
	if jwk.Crv != "Ed25519" {
		return nil, fmt.Errorf("invalid curve for Ed25519: %s", jwk.Crv)
	}
	if jwk.X == "" {
		return nil, fmt.Errorf("missing x coordinate for Ed25519 key")
	}

	// Decode the base64url-encoded public key
	publicKeyBytes, err := base64.RawURLEncoding.DecodeString(jwk.X)
	if err != nil {
		return nil, fmt.Errorf("failed to decode Ed25519 public key: %w", err)
	}

	if len(publicKeyBytes) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid Ed25519 public key size: got %d, want %d", len(publicKeyBytes), ed25519.PublicKeySize)
	}

	return ed25519.PublicKey(publicKeyBytes), nil
}

// TokenCache caches validated tokens to reduce validation overhead
type TokenCache struct {
	mu    sync.RWMutex
	cache map[string]*cacheEntry
}

type cacheEntry struct {
	claims  *Claims
	expires time.Time
}

// NewTokenCache creates a new token cache
func NewTokenCache(ctx context.Context) *TokenCache {
	tc := &TokenCache{
		cache: make(map[string]*cacheEntry),
	}
	// Start cleanup goroutine
	go tc.cleanup(ctx)
	return tc
}

// Get retrieves claims from cache if valid
func (tc *TokenCache) Get(token string) (*Claims, bool) {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	entry, ok := tc.cache[token]
	if !ok {
		return nil, false
	}

	if time.Now().After(entry.expires) {
		return nil, false
	}

	return entry.claims, true
}

// Set stores claims in cache
func (tc *TokenCache) Set(token string, claims *Claims) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	// Cache until token expires
	var expiry time.Time
	if claims.ExpiresAt != nil {
		expiry = claims.ExpiresAt.Time
	}
	if expiry.IsZero() {
		expiry = time.Now().Add(1 * time.Hour)
	}

	tc.cache[token] = &cacheEntry{
		claims:  claims,
		expires: expiry,
	}
}

// cleanup periodically removes expired entries
func (tc *TokenCache) cleanup(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tc.mu.Lock()
			now := time.Now()
			for token, entry := range tc.cache {
				if now.After(entry.expires) {
					delete(tc.cache, token)
				}
			}
			tc.mu.Unlock()
		}
	}
}
