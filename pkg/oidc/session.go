package oidc

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"golang.org/x/crypto/chacha20poly1305"
)

const (
	sessionCookieName = "miren_oidc_session"
	stateCookieName   = "miren_oidc_state"

	// Default session lifetime
	defaultSessionDuration = 24 * time.Hour

	// PKCE challenge length (43-128 characters per RFC 7636)
	pkceVerifierLength = 64
)

// SessionManager handles OIDC session lifecycle using encrypted cookies.
// Cookie values are encrypted and authenticated with XChaCha20-Poly1305.
type SessionManager struct {
	cookieSecure    bool
	cookieDomain    string
	sessionDuration time.Duration
	key             []byte
}

// SessionData contains the authenticated session information
type SessionData struct {
	// IDToken is the raw ID token JWT from the OIDC provider
	IDToken string `json:"id_token"`

	// AccessToken is the OAuth2 access token (optional)
	AccessToken string `json:"access_token,omitempty"`

	// RefreshToken is the OAuth2 refresh token (optional)
	RefreshToken string `json:"refresh_token,omitempty"`

	// Claims are the parsed JWT claims
	Claims map[string]any `json:"claims"`

	// ExpiresAt is when this session expires
	ExpiresAt time.Time `json:"expires_at"`
}

// StateData contains OIDC flow state for CSRF protection
type StateData struct {
	// State is the random state parameter
	State string `json:"state"`

	// PKCEVerifier is the PKCE code verifier (RFC 7636)
	PKCEVerifier string `json:"pkce_verifier"`

	// ReturnPath is where to redirect after auth
	ReturnPath string `json:"return_path"`

	// ExpiresAt is when this state expires (short-lived)
	ExpiresAt time.Time `json:"expires_at"`
}

// SetSecure updates whether cookies should be marked Secure.
func (sm *SessionManager) SetSecure(secure bool) {
	sm.cookieSecure = secure
}

// NewSessionManager creates a new session manager.
// If key is nil, a random 32-byte key is generated. This means sessions
// won't survive server restarts; pass a persistent key for durable sessions.
func NewSessionManager(cookieSecure bool, cookieDomain string, key []byte) *SessionManager {
	if key == nil {
		key = make([]byte, chacha20poly1305.KeySize)
		rand.Read(key)
	}
	return &SessionManager{
		cookieSecure:    cookieSecure,
		cookieDomain:    cookieDomain,
		sessionDuration: defaultSessionDuration,
		key:             key,
	}
}

// sealCookie encrypts and authenticates data with XChaCha20-Poly1305,
// returning base64(nonce || ciphertext).
func (sm *SessionManager) sealCookie(plaintext []byte) (string, error) {
	aead, err := chacha20poly1305.NewX(sm.key)
	if err != nil {
		return "", fmt.Errorf("creating AEAD: %w", err)
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generating nonce: %w", err)
	}

	// nonce is prepended to ciphertext
	sealed := aead.Seal(nonce, nonce, plaintext, nil)
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

// openCookie decodes, decrypts, and verifies a cookie produced by sealCookie.
func (sm *SessionManager) openCookie(value string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("failed to decode cookie: %w", err)
	}

	aead, err := chacha20poly1305.NewX(sm.key)
	if err != nil {
		return nil, fmt.Errorf("creating AEAD: %w", err)
	}

	nonceSize := aead.NonceSize()
	if len(raw) < nonceSize {
		return nil, fmt.Errorf("cookie value too short")
	}

	nonce, ciphertext := raw[:nonceSize], raw[nonceSize:]
	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("cookie decryption failed: %w", err)
	}

	return plaintext, nil
}

// GetSession retrieves the current session from cookies
func (sm *SessionManager) GetSession(r *http.Request) (*SessionData, error) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		if err == http.ErrNoCookie {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read session cookie: %w", err)
	}

	data, err := sm.openCookie(cookie.Value)
	if err != nil {
		return nil, fmt.Errorf("invalid session cookie: %w", err)
	}

	var session SessionData
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session: %w", err)
	}

	if time.Now().After(session.ExpiresAt) {
		return nil, nil
	}

	return &session, nil
}

// SetSession stores a new session in an encrypted cookie
func (sm *SessionManager) SetSession(w http.ResponseWriter, session *SessionData) error {
	if session.ExpiresAt.IsZero() {
		session.ExpiresAt = time.Now().Add(sm.sessionDuration)
	}

	data, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	sealed, err := sm.sealCookie(data)
	if err != nil {
		return fmt.Errorf("failed to encrypt session: %w", err)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sealed,
		Path:     "/",
		Domain:   sm.cookieDomain,
		Expires:  session.ExpiresAt,
		Secure:   sm.cookieSecure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	return nil
}

// ClearSession removes the session cookie
func (sm *SessionManager) ClearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		Domain:   sm.cookieDomain,
		MaxAge:   -1,
		Secure:   sm.cookieSecure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// GenerateState creates a new OIDC flow state with PKCE
func (sm *SessionManager) GenerateState(returnPath string) (*StateData, error) {
	// Generate random state
	stateBytes := make([]byte, 32)
	if _, err := rand.Read(stateBytes); err != nil {
		return nil, fmt.Errorf("failed to generate state: %w", err)
	}
	state := base64.RawURLEncoding.EncodeToString(stateBytes)

	// Generate PKCE verifier
	verifierBytes := make([]byte, pkceVerifierLength)
	if _, err := rand.Read(verifierBytes); err != nil {
		return nil, fmt.Errorf("failed to generate PKCE verifier: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(verifierBytes)

	return &StateData{
		State:        state,
		PKCEVerifier: verifier,
		ReturnPath:   returnPath,
		ExpiresAt:    time.Now().Add(10 * time.Minute),
	}, nil
}

// SetState stores OIDC flow state in an encrypted cookie
func (sm *SessionManager) SetState(w http.ResponseWriter, state *StateData) error {
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	sealed, err := sm.sealCookie(data)
	if err != nil {
		return fmt.Errorf("failed to encrypt state: %w", err)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    sealed,
		Path:     "/",
		Domain:   sm.cookieDomain,
		Expires:  state.ExpiresAt,
		Secure:   sm.cookieSecure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	return nil
}

// GetState retrieves OIDC flow state from cookies
func (sm *SessionManager) GetState(r *http.Request) (*StateData, error) {
	cookie, err := r.Cookie(stateCookieName)
	if err != nil {
		if err == http.ErrNoCookie {
			return nil, fmt.Errorf("state cookie not found")
		}
		return nil, fmt.Errorf("failed to read state cookie: %w", err)
	}

	data, err := sm.openCookie(cookie.Value)
	if err != nil {
		return nil, fmt.Errorf("invalid state cookie: %w", err)
	}

	var state StateData
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal state: %w", err)
	}

	if time.Now().After(state.ExpiresAt) {
		return nil, fmt.Errorf("state has expired")
	}

	return &state, nil
}

// ClearState removes the state cookie
func (sm *SessionManager) ClearState(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    "",
		Path:     "/",
		Domain:   sm.cookieDomain,
		MaxAge:   -1,
		Secure:   sm.cookieSecure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// GetNamedCookie reads and decrypts a cookie by name, returning the raw plaintext.
// Returns nil, nil if the cookie is not present.
func (sm *SessionManager) GetNamedCookie(r *http.Request, name string) ([]byte, error) {
	cookie, err := r.Cookie(name)
	if err != nil {
		if err == http.ErrNoCookie {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read cookie %s: %w", name, err)
	}

	data, err := sm.openCookie(cookie.Value)
	if err != nil {
		return nil, fmt.Errorf("invalid cookie %s: %w", name, err)
	}

	return data, nil
}

// SetNamedCookie encrypts data and stores it in a cookie with the given name and expiry.
func (sm *SessionManager) SetNamedCookie(w http.ResponseWriter, name string, data []byte, expiry time.Time) error {
	sealed, err := sm.sealCookie(data)
	if err != nil {
		return fmt.Errorf("failed to encrypt cookie %s: %w", name, err)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    sealed,
		Path:     "/",
		Domain:   sm.cookieDomain,
		Expires:  expiry,
		Secure:   sm.cookieSecure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	return nil
}

// ClearNamedCookie removes a cookie by name.
func (sm *SessionManager) ClearNamedCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		Domain:   sm.cookieDomain,
		MaxAge:   -1,
		Secure:   sm.cookieSecure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}
