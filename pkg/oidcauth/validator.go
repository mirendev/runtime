package oidcauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"
)

// discoveryData contains OIDC provider configuration from .well-known/openid-configuration.
type discoveryData struct {
	Issuer  string `json:"issuer"`
	JwksURI string `json:"jwks_uri"`
}

type issuerCache struct {
	discovery     *discoveryData
	discoveryTime time.Time
	jwks          *jose.JSONWebKeySet
	jwksTime      time.Time
}

// Validator validates OIDC tokens by performing discovery and JWKS-based verification.
type Validator struct {
	mu     sync.RWMutex
	cache  map[string]*issuerCache
	client *http.Client
}

// NewValidator creates a new OIDC token validator.
func NewValidator() *Validator {
	return &Validator{
		cache:  make(map[string]*issuerCache),
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

const (
	discoveryCacheTTL = time.Hour
	jwksCacheTTL      = time.Hour
)

// ValidateToken validates an OIDC JWT token against the expected issuer and audience.
// It performs OIDC discovery and JWKS verification automatically.
func (v *Validator) ValidateToken(ctx context.Context, tokenString, expectedIssuer, expectedAudience string) (*Claims, error) {
	discovery, err := v.getDiscovery(ctx, expectedIssuer)
	if err != nil {
		return nil, fmt.Errorf("OIDC discovery failed for %s: %w", expectedIssuer, err)
	}

	if discovery.Issuer != expectedIssuer {
		return nil, fmt.Errorf("issuer mismatch in discovery: got %s, want %s", discovery.Issuer, expectedIssuer)
	}

	// First attempt: use cached JWKS
	claims, err := v.validateWithJWKS(ctx, tokenString, expectedIssuer, expectedAudience, false)
	if err != nil && (errors.Is(err, jwt.ErrTokenUnverifiable) || errors.Is(err, jwt.ErrTokenSignatureInvalid)) {
		// Key not found or stale in cached JWKS — refetch once and retry (handles key rotation)
		claims, err = v.validateWithJWKS(ctx, tokenString, expectedIssuer, expectedAudience, true)
	}
	if err != nil {
		return nil, err
	}

	return claims, nil
}

func (v *Validator) validateWithJWKS(ctx context.Context, tokenString, expectedIssuer, expectedAudience string, forceRefresh bool) (*Claims, error) {
	keyFunc, err := v.getJWKSKeyFunc(ctx, expectedIssuer, forceRefresh)
	if err != nil {
		return nil, fmt.Errorf("failed to get JWKS: %w", err)
	}

	parser := jwt.NewParser(
		jwt.WithIssuer(expectedIssuer),
		jwt.WithExpirationRequired(),
		jwt.WithValidMethods([]string{
			"RS256", "RS384", "RS512",
			"ES256", "ES384", "ES512",
			"PS256", "PS384", "PS512",
			"EdDSA",
		}),
	)

	token, err := parser.Parse(tokenString, keyFunc)
	if err != nil {
		return nil, fmt.Errorf("token validation failed: %w", err)
	}

	if !token.Valid {
		return nil, fmt.Errorf("token is invalid")
	}

	mapClaims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("failed to extract claims")
	}

	// Validate audience
	if !audienceContains(mapClaims, expectedAudience) {
		return nil, fmt.Errorf("token audience does not include %s", expectedAudience)
	}

	return mapClaimsToClaims(mapClaims), nil
}

func audienceContains(claims jwt.MapClaims, expected string) bool {
	aud, ok := claims["aud"]
	if !ok {
		return false
	}
	switch v := aud.(type) {
	case string:
		return v == expected
	case []any:
		for _, a := range v {
			if s, ok := a.(string); ok && s == expected {
				return true
			}
		}
	}
	return false
}

func mapClaimsToClaims(mc jwt.MapClaims) *Claims {
	c := &Claims{
		Extra: make(map[string]any),
	}

	if iss, ok := mc["iss"].(string); ok {
		c.Issuer = iss
	}
	if sub, ok := mc["sub"].(string); ok {
		c.Subject = sub
	}

	// Parse audience
	switch v := mc["aud"].(type) {
	case string:
		c.Audience = []string{v}
	case []any:
		for _, a := range v {
			if s, ok := a.(string); ok {
				c.Audience = append(c.Audience, s)
			}
		}
	}

	if exp, ok := mc["exp"].(float64); ok {
		c.Expiry = time.Unix(int64(exp), 0)
	}

	// Copy all extra claims
	for k, val := range mc {
		switch k {
		case "iss", "sub", "aud", "exp", "nbf", "iat", "jti":
			continue
		}
		c.Extra[k] = val
	}

	return c
}

func (v *Validator) getDiscovery(ctx context.Context, issuer string) (*discoveryData, error) {
	v.mu.RLock()
	if c, ok := v.cache[issuer]; ok && c.discovery != nil && time.Since(c.discoveryTime) < discoveryCacheTTL {
		d := c.discovery
		v.mu.RUnlock()
		return d, nil
	}
	v.mu.RUnlock()

	// Fetch without holding the lock
	data, err := v.fetchDiscovery(ctx, issuer)
	if err != nil {
		return nil, err
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	// Double-check after acquiring write lock
	if c, ok := v.cache[issuer]; ok && c.discovery != nil && time.Since(c.discoveryTime) < discoveryCacheTTL {
		return c.discovery, nil
	}

	if v.cache[issuer] == nil {
		v.cache[issuer] = &issuerCache{}
	}
	v.cache[issuer].discovery = data
	v.cache[issuer].discoveryTime = time.Now()

	return data, nil
}

func (v *Validator) fetchDiscovery(ctx context.Context, issuer string) (*discoveryData, error) {
	discoveryURL := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, "GET", discoveryURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery request: %w", err)
	}

	resp, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch discovery document: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discovery endpoint returned status %d", resp.StatusCode)
	}

	var data discoveryData
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to parse discovery document: %w", err)
	}

	if data.JwksURI == "" {
		return nil, fmt.Errorf("discovery document missing jwks_uri")
	}

	return &data, nil
}

func (v *Validator) getJWKSKeyFunc(ctx context.Context, issuer string, forceRefresh bool) (jwt.Keyfunc, error) {
	v.mu.RLock()
	c := v.cache[issuer]
	if c != nil && c.jwks != nil && !forceRefresh && time.Since(c.jwksTime) < jwksCacheTTL {
		jwks := c.jwks
		v.mu.RUnlock()
		return makeKeyFunc(jwks), nil
	}
	var discovery *discoveryData
	if c != nil {
		discovery = c.discovery
	}
	v.mu.RUnlock()

	if discovery == nil {
		return nil, fmt.Errorf("no discovery data for issuer %s", issuer)
	}

	// Fetch without holding the lock
	jwks, err := v.fetchJWKS(ctx, discovery.JwksURI)
	if err != nil {
		return nil, err
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	// Double-check
	c = v.cache[issuer]
	if c != nil && c.jwks != nil && !forceRefresh && time.Since(c.jwksTime) < jwksCacheTTL {
		return makeKeyFunc(c.jwks), nil
	}

	if c == nil {
		c = &issuerCache{discovery: discovery, discoveryTime: time.Now()}
		v.cache[issuer] = c
	}
	c.jwks = jwks
	c.jwksTime = time.Now()

	return makeKeyFunc(jwks), nil
}

func (v *Validator) fetchJWKS(ctx context.Context, jwksURI string) (*jose.JSONWebKeySet, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", jwksURI, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create JWKS request: %w", err)
	}

	resp, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWKS endpoint returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return nil, fmt.Errorf("failed to read JWKS response: %w", err)
	}

	var jwks jose.JSONWebKeySet
	if err := json.Unmarshal(body, &jwks); err != nil {
		return nil, fmt.Errorf("failed to parse JWKS: %w", err)
	}

	return &jwks, nil
}

func makeKeyFunc(jwks *jose.JSONWebKeySet) jwt.Keyfunc {
	return func(token *jwt.Token) (any, error) {
		kid, ok := token.Header["kid"].(string)
		if ok {
			keys := jwks.Key(kid)
			if len(keys) == 0 {
				return nil, fmt.Errorf("key with kid %s not found in JWKS", kid)
			}
			return keys[0].Key, nil
		}

		// No kid header — fall back to single key or algorithm match
		if len(jwks.Keys) == 1 {
			return jwks.Keys[0].Key, nil
		}
		alg, _ := token.Header["alg"].(string)
		for _, k := range jwks.Keys {
			if k.Algorithm == alg {
				return k.Key, nil
			}
		}
		return nil, fmt.Errorf("no suitable key found in JWKS (no kid header, %d keys)", len(jwks.Keys))
	}
}
