package oidc

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestSessionManager_SetAndGetSession(t *testing.T) {
	sm := NewSessionManager(false, "", nil)

	// Create test session
	session := &SessionData{
		IDToken: "test-id-token",
		Claims: map[string]any{
			"sub":   "test-user",
			"email": "test@example.com",
		},
		ExpiresAt: time.Now().Add(time.Hour),
	}

	// Set session
	w := httptest.NewRecorder()
	err := sm.SetSession(w, session)
	if err != nil {
		t.Fatalf("failed to set session: %v", err)
	}

	// Get session cookie
	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("no session cookie set")
	}

	// Create request with cookie
	req := httptest.NewRequest("GET", "/", nil)
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}

	// Get session
	retrieved, err := sm.GetSession(req)
	if err != nil {
		t.Fatalf("failed to get session: %v", err)
	}

	if retrieved == nil {
		t.Fatal("session is nil")
		return
	}

	if retrieved.IDToken != session.IDToken {
		t.Errorf("IDToken mismatch: got %s, want %s", retrieved.IDToken, session.IDToken)
	}

	if retrieved.Claims["sub"] != "test-user" {
		t.Errorf("Claims mismatch: got %v", retrieved.Claims)
	}
}

func TestSessionManager_ClearSession(t *testing.T) {
	sm := NewSessionManager(false, "", nil)

	// Set session first
	session := &SessionData{
		IDToken:   "test-token",
		ExpiresAt: time.Now().Add(time.Hour),
	}

	w := httptest.NewRecorder()
	err := sm.SetSession(w, session)
	if err != nil {
		t.Fatalf("failed to set session: %v", err)
	}

	// Clear session
	w = httptest.NewRecorder()
	sm.ClearSession(w)

	// Check that cookie is cleared (MaxAge=-1)
	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("no cookie set")
	}

	if cookies[0].MaxAge != -1 {
		t.Errorf("cookie not cleared: MaxAge=%d", cookies[0].MaxAge)
	}
}

func TestSessionManager_GenerateState(t *testing.T) {
	sm := NewSessionManager(false, "", nil)

	state, err := sm.GenerateState("/return-path")
	if err != nil {
		t.Fatalf("failed to generate state: %v", err)
	}

	if state.State == "" {
		t.Error("state is empty")
	}

	if state.PKCEVerifier == "" {
		t.Error("PKCE verifier is empty")
	}

	if state.ReturnPath != "/return-path" {
		t.Errorf("return path mismatch: got %s, want /return-path", state.ReturnPath)
	}

	if state.ExpiresAt.IsZero() {
		t.Error("expiration not set")
	}
}

func TestSessionManager_SetAndGetState(t *testing.T) {
	sm := NewSessionManager(false, "", nil)

	state := &StateData{
		State:        "test-state",
		PKCEVerifier: "test-verifier",
		ReturnPath:   "/test-path",
		ExpiresAt:    time.Now().Add(10 * time.Minute),
	}

	// Set state
	w := httptest.NewRecorder()
	err := sm.SetState(w, state)
	if err != nil {
		t.Fatalf("failed to set state: %v", err)
	}

	// Get state cookie
	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("no state cookie set")
	}

	// Create request with cookie
	req := httptest.NewRequest("GET", "/", nil)
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}

	// Get state
	retrieved, err := sm.GetState(req)
	if err != nil {
		t.Fatalf("failed to get state: %v", err)
	}

	if retrieved.State != state.State {
		t.Errorf("state mismatch: got %s, want %s", retrieved.State, state.State)
	}

	if retrieved.PKCEVerifier != state.PKCEVerifier {
		t.Errorf("PKCE verifier mismatch: got %s, want %s", retrieved.PKCEVerifier, state.PKCEVerifier)
	}

	if retrieved.ReturnPath != state.ReturnPath {
		t.Errorf("return path mismatch: got %s, want %s", retrieved.ReturnPath, state.ReturnPath)
	}
}

func TestSessionManager_GetSession_NoSession(t *testing.T) {
	sm := NewSessionManager(false, "", nil)

	req := httptest.NewRequest("GET", "/", nil)

	session, err := sm.GetSession(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if session != nil {
		t.Error("expected nil session, got non-nil")
	}
}

func TestSessionManager_GetSession_ExpiredSession(t *testing.T) {
	sm := NewSessionManager(false, "", nil)

	// Create expired session
	session := &SessionData{
		IDToken:   "test-token",
		ExpiresAt: time.Now().Add(-time.Hour), // expired 1 hour ago
	}

	w := httptest.NewRecorder()
	err := sm.SetSession(w, session)
	if err != nil {
		t.Fatalf("failed to set session: %v", err)
	}

	// Create request with cookie
	req := httptest.NewRequest("GET", "/", nil)
	for _, cookie := range w.Result().Cookies() {
		req.AddCookie(cookie)
	}

	// Get session - should be nil due to expiration
	retrieved, err := sm.GetSession(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if retrieved != nil {
		t.Error("expected nil for expired session")
	}
}

func TestSessionManager_WithSecure(t *testing.T) {
	insecure := NewSessionManager(false, "", nil)
	secure := insecure.WithSecure(true)

	if secure == insecure {
		t.Fatal("WithSecure must return a distinct instance")
	}
	if insecure.cookieSecure {
		t.Error("WithSecure mutated the original manager")
	}

	// Every cookie-emitting method must honor the derived flag, including the
	// clears: a browser only deletes a cookie when the clear's attributes
	// match the original.
	emit := func(sm *SessionManager, w *httptest.ResponseRecorder) {
		if err := sm.SetSession(w, &SessionData{IDToken: "t", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
			t.Fatalf("SetSession: %v", err)
		}
		if err := sm.SetState(w, &StateData{State: "s", ExpiresAt: time.Now().Add(time.Minute)}); err != nil {
			t.Fatalf("SetState: %v", err)
		}
		if err := sm.SetNamedCookie(w, "named", []byte("x"), time.Now().Add(time.Hour)); err != nil {
			t.Fatalf("SetNamedCookie: %v", err)
		}
		sm.ClearSession(w)
		sm.ClearState(w)
		sm.ClearNamedCookie(w, "named")
	}

	for _, tc := range []struct {
		name string
		sm   *SessionManager
		want bool
	}{
		{"original stays insecure", insecure, false},
		{"derived is secure", secure, true},
		{"derived back to insecure", secure.WithSecure(false), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			emit(tc.sm, w)
			cookies := w.Result().Cookies()
			if len(cookies) != 6 {
				t.Fatalf("expected 6 cookies, got %d", len(cookies))
			}
			for _, c := range cookies {
				if c.Secure != tc.want {
					t.Errorf("cookie %s: Secure=%v, want %v", c.Name, c.Secure, tc.want)
				}
			}
		})
	}

	// The copy shares the key, so a session sealed by one opens with the other.
	w := httptest.NewRecorder()
	if err := secure.SetSession(w, &SessionData{IDToken: "shared", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatalf("SetSession: %v", err)
	}
	req := httptest.NewRequest("GET", "/", nil)
	for _, c := range w.Result().Cookies() {
		req.AddCookie(c)
	}
	got, err := insecure.GetSession(req)
	if err != nil {
		t.Fatalf("GetSession via original: %v", err)
	}
	if got == nil || got.IDToken != "shared" {
		t.Fatalf("expected session sealed by derived manager to open with original, got %+v", got)
	}
}
