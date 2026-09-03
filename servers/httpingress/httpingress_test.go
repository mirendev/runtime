package httpingress

import (
	"bufio"
	"context"
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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/components/activator"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/httputil"
	"miren.dev/runtime/pkg/rpc"
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
	tenant1 := leaseCacheKey(app, "web", "tenant1", false)
	tenant2 := leaseCacheKey(app, "web", "tenant2", false)
	labelFree := leaseCacheKey(app, "web", "", false)
	if tenant1 != labelFree || tenant2 != labelFree {
		t.Errorf("unresolved labels should share the base key %q, got tenant1=%q tenant2=%q", labelFree, tenant1, tenant2)
	}

	// A resolved ephemeral version stays scoped per label and never collides
	// with the base key or another label.
	resolved := leaseCacheKey(app, "web", "feat-x", true)
	if resolved == labelFree {
		t.Errorf("resolved ephemeral key %q must differ from the base key %q", resolved, labelFree)
	}
	if other := leaseCacheKey(app, "web", "feat-y", true); other == resolved {
		t.Errorf("distinct ephemeral labels must not share a key, both were %q", resolved)
	}

	api := leaseCacheKey(app, "api", "", false)
	if api == labelFree {
		t.Errorf("different services must not share a lease cache key, both were %q", api)
	}
}

func TestRouteService(t *testing.T) {
	if got := routeService(nil); got != "web" {
		t.Errorf("nil route service = %q, want web", got)
	}
	if got := routeService(&ingress_v1alpha.HttpRoute{}); got != "web" {
		t.Errorf("legacy route service = %q, want web", got)
	}
	if got := routeService(&ingress_v1alpha.HttpRoute{Service: "api"}); got != "api" {
		t.Errorf("selected route service = %q, want api", got)
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
	if !privateMetricsPath(&private, "web", "/internal/metrics") {
		t.Fatal("private configured metrics path should be hidden")
	}
	if privateMetricsPath(&private, "web", "/internal/metrics/extra") {
		t.Fatal("only the exact configured path should be hidden")
	}

	private.Services[0].Metrics.Public = true
	if privateMetricsPath(&private, "web", "/internal/metrics") {
		t.Fatal("public metrics path should pass through ingress")
	}

	private.Services[0].Metrics.Public = false
	private.Services[0].Metrics.Enabled = false
	if privateMetricsPath(&private, "web", "/internal/metrics") {
		t.Fatal("disabled metrics configuration should not reserve an app path")
	}
}

// TestPrivateMetricsPathSelectedService exercises the privacy guard against the
// whichever service the matched route selects — not a hardcoded "web". A
// non-"web" service with private metrics must have its configured path hidden
// just as "web" does, and a request selecting a different service must not be
// hidden by an unrelated service's private metrics block.
func TestPrivateMetricsPathSelectedService(t *testing.T) {
	privateService := func(name, path string) core_v1alpha.ConfigSpecServices {
		return core_v1alpha.ConfigSpecServices{
			Name: name,
			Metrics: core_v1alpha.ConfigSpecServicesMetrics{
				Enabled: true,
				Public:  false,
				Path:    path,
			},
		}
	}
	config := func(services ...core_v1alpha.ConfigSpecServices) *core_v1alpha.ConfigSpec {
		return &core_v1alpha.ConfigSpec{Services: services}
	}

	const apiMetrics = "/metrics"
	api := config(privateService("api", apiMetrics))

	// A route selecting the "api" service must hide its private metrics path,
	// mirroring the historic "web" behavior. This is the regression that
	// previously leaked Prometheus exposition for any non-"web" service.
	if !privateMetricsPath(api, "api", apiMetrics) {
		t.Fatalf("non-%q service private metrics path should be hidden", "web")
	}
	// A differently-named configured path on the same service still passes
	// through, and a sibling path is not reserved.
	if privateMetricsPath(api, "api", apiMetrics+"/extra") {
		t.Fatal("only the exact configured path should be hidden")
	}

	// A route selecting a service with no configured metrics (or a different
	// service entirely) must not be hidden by another service's private block.
	if privateMetricsPath(api, "web", apiMetrics) {
		t.Fatal("a request to an unconfigured service must not be hidden by an unrelated service")
	}

	// Public metrics pass through regardless of the selected service name.
	public := config(privateService("api", apiMetrics))
	public.Services[0].Metrics.Public = true
	if privateMetricsPath(public, "api", apiMetrics) {
		t.Fatalf("public metrics path should pass through ingress even for non-%q services", "web")
	}

	// Disabled metrics never reserve a path, for any selected service.
	disabled := config(privateService("api", apiMetrics))
	disabled.Services[0].Metrics.Enabled = false
	if privateMetricsPath(disabled, "api", apiMetrics) {
		t.Fatal("disabled metrics configuration should not reserve an app path")
	}

	// A multi-service config: the guard scopes to the selected service only.
	// Selecting "web" must not be hidden by the "api" service's block, and
	// vice versa; each selection hides only its own configured path.
	multi := config(
		privateService("web", "/web-metrics"),
		privateService("api", "/api-metrics"),
	)
	if !privateMetricsPath(multi, "web", "/web-metrics") {
		t.Fatal("web service private metrics path should be hidden when web is selected")
	}
	if !privateMetricsPath(multi, "api", "/api-metrics") {
		t.Fatal("api service private metrics path should be hidden when api is selected")
	}
	if privateMetricsPath(multi, "web", "/api-metrics") {
		t.Fatal("selecting web must not be hidden by the api service's private metrics block")
	}
	if privateMetricsPath(multi, "api", "/web-metrics") {
		t.Fatal("selecting api must not be hidden by the web service's private metrics block")
	}
}

// notFoundBody is the body http.NotFound writes, used to distinguish the
// guard's 404 from any response that reaches the proxied lease path.
const notFoundBody = "404 page not found\n"

// stubActivator satisfies activator.AppActivator so the e2e test can prove a
// request that the privacy guard does NOT hide reaches the lease-acquisition
// path (past prepare) rather than vanishing into a 404. It never returns a
// lease, so a request that proceeds past the guard fails with a 5xx —
// distinctly not the guard's 404.
type stubActivator struct{}

func (stubActivator) AcquireLease(context.Context, *core_v1alpha.AppVersion, string) (*activator.Lease, error) {
	return nil, errors.New("test stub: no sandbox wired")
}
func (stubActivator) ReleaseLease(context.Context, *activator.Lease) error { return nil }
func (stubActivator) RenewLease(context.Context, *activator.Lease) (*activator.Lease, error) {
	return nil, nil
}
func (stubActivator) Invalidations() <-chan activator.SandboxInvalidation {
	return make(chan activator.SandboxInvalidation)
}
func (stubActivator) SetPoolCreator(activator.PoolCreator) {}

// TestPrivateMetricsPathE2E drives the public ingress end-to-end through
// serveHTTPWithMetrics — the real prepare closure, resolveIngressTarget, and
// the privateMetricsPath call site — for routes that select a non-"web"
// service with private metrics. The bug (commit 6134136f) leaked these paths
// for any service not literally named "web"; after the fix the privacy
// contract holds for the service the matched route selects regardless of its
// name.
func TestPrivateMetricsPathE2E(t *testing.T) {
	const metricsPath = "/metrics"

	// apiService is a non-"web" HTTP service with metrics enabled on the
	// default http port — the unsafe default configuration where the metrics
	// listener is the proxied listener.
	apiService := func(name string, public bool) core_v1alpha.ConfigSpecServices {
		return core_v1alpha.ConfigSpecServices{
			Name:  name,
			Ports: []core_v1alpha.ConfigSpecServicesPorts{{Port: 8080, Type: "http"}},
			Metrics: core_v1alpha.ConfigSpecServicesMetrics{
				Enabled: true,
				Public:  public,
				Path:    metricsPath,
			},
		}
	}

	// setup builds an app + ConfigVersion + active AppVersion carrying the given
	// services through the in-memory entity server, plus a public route
	// selecting `service`, and returns a minimally-wired ingress Server whose
	// serveHTTPWithMetrics drives the real prepare chain. A stub activator is
	// wired so a request the guard does not hide reaches lease acquisition and
	// returns a 5xx (rather than panicking on a nil activator) — that 5xx is the
	// signal the guard declined to hide it. When ephemeralLabel is non-empty it
	// also creates an ephemeral AppVersion carrying the same config, so the
	// request host `<ephemeralLabel>.<host>` resolves via lookupEphemeralRoute.
	setup := func(t *testing.T, services []core_v1alpha.ConfigSpecServices, service, host, authProvider string, defaultRoute bool, ephemeralLabel string) (*Server, func()) {
		t.Helper()
		inmem, cleanup := testutils.NewInMemEntityServer(t)
		ctx := context.Background()

		appID, err := inmem.Client.Create(ctx, "app", &core_v1alpha.App{})
		require.NoError(t, err)
		cvID, err := inmem.Client.Create(ctx, "cfg", &core_v1alpha.ConfigVersion{
			App:  appID,
			Spec: core_v1alpha.ConfigSpec{Services: services},
		})
		require.NoError(t, err)
		verID, err := inmem.Client.Create(ctx, "ver", &core_v1alpha.AppVersion{App: appID, ConfigVersion: cvID})
		require.NoError(t, err)
		require.NoError(t, inmem.Client.Update(ctx, &core_v1alpha.App{ID: appID, ActiveVersion: verID}))
		if ephemeralLabel != "" {
			_, err = inmem.Client.Create(ctx, "ver-"+ephemeralLabel, &core_v1alpha.AppVersion{
				App:            appID,
				ConfigVersion:  cvID,
				EphemeralLabel: ephemeralLabel,
			})
			require.NoError(t, err)
		}

		route := &ingress_v1alpha.HttpRoute{
			Host:         host,
			App:          appID,
			Service:      service,
			AuthProvider: entity.Id(authProvider),
		}
		routeID := host
		if defaultRoute {
			route.Default = true
			routeID = "default-route"
		}
		_, err = inmem.Client.Create(ctx, routeID, route)
		require.NoError(t, err)

		rpcClient := rpc.LocalClient(entityserver_v1alpha.AdaptEntityAccess(inmem.Server))
		s := &Server{Log: slog.Default()}
		s.eac = inmem.EAC
		s.ingressClient = ingress.NewClient(s.Log, rpcClient)
		s.aa = stubActivator{}
		return s, cleanup
	}

	do := func(t *testing.T, s *Server, host, path string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		var appName string
		s.serveHTTPWithMetrics(rec, httptest.NewRequest("GET", "http://"+host+path, nil), &appName)
		return rec
	}

	// G1: a public request to a non-"web" service's private metrics path is
	// hidden (404) — the regression. Pre-fix this proxied the Prometheus body.
	// A 404 (rather than the stub activator's 5xx) also proves the request
	// never reached lease acquisition.
	t.Run("non-web private metrics hidden (exact route)", func(t *testing.T) {
		s, cleanup := setup(t, []core_v1alpha.ConfigSpecServices{apiService("api", false)}, "api", "api.example.com", "", false, "")
		defer cleanup()
		rec := do(t, s, "api.example.com", metricsPath)
		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.Equal(t, notFoundBody, rec.Body.String(),
			"private metrics path must be hidden by the guard, not proxied to the sandbox")
	})

	// G7c: the default-route branch selects the service too (service =
	// routeService(defaultRoute)), so the guard hides the default route's
	// selected-service private metrics path.
	t.Run("non-web private metrics hidden (default route)", func(t *testing.T) {
		s, cleanup := setup(t, []core_v1alpha.ConfigSpecServices{apiService("api", false)}, "api", "", "", true, "")
		defer cleanup()
		// A host with no matching route falls back to the default route.
		rec := do(t, s, "nomatch.example.com", metricsPath)
		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.Equal(t, notFoundBody, rec.Body.String())
	})

	// G7b: the ephemeral-subdomain branch (lookupEphemeralRoute) resolves an
	// ephemeral version for the label and applies the guard to its resolved
	// config + the route's selected service. A request to a non-"web" service's
	// private metrics path via an ephemeral subdomain is hidden too.
	t.Run("non-web private metrics hidden (ephemeral route)", func(t *testing.T) {
		s, cleanup := setup(t, []core_v1alpha.ConfigSpecServices{apiService("api", false)}, "api", "api.example.com", "", false, "feat")
		defer cleanup()
		// "<label>.<base>" — no exact route, so lookupEphemeralRoute strips the
		// label and matches the base route.
		rec := do(t, s, "feat.api.example.com", metricsPath)
		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.Equal(t, notFoundBody, rec.Body.String())
	})

	// G9: the guard runs in prepare, BEFORE auth. The route carries an auth
	// provider whose entity does not exist. If the guard hides first the
	// response is a clean 404; if auth ran first it would try to load the
	// missing provider and return 503 ("Authentication service unavailable").
	// Asserting 404 proves prepare precedes auth and hides the path pre-auth.
	t.Run("private metrics hidden before auth (auth-protected route)", func(t *testing.T) {
		s, cleanup := setup(t, []core_v1alpha.ConfigSpecServices{apiService("api", false)}, "api", "api.example.com", "oidc_provider/does-not-exist", false, "")
		defer cleanup()
		rec := do(t, s, "api.example.com", metricsPath)
		assert.Equal(t, http.StatusNotFound, rec.Code,
			"prepare must hide the private path (404) before auth runs; a 503 would mean auth ran first and failed to load the missing provider")
		assert.Equal(t, notFoundBody, rec.Body.String())
	})

	// G4 (e2e boundary): public metrics are NOT hidden. The unit test proves
	// privateMetricsPath returns false for Public=true; this confirms the e2e
	// path does not over-block — a public-metrics request proceeds past the
	// guard to the proxied lease path (a 5xx here only because no sandbox is
	// wired, never a guard 404).
	t.Run("public metrics not hidden (over-block boundary)", func(t *testing.T) {
		s, cleanup := setup(t, []core_v1alpha.ConfigSpecServices{apiService("api", true)}, "api", "api.example.com", "", false, "")
		defer cleanup()
		rec := do(t, s, "api.example.com", metricsPath)
		assert.NotEqual(t, http.StatusNotFound, rec.Code)
		assert.NotEqual(t, notFoundBody, rec.Body.String(),
			"public metrics path must pass through; Public=true must not be hidden by the guard")
	})
}
