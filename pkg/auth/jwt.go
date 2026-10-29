package auth

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
)

const (
	// jwksRefreshInterval is how old cloud's keys get before the refresher
	// fetches them again. Keys past it stay in use; only a successful fetch
	// replaces them.
	jwksRefreshInterval = time.Hour
	// jwksRetryInterval spaces out fetches, so an unreachable cloud or a token
	// naming a key cloud never published cannot turn every request into one.
	jwksRetryInterval = time.Minute
)

// JWTValidator validates JWT tokens from miren.cloud using EdDSA signatures.
// It fetches public keys from the JWKS endpoint at {cloudURL}/.well-known/jwks.json
// and caches them for efficient validation. Only Ed25519 keys are supported.
//
// Cloud's last published keys stay trusted until cloud publishes different
// ones. An unreachable cloud is not news that a key was withdrawn, and treating
// it as such would lock every cloud user out of a cluster that cannot reach it.
type JWTValidator struct {
	cloudURL   string
	httpClient *http.Client
	logger     *slog.Logger
	cachePath  string

	refreshInterval time.Duration
	retryInterval   time.Duration

	// fetchMu admits one fetch at a time, shared by the refresher and requests
	// for a key nobody has fetched yet.
	fetchMu sync.Mutex

	mu          sync.Mutex
	keys        map[string]crypto.PublicKey // Map of kid -> public key
	fetchedAt   time.Time
	lastAttempt time.Time
	lastErr     error
	failing     bool
}

// NewJWTValidator creates a new JWT validator
func NewJWTValidator(cloudURL string, logger *slog.Logger) *JWTValidator {
	return &JWTValidator{
		cloudURL: cloudURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger:          logger.With("model", "jwt"),
		refreshInterval: jwksRefreshInterval,
		retryInterval:   jwksRetryInterval,
	}
}

// Start runs the refresher until ctx ends. It fetches cloud's keys at once
// unless fresh ones were restored, again each refreshInterval, and after a
// failure retries each retryInterval until a fetch succeeds. Requests never
// refresh known keys themselves, so nothing about an unreachable cloud costs
// them anything.
func (v *JWTValidator) Start(ctx context.Context) {
	go v.refreshLoop(ctx)
}

func (v *JWTValidator) refreshLoop(ctx context.Context) {
	timer := time.NewTimer(v.nextRefresh())
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if ctx.Err() != nil {
			return
		}
		v.fetchMu.Lock()
		_ = v.refreshLocked(ctx)
		v.fetchMu.Unlock()
		timer.Reset(v.nextRefresh())
	}
}

// nextRefresh is how long the refresher waits before its next fetch.
func (v *JWTValidator) nextRefresh() time.Duration {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.lastErr != nil {
		return v.retryInterval - time.Since(v.lastAttempt)
	}
	return v.refreshInterval - time.Since(v.fetchedAt)
}

// Claims represents the JWT claims from miren.cloud
type Claims struct {
	jwt.RegisteredClaims
	// Cloud service accounts carry a numeric database ID; user tokens omit it.
	// Older callers may carry a string. This claim is diagnostic, not RBAC scope.
	OrganizationID string   `json:"organization_id,omitempty"`
	GroupIDs       []string `json:"group_ids,omitempty"`
	// Email and Name describe the user at the time cloud minted the token.
	// Service-account tokens, and user tokens from older clouds, carry neither.
	Email string `json:"email,omitempty"`
	Name  string `json:"name,omitempty"`
}

// UnmarshalJSON preserves the diagnostic string API while accepting cloud's
// numeric organization claim without losing int64 precision.
func (c *Claims) UnmarshalJSON(data []byte) error {
	type claims Claims
	var decoded struct {
		*claims
		OrganizationID json.RawMessage `json:"organization_id"`
	}
	decoded.claims = (*claims)(c)
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	c.OrganizationID = ""
	if len(decoded.OrganizationID) == 0 || string(decoded.OrganizationID) == "null" {
		return nil
	}
	if decoded.OrganizationID[0] == '"' {
		return json.Unmarshal(decoded.OrganizationID, &c.OrganizationID)
	}
	var id json.Number
	if err := json.Unmarshal(decoded.OrganizationID, &id); err != nil {
		return err
	}
	c.OrganizationID = id.String()
	return nil
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

		publicKey, err := v.publicKey(ctx, kid)
		if err != nil {
			return nil, err
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

// savedKeys is the on-disk form of cloud's JWKS document. CloudURL ties it to
// the cloud that published it.
type savedKeys struct {
	CloudURL string          `json:"cloud_url"`
	JWKS     json.RawMessage `json:"jwks"`
}

// UseKeyCache saves cloud's keys at path after each fetch, and restores any
// saved there now, so a restart while cloud is unreachable can still validate
// tokens. Call it before the validator is shared.
func (v *JWTValidator) UseKeyCache(path string) {
	v.cachePath = path
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	var saved savedKeys
	if err == nil {
		err = json.Unmarshal(data, &saved)
	}
	if err == nil && saved.CloudURL != v.cloudURL {
		err = fmt.Errorf("keys belong to %q, not %q", saved.CloudURL, v.cloudURL)
	}
	var keys map[string]crypto.PublicKey
	if err == nil {
		keys, err = v.parseJWKS(saved.JWKS)
	}
	if err != nil {
		v.logger.Warn("ignoring saved cloud signing keys", "path", path, "error", err)
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		v.logger.Warn("ignoring saved cloud signing keys", "path", path, "error", err)
		return
	}
	v.keys = keys
	// Saved keys are as old as the file, so the refresher replaces stale ones
	// as soon as it can reach cloud.
	v.fetchedAt = info.ModTime()
	v.logger.Info("restored saved cloud signing keys", "keys", len(keys))
}

// publicKey returns cloud's key for kid. A known key is returned at once, even
// when cloud cannot be reached to confirm it; an unknown one waits for a fetch.
func (v *JWTValidator) publicKey(ctx context.Context, kid string) (crypto.PublicKey, error) {
	v.mu.Lock()
	key, known := v.keys[kid]
	v.mu.Unlock()
	if known {
		return key, nil
	}

	// Cloud may have started signing with a key it published after our last
	// fetch, so an unknown kid is worth asking about, within the retry limit.
	// The fetch serves every caller until the next one, so it must not inherit
	// this request's cancellation; the HTTP client's timeout bounds it.
	v.fetchMu.Lock()
	err := v.refreshLocked(context.WithoutCancel(ctx))
	v.fetchMu.Unlock()

	v.mu.Lock()
	defer v.mu.Unlock()
	if key, known := v.keys[kid]; known {
		return key, nil
	}
	if v.keys == nil && err != nil {
		return nil, fmt.Errorf("failed to get public keys: %w", err)
	}
	return nil, fmt.Errorf("key with ID %s not found", kid)
}

// refreshLocked fetches cloud's keys unless a fetch ran within retryInterval,
// in which case it reports that fetch's result. The caller holds fetchMu.
func (v *JWTValidator) refreshLocked(ctx context.Context) error {
	v.mu.Lock()
	if time.Since(v.lastAttempt) < v.retryInterval {
		err := v.lastErr
		v.mu.Unlock()
		return err
	}
	v.lastAttempt = time.Now()
	v.mu.Unlock()

	data, keys, err := v.fetchKeys(ctx)

	v.mu.Lock()
	v.lastErr = err
	if err != nil {
		if !v.failing {
			v.failing = true
			if v.keys != nil {
				v.logger.Warn("cannot refresh cloud signing keys; validating tokens with the last keys fetched",
					"keys", len(v.keys), "fetched_at", v.fetchedAt, "error", err)
			} else {
				v.logger.Warn("cannot fetch cloud signing keys; cloud tokens cannot be validated until cloud is reachable",
					"error", err)
			}
		}
		v.mu.Unlock()
		return err
	}
	if v.failing {
		v.failing = false
		v.logger.Info("cloud signing keys refreshed", "keys", len(keys))
	}
	v.keys = keys
	v.fetchedAt = time.Now()
	v.mu.Unlock()

	if v.cachePath != "" {
		saved, err := json.Marshal(savedKeys{CloudURL: v.cloudURL, JWKS: data})
		if err == nil {
			err = writeFileAtomic(v.cachePath, saved)
		}
		if err != nil {
			v.logger.Warn("failed to save cloud signing keys; a restart while cloud is unreachable will reject cloud tokens",
				"path", v.cachePath, "error", err)
		}
	}
	return nil
}

// fetchKeys fetches the JWKS document from miren.cloud's well-known endpoint.
func (v *JWTValidator) fetchKeys(ctx context.Context) ([]byte, map[string]crypto.PublicKey, error) {
	jwksURL := fmt.Sprintf("%s/.well-known/jwks.json", v.cloudURL)
	req, err := http.NewRequestWithContext(ctx, "GET", jwksURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create JWKS request: %w", err)
	}

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch JWKS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("failed to fetch JWKS: status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read JWKS: %w", err)
	}
	keys, err := v.parseJWKS(data)
	if err != nil {
		return nil, nil, err
	}
	return data, keys, nil
}

// parseJWKS extracts the Ed25519 keys from a JWKS document.
func (v *JWTValidator) parseJWKS(data []byte) (map[string]crypto.PublicKey, error) {
	var jwks JWKS
	if err := json.Unmarshal(data, &jwks); err != nil {
		return nil, fmt.Errorf("failed to decode JWKS: %w", err)
	}

	if len(jwks.Keys) == 0 {
		return nil, fmt.Errorf("no keys found in JWKS")
	}

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
				"cloud_url", v.cloudURL,
				"error", err,
			)
			continue
		}

		keys[jwk.Kid] = publicKey
	}

	if len(keys) == 0 {
		return nil, fmt.Errorf("no valid keys found in JWKS")
	}
	return keys, nil
}

// writeFileAtomic replaces path with data, so a crash mid-write leaves the
// previous contents rather than a truncated file.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
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
