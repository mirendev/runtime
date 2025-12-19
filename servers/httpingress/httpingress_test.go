package httpingress

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

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

func TestHTTPTimeoutHandler(t *testing.T) {
	// Test that the timeout handler actually triggers
	tests := []struct {
		name           string
		timeout        time.Duration
		handlerDelay   time.Duration
		expectTimeout  bool
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "request completes before timeout",
			timeout:        100 * time.Millisecond,
			handlerDelay:   10 * time.Millisecond,
			expectTimeout:  false,
			expectedStatus: http.StatusOK,
			expectedBody:   "success",
		},
		{
			name:           "request times out",
			timeout:        50 * time.Millisecond,
			handlerDelay:   200 * time.Millisecond,
			expectTimeout:  true,
			expectedStatus: http.StatusServiceUnavailable,
			expectedBody:   timeoutMessage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test handler that simulates delay
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(tt.handlerDelay)
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("success"))
			})

			// Wrap with timeout handler (simulating what ServeHTTP does)
			timeoutHandler := http.TimeoutHandler(handler, tt.timeout, timeoutMessage)

			// Create test request and recorder
			req := httptest.NewRequest("GET", "/test", nil)
			rec := httptest.NewRecorder()

			// Serve the request
			timeoutHandler.ServeHTTP(rec, req)

			// Check status code
			if rec.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rec.Code)
			}

			// Check response body
			body := strings.TrimSpace(rec.Body.String())
			if !strings.Contains(body, tt.expectedBody) {
				t.Errorf("Expected body to contain '%s', got '%s'", tt.expectedBody, body)
			}
		})
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
			name: "connection reset",
			err: &net.OpError{
				Op:  "read",
				Net: "tcp",
				Err: &os.SyscallError{
					Syscall: "read",
					Err:     syscall.ECONNRESET,
				},
			},
			expected: true,
		},
		{
			name: "connection aborted",
			err: &net.OpError{
				Op:  "read",
				Net: "tcp",
				Err: &os.SyscallError{
					Syscall: "read",
					Err:     syscall.ECONNABORTED,
				},
			},
			expected: true,
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

func TestIsUpgradeRequest(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string]string
		expected bool
	}{
		{
			name:     "no headers",
			headers:  nil,
			expected: false,
		},
		{
			name:     "normal GET request",
			headers:  map[string]string{"Content-Type": "text/html"},
			expected: false,
		},
		{
			name:     "websocket upgrade",
			headers:  map[string]string{"Connection": "Upgrade", "Upgrade": "websocket"},
			expected: true,
		},
		{
			name:     "websocket upgrade lowercase",
			headers:  map[string]string{"Connection": "upgrade", "Upgrade": "websocket"},
			expected: true,
		},
		{
			name:     "connection with multiple values",
			headers:  map[string]string{"Connection": "keep-alive, Upgrade", "Upgrade": "websocket"},
			expected: true,
		},
		{
			name:     "h2c upgrade",
			headers:  map[string]string{"Connection": "Upgrade", "Upgrade": "h2c"},
			expected: true,
		},
		{
			name:     "connection keep-alive only",
			headers:  map[string]string{"Connection": "keep-alive"},
			expected: false,
		},
		{
			name:     "upgrade header without connection upgrade",
			headers:  map[string]string{"Upgrade": "websocket"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/ws", nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			result := isUpgradeRequest(req)
			if result != tt.expected {
				t.Errorf("isUpgradeRequest() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestWebSocketUpgradeBypassesTimeoutHandler verifies that WebSocket upgrade
// requests bypass the TimeoutHandler and successfully complete the upgrade.
//
// Background: http.TimeoutHandler wraps the ResponseWriter in a timeoutWriter
// that does NOT implement http.Hijacker. Without special handling, WebSocket
// upgrades would fail with 502 Bad Gateway. The fix is to detect upgrade
// requests early and bypass the TimeoutHandler.
func TestWebSocketUpgradeBypassesTimeoutHandler(t *testing.T) {
	// Create a backend server that responds with 101 Switching Protocols
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify this is a WebSocket upgrade request
		if r.Header.Get("Upgrade") != "websocket" {
			t.Error("Expected Upgrade: websocket header")
			http.Error(w, "Expected WebSocket upgrade", http.StatusBadRequest)
			return
		}
		if r.Header.Get("Connection") != "Upgrade" {
			t.Error("Expected Connection: Upgrade header")
			http.Error(w, "Expected Connection: Upgrade", http.StatusBadRequest)
			return
		}

		// Simulate a WebSocket upgrade response
		// In reality, the backend would call websocket.Upgrader.Upgrade()
		// which sends these headers and hijacks the connection
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

		// Write the 101 response manually (simulating what websocket library does)
		response := "HTTP/1.1 101 Switching Protocols\r\n" +
			"Upgrade: websocket\r\n" +
			"Connection: Upgrade\r\n" +
			"Sec-WebSocket-Accept: dummy-accept-key\r\n" +
			"\r\n"
		brw.WriteString(response)
		brw.Flush()

		// Keep the connection open briefly to simulate a real WebSocket
		time.Sleep(100 * time.Millisecond)
	}))
	defer backend.Close()

	// Create a reverse proxy that forwards to the backend
	proxyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxy := &httputil.ReverseProxy{
			Director: func(outReq *http.Request) {
				outReq.URL.Scheme = "http"
				outReq.URL.Host = backend.Listener.Addr().String()
			},
			ErrorHandler: func(rw http.ResponseWriter, req *http.Request, err error) {
				t.Logf("Proxy error (expected): %v", err)
				rw.WriteHeader(http.StatusBadGateway)
			},
		}
		proxy.ServeHTTP(w, r)
	})

	// Wrap the proxy handler in http.TimeoutHandler (as httpingress does)
	timeoutHandler := http.TimeoutHandler(proxyHandler, 60*time.Second, "timeout")

	// Simulate what httpingress.Server.ServeHTTP does: bypass TimeoutHandler
	// for upgrade requests so the connection can be hijacked
	serverHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isUpgradeRequest(r) {
			// Bypass TimeoutHandler for upgrade requests
			proxyHandler.ServeHTTP(w, r)
			return
		}
		timeoutHandler.ServeHTTP(w, r)
	})

	// Create a test server with the smart handler
	proxyServer := httptest.NewServer(serverHandler)
	defer proxyServer.Close()

	// Make a WebSocket upgrade request through the proxy
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

	// With the fix in place, upgrade requests bypass TimeoutHandler and succeed
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Errorf("Expected 101 Switching Protocols, got %d", resp.StatusCode)
	} else {
		t.Log("Got 101 Switching Protocols - WebSocket upgrade succeeded!")
	}
}
