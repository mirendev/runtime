package httpingress

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"miren.dev/runtime/api/app"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/components/activator"
	"miren.dev/runtime/discovery"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
)

const (
	timeoutMessage = "Request timeout"
	// leaseAcquisitionTimeout is the maximum time to wait for sandbox boot
	// This is longer than request timeout to prevent dangling resources
	leaseAcquisitionTimeout = 2 * time.Minute
)

type IngressConfig struct {
	RequestTimeout time.Duration
}

type Server struct {
	Log *slog.Logger

	config        IngressConfig
	rpcClient     rpc.Client
	eac           *entityserver_v1alpha.EntityAccessClient
	ingressClient *ingress.Client
	appClient     *app.Client
	handler       http.Handler

	aa activator.AppActivator

	httpMetrics *metrics.HTTPMetrics

	mu   sync.Mutex
	apps map[string]*appUsage
}

type appUsage struct {
	leases []*lease
}

func NewServer(
	ctx context.Context,
	log *slog.Logger,
	config IngressConfig,
	rpcClient rpc.Client,
	aa activator.AppActivator,
	httpMetrics *metrics.HTTPMetrics,
) *Server {
	eac := entityserver_v1alpha.NewEntityAccessClient(rpcClient)

	if config.RequestTimeout <= 0 {
		if config.RequestTimeout < 0 {
			log.Warn("invalid request timeout; using default 60s", "configured", config.RequestTimeout)
		}
		config.RequestTimeout = 60 * time.Second
	}

	serv := &Server{
		Log:           log.With("module", "httpingress"),
		config:        config,
		rpcClient:     rpcClient,
		eac:           eac,
		ingressClient: ingress.NewClient(log, rpcClient),
		appClient:     app.NewClient(log, rpcClient),
		aa:            aa,
		httpMetrics:   httpMetrics,
		apps:          make(map[string]*appUsage),
	}

	// Build the timeout handler once at initialization
	serv.handler = http.TimeoutHandler(
		http.HandlerFunc(serv.handleRequest),
		config.RequestTimeout,
		timeoutMessage,
	)

	if httpMetrics == nil {
		serv.Log.Warn("HTTPMetrics is nil in httpingress")
	} else {
		serv.Log.Info("HTTPMetrics initialized in httpingress")
	}

	go serv.checkLeases(ctx)

	return serv
}

func (h *Server) checkLeases(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.expireLeases(ctx)
		}
	}
}

func (h *Server) expireLeases(ctx context.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for app, ar := range h.apps {
		var newLeases []*lease

		for i, l := range ar.leases {
			if l.Uses == 0 {
				h.Log.Debug("expiring lease", "app", app, "url", l.Lease.URL)
				h.aa.ReleaseLease(ctx, l.Lease)
			} else {
				h.Log.Debug("lease still in use", "app", app, "url", l.Lease.URL, "uses", l.Uses)
				lease, err := h.aa.RenewLease(ctx, l.Lease)
				if err != nil {
					h.Log.Error("error renewing lease", "error", err, "app", app, "url", l.Lease.URL)
					continue
				}

				ar.leases[i].Lease = lease
				newLeases = append(newLeases, ar.leases[i])
			}
		}

		if len(newLeases) == 0 {
			h.Log.Debug("No application leases left", "app", app)
			delete(h.apps, app)
		} else {
			ar.leases = newLeases
		}
	}
}

func (h *Server) DeriveApp(host string) (string, bool) {
	if host == "" {
		return "", false
	}

	_, err := netip.ParseAddr(host)
	if err == nil {
		return "", false
	}

	if app, _, ok := strings.Cut(host, "."); ok {
		return app, true
	}

	// Ok, it's JUST a name, so let's try it.
	return host, true
}

type lease struct {
	Uses  int
	Lease *activator.Lease
}

func (h *Server) retainLease(ctx context.Context, app string, l *activator.Lease) *lease {
	h.mu.Lock()
	defer h.mu.Unlock()

	ll := &lease{
		Lease: l,
		Uses:  1,
	}

	ar, ok := h.apps[app]
	if ok {
		ar.leases = append(ar.leases, ll)
	} else {
		h.apps[app] = &appUsage{
			leases: []*lease{ll},
		}
	}

	return ll
}

func (h *Server) useLease(ctx context.Context, app string) (*lease, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	ar, ok := h.apps[app]
	if !ok {
		return nil, nil
	}

	if len(ar.leases) == 0 {
		return nil, nil
	}

	for _, l := range ar.leases {
		if l.Uses <= l.Lease.Size {
			l.Uses++
			return l, nil
		}
	}

	return nil, nil
}

func (h *Server) releaseLease(ctx context.Context, lease *lease) {
	h.mu.Lock()
	defer h.mu.Unlock()

	lease.Uses--
}

func (h *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	h.handler.ServeHTTP(w, req)
}

// handleRequest is the inner handler wrapped by TimeoutHandler
func (h *Server) handleRequest(w http.ResponseWriter, req *http.Request) {
	// Handle Miren server health check endpoint before routing
	// Using .well-known per RFC 8615 to avoid collision with app routes
	if req.URL.Path == "/.well-known/miren/health" {
		h.handleHealth(w, req)
		return
	}

	start := time.Now()

	var appName string
	var statusCode int
	var bytesWritten int

	rw := &responseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK, // default if not explicitly set
	}

	h.serveHTTPWithMetrics(rw, req, &appName)

	statusCode = rw.statusCode
	bytesWritten = rw.bytesWritten

	// TimeoutHandler writes 503 on timeout, but our metrics would show the
	// original status. Check if timeout occurred and override metrics accordingly.
	if err := req.Context().Err(); err == context.DeadlineExceeded {
		statusCode = http.StatusServiceUnavailable
		if appName == "" {
			appName = "unknown"
		}
	}

	if h.httpMetrics != nil {
		if appName == "" {
			appName = "unknown"
		}

		duration := time.Since(start)
		// Use background context to ensure metrics are recorded even if request context is cancelled
		metricsCtx := context.Background()
		err := h.httpMetrics.RecordRequest(metricsCtx, metrics.HTTPRequest{
			Timestamp:    start,
			App:          appName,
			Method:       req.Method,
			Path:         req.URL.Path,
			StatusCode:   statusCode,
			DurationMs:   duration.Milliseconds(),
			ResponseSize: int64(bytesWritten),
		})
		if err != nil {
			h.Log.Error("Failed to record HTTP request", "error", err, "app", appName)
		}
	}
}

func (h *Server) serveHTTPWithMetrics(w http.ResponseWriter, req *http.Request, appName *string) {
	onlyHost, _, err := net.SplitHostPort(req.Host)
	if err != nil {
		onlyHost = req.Host
	}

	ctx := req.Context()

	// CRITICAL TO KNOW
	// The context on requset is closed automaticaly when the client on the over side closes!
	// So if you're doing critical work, don't use this context! Use a separate context and ping
	// this one to figure out if you should continue with your critical work or clean up.

	// Use ingress client to lookup route
	route, err := h.ingressClient.Lookup(ctx, onlyHost)
	if err != nil {
		h.Log.Error("error looking up http route", "error", err, "host", onlyHost)
		http.Error(w, fmt.Sprintf("error looking up http route: %s", onlyHost), http.StatusInternalServerError)
		return
	}

	var targetAppId entity.Id
	var routeType string

	if route != nil {
		// Use the http route if found
		targetAppId = route.App
		routeType = "route"
		h.Log.Debug("using http route", "host", onlyHost, "app", targetAppId)
	} else {
		// No route found, try to find a default route
		h.Log.Debug("no http route found, checking for default route", "host", onlyHost)

		defaultRoute, err := h.ingressClient.LookupDefault(ctx)
		if err != nil {
			h.Log.Error("error looking up default route", "error", err)
			http.Error(w, fmt.Sprintf("no http route found: %s", onlyHost), http.StatusNotFound)
			return
		}

		if defaultRoute == nil {
			h.Log.Debug("no default route found", "host", onlyHost)
			http.Error(w, fmt.Sprintf("no http route found: %s", onlyHost), http.StatusNotFound)
			return
		}

		// Use the default route
		targetAppId = defaultRoute.App
		routeType = "default"
		h.Log.Debug("using default route", "host", onlyHost, "app", targetAppId)
	}

	// Get app details first to have the name for metrics
	gr, err := h.eac.Get(ctx, targetAppId.String())
	if err != nil {
		h.Log.Error("error looking up application", "error", err, "app", targetAppId)
		http.Error(w, fmt.Sprintf("error looking up application: %s", targetAppId), http.StatusInternalServerError)
		return
	}

	var app core_v1alpha.App
	app.Decode(gr.Entity().Entity())

	var appMD core_v1alpha.Metadata
	appMD.Decode(gr.Entity().Entity())

	// Store app name for metrics
	*appName = appMD.Name

	h.Log.Info("routing request", "host", onlyHost, "app", targetAppId, "name", *appName, "type", routeType)

	// Common lease handling logic
	curLease, err := h.useLease(ctx, targetAppId.String())
	if err != nil {
		h.Log.Error("error taking lease", "error", err, "app", targetAppId)
		http.Error(w, fmt.Sprintf("error taking lease: %s", targetAppId), http.StatusInternalServerError)
		return
	}

	if curLease != nil {
		h.Log.Info("using existing lease", "app", targetAppId, "url", curLease.Lease.URL)
		h.forwardToLease(ctx, w, req, curLease)
		return
	}

	if app.ActiveVersion == "" {
		h.Log.Debug("no active version for app", "app", targetAppId)
		http.Error(w, fmt.Sprintf("no active version for app: %s", targetAppId), http.StatusNotFound)
		return
	}

	vr, err := h.eac.Get(ctx, app.ActiveVersion.String())
	if err != nil {
		h.Log.Error("error looking up application version", "error", err, "version", app.ActiveVersion)
		http.Error(w, fmt.Sprintf("error looking up application version: %s", app.ActiveVersion), http.StatusInternalServerError)
		return
	}

	var av core_v1alpha.AppVersion
	av.Decode(vr.Entity().Entity())

	// Give lease acquisition a generous timeout to complete sandbox boot
	// even if the client request times out. This prevents dangling resources.
	actContext, actCancel := context.WithTimeout(context.Background(), leaseAcquisitionTimeout)
	defer actCancel()

	actLease, err := h.aa.AcquireLease(actContext, &av, "web")
	if err != nil {
		if errors.Is(err, activator.ErrSandboxDiedEarly) {
			h.Log.Error("sandbox died early while acquiring lease", "error", err, "app", targetAppId)
			http.Error(w, fmt.Sprintf("The application %s failed to boot. Please check the applications logs.\n", targetAppId), http.StatusRequestTimeout)
		} else {
			h.Log.Error("error acquiring lease", "error", err, "app", targetAppId)
			http.Error(w, fmt.Sprintf("error acquiring lease: %s", targetAppId), http.StatusInternalServerError)
		}

		return
	}

	if actLease == nil {
		h.Log.Debug("no lease available for app", "app", targetAppId)
		http.Error(w, fmt.Sprintf("no lease available for app: %s", targetAppId), http.StatusServiceUnavailable)
		return
	}

	localLease := h.retainLease(ctx, targetAppId.String(), actLease)

	defer h.releaseLease(ctx, localLease)

	he := &discovery.HTTPEndpoint{
		Host: actLease.URL,
	}

	he.ServeHTTP(w, req)
}

func (h *Server) forwardToLease(ctx context.Context, w http.ResponseWriter, req *http.Request, lease *lease) {
	defer h.releaseLease(ctx, lease)

	he := &discovery.HTTPEndpoint{
		Host: lease.Lease.URL,
	}

	he.ServeHTTP(w, req)
}

/*
func (h *LeaseHTTP) extractEndpoint(ctx context.Context, container containerd.Container) (discovery.Endpoint, error) {
	labels, err := container.Labels(ctx)
	if err == nil {
		if host, ok := labels[httpHostLabel]; ok {
			h.Log.Info("http endpoint found", "id", container.ID(), "host", host)
			var ep discovery.Endpoint

			if dir, ok := labels[staticDirLabel]; ok {
				h.Log.Info("using local container endpoint for static_dir", "id", container.ID())
				ep = &discovery.LocalContainerEndpoint{
					Log: h.Log,
					HTTP: discovery.HTTPEndpoint{
						Host: "http://" + host,
					},
					Client:    h.CC,
					Namespace: h.Namespace,
					Dir:       dir,
					Id:        container.ID(),
				}
			} else {
				ep = &discovery.HTTPEndpoint{
					Host: "http://" + host,
				}
			}

			return ep, nil
		}
	}

	return nil, fmt.Errorf("unable to derive endpoint")
}
*/

// responseWriter wraps http.ResponseWriter to capture status code and response size
type responseWriter struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.bytesWritten += n
	return n, err
}

// Unwrap returns the underlying ResponseWriter for middleware compatibility
func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// HealthResponse represents the JSON response for /health endpoint
type HealthResponse struct {
	Status string                 `json:"status"`
	Checks map[string]HealthCheck `json:"checks"`
}

// HealthCheck represents a single component health check
type HealthCheck struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// handleHealth responds to /.well-known/miren/health endpoint with component health checks
// Uses .well-known URI per RFC 8615 to avoid collision with application routes
func (h *Server) handleHealth(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	response := HealthResponse{
		Status: "healthy",
		Checks: make(map[string]HealthCheck),
	}

	// Check etcd connection by listing apps (lightweight query)
	if h.eac != nil {
		_, err := h.eac.List(ctx, entity.Ref(entity.EntityKind, core_v1alpha.KindApp))
		if err != nil {
			response.Status = "unhealthy"
			response.Checks["etcd"] = HealthCheck{
				Status: "unhealthy",
				Error:  err.Error(),
			}
		} else {
			response.Checks["etcd"] = HealthCheck{
				Status: "healthy",
			}
		}
	}

	// Set response headers and status
	w.Header().Set("Content-Type", "application/json")
	if response.Status == "healthy" {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	// Encode response as JSON
	json.NewEncoder(w).Encode(response)
}
