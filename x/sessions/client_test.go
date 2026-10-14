package sessions_test

import (
	"context"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"miren.dev/runtime/x/sessions"
)

func TestWorkloadIdentityRotationAndErrors(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/apps/workers/sessions/task" {
			t.Errorf("wrong app/session route: %s", r.URL.Path)
			w.WriteHeader(400)
			return
		}
		switch r.Header.Get("Authorization") {
		case "Bearer first-token":
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"error":"private-server-data","code":"not-found","category":"session","StatusCode":200}`))
		case "Bearer rotated-token":
			_, _ = w.Write([]byte(`{"session":{"id":"session/workers/task","app":"app/workers","service":"agent","max_sessions_per_sandbox":4,"idle_timeout_seconds":47,"desired_state":"suspended","activity":"idle","phase":"inactive"}}`))
		default:
			w.WriteHeader(401)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	ca := filepath.Join(dir, "ca.pem")
	token := filepath.Join(dir, "token")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(token, []byte("first-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MIREN_API_ADDRESS", strings.TrimPrefix(server.URL, "https://"))
	t.Setenv("MIREN_CA_CERT_PATH", ca)
	t.Setenv("MIREN_IDENTITY_TOKEN_PATH", token)
	client, err := sessions.NewClient(sessions.ConfigFromEnv("workers"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Get(t.Context(), "task")
	var apiErr *sessions.HTTPError
	if !errors.Is(err, sessions.ErrNotFound) || !errors.As(err, &apiErr) || apiErr.StatusCode != 404 || apiErr.Code != "not-found" || apiErr.Category != "session" || apiErr.Message != "private-server-data" {
		t.Fatalf("coordinator error not preserved: %#v", err)
	}
	if strings.Contains(err.Error(), "private-server-data") {
		t.Fatal("ordinary error log exposes server data")
	}
	if err := os.WriteFile(token, []byte("rotated-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := client.Get(t.Context(), "session/workers/task")
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != "session/workers/task" || s.Service != "agent" || s.MaxSessionsPerSandbox != 4 || s.IdleTimeoutSeconds != 47 || s.DesiredState != sessions.Suspended || s.Activity != "idle" || s.Phase != "inactive" {
		t.Fatalf("incorrect lifecycle summary: %+v", s)
	}
	for _, id := range []string{"session/other/task", "../task", "..", "/task", "session/workers/task/extra"} {
		if _, err := client.Get(t.Context(), id); err == nil {
			t.Errorf("accepted out-of-scope ID %q", id)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := client.Get(ctx, "task"); !errors.Is(err, context.Canceled) {
		t.Fatalf("request did not preserve cancellation: %v", err)
	}
}

func TestRedirectsDoNotForwardIdentity(t *testing.T) {
	var reached atomic.Bool
	destination := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Store(true)
		w.WriteHeader(204)
	}))
	defer destination.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	h := server.Client()
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return nil }
	client, err := sessions.NewClient(sessions.Config{URL: server.URL, App: "workers", Token: "private-token", HTTPClient: h})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Get(t.Context(), "task")
	var apiErr *sessions.HTTPError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusTemporaryRedirect || reached.Load() {
		t.Fatalf("redirect was followed or not reported: %v, destination reached=%v", err, reached.Load())
	}
	// NewClient must not change a caller-owned HTTP client's redirect policy.
	if err := h.CheckRedirect(nil, nil); err != nil {
		t.Fatal("client mutated the supplied HTTP client")
	}
}

func TestCoordinatorTLSAndConfig(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"sessions":[]}`))
	}))
	defer server.Close()
	client, err := sessions.NewClient(sessions.Config{URL: server.URL, App: "workers", Token: "token"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.List(t.Context()); err == nil {
		t.Fatal("untrusted coordinator certificate accepted")
	}
	for _, cfg := range []sessions.Config{
		{URL: "http://example.com", App: "workers", Token: "token"},
		{URL: "https://user:password@example.com", App: "workers", Token: "token"},
		{URL: "https://example.com/api", App: "workers", Token: "token"},
		{URL: "https://example.com", App: "../workers", Token: "token"},
		{URL: "https://example.com", App: "workers"},
		{URL: "https://example.com", App: "workers", Token: "token", TokenPath: "file"},
	} {
		if _, err := sessions.NewClient(cfg); err == nil {
			t.Error("invalid coordinator config accepted")
		}
	}
}
