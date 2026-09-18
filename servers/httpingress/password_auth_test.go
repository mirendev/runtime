package httpingress

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/oidc"
)

func makePasswordProviderEntity(ident, hash string) *entity.Entity {
	return entity.New(
		entity.DBId, entity.Id(ident),
		entity.Ref(entity.EntityKind, ingress_v1alpha.KindPasswordProvider),
		entity.String(ingress_v1alpha.PasswordProviderPasswordHashId, hash),
	)
}

func newPasswordTestServer() *Server {
	signingKey := make([]byte, 32)

	return &Server{
		Log:              slog.Default(),
		sessionManager:   oidc.NewSessionManager(false, "", signingKey),
		oidcHandlers:     make(map[string]*oidcHandler),
		passwordHandlers: make(map[string]*passwordHandler),
	}
}

func TestPasswordProviderMatches(t *testing.T) {
	base := &ingress_v1alpha.PasswordProvider{
		ID:           "provider-1",
		PasswordHash: "$2a$10$test",
	}

	handler := &passwordHandler{provider: base}

	t.Run("identical provider matches", func(t *testing.T) {
		same := &ingress_v1alpha.PasswordProvider{
			ID:           "provider-1",
			PasswordHash: "$2a$10$test",
		}
		if !passwordProviderMatches(handler, same) {
			t.Error("expected match for identical provider")
		}
	})

	t.Run("different hash does not match", func(t *testing.T) {
		different := &ingress_v1alpha.PasswordProvider{
			ID:           "provider-1",
			PasswordHash: "$2a$10$other",
		}
		if passwordProviderMatches(handler, different) {
			t.Error("expected mismatch for different hash")
		}
	})

	t.Run("different ID does not match", func(t *testing.T) {
		different := &ingress_v1alpha.PasswordProvider{
			ID:           "provider-2",
			PasswordHash: "$2a$10$test",
		}
		if passwordProviderMatches(handler, different) {
			t.Error("expected mismatch for different ID")
		}
	})
}

func TestGetOrCreatePasswordHandlerCacheInvalidation(t *testing.T) {
	srv := newPasswordTestServer()

	providerIdent := "test/pw-provider"
	hash1, _ := bcrypt.GenerateFromPassword([]byte("secret1"), bcrypt.MinCost)

	route := &ingress_v1alpha.HttpRoute{
		Host:         "app.example.com",
		AuthProvider: entity.Id(providerIdent),
	}

	entA := makePasswordProviderEntity(providerIdent, string(hash1))

	h1, err := srv.getOrCreatePasswordHandler(route, "https://app.example.com", entA)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if h1.provider.PasswordHash != string(hash1) {
		t.Fatalf("expected hash=%s, got %s", string(hash1), h1.provider.PasswordHash)
	}

	// Same entity should return cached handler
	h2, err := srv.getOrCreatePasswordHandler(route, "https://app.example.com", entA)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if h1 != h2 {
		t.Error("expected same handler instance on cache hit")
	}

	// Pass a new entity with updated hash
	hash2, _ := bcrypt.GenerateFromPassword([]byte("secret2"), bcrypt.MinCost)
	entB := makePasswordProviderEntity(providerIdent, string(hash2))

	h3, err := srv.getOrCreatePasswordHandler(route, "https://app.example.com", entB)
	if err != nil {
		t.Fatalf("third call: %v", err)
	}
	if h3.provider.PasswordHash != string(hash2) {
		t.Fatalf("expected updated hash after provider change")
	}
	if h1 == h3 {
		t.Error("expected different handler instance after provider change")
	}
}

func TestPasswordSessionRoundtrip(t *testing.T) {
	signingKey := make([]byte, 32)
	sm := oidc.NewSessionManager(false, "", signingKey)

	hash, _ := bcrypt.GenerateFromPassword([]byte("correctpassword"), bcrypt.MinCost)
	handler := &passwordHandler{
		route: &ingress_v1alpha.HttpRoute{
			Host: "app.example.com",
		},
		provider: &ingress_v1alpha.PasswordProvider{
			PasswordHash: string(hash),
		},
		sm:     sm,
		logger: slog.Default(),
	}

	t.Run("no cookie returns unauthenticated", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		if handler.checkSession(req) {
			t.Error("expected unauthenticated with no cookie")
		}
	})

	t.Run("set and check session", func(t *testing.T) {
		w := httptest.NewRecorder()
		if err := handler.setSession(w); err != nil {
			t.Fatalf("setSession: %v", err)
		}

		cookies := w.Result().Cookies()
		var sessionCookie *http.Cookie
		for _, c := range cookies {
			if c.Name == pwSessionCookieName {
				sessionCookie = c
				break
			}
		}
		if sessionCookie == nil {
			t.Fatal("expected session cookie to be set")
		}

		req := httptest.NewRequest("GET", "/", nil)
		req.AddCookie(sessionCookie)
		if !handler.checkSession(req) {
			t.Error("expected authenticated with valid session cookie")
		}
	})

	t.Run("wrong route host is rejected", func(t *testing.T) {
		w := httptest.NewRecorder()
		if err := handler.setSession(w); err != nil {
			t.Fatalf("setSession: %v", err)
		}

		cookies := w.Result().Cookies()
		var sessionCookie *http.Cookie
		for _, c := range cookies {
			if c.Name == pwSessionCookieName {
				sessionCookie = c
				break
			}
		}

		otherHandler := &passwordHandler{
			route: &ingress_v1alpha.HttpRoute{
				Host: "other.example.com",
			},
			sm: sm,
		}

		req := httptest.NewRequest("GET", "/", nil)
		req.AddCookie(sessionCookie)
		if otherHandler.checkSession(req) {
			t.Error("expected session to be rejected for wrong route host")
		}
	})
}

func TestSanitizeReturnPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "/"},
		{"/", "/"},
		{"/dashboard", "/dashboard"},
		{"/a/b/c", "/a/b/c"},
		{"https://evil.com", "/"},
		{"//evil.com", "/"},
		{"http://evil.com/steal", "/"},
		{"/foo/../bar", "/bar"},
		{"/../../../etc/passwd", "/etc/passwd"},
		{"/valid/path", "/valid/path"},

		// Browsers read a backslash in the second position as a second slash,
		// which turns these into scheme-relative URLs pointing off-site.
		{`/\evil.com`, "/"},
		{`/\evil.com/path`, "/"},
		{`\/evil.com`, "/"},
		{`/\\evil.com`, "/"},
		{`\\evil.com`, "/"},

		// A backslash past the origin is just an odd path component.
		{`/foo\bar`, `/foo\bar`},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeReturnPath(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeReturnPath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestLoginFormHTMLEscaping(t *testing.T) {
	signingKey := make([]byte, 32)
	sm := oidc.NewSessionManager(false, "", signingKey)

	handler := &passwordHandler{
		route: &ingress_v1alpha.HttpRoute{Host: "app.example.com"},
		provider: &ingress_v1alpha.PasswordProvider{
			PasswordHash: "$2a$10$test",
		},
		sm:     sm,
		logger: slog.Default(),
	}

	w := httptest.NewRecorder()
	handler.serveLoginForm(w, `"><script>alert(1)</script>`, "")

	body := w.Body.String()
	if strings.Contains(body, "<script>") {
		t.Error("XSS payload was not escaped in login form HTML")
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Error("expected HTML-escaped script tag in form output")
	}
}

func TestPasswordLoginFlow(t *testing.T) {
	signingKey := make([]byte, 32)
	sm := oidc.NewSessionManager(false, "", signingKey)

	hash, _ := bcrypt.GenerateFromPassword([]byte("correctpassword"), bcrypt.MinCost)
	handler := &passwordHandler{
		route: &ingress_v1alpha.HttpRoute{
			Host: "app.example.com",
		},
		provider: &ingress_v1alpha.PasswordProvider{
			PasswordHash: string(hash),
		},
		sm:     sm,
		logger: slog.Default(),
	}

	t.Run("GET login shows form", func(t *testing.T) {
		req := httptest.NewRequest("GET", passwordLoginPath, nil)
		w := httptest.NewRecorder()
		handler.handleLogin(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
		body := w.Body.String()
		if !strings.Contains(body, "Password Required") {
			t.Error("expected login form HTML")
		}
	})

	t.Run("wrong password shows error", func(t *testing.T) {
		form := url.Values{"password": {"wrong"}, "return": {"/"}}
		req := httptest.NewRequest("POST", passwordLoginPath, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		handler.handleLogin(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200 (form re-render), got %d", w.Code)
		}
		body := w.Body.String()
		if !strings.Contains(body, "Incorrect password") {
			t.Error("expected error message in form")
		}
	})

	t.Run("correct password sets cookie and redirects", func(t *testing.T) {
		form := url.Values{"password": {"correctpassword"}, "return": {"/dashboard"}}
		req := httptest.NewRequest("POST", passwordLoginPath, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		handler.handleLogin(w, req)

		if w.Code != http.StatusFound {
			t.Errorf("expected 302, got %d", w.Code)
		}

		loc := w.Header().Get("Location")
		if loc != "/dashboard" {
			t.Errorf("expected redirect to /dashboard, got %s", loc)
		}

		var foundCookie bool
		for _, c := range w.Result().Cookies() {
			if c.Name == pwSessionCookieName {
				foundCookie = true
				break
			}
		}
		if !foundCookie {
			t.Error("expected session cookie to be set after successful login")
		}
	})

	t.Run("open redirect is blocked", func(t *testing.T) {
		form := url.Values{"password": {"correctpassword"}, "return": {"https://evil.com/steal"}}
		req := httptest.NewRequest("POST", passwordLoginPath, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		handler.handleLogin(w, req)

		if w.Code != http.StatusFound {
			t.Errorf("expected 302, got %d", w.Code)
		}

		loc := w.Header().Get("Location")
		if loc != "/" {
			t.Errorf("expected redirect to / (sanitized), got %s", loc)
		}
	})

	t.Run("logout clears cookie and redirects", func(t *testing.T) {
		req := httptest.NewRequest("GET", passwordLogoutPath, nil)
		w := httptest.NewRecorder()
		handler.handleLogout(w, req)

		if w.Code != http.StatusFound {
			t.Errorf("expected 302, got %d", w.Code)
		}

		loc := w.Header().Get("Location")
		if loc != "/" {
			t.Errorf("expected redirect to /, got %s", loc)
		}

		for _, c := range w.Result().Cookies() {
			if c.Name == pwSessionCookieName && c.MaxAge == -1 {
				return
			}
		}
		t.Error("expected session cookie to be cleared")
	})
}

// Regression test for MIR-1734: the Secure flag on an auth cookie must be
// decided by the scheme of the request that receives it, never by whatever
// scheme a concurrent request happened to present. Before the fix, every
// middleware wrote its request's scheme into the one shared SessionManager,
// so an https login could be handed a non-Secure cookie (and vice versa)
// whenever an http request was inflight at the same time.
func TestPasswordMiddlewareSecureFlagIsPerScheme(t *testing.T) {
	srv := newPasswordTestServer()
	hash, _ := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.MinCost)
	route := &ingress_v1alpha.HttpRoute{Host: "app.example.com"}
	ent := makePasswordProviderEntity("test/pw", string(hash))

	mw := srv.passwordMiddleware(route, ent, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	login := func(proto string) *http.Cookie {
		form := url.Values{"password": {"pw"}}
		req := httptest.NewRequest("POST", passwordLoginPath, strings.NewReader(form.Encode()))
		req.Host = route.Host
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Forwarded-Proto", proto)

		w := httptest.NewRecorder()
		mw(w, req)
		if w.Code != http.StatusFound {
			t.Errorf("%s login: expected 302, got %d", proto, w.Code)
			return nil
		}
		for _, c := range w.Result().Cookies() {
			if c.Name == pwSessionCookieName {
				return c
			}
		}
		t.Errorf("%s login: no session cookie set", proto)
		return nil
	}

	// Hammer both schemes concurrently so a shared-state regression shows up
	// as a wrong flag (and, under -race, as a detected race).
	const rounds = 50
	results := make(chan struct {
		proto  string
		secure bool
	}, rounds*2)

	var wg sync.WaitGroup
	for i := 0; i < rounds; i++ {
		for _, proto := range []string{"https", "http"} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if c := login(proto); c != nil {
					results <- struct {
						proto  string
						secure bool
					}{proto, c.Secure}
				}
			}()
		}
	}
	wg.Wait()
	close(results)

	for r := range results {
		want := r.proto == "https"
		if r.secure != want {
			t.Errorf("%s login got Secure=%v, want %v", r.proto, r.secure, want)
		}
	}

	// The two handlers are distinct cache entries with distinct managers,
	// and neither touched the server's shared instance.
	hs, _ := srv.getOrCreatePasswordHandler(route, "https://app.example.com", ent)
	hp, _ := srv.getOrCreatePasswordHandler(route, "http://app.example.com", ent)
	if hs.sm == hp.sm || hs.sm == srv.sessionManager || hp.sm == srv.sessionManager {
		t.Error("expected each scheme's handler to hold its own session manager copy")
	}
}
