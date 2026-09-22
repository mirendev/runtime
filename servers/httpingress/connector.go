package httpingress

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/connectors"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/oidc"
)

// connDataCookieName carries the opaque per-flow blob returned by a
// connector's LoginURL. Encrypted by the shared sessionManager.
const connDataCookieName = "miren_oidc_conn"

// connectorHandler manages route protection backed by a connectors.Connector
// (the dex-wrapped abstraction). Mirrors oidcHandler's surface so the
// existing claim-injection path can run unchanged.
type connectorHandler struct {
	route          *ingress_v1alpha.HttpRoute
	provider       *ingress_v1alpha.OidcProvider
	connector      connectors.Connector
	sessionManager *oidc.SessionManager
	redirectURL    string
	logger         *slog.Logger
}

// newConnectorHandler constructs a handler for the given route + connector
// provider. The redirectURL is pinned at construction time because Dex's
// connectors validate it matches the callbackURL passed to LoginURL.
func newConnectorHandler(route *ingress_v1alpha.HttpRoute, provider *ingress_v1alpha.OidcProvider, sessionManager *oidc.SessionManager, baseURL string, logger *slog.Logger) (*connectorHandler, error) {
	if provider.ConnectorType == "" {
		return nil, fmt.Errorf("connector provider missing connector_type")
	}
	if provider.ClientId == "" || provider.ClientSecret == "" {
		return nil, fmt.Errorf("connector provider missing client credentials")
	}

	redirectURL := baseURL + oidcCallbackPath
	connLogger := logger.With("module", "connector", "host", route.Host, "provider", provider.Name, "type", provider.ConnectorType)

	conn, err := buildConnector(provider, redirectURL, connLogger)
	if err != nil {
		return nil, err
	}

	return &connectorHandler{
		route:          route,
		provider:       provider,
		connector:      conn,
		sessionManager: sessionManager,
		redirectURL:    redirectURL,
		logger:         connLogger,
	}, nil
}

// buildConnector dispatches on connector_type to construct the right
// connectors.Connector. Adding a new connector means a new case here plus
// a config struct in pkg/connectors.
func buildConnector(provider *ingress_v1alpha.OidcProvider, redirectURL string, logger *slog.Logger) (connectors.Connector, error) {
	switch provider.ConnectorType {
	case "github":
		var cfg struct {
			Orgs         []connectors.GitHubOrg `json:"orgs,omitempty"`
			UseLoginAsID bool                   `json:"use_login_as_id,omitempty"`
		}
		if provider.ConfigJson != "" {
			if err := json.Unmarshal([]byte(provider.ConfigJson), &cfg); err != nil {
				return nil, fmt.Errorf("github connector: parse config_json: %w", err)
			}
		}
		return connectors.NewGitHub(connectors.GitHubConfig{
			ClientID:     provider.ClientId,
			ClientSecret: provider.ClientSecret,
			RedirectURI:  redirectURL,
			Orgs:         cfg.Orgs,
			UseLoginAsID: cfg.UseLoginAsID,
		}, logger)
	default:
		return nil, fmt.Errorf("unsupported connector type: %q", provider.ConnectorType)
	}
}

func (h *connectorHandler) checkAuth(w http.ResponseWriter, r *http.Request) (bool, map[string]any) {
	session, err := h.sessionManager.GetSession(r)
	if err != nil {
		h.logger.Error("failed to get session", "error", err)
		return false, nil
	}
	if session == nil {
		return false, nil
	}
	if session.Claims == nil {
		h.sessionManager.ClearSession(w)
		return false, nil
	}
	return true, session.Claims
}

func (h *connectorHandler) redirectToProvider(w http.ResponseWriter, r *http.Request) {
	state, err := h.sessionManager.GenerateState(r.URL.RequestURI())
	if err != nil {
		h.logger.Error("failed to generate state", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	loginURL, connData, err := h.connector.LoginURL(h.redirectURL, state.State)
	if err != nil {
		h.logger.Error("failed to generate login URL", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if err := h.sessionManager.SetState(w, state); err != nil {
		h.logger.Error("failed to set state cookie", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Round-trip the connector's opaque connData blob through its own
	// encrypted cookie. Some connectors return nil here (github populates
	// it later, in HandleCallback) — skip the cookie in that case so we
	// don't write empty cookies on every login.
	if len(connData) > 0 {
		if err := h.sessionManager.SetNamedCookie(w, connDataCookieName, connData, state.ExpiresAt); err != nil {
			h.logger.Error("failed to set conn data cookie", "error", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
	}

	http.Redirect(w, r, loginURL, http.StatusFound)
}

func (h *connectorHandler) handleCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	state, err := h.sessionManager.GetState(r)
	if err != nil {
		h.logger.Error("failed to get state", "error", err)
		http.Error(w, "Invalid state", http.StatusBadRequest)
		return
	}

	queryState := r.URL.Query().Get("state")
	if queryState != state.State {
		h.logger.Error("state mismatch", "expected", state.State, "got", queryState)
		http.Error(w, "State mismatch", http.StatusBadRequest)
		return
	}

	if errParam := r.URL.Query().Get("error"); errParam != "" {
		errDesc := r.URL.Query().Get("error_description")
		h.logger.Error("connector provider error", "error", errParam, "description", errDesc)
		http.Error(w, fmt.Sprintf("Authentication failed: %s", errDesc), http.StatusBadRequest)
		return
	}

	connData, err := h.sessionManager.GetNamedCookie(r, connDataCookieName)
	if err != nil {
		h.logger.Error("failed to read conn data cookie", "error", err)
		http.Error(w, "Invalid session", http.StatusBadRequest)
		return
	}

	identity, err := h.connector.HandleCallback(ctx, connData, r)
	if err != nil {
		h.logger.Error("connector callback failed", "error", err)
		http.Error(w, "Authentication failed", http.StatusBadRequest)
		return
	}

	session := &oidc.SessionData{
		Claims:    identity.Claims(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	if err := h.sessionManager.SetSession(w, session); err != nil {
		h.logger.Error("failed to set session", "error", err)
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	h.sessionManager.ClearState(w)
	h.sessionManager.ClearNamedCookie(w, connDataCookieName)

	returnPath := safeReturnPath(state.ReturnPath)

	h.logger.Info("connector authentication successful", "user_id", identity.UserID, "return_path", returnPath)
	http.Redirect(w, r, returnPath, http.StatusFound)
}

// injectClaims is identical in shape to oidcHandler.injectClaims — it
// strips configured claim headers (to prevent client spoofing) and then
// sets the ones present in claims. Duplicated rather than factored to
// keep the two flows visibly parallel while we're early; pull out into a
// shared helper once a third provider type lands.
func (h *connectorHandler) injectClaims(r *http.Request, claims map[string]any) {
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
			b, err := json.Marshal(v)
			if err != nil {
				continue
			}
			strValue = string(b)
		}

		r.Header.Set(mapping.Header, strValue)
	}
}

// connectorProviderMatches returns true if the cached handler's provider
// config matches the current entity from the store. Reuses the same
// pattern as oidcProviderMatches.
func connectorProviderMatches(cached *connectorHandler, current *ingress_v1alpha.OidcProvider) bool {
	cp := cached.provider
	return cp.ID == current.ID &&
		cp.ClientId == current.ClientId &&
		cp.ClientSecret == current.ClientSecret &&
		cp.ConnectorType == current.ConnectorType &&
		cp.ConfigJson == current.ConfigJson
}

func (s *Server) getOrCreateConnectorHandler(route *ingress_v1alpha.HttpRoute, baseURL string, providerEntity entity.AttrGetter) (*connectorHandler, error) {
	key := route.Host + "|" + baseURL
	if route.Default {
		key = "__default__|" + baseURL
	}

	var provider ingress_v1alpha.OidcProvider
	provider.Decode(providerEntity)

	s.connectorMu.RLock()
	if h, ok := s.connectorHandlers[key]; ok && connectorProviderMatches(h, &provider) {
		s.connectorMu.RUnlock()
		return h, nil
	}
	s.connectorMu.RUnlock()

	s.connectorMu.Lock()
	defer s.connectorMu.Unlock()

	if h, ok := s.connectorHandlers[key]; ok && connectorProviderMatches(h, &provider) {
		return h, nil
	}

	handler, err := newConnectorHandler(route, &provider, s.sessionManagerFor(baseURL), baseURL, s.Log)
	if err != nil {
		return nil, err
	}

	s.connectorHandlers[key] = handler
	return handler, nil
}

func (s *Server) connectorMiddleware(route *ingress_v1alpha.HttpRoute, providerEntity entity.AttrGetter, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scheme := s.requestScheme(r)
		baseURL := fmt.Sprintf("%s://%s", scheme, r.Host)

		handler, err := s.getOrCreateConnectorHandler(route, baseURL, providerEntity)
		if err != nil {
			s.Log.Error("failed to get connector handler", "error", err, "host", r.Host)
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
