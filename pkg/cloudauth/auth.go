package cloudauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
)

// AuthClient handles service account authentication with miren.cloud
type AuthClient struct {
	serverURL  string
	keyPair    *KeyPair
	httpClient *http.Client

	mu           sync.RWMutex
	currentToken string
	tokenExpiry  time.Time
}

// NewAuthClient creates a new authentication client
func NewAuthClient(serverURL string, keyPair *KeyPair) (*AuthClient, error) {
	if keyPair == nil {
		return nil, fmt.Errorf("keyPair cannot be nil")
	}
	return &AuthClient{
		serverURL:  serverURL,
		keyPair:    keyPair,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// BeginAuthRequest is the request to begin authentication
type BeginAuthRequest struct {
	Fingerprint string `json:"fingerprint"`
}

// BeginAuthResponse is the response from begin authentication
type BeginAuthResponse struct {
	Envelope  string `json:"envelope"`
	Challenge string `json:"challenge"`
}

// CompleteAuthRequest is the request to complete authentication
type CompleteAuthRequest struct {
	Envelope  string `json:"envelope"`
	Signature string `json:"signature"`
}

// CompleteAuthResponse is the response from complete authentication
type CompleteAuthResponse struct {
	ServiceAccount struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"service_account"`
	Token     string `json:"token"`
	ExpiresIn int    `json:"expires_in,omitempty"` // Optional: seconds until expiry
}

// Authenticate performs the public key authentication flow and returns a JWT
func (a *AuthClient) Authenticate(ctx context.Context) (string, error) {
	// Check if we have a valid cached token
	a.mu.RLock()
	if a.currentToken != "" && time.Now().Before(a.tokenExpiry) {
		token := a.currentToken
		a.mu.RUnlock()
		return token, nil
	}
	a.mu.RUnlock()

	// Step 1: Begin authentication
	beginURL := fmt.Sprintf("%s/auth/service-account/begin", a.serverURL)
	beginReq := BeginAuthRequest{
		Fingerprint: a.keyPair.Fingerprint(),
	}

	beginData, err := json.Marshal(beginReq)
	if err != nil {
		return "", fmt.Errorf("failed to marshal begin request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", beginURL, bytes.NewBuffer(beginData))
	if err != nil {
		return "", fmt.Errorf("failed to create begin request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send begin request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp map[string]string
		err := json.NewDecoder(resp.Body).Decode(&errResp)
		if err != nil {
			return "", fmt.Errorf("failed to decode error response: %w", err)
		}

		errmsg := errResp["error"]
		if errmsg == "" {
			errmsg = "unknown error"
		}
		return "", fmt.Errorf("begin authentication failed: %s", errmsg)
	}

	var beginResp BeginAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&beginResp); err != nil {
		return "", fmt.Errorf("failed to decode begin response: %w", err)
	}

	// Step 2: Sign the challenge
	signature, err := a.keyPair.Sign([]byte(beginResp.Challenge))
	if err != nil {
		return "", fmt.Errorf("failed to sign challenge: %w", err)
	}

	// Step 3: Complete authentication
	completeURL := fmt.Sprintf("%s/auth/service-account/complete", a.serverURL)
	completeReq := CompleteAuthRequest{
		Envelope:  beginResp.Envelope,
		Signature: signature,
	}

	completeData, err := json.Marshal(completeReq)
	if err != nil {
		return "", fmt.Errorf("failed to marshal complete request: %w", err)
	}

	req, err = http.NewRequestWithContext(ctx, "POST", completeURL, bytes.NewBuffer(completeData))
	if err != nil {
		return "", fmt.Errorf("failed to create complete request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err = a.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send complete request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp map[string]string
		json.NewDecoder(resp.Body).Decode(&errResp)
		return "", fmt.Errorf("complete authentication failed: %s", errResp["error"])
	}

	var completeResp CompleteAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&completeResp); err != nil {
		return "", fmt.Errorf("failed to decode complete response: %w", err)
	}

	// Determine token expiry
	var expiry time.Time

	// First check if server provided expires_in
	if completeResp.ExpiresIn > 0 {
		expiry = time.Now().Add(time.Duration(completeResp.ExpiresIn) * time.Second)
	} else {
		// Otherwise, parse the token to get the actual expiry time
		parser := jwt.NewParser(jwt.WithoutClaimsValidation())
		token, _, err := parser.ParseUnverified(completeResp.Token, jwt.MapClaims{})
		if err != nil {
			return "", fmt.Errorf("failed to parse token for expiry: %w", err)
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			return "", fmt.Errorf("failed to extract claims from token")
		}

		// Extract expiry from the exp claim
		if exp, ok := claims["exp"]; ok {
			switch v := exp.(type) {
			case float64:
				expiry = time.Unix(int64(v), 0)
			case json.Number:
				if i, err := v.Int64(); err == nil {
					expiry = time.Unix(i, 0)
				}
			}
		}
	}

	// If we couldn't determine expiry, fall back to 1 hour
	if expiry.IsZero() {
		expiry = time.Now().Add(1 * time.Hour)
	}

	// Cache the token with a 5-minute buffer before expiry
	a.mu.Lock()
	a.currentToken = completeResp.Token
	a.tokenExpiry = expiry.Add(-5 * time.Minute)
	a.mu.Unlock()

	return completeResp.Token, nil
}

// GetToken returns a valid JWT, refreshing if necessary
func (a *AuthClient) GetToken(ctx context.Context) (string, error) {
	return a.Authenticate(ctx)
}

// InvalidateToken clears the cached token
func (a *AuthClient) InvalidateToken() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.currentToken = ""
	a.tokenExpiry = time.Time{}
}
