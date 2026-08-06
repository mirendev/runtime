package oidc

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/oauth2"
)

// Client handles OIDC authentication flows with OAuth2 providers.
// It implements the authorization code flow with PKCE (RFC 7636).
type Client struct {
	providerURL  string
	clientID     string
	clientSecret string
	scopes       []string
	redirectURL  string
	logger       *slog.Logger

	mu            sync.RWMutex
	oauth2Config  *oauth2.Config
	discoveryData *discoveryData
	discoveryTime time.Time
}

// discoveryData contains OIDC provider configuration from .well-known/openid-configuration
type discoveryData struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	UserinfoEndpoint      string   `json:"userinfo_endpoint"`
	JwksURI               string   `json:"jwks_uri"`
	ScopesSupported       []string `json:"scopes_supported"`
}

// NewClient creates a new OIDC client
func NewClient(providerURL, clientID, clientSecret, redirectURL string, scopes []string, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}

	return &Client{
		providerURL:  strings.TrimSuffix(providerURL, "/"),
		clientID:     clientID,
		clientSecret: clientSecret,
		scopes:       scopes,
		redirectURL:  redirectURL,
		logger:       logger.With("module", "oidc"),
	}
}

// AuthorizationURL generates the URL to redirect users to for authentication.
// It includes PKCE challenge for added security and a resource indicator (RFC 8707).
func (c *Client) AuthorizationURL(state, pkceVerifier, resource string) (string, error) {
	config, err := c.getOAuth2Config(context.Background())
	if err != nil {
		return "", fmt.Errorf("failed to get OAuth2 config: %w", err)
	}

	// Generate PKCE challenge from verifier
	pkceChallenge := generatePKCEChallenge(pkceVerifier)

	// Build authorization URL with PKCE and resource indicator
	opts := []oauth2.AuthCodeOption{
		oauth2.SetAuthURLParam("code_challenge", pkceChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	}
	if resource != "" {
		opts = append(opts, oauth2.SetAuthURLParam("resource", resource))
	}

	url := config.AuthCodeURL(state, opts...)

	return url, nil
}

// ExchangeCode exchanges an authorization code for tokens using PKCE
func (c *Client) ExchangeCode(ctx context.Context, code, pkceVerifier string) (*oauth2.Token, error) {
	config, err := c.getOAuth2Config(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get OAuth2 config: %w", err)
	}

	// Exchange code for tokens with PKCE verifier
	token, err := config.Exchange(ctx, code,
		oauth2.SetAuthURLParam("code_verifier", pkceVerifier),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange code: %w", err)
	}

	return token, nil
}

// ParseIDToken validates an ID token's signature via the provider's JWKS
// and checks standard claims (issuer, audience).
func (c *Client) ParseIDToken(ctx context.Context, idToken string) (map[string]any, error) {
	discovery, err := c.getDiscovery(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get discovery data: %w", err)
	}

	keyFunc, err := c.getJWKSKeyFunc(ctx, discovery.JwksURI)
	if err != nil {
		return nil, fmt.Errorf("failed to get JWKS key function: %w", err)
	}

	token, err := jwt.Parse(idToken, keyFunc)
	if err != nil {
		return nil, fmt.Errorf("failed to verify ID token: %w", err)
	}

	if !token.Valid {
		return nil, fmt.Errorf("ID token is invalid")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("failed to extract claims")
	}

	if iss, ok := claims["iss"].(string); !ok || iss != discovery.Issuer {
		return nil, fmt.Errorf("issuer mismatch: got %v, want %s", claims["iss"], discovery.Issuer)
	}

	if err := c.validateAudience(claims); err != nil {
		return nil, err
	}

	return claims, nil
}

// validateAudience checks that the token audience includes our client ID
func (c *Client) validateAudience(claims jwt.MapClaims) error {
	aud, ok := claims["aud"]
	if !ok {
		return fmt.Errorf("audience (aud) claim missing")
	}

	// Audience can be a string or array of strings
	switch v := aud.(type) {
	case string:
		if v == c.clientID {
			return nil
		}
	case []any:
		for _, a := range v {
			if s, ok := a.(string); ok && s == c.clientID {
				return nil
			}
		}
	}

	return fmt.Errorf("audience does not include client ID")
}

// getOAuth2Config lazily initializes and caches the OAuth2 config
func (c *Client) getOAuth2Config(ctx context.Context) (*oauth2.Config, error) {
	c.mu.RLock()
	if c.oauth2Config != nil {
		config := c.oauth2Config
		c.mu.RUnlock()
		return config, nil
	}
	c.mu.RUnlock()

	// Need to discover endpoints
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if c.oauth2Config != nil {
		return c.oauth2Config, nil
	}

	discovery, err := c.getDiscoveryLocked(ctx)
	if err != nil {
		return nil, err
	}

	c.oauth2Config = &oauth2.Config{
		ClientID:     c.clientID,
		ClientSecret: c.clientSecret,
		RedirectURL:  c.redirectURL,
		Endpoint: oauth2.Endpoint{
			AuthURL:  discovery.AuthorizationEndpoint,
			TokenURL: discovery.TokenEndpoint,
		},
		Scopes: c.scopes,
	}

	return c.oauth2Config, nil
}

// getDiscovery fetches OIDC provider configuration with caching
func (c *Client) getDiscovery(ctx context.Context) (*discoveryData, error) {
	c.mu.RLock()
	if c.discoveryData != nil && time.Since(c.discoveryTime) < time.Hour {
		data := c.discoveryData
		c.mu.RUnlock()
		return data, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	return c.getDiscoveryLocked(ctx)
}

// getDiscoveryLocked fetches OIDC provider configuration (caller must hold write lock)
func (c *Client) getDiscoveryLocked(ctx context.Context) (*discoveryData, error) {
	// Double-check cache after acquiring lock
	if c.discoveryData != nil && time.Since(c.discoveryTime) < time.Hour {
		return c.discoveryData, nil
	}

	// Fetch from well-known endpoint
	discoveryURL := fmt.Sprintf("%s/.well-known/openid-configuration", c.providerURL)

	req, err := http.NewRequestWithContext(ctx, "GET", discoveryURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery request: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch discovery document: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discovery endpoint returned status %d", resp.StatusCode)
	}

	var data discoveryData
	if err := parseJSON(resp.Body, &data); err != nil {
		return nil, fmt.Errorf("failed to parse discovery document: %w", err)
	}

	// Validate required fields
	if data.AuthorizationEndpoint == "" || data.TokenEndpoint == "" {
		return nil, fmt.Errorf("discovery document missing required endpoints")
	}

	c.discoveryData = &data
	c.discoveryTime = time.Now()

	return &data, nil
}

// getJWKSKeyFunc returns a key function for JWT validation using JWKS.
// It fetches the JWKS document and parses keys into proper crypto types
// using go-jose so that jwt-go can verify signatures.
func (c *Client) getJWKSKeyFunc(ctx context.Context, jwksURI string) (jwt.Keyfunc, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", jwksURI, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create JWKS request: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWKS endpoint returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read JWKS response: %w", err)
	}

	var jwks jose.JSONWebKeySet
	if err := json.Unmarshal(body, &jwks); err != nil {
		return nil, fmt.Errorf("failed to parse JWKS: %w", err)
	}

	return func(token *jwt.Token) (any, error) {
		kid, ok := token.Header["kid"].(string)
		if !ok {
			return nil, fmt.Errorf("token missing kid header")
		}

		keys := jwks.Key(kid)
		if len(keys) == 0 {
			return nil, fmt.Errorf("key with kid %s not found in JWKS", kid)
		}

		return keys[0].Key, nil
	}, nil
}

// generatePKCEChallenge creates a SHA256 hash of the verifier for PKCE
func generatePKCEChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

// parseJSON is a helper to parse JSON from an io.Reader
func parseJSON(r io.Reader, v any) error {
	return json.NewDecoder(r).Decode(v)
}
