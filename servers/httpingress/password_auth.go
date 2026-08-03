package httpingress

import (
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/oidc"
)

const (
	passwordLoginPath       = "/.well-known/miren/auth/login"
	passwordLogoutPath      = "/.well-known/miren/auth/logout"
	pwSessionCookieName     = "miren_pw_session"
	passwordSessionDuration = 24 * time.Hour
)

type passwordSessionData struct {
	RouteHost string    `json:"route_host"`
	ExpiresAt time.Time `json:"expires_at"`
}

type passwordHandler struct {
	route    *ingress_v1alpha.HttpRoute
	provider *ingress_v1alpha.PasswordProvider
	sm       *oidc.SessionManager
	logger   *slog.Logger
}

func (h *passwordHandler) checkSession(r *http.Request) bool {
	data, err := h.sm.GetNamedCookie(r, pwSessionCookieName)
	if err != nil || data == nil {
		return false
	}

	var session passwordSessionData
	if err := json.Unmarshal(data, &session); err != nil {
		return false
	}

	if time.Now().After(session.ExpiresAt) {
		return false
	}

	routeHost := h.route.Host
	if h.route.Default {
		routeHost = "__default__"
	}
	return session.RouteHost == routeHost
}

func (h *passwordHandler) setSession(w http.ResponseWriter) error {
	routeHost := h.route.Host
	if h.route.Default {
		routeHost = "__default__"
	}

	session := passwordSessionData{
		RouteHost: routeHost,
		ExpiresAt: time.Now().Add(passwordSessionDuration),
	}

	data, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	return h.sm.SetNamedCookie(w, pwSessionCookieName, data, session.ExpiresAt)
}

func (h *passwordHandler) clearSession(w http.ResponseWriter) {
	h.sm.ClearNamedCookie(w, pwSessionCookieName)
}

// isValidRedirect reports whether p can be handed to http.Redirect without
// letting the caller pick the origin: it must be a path on this host, not an
// absolute or scheme-relative URL.
//
// The "/\" case is the one that is easy to miss. Browsers normalize backslashes
// to forward slashes before parsing a URL, so a browser reads "/\evil.com" as
// "//evil.com" and follows it off-site, even though net/url treats it as an
// ordinary path.
//
// The name is not incidental: CodeQL's open-redirect query treats guards named
// isValidRedirect (and a handful of variations) as sanitizers, so writing the
// check as a named predicate keeps the analyzer in agreement with the code.
func isValidRedirect(p string) bool {
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.HasPrefix(p, `/\`) {
		return false
	}

	u, err := url.Parse(p)
	return err == nil && u.Scheme == "" && u.Host == ""
}

func sanitizeReturnPath(raw string) string {
	// Reject off-site shapes outright rather than letting Clean fold them into
	// a local path: "//evil.com" should send the user home, not to "/evil.com".
	if raw == "" || strings.Contains(raw, "://") || strings.HasPrefix(raw, "//") {
		return "/"
	}

	// Clean collapses the leading "//" that would otherwise make this
	// scheme-relative, but it leaves "/\" alone, so the guard still has work
	// to do afterwards.
	cleaned := path.Clean("/" + raw)
	if !isValidRedirect(cleaned) {
		return "/"
	}

	return cleaned
}

func (h *passwordHandler) serveLoginForm(w http.ResponseWriter, returnPath string, errorMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")

	errorHTML := ""
	if errorMsg != "" {
		errorHTML = `<p style="color:#c0392b;margin-bottom:16px">` + errorMsg + `</p>`
	}

	fmt.Fprintf(w, loginFormHTML, errorHTML, html.EscapeString(returnPath))
}

func (h *passwordHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.serveLoginForm(w, sanitizeReturnPath(r.URL.Query().Get("return")), "")
		return
	}

	if err := r.ParseForm(); err != nil {
		h.logger.Error("failed to parse form", "error", err)
		h.serveLoginForm(w, "", "Invalid request.")
		return
	}

	password := r.FormValue("password")
	returnPath := sanitizeReturnPath(r.FormValue("return"))

	err := bcrypt.CompareHashAndPassword([]byte(h.provider.PasswordHash), []byte(password))
	if err != nil {
		h.logger.Info("password authentication failed", "host", h.route.Host)
		h.serveLoginForm(w, returnPath, "Incorrect password.")
		return
	}

	if err := h.setSession(w); err != nil {
		h.logger.Error("failed to set session", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	h.logger.Info("password authentication successful", "host", h.route.Host)
	http.Redirect(w, r, returnPath, http.StatusFound)
}

func (h *passwordHandler) handleLogout(w http.ResponseWriter, r *http.Request) {
	h.clearSession(w)
	http.Redirect(w, r, "/", http.StatusFound)
}

func passwordProviderMatches(cached *passwordHandler, current *ingress_v1alpha.PasswordProvider) bool {
	cp := cached.provider
	return cp.ID == current.ID && cp.PasswordHash == current.PasswordHash
}

func (s *Server) getOrCreatePasswordHandler(route *ingress_v1alpha.HttpRoute, baseURL string, providerEntity entity.AttrGetter) (*passwordHandler, error) {
	key := route.Host + "|" + baseURL
	if route.Default {
		key = "__default__|" + baseURL
	}

	var provider ingress_v1alpha.PasswordProvider
	provider.Decode(providerEntity)

	s.passwordMu.RLock()
	if h, ok := s.passwordHandlers[key]; ok && passwordProviderMatches(h, &provider) {
		s.passwordMu.RUnlock()
		return h, nil
	}
	s.passwordMu.RUnlock()

	s.passwordMu.Lock()
	defer s.passwordMu.Unlock()

	if h, ok := s.passwordHandlers[key]; ok && passwordProviderMatches(h, &provider) {
		return h, nil
	}

	handler := &passwordHandler{
		route:    route,
		provider: &provider,
		sm:       s.oidcSessionManager,
		logger:   s.Log.With("module", "password-auth", "host", route.Host, "provider", provider.Name),
	}

	s.passwordHandlers[key] = handler
	return handler, nil
}

func (s *Server) passwordMiddleware(route *ingress_v1alpha.HttpRoute, providerEntity entity.AttrGetter, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scheme := requestScheme(r)
		baseURL := fmt.Sprintf("%s://%s", scheme, r.Host)

		s.oidcSessionManager.SetSecure(scheme == "https")

		handler, err := s.getOrCreatePasswordHandler(route, baseURL, providerEntity)
		if err != nil {
			s.Log.Error("failed to get password handler", "error", err, "host", r.Host)
			http.Error(w, "Authentication service unavailable", http.StatusServiceUnavailable)
			return
		}

		if r.URL.Path == passwordLoginPath {
			handler.handleLogin(w, r)
			return
		}

		if r.URL.Path == passwordLogoutPath {
			handler.handleLogout(w, r)
			return
		}

		if handler.checkSession(r) {
			next(w, r)
			return
		}

		handler.serveLoginForm(w, sanitizeReturnPath(r.URL.RequestURI()), "")
	}
}

// passwordMu and passwordHandlers are added to the Server struct in httpingress.go

var loginFormHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Password Required</title>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #f5f5f5; display: flex; justify-content: center; align-items: center; min-height: 100vh; }
  .card { background: #fff; border-radius: 8px; box-shadow: 0 2px 8px rgba(0,0,0,0.1); padding: 32px; width: 100%%; max-width: 380px; }
  h1 { font-size: 20px; margin-bottom: 24px; text-align: center; color: #333; }
  input[type=password] { width: 100%%; padding: 10px 12px; border: 1px solid #ddd; border-radius: 4px; font-size: 14px; margin-bottom: 16px; }
  input[type=password]:focus { outline: none; border-color: #4a90d9; box-shadow: 0 0 0 2px rgba(74,144,217,0.2); }
  button { width: 100%%; padding: 10px; background: #4a90d9; color: #fff; border: none; border-radius: 4px; font-size: 14px; cursor: pointer; }
  button:hover { background: #357abd; }
</style>
</head>
<body>
<div class="card">
  <h1>Password Required</h1>
  %s
  <form method="POST" action="` + passwordLoginPath + `">
    <input type="password" name="password" placeholder="Enter password" autofocus required>
    <input type="hidden" name="return" value="%s">
    <button type="submit">Continue</button>
  </form>
</div>
</body>
</html>`
