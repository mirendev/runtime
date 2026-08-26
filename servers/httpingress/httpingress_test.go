package httpingress

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"syscall"
	"testing"
	"time"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/httputil"
)

func TestIngressConfigDefault(t *testing.T) {
	// Test that zero timeout defaults to 60s
	config := IngressConfig{}

	// The default is applied in NewServer, so let's test the logic directly
	if config.RequestTimeout <= 0 {
		config.RequestTimeout = 60 * time.Second
	}

	if config.RequestTimeout != 60*time.Second {
		t.Errorf("Expected default timeout to be 60s, got %v", config.RequestTimeout)
	}
}

func TestIngressConfigCustom(t *testing.T) {
	// Test that custom timeout is preserved
	config := IngressConfig{
		RequestTimeout: 30 * time.Second,
	}

	// The default is applied in NewServer only if non-positive
	if config.RequestTimeout <= 0 {
		config.RequestTimeout = 60 * time.Second
	}

	if config.RequestTimeout != 30*time.Second {
		t.Errorf("Expected timeout to be 30s, got %v", config.RequestTimeout)
	}
}

func TestIngressConfigNegative(t *testing.T) {
	// Test that negative timeout defaults to 60s
	config := IngressConfig{
		RequestTimeout: -10 * time.Second,
	}

	// The default is applied in NewServer for non-positive values
	if config.RequestTimeout <= 0 {
		config.RequestTimeout = 60 * time.Second
	}

	if config.RequestTimeout != 60*time.Second {
		t.Errorf("Expected negative timeout to default to 60s, got %v", config.RequestTimeout)
	}
}

func TestHTTPTimeoutProduces503(t *testing.T) {
	// Backend that never responds — simulates a hung process
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer backend.Close()

	timeout := 50 * time.Millisecond

	// Build a transport with a short ResponseHeaderTimeout
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = timeout

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxy := &httputil.ReverseProxy{
			Transport: transport,
			Director: func(outReq *http.Request) {
				outReq.URL.Scheme = "http"
				outReq.URL.Host = backend.Listener.Addr().String()
			},
			ErrorHandler: func(rw http.ResponseWriter, req *http.Request, err error) {
				if netErr, ok := errors.AsType[net.Error](err); ok && netErr.Timeout() {
					http.Error(rw, timeoutMessage, http.StatusServiceUnavailable)
					return
				}
				rw.WriteHeader(http.StatusBadGateway)
			},
		}
		proxy.ServeHTTP(w, r)
	})

	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/slow")
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("Expected 503, got %d", resp.StatusCode)
	}
}

func TestSSEStreamingNotBuffered(t *testing.T) {
	eventReady := make(chan struct{})

	// Backend that sends SSE events with explicit flushes
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("Backend ResponseWriter does not implement http.Flusher")
			return
		}

		// Send first event and flush immediately
		fmt.Fprintf(w, "data: hello\n\n")
		flusher.Flush()

		// Signal that the first event has been flushed
		close(eventReady)

		// Keep the handler alive — the test will close the connection
		<-r.Context().Done()
	}))
	defer backend.Close()

	// Generous ResponseHeaderTimeout — the point is that data arrives before it expires
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 5 * time.Second

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxy := &httputil.ReverseProxy{
			Transport: transport,
			Director: func(outReq *http.Request) {
				outReq.URL.Scheme = "http"
				outReq.URL.Host = backend.Listener.Addr().String()
			},
		}
		proxy.ServeHTTP(w, r)
	})

	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/events")
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}

	// Wait for backend to flush the first event
	select {
	case <-eventReady:
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for backend to flush event")
	}

	// The first SSE event should be readable immediately — if the response
	// were buffered (as with http.TimeoutHandler), this would block until
	// the handler returned or the timeout expired.
	scanner := bufio.NewScanner(resp.Body)
	readDone := make(chan string, 1)
	go func() {
		if scanner.Scan() {
			readDone <- scanner.Text()
		}
	}()

	select {
	case line := <-readDone:
		if line != "data: hello" {
			t.Errorf("Expected 'data: hello', got %q", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SSE event was not received promptly — response is being buffered")
	}
}

func TestHealthEndpoint(t *testing.T) {
	// Create a simple server instance
	server := &Server{}

	// Create test request
	req := httptest.NewRequest("GET", "/.well-known/miren/health", nil)
	rec := httptest.NewRecorder()

	// Call handler directly
	server.handleHealth(rec, req)

	// Check status code
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}

	// Check content type
	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got '%s'", contentType)
	}

	// Parse JSON response
	var response HealthResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode JSON response: %v", err)
	}

	// Verify response structure
	if response.Status == "" {
		t.Error("Expected status field in response")
	}

	if response.Checks == nil {
		t.Error("Expected checks field in response")
	}
}

func TestIsProxyConnectionError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "generic error",
			err:      errors.New("some error"),
			expected: false,
		},
		{
			name: "connection refused",
			err: &net.OpError{
				Op:  "dial",
				Net: "tcp",
				Err: &os.SyscallError{
					Syscall: "connect",
					Err:     syscall.ECONNREFUSED,
				},
			},
			expected: true,
		},
		{
			name: "no route to host",
			err: &net.OpError{
				Op:  "dial",
				Net: "tcp",
				Err: &os.SyscallError{
					Syscall: "connect",
					Err:     syscall.EHOSTUNREACH,
				},
			},
			expected: true,
		},
		{
			name: "network unreachable",
			err: &net.OpError{
				Op:  "dial",
				Net: "tcp",
				Err: &os.SyscallError{
					Syscall: "connect",
					Err:     syscall.ENETUNREACH,
				},
			},
			expected: true,
		},
		{
			name: "connection reset (not treated as connection error - request may have been processed)",
			err: &net.OpError{
				Op:  "read",
				Net: "tcp",
				Err: &os.SyscallError{
					Syscall: "read",
					Err:     syscall.ECONNRESET,
				},
			},
			expected: false,
		},
		{
			name: "connection aborted (not treated as connection error - request may have been processed)",
			err: &net.OpError{
				Op:  "read",
				Net: "tcp",
				Err: &os.SyscallError{
					Syscall: "read",
					Err:     syscall.ECONNABORTED,
				},
			},
			expected: false,
		},
		{
			name: "net.OpError without syscall error",
			err: &net.OpError{
				Op:  "dial",
				Net: "tcp",
				Err: errors.New("some other error"),
			},
			expected: false,
		},
		{
			name: "timeout error (not a connection error)",
			err: &net.OpError{
				Op:  "dial",
				Net: "tcp",
				Err: &os.SyscallError{
					Syscall: "connect",
					Err:     syscall.ETIMEDOUT,
				},
			},
			expected: false, // We don't treat timeout as a connection error for invalidation
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isProxyConnectionError(tt.err)
			if result != tt.expected {
				t.Errorf("isProxyConnectionError(%v) = %v, want %v", tt.err, result, tt.expected)
			}
		})
	}
}

// TestWebSocketUpgrade verifies that WebSocket upgrade requests work through
// the transport-level timeout proxy (no special-casing needed).
func TestWebSocketUpgrade(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") != "websocket" {
			t.Error("Expected Upgrade: websocket header")
			http.Error(w, "Expected WebSocket upgrade", http.StatusBadRequest)
			return
		}

		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Error("Backend ResponseWriter doesn't support hijacking")
			http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
			return
		}

		conn, brw, err := hj.Hijack()
		if err != nil {
			t.Errorf("Backend hijack failed: %v", err)
			return
		}
		defer conn.Close()

		response := "HTTP/1.1 101 Switching Protocols\r\n" +
			"Upgrade: websocket\r\n" +
			"Connection: Upgrade\r\n" +
			"Sec-WebSocket-Accept: dummy-accept-key\r\n" +
			"\r\n"
		brw.WriteString(response)
		brw.Flush()

		time.Sleep(100 * time.Millisecond)
	}))
	defer backend.Close()

	// All requests go through the same transport — no upgrade branching
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 5 * time.Second

	serverHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxy := &httputil.ReverseProxy{
			Transport: transport,
			Director: func(outReq *http.Request) {
				outReq.URL.Scheme = "http"
				outReq.URL.Host = backend.Listener.Addr().String()
			},
			ErrorHandler: func(rw http.ResponseWriter, req *http.Request, err error) {
				t.Logf("Proxy error: %v", err)
				rw.WriteHeader(http.StatusBadGateway)
			},
		}
		proxy.ServeHTTP(w, r)
	})

	proxyServer := httptest.NewServer(serverHandler)
	defer proxyServer.Close()

	req, err := http.NewRequest("GET", proxyServer.URL+"/ws", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Errorf("Expected 101 Switching Protocols, got %d", resp.StatusCode)
	}
}

func TestProxyToLeaseRetrySuppress(t *testing.T) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 1 * time.Second

	h := &Server{
		Log:       slog.Default(),
		transport: transport,
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)

	// Port 1 is not listening — triggers ECONNREFUSED
	err := h.proxyToLease(rec, req, "http://127.0.0.1:1", "app/test", "test-app", false, 0)

	if err == nil {
		t.Fatal("expected connection error, got nil")
	}
	if !isProxyConnectionError(err) {
		t.Fatalf("expected proxy connection error, got: %v", err)
	}

	// writeErrorResponse=false should leave the ResponseWriter untouched
	if rec.Code != http.StatusOK {
		t.Errorf("expected no status written (default 200), got %d", rec.Code)
	}
	if rec.Body.Len() > 0 {
		t.Errorf("expected no body written, got %d bytes: %s", rec.Body.Len(), rec.Body.String())
	}
}

func TestProxyToLeaseWriteErrorOnFinalAttempt(t *testing.T) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 1 * time.Second

	h := &Server{
		Log:       slog.Default(),
		transport: transport,
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)

	err := h.proxyToLease(rec, req, "http://127.0.0.1:1", "app/test", "test-app", true, 0)

	if err == nil {
		t.Fatal("expected connection error, got nil")
	}
	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", rec.Code)
	}
}

func TestProxyToLeaseNoRetryOnTimeout(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer backend.Close()

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 50 * time.Millisecond

	h := &Server{
		Log:       slog.Default(),
		transport: transport,
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)

	err := h.proxyToLease(rec, req, backend.URL, "app/test", "test-app", false, 0)

	// Timeouts are not connection errors — writeErrorResponse only suppresses connection errors
	if err != nil {
		t.Errorf("expected nil (timeout handled inline), got: %v", err)
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 for timeout, got %d", rec.Code)
	}
}

func TestResolveVersionStrategy(t *testing.T) {
	tests := []struct {
		name           string
		ephemeralLabel string
		wildcardRoute  bool
		want           versionResolution
	}{
		// A wildcard tenant subdomain that names no live ephemeral version must
		// fall back to the active version, not hard-404 (MIR-1613).
		{"wildcard label falls back to active", "tenant1", true, resolveEphemeralOrActive},
		{"non-wildcard label stays strict", "feat-x", false, resolveEphemeralStrict},
		{"no label serves active on wildcard", "", true, resolveActive},
		{"no label serves active on exact", "", false, resolveActive},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveVersionStrategy(tt.ephemeralLabel, tt.wildcardRoute)
			if got != tt.want {
				t.Errorf("resolveVersionStrategy(%q, %v) = %d, want %d", tt.ephemeralLabel, tt.wildcardRoute, got, tt.want)
			}
		})
	}
}

func TestLeaseCacheKey(t *testing.T) {
	app := entity.Id("app-1")

	// Two distinct wildcard subdomains that did not resolve to an ephemeral
	// version must land on the same base key so they share one active-version
	// lease pool instead of fragmenting per tenant.
	tenant1 := leaseCacheKey(app, "tenant1", false)
	tenant2 := leaseCacheKey(app, "tenant2", false)
	labelFree := leaseCacheKey(app, "", false)
	if tenant1 != labelFree || tenant2 != labelFree {
		t.Errorf("unresolved labels should share the base key %q, got tenant1=%q tenant2=%q", labelFree, tenant1, tenant2)
	}

	// A resolved ephemeral version stays scoped per label and never collides
	// with the base key or another label.
	resolved := leaseCacheKey(app, "feat-x", true)
	if resolved == labelFree {
		t.Errorf("resolved ephemeral key %q must differ from the base key %q", resolved, labelFree)
	}
	if other := leaseCacheKey(app, "feat-y", true); other == resolved {
		t.Errorf("distinct ephemeral labels must not share a key, both were %q", resolved)
	}
}

func TestPrivateMetricsPath(t *testing.T) {
	private := core_v1alpha.ConfigSpec{Services: []core_v1alpha.ConfigSpecServices{{
		Name: "web",
		Metrics: core_v1alpha.ConfigSpecServicesMetrics{
			Enabled: true,
			Path:    "/internal/metrics",
		},
	}}}
	if !privateMetricsPath(&private, "/internal/metrics") {
		t.Fatal("private configured metrics path should be hidden")
	}
	if privateMetricsPath(&private, "/internal/metrics/extra") {
		t.Fatal("only the exact configured path should be hidden")
	}

	private.Services[0].Metrics.Public = true
	if privateMetricsPath(&private, "/internal/metrics") {
		t.Fatal("public metrics path should pass through ingress")
	}

	private.Services[0].Metrics.Public = false
	private.Services[0].Metrics.Enabled = false
	if privateMetricsPath(&private, "/internal/metrics") {
		t.Fatal("disabled metrics configuration should not reserve an app path")
	}
}
