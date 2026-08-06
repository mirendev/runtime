package httpingress

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/oidc"
)

const (
	// Well-known path for OIDC callback
	oidcCallbackPath = "/.well-known/miren/oidc/callback"
)

// loadOrGenerateSigningKey reads the OIDC cookie signing key from disk,
// or generates and persists a new 32-byte random key if none exists.
func loadOrGenerateSigningKey(dataPath string) ([]byte, error) {
	keyPath := filepath.Join(dataPath, "server", "oidc-signing.key")

	data, err := os.ReadFile(keyPath)
	if err == nil && len(data) == 32 {
		return data, nil
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generating OIDC signing key: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(keyPath), 0700); err != nil {
		return nil, fmt.Errorf("creating directory for OIDC signing key: %w", err)
	}

	if err := os.WriteFile(keyPath, key, 0600); err != nil {
		return nil, fmt.Errorf("writing OIDC signing key: %w", err)
	}

	return key, nil
}

// oidcHandler manages OIDC authentication for a route
type oidcHandler struct {
	route          *ingress_v1alpha.HttpRoute
	provider       *ingress_v1alpha.OidcProvider
	client         *oidc.Client
	sessionManager *oidc.SessionManager
	resource       string
	logger         *slog.Logger
}

// newOIDCHandler creates a new OIDC handler for a route with a resolved provider.
// The baseURL is used to construct the redirect URL for OAuth2 callbacks.
// The resource parameter is the RFC 8707 resource indicator for the authorization request.
func newOIDCHandler(route *ingress_v1alpha.HttpRoute, provider *ingress_v1alpha.OidcProvider, sessionManager *oidc.SessionManager, baseURL, resource string, logger *slog.Logger) (*oidcHandler, error) {
	if provider.ProviderUrl == "" || provider.ClientId == "" {
		return nil, fmt.Errorf("OIDC provider missing required fields")
	}

	// Parse scopes, ensuring "openid" is always included
	scopes := strings.Fields(provider.Scopes)
	hasOpenID := false
	for _, s := range scopes {
		if s == "openid" {
			hasOpenID = true
			break
		}
	}
	if !hasOpenID {
		scopes = append([]string{"openid"}, scopes...)
	}

	redirectURL := fmt.Sprintf("%s%s", baseURL, oidcCallbackPath)

	client := oidc.NewClient(
		provider.ProviderUrl,
		provider.ClientId,
		provider.ClientSecret,
		redirectURL,
		scopes,
		logger,
	)

	return &oidcHandler{
		route:          route,
		provider:       provider,
		client:         client,
		sessionManager: sessionManager,
		resource:       resource,
		logger:         logger.With("module", "oidc", "host", route.Host, "provider", provider.Name),
	}, nil
}

// checkAuth verifies if the request has a valid OIDC session.
// Returns true if authenticated, false if redirect to OIDC provider is needed.
func (h *oidcHandler) checkAuth(w http.ResponseWriter, r *http.Request) (authenticated bool, claims map[string]interface{}) {
	session, err := h.sessionManager.GetSession(r)
	if err != nil {
		h.logger.Error("failed to get session", "error", err)
		return false, nil
	}

	if session != nil {
		// Use stored claims from session (verified at token exchange time)
		if session.Claims != nil {
			return true, session.Claims
		}

		// Fallback: validate and parse claims from ID token
		claims, err := h.client.ParseIDToken(r.Context(), session.IDToken)
		if err != nil {
			h.logger.Error("failed to parse ID token", "error", err)
			h.sessionManager.ClearSession(w)
			return false, nil
		}
		return true, claims
	}

	return false, nil
}

// redirectToProvider redirects the user to the OIDC provider for authentication
func (h *oidcHandler) redirectToProvider(w http.ResponseWriter, r *http.Request) {
	// Generate state and PKCE verifier
	state, err := h.sessionManager.GenerateState(r.URL.RequestURI())
	if err != nil {
		h.logger.Error("failed to generate state", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Store state in cookie
	if err := h.sessionManager.SetState(w, state); err != nil {
		h.logger.Error("failed to set state", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Generate authorization URL with resource indicator (RFC 8707)
	authURL, err := h.client.AuthorizationURL(state.State, state.PKCEVerifier, h.resource)
	if err != nil {
		h.logger.Error("failed to generate auth URL", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Redirect to provider
	http.Redirect(w, r, authURL, http.StatusFound)
}

// handleCallback processes the OAuth2 callback from the OIDC provider
func (h *oidcHandler) handleCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get state from cookie
	state, err := h.sessionManager.GetState(r)
	if err != nil {
		h.logger.Error("failed to get state", "error", err)
		http.Error(w, "Invalid state", http.StatusBadRequest)
		return
	}

	// Verify state parameter matches
	queryState := r.URL.Query().Get("state")
	if queryState != state.State {
		h.logger.Error("state mismatch", "expected", state.State, "got", queryState)
		http.Error(w, "State mismatch", http.StatusBadRequest)
		return
	}

	// Check for error from provider
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		errDesc := r.URL.Query().Get("error_description")
		h.logger.Error("OIDC provider error", "error", errParam, "description", errDesc)
		http.Error(w, fmt.Sprintf("Authentication failed: %s", errDesc), http.StatusBadRequest)
		return
	}

	// Get authorization code
	code := r.URL.Query().Get("code")
	if code == "" {
		h.logger.Error("authorization code missing")
		http.Error(w, "Authorization code missing", http.StatusBadRequest)
		return
	}

	// Exchange code for tokens
	token, err := h.client.ExchangeCode(ctx, code, state.PKCEVerifier)
	if err != nil {
		h.logger.Error("failed to exchange code", "error", err)
		http.Error(w, "Failed to exchange authorization code", http.StatusInternalServerError)
		return
	}

	// Extract ID token
	idToken, ok := token.Extra("id_token").(string)
	if !ok || idToken == "" {
		h.logger.Error("ID token missing from token response")
		http.Error(w, "ID token missing", http.StatusInternalServerError)
		return
	}

	// Verify and parse claims from ID token
	claims, err := h.client.ParseIDToken(ctx, idToken)
	if err != nil {
		h.logger.Error("failed to parse ID token", "error", err)
		http.Error(w, "Failed to verify ID token", http.StatusInternalServerError)
		return
	}

	// Create session
	session := &oidc.SessionData{
		IDToken:      idToken,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		Claims:       claims,
	}

	// Store session
	if err := h.sessionManager.SetSession(w, session); err != nil {
		h.logger.Error("failed to set session", "error", err)
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	// Clear state cookie
	h.sessionManager.ClearState(w)

	returnPath := safeReturnPath(state.ReturnPath)

	h.logger.Info("OIDC authentication successful", "subject", claims["sub"], "return_path", returnPath)
	http.Redirect(w, r, returnPath, http.StatusFound)
}

// injectClaims adds JWT claims as HTTP headers based on the route configuration.
// All configured claim headers are first stripped from the request to prevent
// clients from spoofing identity headers.
func (h *oidcHandler) injectClaims(r *http.Request, claims map[string]interface{}) {
	// Strip all configured claim headers to prevent spoofing.
	// This is important for claims not present in the JWT — without stripping,
	// a client-provided header would pass through to the app.
	for _, mapping := range h.route.ClaimMappings {
		if mapping.Header != "" {
			r.Header.Del(mapping.Header)
		}
	}

	for _, mapping := range h.route.ClaimMappings {
		if mapping.Claim == "" || mapping.Header == "" {
			continue
		}

		value, ok := claims[mapping.Claim]
		if !ok {
			continue
		}

		var strValue string
		switch v := value.(type) {
		case string:
			strValue = v
		case float64:
			strValue = fmt.Sprintf("%v", v)
		case bool:
			strValue = fmt.Sprintf("%v", v)
		default:
			// Structured types (arrays, objects) are JSON-encoded
			b, err := json.Marshal(v)
			if err != nil {
				continue
			}
			strValue = string(b)
		}

		r.Header.Set(mapping.Header, strValue)
	}
}

// requestScheme determines the scheme of the incoming request by checking
// proxy headers (X-Forwarded-Proto, Forwarded), the TLS state, and
// falling back to http.
func requestScheme(r *http.Request) string {
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		return proto
	}

	if fwd := r.Header.Get("Forwarded"); fwd != "" {
		for part := range strings.SplitSeq(fwd, ";") {
			part = strings.TrimSpace(part)
			if after, ok := strings.CutPrefix(part, "proto="); ok {
				return after
			}
		}
	}

	if r.TLS != nil {
		return "https"
	}

	return "http"
}

// oidcProviderMatches returns true if the cached handler's provider config
// matches the current provider entity from the store.
func oidcProviderMatches(cached *oidcHandler, current *ingress_v1alpha.OidcProvider) bool {
	cp := cached.provider
	return cp.ID == current.ID &&
		cp.ClientId == current.ClientId &&
		cp.ClientSecret == current.ClientSecret &&
		cp.ProviderUrl == current.ProviderUrl &&
		cp.Scopes == current.Scopes
}

// getOrCreateOIDCHandler returns a cached oidcHandler for the given route,
// creating one on first access. The handler (and its oidc.Client) is reused
// across requests so that discovery and JWKS caches are effective.
// If the provider config has changed since the handler was cached, the stale
// handler is replaced with a new one. If the entity store is unreachable,
// the cached handler is evicted and an error is returned (fail-closed).
func (s *Server) getOrCreateOIDCHandler(route *ingress_v1alpha.HttpRoute, baseURL string, providerEntity entity.AttrGetter) (*oidcHandler, error) {
	key := route.Host + "|" + baseURL
	if route.Default {
		key = "__default__|" + baseURL
	}

	var provider ingress_v1alpha.OidcProvider
	provider.Decode(providerEntity)

	s.oidcMu.RLock()
	if h, ok := s.oidcHandlers[key]; ok && oidcProviderMatches(h, &provider) {
		s.oidcMu.RUnlock()
		return h, nil
	}
	s.oidcMu.RUnlock()

	s.oidcMu.Lock()
	defer s.oidcMu.Unlock()

	// Double-check after acquiring write lock
	if h, ok := s.oidcHandlers[key]; ok && oidcProviderMatches(h, &provider) {
		return h, nil
	}

	var resource string
	if route.Default {
		resource = "cluster:default"
	} else {
		resource = route.Host
	}

	handler, err := newOIDCHandler(route, &provider, s.oidcSessionManager, baseURL, resource, s.Log)
	if err != nil {
		return nil, err
	}

	s.oidcHandlers[key] = handler
	return handler, nil
}

func (s *Server) oidcMiddleware(route *ingress_v1alpha.HttpRoute, providerEntity entity.AttrGetter, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scheme := requestScheme(r)
		baseURL := fmt.Sprintf("%s://%s", scheme, r.Host)

		s.oidcSessionManager.SetSecure(scheme == "https")

		handler, err := s.getOrCreateOIDCHandler(route, baseURL, providerEntity)
		if err != nil {
			s.Log.Error("failed to get OIDC handler", "error", err, "host", r.Host)
			http.Error(w, "Authentication service unavailable", http.StatusServiceUnavailable)
			return
		}

		if r.URL.Path == oidcCallbackPath {
			handler.handleCallback(w, r)
			return
		}

		authenticated, claims := handler.checkAuth(w, r)
		if !authenticated {
			handler.redirectToProvider(w, r)
			return
		}

		handler.injectClaims(r, claims)
		next(w, r)
	}
}
