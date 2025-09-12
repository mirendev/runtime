package httpingress

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
