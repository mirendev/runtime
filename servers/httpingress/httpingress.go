package httpingress

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"miren.dev/runtime/api/app"
	computeapi "miren.dev/runtime/api/core"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/httpingress/httpingress_v1alpha"
	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/components/activator"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/entity"
	ephemeralx "miren.dev/runtime/pkg/ephemeral"
	"miren.dev/runtime/pkg/httputil"
	"miren.dev/runtime/pkg/oidc"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/waf"
	"miren.dev/runtime/pkg/workloadidentity"
)

// idleTimeoutConn wraps a net.Conn and sets a read deadline before each
// Read call. If no data arrives within the idle timeout, the read fails
// with a timeout error. Each successful read resets the deadline, so
// active streams (SSE, WebSocket, chunked) are unaffected as long as
// data keeps flowing.
type idleTimeoutConn struct {
	net.Conn
	idleTimeout time.Duration
}

func (c *idleTimeoutConn) Read(p []byte) (int, error) {
	c.SetReadDeadline(time.Now().Add(c.idleTimeout))
	return c.Conn.Read(p)
}

var httpingressTracer = otel.Tracer("miren.dev/runtime/httpingress")

const (
	timeoutMessage = "Request timeout"
	// leaseAcquisitionTimeout is the maximum time to wait for sandbox boot.
	// It runs on its own context rather than the request's, so a client that
	// gives up first still leaves a booting sandbox to finish rather than
	// stranding it. Unrelated to the request timeout, which a route can now
	// raise past this.
	leaseAcquisitionTimeout = 2 * time.Minute
	// minLeaseTTL is the minimum time a lease is kept in cache after its last
	// use before it becomes eligible for eviction. This prevents low-traffic
	// apps from having their leases evicted on every 30s tick, which would
	// force every request through the full entity store + activator pipeline.
	minLeaseTTL = 5 * time.Minute
)

type IngressConfig struct {
	RequestTimeout time.Duration
	DataPath       string
	WorkloadIssuer *workloadidentity.Issuer
}

type Server struct {
	Log *slog.Logger

	config        IngressConfig
	rpcClient     rpc.Client
	eac           *entityserver_v1alpha.EntityAccessClient
	ingressClient *ingress.Client
	appClient     *app.Client

	aa        activator.AppActivator
	transport http.RoundTripper

	// transports memoizes one proxy transport per distinct per-route timeout
	// override. Each needs its own connection pool: Go keys pooled connections
	// by (scheme, host) only, so a connection dialed under a 10m idle deadline
	// would otherwise be handed to a request that wants the 60s default.
	transportMu sync.Mutex
	transports  map[time.Duration]http.RoundTripper

	// versionConfigs keeps a bounded cache of immutable AppVersion and
	// ConfigVersion data. The app entity is still resolved for every request so
	// an active-version switch takes effect immediately, while the metrics
	// privacy check does not add a ConfigVersion lookup to every request.
	versionConfigs *lru.Cache[entity.Id, *cachedVersionConfig]

	httpMetrics *metrics.HTTPMetrics
	logWriter   observability.LogWriter

	mu   sync.Mutex
	apps map[string]*appUsage

	oidcSessionManager *oidc.SessionManager
	oidcMu             sync.RWMutex
	oidcHandlers       map[string]*oidcHandler

	wafEngine       *waf.Engine
	wafProfileMu    sync.RWMutex
	wafProfileCache map[entity.Id]*wafProfileEntry

	passwordMu       sync.RWMutex
	passwordHandlers map[string]*passwordHandler

	connectorMu       sync.RWMutex
	connectorHandlers map[string]*connectorHandler

	workloadIssuer *workloadidentity.Issuer
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
	logWriter observability.LogWriter,
) *Server {
	eac := entityserver_v1alpha.NewEntityAccessClient(rpcClient)

	if config.RequestTimeout <= 0 {
		if config.RequestTimeout < 0 {
			log.Warn("invalid request timeout; using default 60s", "configured", config.RequestTimeout)
		}
		config.RequestTimeout = 60 * time.Second
	}

	var signingKey []byte
	if config.DataPath != "" {
		var err error
		signingKey, err = loadOrGenerateSigningKey(config.DataPath)
		if err != nil {
			log.Error("failed to load OIDC signing key, sessions will not survive restarts", "error", err)
		}
	}

	serv := &Server{
		Log:                log.With("module", "httpingress"),
		config:             config,
		rpcClient:          rpcClient,
		eac:                eac,
		ingressClient:      ingress.NewClient(log, rpcClient),
		appClient:          app.NewClient(log, rpcClient),
		aa:                 aa,
		transport:          newProxyTransport(config.RequestTimeout),
		transports:         make(map[time.Duration]http.RoundTripper),
		httpMetrics:        httpMetrics,
		logWriter:          logWriter,
		apps:               make(map[string]*appUsage),
		oidcSessionManager: oidc.NewSessionManager(false, "", signingKey),
		oidcHandlers:       make(map[string]*oidcHandler),
		wafEngine:          waf.NewEngine(log.With("component", "waf")),
		wafProfileCache:    make(map[entity.Id]*wafProfileEntry),
		passwordHandlers:   make(map[string]*passwordHandler),
		connectorHandlers:  make(map[string]*connectorHandler),
		workloadIssuer:     config.WorkloadIssuer,
	}
	serv.versionConfigs, _ = lru.New[entity.Id, *cachedVersionConfig](256)

	if httpMetrics == nil {
		serv.Log.Warn("HTTPMetrics is nil in httpingress")
	} else {
		serv.Log.Debug("HTTPMetrics initialized in httpingress")
	}

	go serv.checkLeases(ctx)
	go serv.watchInvalidations(ctx)

	return serv
}

// newProxyTransport builds a transport that gives up on a proxied request once
// it has been silent for timeout. Two knobs enforce that: ResponseHeaderTimeout
// bounds the wait for the app's response headers, and idleTimeoutConn bounds
// the gap between reads once bytes are flowing.
func newProxyTransport(timeout time.Duration) http.RoundTripper {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.ResponseHeaderTimeout = timeout
	t.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		return &idleTimeoutConn{Conn: conn, idleTimeout: timeout}, nil
	}
	return t
}

// transportFor returns the transport enforcing the given request timeout,
// building and memoizing one the first time a route asks for a value other than
// the server default. The number of distinct transports is bounded by the
// number of distinct per-route overrides an operator has configured.
func (h *Server) transportFor(timeout time.Duration) http.RoundTripper {
	if timeout <= 0 || timeout == h.config.RequestTimeout {
		return h.transport
	}

	h.transportMu.Lock()
	defer h.transportMu.Unlock()

	if t, ok := h.transports[timeout]; ok {
		return t
	}

	h.Log.Info("building proxy transport for per-route request timeout", "timeout", timeout)
	t := newProxyTransport(timeout)
	h.transports[timeout] = t
	return t
}

// routeRequestTimeout resolves a route's request timeout override, falling back
// to the server default (0 here, which transportFor reads as "use the default")
// on empty, invalid, or non-positive values. SetRouteRequestTimeout rejects
// those at the write boundary, so a typo can only arrive by editing the entity
// directly, and quietly ignoring it beats failing the request.
func routeRequestTimeout(route *ingress_v1alpha.HttpRoute) time.Duration {
	if route == nil || route.RequestTimeout == "" {
		return 0
	}

	d, err := time.ParseDuration(route.RequestTimeout)
	if err != nil || d <= 0 {
		return 0
	}

	return d
}

// watchInvalidations listens for sandbox invalidation signals from the
// activator and immediately drops any cached leases pointing to dead sandboxes.
// This prevents requests from being routed to sandboxes that are shutting down.
func (h *Server) watchInvalidations(ctx context.Context) {
	ch := h.aa.Invalidations()
	for {
		select {
		case <-ctx.Done():
			return
		case inv, ok := <-ch:
			if !ok {
				return
			}
			h.invalidateSandboxLeases(ctx, inv.SandboxID)
		}
	}
}

// invalidateSandboxLeases removes all cached leases that point to the given sandbox.
func (h *Server) invalidateSandboxLeases(ctx context.Context, sandboxID entity.Id) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for app, ar := range h.apps {
		var kept []*lease
		for _, l := range ar.leases {
			if l.Lease.Sandbox().ID == sandboxID {
				h.Log.Info("invalidating cached lease for stopped sandbox",
					"app", app, "sandbox", sandboxID, "url", l.Lease.URL)
				h.aa.ReleaseLease(ctx, l.Lease)
			} else {
				kept = append(kept, l)
			}
		}
		if len(kept) == 0 {
			delete(h.apps, app)
		} else {
			ar.leases = kept
		}
	}
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
			if l.Uses == 0 && time.Since(l.LastUsed) > minLeaseTTL {
				h.Log.Debug("expiring lease", "app", app, "url", l.Lease.URL)
				h.aa.ReleaseLease(ctx, l.Lease)
				continue
			}

			// Renew all retained leases — both active (Uses > 0) and idle
			// but within TTL. This validates with the activator that the
			// sandbox is still alive, so we never serve a stale route.
			lease, err := h.aa.RenewLease(ctx, l.Lease)
			if err != nil {
				h.Log.Error("error renewing lease", "error", err, "app", app, "url", l.Lease.URL)
				h.aa.ReleaseLease(ctx, l.Lease)
				continue
			}

			ar.leases[i].Lease = lease
			newLeases = append(newLeases, ar.leases[i])
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
	Uses     int
	LastUsed time.Time
	Lease    *activator.Lease
}

func (h *Server) retainLease(ctx context.Context, app string, l *activator.Lease) *lease {
	h.mu.Lock()
	defer h.mu.Unlock()

	ll := &lease{
		Lease:    l,
		Uses:     1,
		LastUsed: time.Now(),
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
			l.LastUsed = time.Now()
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

// invalidateLease removes a lease from the cache entirely.
// This is called when a proxy error indicates the sandbox is dead.
// The lease is removed immediately rather than waiting for expireLeases.
func (h *Server) invalidateLease(ctx context.Context, app string, lease *lease) {
	h.mu.Lock()
	defer h.mu.Unlock()

	ar, ok := h.apps[app]
	if !ok {
		return
	}

	for i, l := range ar.leases {
		if l == lease {
			h.Log.Info("invalidating stale lease due to proxy error", "app", app, "url", lease.Lease.URL)
			// Release the lease back to the activator
			h.aa.ReleaseLease(ctx, l.Lease)
			// Remove from our cache
			ar.leases = append(ar.leases[:i], ar.leases[i+1:]...)
			break
		}
	}

	if len(ar.leases) == 0 {
		delete(h.apps, app)
	}
}

// invalidateAppLeases removes all cached leases for an app.
// During rollover, multiple cached leases may point to the same dead sandbox,
// so invalidating all of them ensures the retry acquires a fresh lease.
func (h *Server) invalidateAppLeases(ctx context.Context, app string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	ar, ok := h.apps[app]
	if !ok {
		return
	}

	for _, l := range ar.leases {
		h.Log.Info("invalidating stale lease due to proxy error", "app", app, "url", l.Lease.URL)
		h.aa.ReleaseLease(ctx, l.Lease)
	}

	delete(h.apps, app)
}

func (h *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	h.handleRequest(w, req)
}

func (h *Server) handleRequest(w http.ResponseWriter, req *http.Request) {
	defer func() {
		if r := recover(); r != nil {
			h.Log.Error("panic in request handler",
				"error", r,
				"stack", string(debug.Stack()),
				"method", req.Method,
				"path", req.URL.Path,
				"host", req.Host,
			)
			panic(r)
		}
	}()

	// Extract inbound trace context (traceparent/tracestate headers)
	ctx := otel.GetTextMapPropagator().Extract(req.Context(), propagation.HeaderCarrier(req.Header))

	ctx, span := httpingressTracer.Start(ctx, "httpingress",
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			attribute.String("http.method", req.Method),
			attribute.String("url.path", req.URL.Path),
			attribute.String("server.address", req.Host),
		))
	defer span.End()
	req = req.WithContext(ctx)

	// Handle OIDC discovery — only on the issuer's own hostname to avoid
	// shadowing apps that serve their own /.well-known/openid-configuration
	if req.URL.Path == "/.well-known/openid-configuration" && h.isIssuerHost(req.Host) {
		h.handleOIDCDiscovery(w, req)
		return
	}

	// Handle Miren server health check endpoint before routing
	// Using .well-known per RFC 8615 to avoid collision with app routes
	if req.URL.Path == "/.well-known/miren/health" {
		h.handleHealth(w, req)
		return
	}
	if req.URL.Path == "/.well-known/miren/jwks" {
		h.handleJWKS(w, req)
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

	span.SetAttributes(
		attribute.Int("http.response.status_code", statusCode),
		attribute.String("miren.app.name", appName),
	)

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
	// Block public access to the admin endpoint - it's only accessible via internal requests
	if req.URL.Path == "/.well-known/miren/admin" {
		http.NotFound(w, req)
		return
	}

	onlyHost, _, err := net.SplitHostPort(req.Host)
	if err != nil {
		onlyHost = req.Host
	}

	ctx := req.Context()

	// CRITICAL TO KNOW
	// The context on requset is closed automaticaly when the client on the over side closes!
	// So if you're doing critical work, don't use this context! Use a separate context and ping
	// this one to figure out if you should continue with your critical work or clean up.

	// Use ingress client to lookup route (with wildcard fallback)
	route, err := h.ingressClient.LookupWithWildcard(ctx, onlyHost)
	if err != nil {
		h.Log.Error("error looking up http route", "error", err, "host", onlyHost)
		http.Error(w, fmt.Sprintf("error looking up http route: %s", onlyHost), http.StatusInternalServerError)
		return
	}

	var targetAppId entity.Id
	var service = "web"
	var routeType string
	var ephemeralLabel string

	if route != nil {
		// Exact or wildcard route matched
		targetAppId = route.App
		service = routeService(route)
		routeType = "route"

		// Check for ephemeral subdomain label (only relevant for wildcard routes)
		ephemeralLabel = ingress.ExtractSubdomainLabel(onlyHost, route.Host)
	} else if label, baseRoute, err := h.lookupEphemeralRoute(ctx, onlyHost); err == nil && baseRoute != nil {
		// No exact or wildcard match, but stripping the first subdomain label
		// matched an existing route — this is an ephemeral subdomain request.
		route = baseRoute
		targetAppId = baseRoute.App
		service = routeService(baseRoute)
		routeType = "route"
		ephemeralLabel = label
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

		route = defaultRoute
		targetAppId = defaultRoute.App
		service = routeService(defaultRoute)
		routeType = "default"
		h.Log.Debug("using default route", "host", onlyHost, "app", targetAppId)
	}

	requestTimeout := routeRequestTimeout(route)

	var target *resolvedIngressTarget
	prepare := func(w http.ResponseWriter, r *http.Request) bool {
		// A wildcard route also serves as a multi-tenant subdomain, so a failed
		// ephemeral lookup should fall back to the active version rather than 404.
		wildcardRoute := route != nil && ingress.IsWildcardHost(route.Host)
		resolved, ok := h.resolveIngressTarget(w, r, targetAppId, ephemeralLabel, wildcardRoute)
		if !ok {
			return false
		}
		target = resolved
		*appName = target.appMetadata.Name

		if privateMetricsPath(target.config, service, r.URL.Path) {
			http.NotFound(w, r)
			return false
		}
		return true
	}

	handler := h.buildRouteHandler(route, appName, prepare, func(w http.ResponseWriter, r *http.Request) {
		h.serveAuthenticatedRequest(w, r, targetAppId, service, routeType, target, appName, requestTimeout)
	})

	handler(w, req)
}

type resolvedIngressTarget struct {
	app               core_v1alpha.App
	appMetadata       core_v1alpha.Metadata
	version           core_v1alpha.AppVersion
	config            *core_v1alpha.ConfigSpec
	ephemeralLabel    string
	ephemeralResolved bool
}

type cachedVersionConfig struct {
	version core_v1alpha.AppVersion
	config  core_v1alpha.ConfigSpec
}

func (h *Server) resolveIngressTarget(w http.ResponseWriter, req *http.Request, appID entity.Id, ephemeralLabel string, wildcardRoute bool) (*resolvedIngressTarget, bool) {
	ctx := req.Context()
	gr, err := h.eac.Get(ctx, appID.String())
	if err != nil {
		h.Log.Error("error looking up application", "error", err, "app", appID)
		http.Error(w, fmt.Sprintf("error looking up application: %s", appID), http.StatusInternalServerError)
		return nil, false
	}

	target := &resolvedIngressTarget{ephemeralLabel: ephemeralLabel}
	target.app.Decode(gr.Entity().Entity())
	target.appMetadata.Decode(gr.Entity().Entity())

	strategy := resolveVersionStrategy(ephemeralLabel, wildcardRoute)
	if strategy != resolveActive {
		ephemeralVersion, err := ephemeralx.LookupByLabel(ctx, h.eac, appID, ephemeralLabel)
		if err != nil {
			h.Log.Error("error looking up ephemeral version", "error", err, "label", ephemeralLabel)
			http.Error(w, fmt.Sprintf("error looking up ephemeral version: %s", ephemeralLabel), http.StatusInternalServerError)
			return nil, false
		}
		if ephemeralVersion == nil && strategy == resolveEphemeralStrict {
			h.Log.Debug("no ephemeral version found", "label", ephemeralLabel, "app", appID)
			http.Error(w, fmt.Sprintf("ephemeral version %q not found or has expired", ephemeralLabel), http.StatusNotFound)
			return nil, false
		}
		if ephemeralVersion != nil {
			resolved, err := h.resolveVersionConfig(ctx, ephemeralVersion.ID, ephemeralVersion)
			if err != nil {
				h.Log.Error("error resolving ephemeral application configuration", "error", err, "version", ephemeralVersion.ID)
				http.Error(w, "error resolving application configuration", http.StatusInternalServerError)
				return nil, false
			}
			target.version = resolved.version
			target.config = &resolved.config
			target.ephemeralResolved = true
			return target, true
		}
	}

	if target.app.ActiveVersion == "" {
		h.Log.Debug("no active version for app", "app", appID)
		http.Error(w, fmt.Sprintf("no active version for app: %s", appID), http.StatusNotFound)
		return nil, false
	}

	resolved, err := h.resolveVersionConfig(ctx, target.app.ActiveVersion, nil)
	if err != nil {
		h.Log.Error("error resolving active application configuration", "error", err, "version", target.app.ActiveVersion)
		http.Error(w, "error resolving application configuration", http.StatusInternalServerError)
		return nil, false
	}
	target.version = resolved.version
	target.config = &resolved.config
	return target, true
}

func (h *Server) resolveVersionConfig(ctx context.Context, versionID entity.Id, known *core_v1alpha.AppVersion) (*cachedVersionConfig, error) {
	if h.versionConfigs != nil {
		if cached, ok := h.versionConfigs.Get(versionID); ok {
			return cached, nil
		}
	}

	var version core_v1alpha.AppVersion
	if known != nil {
		version = *known
	} else {
		response, err := h.eac.Get(ctx, versionID.String())
		if err != nil {
			return nil, fmt.Errorf("looking up application version %s: %w", versionID, err)
		}
		version.Decode(response.Entity().Entity())
	}

	config, err := computeapi.ResolveConfig(ctx, h.eac, &version)
	if err != nil {
		return nil, fmt.Errorf("resolving configuration for version %s: %w", versionID, err)
	}
	resolved := &cachedVersionConfig{version: version, config: *config}
	if h.versionConfigs != nil {
		h.versionConfigs.Add(versionID, resolved)
	}
	return resolved, nil
}

func privateMetricsPath(config *core_v1alpha.ConfigSpec, service, requestPath string) bool {
	if config == nil {
		return false
	}
	// The public HTTP ingress proxies to the service the matched route
	// selects (see routeService), so the guard must consider only that
	// service's metrics block. A different service's configured path may
	// legitimately pass through (it is served elsewhere, not on the proxied
	// listener) and must not be reserved here.
	for _, svc := range config.Services {
		if svc.Name == service && svc.Metrics.Enabled && !svc.Metrics.Public && svc.Metrics.Path == requestPath {
			return true
		}
	}
	return false
}

// buildRouteHandler composes the per-request middleware chain around serve.
// Each call wraps the previous handler, so the last one applied runs first and
// the execution order is WAF → maintenance → preparation → auth → serve.
//
// Both boundaries are load-bearing. WAF stays outermost so a maintenance window
// doesn't become an open window for scanners. Maintenance runs ahead of auth
// because a holding page is public information: otherwise a visitor completes a
// full round trip to an identity provider only to be told the site is down, and
// an API client gets a 401 when the honest answer is 503. Preparation resolves
// the app target and hides private metrics before authentication without
// allowing either lookup to obscure a maintenance response.
//
// The order lives here, rather than inline, so it is something a test can hold
// onto — see TestMiddlewareChainOrder.
func (h *Server) buildRouteHandler(route *ingress_v1alpha.HttpRoute, appName *string, prepare func(http.ResponseWriter, *http.Request) bool, serve http.HandlerFunc) http.HandlerFunc {
	handler := serve

	handler = h.authMiddleware(route, handler)

	if prepare != nil {
		next := handler
		handler = func(w http.ResponseWriter, r *http.Request) {
			if prepare(w, r) {
				next(w, r)
			}
		}
	}

	handler = h.maintenanceMiddleware(route, appName, handler)

	handler = h.wafMiddleware(route, handler)

	return handler
}

// lookupEphemeralRoute checks whether the request host is an ephemeral
// subdomain of an existing route. It strips the first DNS label and looks up
// the remainder. For example, "feat-x.app.example.com" strips to
// "app.example.com". If that matches a route, it returns the label ("feat-x")
// and the matched route. This allows ephemeral subdomains to work with normal
// (non-wildcard) routes — the user only needs a wildcard DNS record, not a
// wildcard route entity.
func (h *Server) lookupEphemeralRoute(ctx context.Context, host string) (string, *ingress_v1alpha.HttpRoute, error) {
	idx := strings.Index(host, ".")
	if idx <= 0 || idx == len(host)-1 {
		return "", nil, nil
	}

	label := host[:idx]
	base := host[idx+1:]

	route, err := h.ingressClient.LookupWithWildcard(ctx, base)
	if err != nil {
		return "", nil, err
	}
	if route == nil {
		return "", nil, nil
	}

	return label, route, nil
}

func (h *Server) authMiddleware(route *ingress_v1alpha.HttpRoute, next http.HandlerFunc) http.HandlerFunc {
	if entity.Empty(route.AuthProvider) {
		return next
	}

	return func(w http.ResponseWriter, r *http.Request) {
		resp, err := h.eac.Get(r.Context(), string(route.AuthProvider))
		if err != nil {
			h.Log.Error("failed to get auth provider entity", "error", err, "provider", route.AuthProvider)
			http.Error(w, "Authentication service unavailable", http.StatusServiceUnavailable)
			return
		}

		ent := resp.Entity().Entity()

		switch {
		case entity.Is(ent, ingress_v1alpha.KindOidcProvider):
			// The oidc_provider entity backs both OIDC discovery clients
			// and connector-based providers; dispatch on connector_type.
			var op ingress_v1alpha.OidcProvider
			op.Decode(ent)
			if op.ConnectorType != "" && op.ConnectorType != "oidc" {
				h.connectorMiddleware(route, ent, next)(w, r)
			} else {
				h.oidcMiddleware(route, ent, next)(w, r)
			}
		case entity.Is(ent, ingress_v1alpha.KindPasswordProvider):
			h.passwordMiddleware(route, ent, next)(w, r)
		default:
			h.Log.Error("unknown auth provider kind", "provider", route.AuthProvider)
			http.Error(w, "Authentication service unavailable", http.StatusServiceUnavailable)
		}
	}
}

// versionResolution describes how a request resolves to an app version.
type versionResolution int

const (
	resolveActive            versionResolution = iota // serve app.ActiveVersion
	resolveEphemeralStrict                            // resolve ephemeral by label; 404 if missing
	resolveEphemeralOrActive                          // resolve ephemeral by label; fall back to active if missing
)

// resolveVersionStrategy decides how to resolve the app version for a request.
// A request with no ephemeral label always serves the active version. When an
// ephemeral label is present, a wildcard route (which doubles as a multi-tenant
// subdomain) falls back to the active version if the label does not resolve,
// while a non-wildcard route stays strict and 404s on a miss.
func resolveVersionStrategy(ephemeralLabel string, wildcardRoute bool) versionResolution {
	if ephemeralLabel == "" {
		return resolveActive
	}
	if wildcardRoute {
		return resolveEphemeralOrActive
	}
	return resolveEphemeralStrict
}

// routeService returns the selected app service. Empty is the on-disk
// representation of the historical web-only route and remains web-compatible.
func routeService(route *ingress_v1alpha.HttpRoute) string {
	if route == nil || route.Service == "" {
		return "web"
	}
	return route.Service
}

// leaseCacheKey returns the lease cache key for a request. A request that
// resolved an ephemeral version is scoped per label so it never shares a lease
// with the active version or another label. Everything else, including a
// wildcard subdomain that fell back to the active version, shares the app-service
// base key so those requests reuse one active-version lease pool instead of
// fragmenting into a per-tenant entry. Every key includes the selected service.
func leaseCacheKey(appID entity.Id, service, ephemeralLabel string, ephemeralResolved bool) string {
	key := appID.String() + ":service:" + service
	if ephemeralResolved {
		return key + ":eph:" + ephemeralLabel
	}
	return key
}

// serveAuthenticatedRequest handles the request after authentication (if any)
func (h *Server) serveAuthenticatedRequest(w http.ResponseWriter, req *http.Request, targetAppId entity.Id, service, routeType string, target *resolvedIngressTarget, appName *string, requestTimeout time.Duration) {
	ctx := req.Context()
	ephemeralLabel := target.ephemeralLabel

	ctx, leaseSpan := httpingressTracer.Start(ctx, "httpingress.lease",
		trace.WithAttributes(
			attribute.String("miren.app.id", targetAppId.String()),
			attribute.String("miren.app.name", *appName),
			attribute.String("miren.route.type", routeType),
		))
	defer leaseSpan.End()

	// Resolve the version identity up front so the lease cache key reflects
	// the version actually served. A resolved ephemeral version scopes the key
	// per-label; an unresolved label on a wildcard route falls back to the
	// active version and shares its lease pool rather than fragmenting into a
	// per-tenant entry. Label-free requests skip the lookup and stay on the
	// fast active path.
	leaseKey := leaseCacheKey(targetAppId, service, ephemeralLabel, target.ephemeralResolved)

	// Retry loop: if a cached lease fails with a connection error (stale sandbox),
	// invalidate all cached leases and retry once to acquire a fresh lease.
	const maxRetries = 1
	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Try to use a cached lease
		curLease, err := h.useLease(ctx, leaseKey)
		if err != nil {
			h.Log.Error("error taking lease", "error", err, "app", targetAppId)
			http.Error(w, fmt.Sprintf("error taking lease: %s", targetAppId), http.StatusInternalServerError)
			return
		}

		if curLease != nil {
			leaseSpan.SetAttributes(
				attribute.Bool("miren.lease.cached", true),
				attribute.String("miren.lease.url", curLease.Lease.URL),
			)
			req = req.WithContext(ctx)
			// On non-final attempts, suppress error response so we can retry
			writeErr := attempt == maxRetries
			err = h.proxyToLease(w, req, curLease.Lease.URL, targetAppId.String(), *appName, writeErr, requestTimeout)
			if err != nil && isProxyConnectionError(err) {
				// Cached lease pointed at a dead sandbox — invalidate all app
				// leases (they likely all point to the same dead sandbox) and retry
				h.invalidateAppLeases(context.Background(), leaseKey)
				h.Log.Warn("stale lease, retrying with fresh lease",
					"stale_url", curLease.Lease.URL,
					"attempt", attempt,
					"app", targetAppId)
				continue
			}
			if err != nil {
				h.invalidateLease(context.Background(), leaseKey, curLease)
			} else {
				h.releaseLease(ctx, curLease)
			}
			return
		}

		// No cached lease — acquire a fresh one
		leaseSpan.SetAttributes(attribute.Bool("miren.lease.cached", false))

		av := target.version

		if target.ephemeralResolved {
			leaseSpan.SetAttributes(
				attribute.String("miren.app.version", string(av.ID)),
				attribute.String("miren.ephemeral.label", ephemeralLabel),
			)
		} else {
			leaseSpan.SetAttributes(
				attribute.String("miren.app.version", target.version.ID.String()),
			)
		}

		// Give lease acquisition a generous timeout to complete sandbox boot
		// even if the client request times out. This prevents dangling resources.
		actContext, actCancel := context.WithTimeout(context.Background(), leaseAcquisitionTimeout)
		defer actCancel()

		actLease, err := h.aa.AcquireLease(actContext, &av, service)
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

		leaseSpan.SetAttributes(attribute.String("miren.lease.url", actLease.URL))

		localLease := h.retainLease(ctx, leaseKey, actLease)

		req = req.WithContext(ctx)
		// Fresh lease — always write error response (no retry on fresh lease failure)
		err = h.proxyToLease(w, req, actLease.URL, targetAppId.String(), *appName, true, requestTimeout)
		if err != nil {
			// Connection error on a fresh lease - the sandbox may have died
			// between lease acquisition and proxy. Invalidate immediately.
			h.invalidateLease(context.Background(), leaseKey, localLease)
		} else {
			h.releaseLease(ctx, localLease)
		}
		return
	}
}

func (h *Server) logRequestFromStats(appEntityID, appName string, stats httputil.ProxyStats) {
	if h.logWriter == nil {
		return
	}

	// Build full path with query string
	path := stats.RequestPath
	if stats.RequestQuery != "" {
		path = path + "?" + stats.RequestQuery
	}

	// logfmt, ordered for reading: lead with what you triage on (status, method,
	// path, latency), then the sizes and edge details.
	logMsg := fmt.Sprintf("status=%d method=%s path=\"%s\" duration_ms=%d response=%d body=%d host=%s source_ip=%s",
		stats.StatusCode, stats.RequestMethod, path, stats.Duration.Milliseconds(),
		stats.ResponseBytes, stats.ContentLength, stats.RequestHost, stats.RemoteAddr)

	err := h.logWriter.WriteEntry(appEntityID, observability.LogEntry{
		Timestamp: time.Now(),
		Stream:    observability.UserOOB,
		Body:      logMsg,
		Attributes: map[string]string{
			"source": "router",
			"method": stats.RequestMethod,
			"path":   stats.RequestPath,
			"host":   stats.RequestHost,
		},
	})
	if err != nil {
		h.Log.Error("failed to write request log entry", "error", err, "app", appName)
	}
}

// proxyToLease proxies the request to the target URL and returns any connection error.
// If the proxy fails with a connection error (connection refused, no route to host, etc.),
// it returns the error so the caller can invalidate the lease.
//
// When writeErrorResponse is false and a connection error occurs, the ErrorHandler
// captures the error but does NOT write to the ResponseWriter, allowing the caller
// to retry with a fresh lease. This is safe because connection errors happen during
// TCP dial, before any response bytes are sent.
func (h *Server) proxyToLease(w http.ResponseWriter, req *http.Request, targetURL, appEntityID, appName string, writeErrorResponse bool, requestTimeout time.Duration) error {
	targetParsed, err := url.Parse(targetURL)
	if err != nil {
		h.Log.Error("failed to parse target URL", "error", err, "url", targetURL)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return nil // Not a connection error, don't invalidate
	}

	// Capture any proxy error for the caller
	var proxyErr error

	proxy := &httputil.ReverseProxy{
		Transport: h.transportFor(requestTimeout),
		Director: func(outReq *http.Request) {
			outReq.URL.Scheme = targetParsed.Scheme
			outReq.URL.Host = targetParsed.Host

			// Set X-Forwarded-Proto to indicate the original protocol
			if req.TLS == nil {
				outReq.Header.Set("X-Forwarded-Proto", "http")
			} else {
				outReq.Header.Set("X-Forwarded-Proto", "https")
			}

			outReq.Header.Set("X-Forwarded-Host", req.Host)

			// Mark this as a public request (strip any client-provided value first)
			outReq.Header.Set("X-Miren-Access", "public")

			// Inject trace context so user apps can continue the trace
			otel.GetTextMapPropagator().Inject(outReq.Context(), propagation.HeaderCarrier(outReq.Header))
		},
		ErrorHandler: func(rw http.ResponseWriter, r *http.Request, err error) {
			proxyErr = err
			if !writeErrorResponse && isProxyConnectionError(err) {
				h.Log.Warn("proxy connection error to sandbox (will retry)", "error", err, "url", targetURL, "app", appName)
				return
			}
			if netErr, ok := errors.AsType[net.Error](err); ok && netErr.Timeout() {
				h.Log.Warn("request timeout", "url", targetURL, "app", appName)
				http.Error(rw, timeoutMessage, http.StatusServiceUnavailable)
				return
			}
			if isProxyConnectionError(err) {
				h.Log.Warn("proxy connection error to sandbox", "error", err, "url", targetURL, "app", appName)
			} else {
				h.Log.Error("proxy error", "error", err, "url", targetURL, "app", appName)
			}
			rw.WriteHeader(http.StatusBadGateway)
		},
		Callback: func(stats httputil.ProxyStats) {
			h.logRequestFromStats(appEntityID, appName, stats)
		},
	}

	proxy.ServeHTTP(w, req)

	// Return connection errors so the caller can invalidate the lease
	if proxyErr != nil && isProxyConnectionError(proxyErr) {
		return proxyErr
	}
	return nil
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

// isProxyConnectionError checks if an error indicates the backend is unreachable
// because a TCP connection was never established. This is used to trigger retries
// on stale cached leases, so it intentionally excludes ECONNRESET/ECONNABORTED —
// those indicate a connection that *was* established and may have partially
// processed the request, making it unsafe to retry non-idempotent methods.
func isProxyConnectionError(err error) bool {
	if err == nil {
		return false
	}

	// Check for net.OpError which wraps most connection failures
	if opErr, ok := errors.AsType[*net.OpError](err); ok {
		// Check for syscall errors (connection refused, no route to host, etc.)
		if syscallErr, ok := errors.AsType[*os.SyscallError](opErr.Err); ok {
			if errno, ok := syscallErr.Err.(syscall.Errno); ok {
				//exhaustive:ignore syscall.Errno has ~130 members; default handles the rest
				switch errno {
				case syscall.ECONNREFUSED: // connection refused
					return true
				case syscall.EHOSTUNREACH: // no route to host
					return true
				case syscall.ENETUNREACH: // network unreachable
					return true
				}
			}
		}
	}

	return false
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

// DoRequest handles internal HTTP requests to app sandboxes. This method reuses
// the same lease management infrastructure as the HTTP proxy but is called
// directly via Go method invocation rather than through the HTTP listener.
func (h *Server) DoRequest(ctx context.Context, req *httpingress_v1alpha.InternalHttpRequest) (*httpingress_v1alpha.InternalHttpResponse, error) {
	startTime := time.Now()
	resp := &httpingress_v1alpha.InternalHttpResponse{}

	// Validate required fields
	appId := req.AppId()
	if appId == "" {
		resp.SetError("app_id is required")
		return resp, nil
	}

	method := req.Method()
	if method == "" {
		method = "GET"
	}

	path := req.Path()
	if path == "" {
		path = "/"
	}

	service := req.Service()
	if service == "" {
		service = "web"
	}

	// Apply timeout if specified
	if req.TimeoutMs() > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(req.TimeoutMs())*time.Millisecond)
		defer cancel()
	}

	// Look up the app
	gr, err := h.eac.Get(ctx, appId)
	if err != nil {
		resp.SetError(fmt.Sprintf("error looking up app: %v", err))
		return resp, nil
	}

	var appEntity core_v1alpha.App
	appEntity.Decode(gr.Entity().Entity())

	if appEntity.ActiveVersion == "" {
		resp.SetError("no active version for app")
		return resp, nil
	}

	// Look up the app version
	vr, err := h.eac.Get(ctx, appEntity.ActiveVersion.String())
	if err != nil {
		resp.SetError(fmt.Sprintf("error looking up app version: %v", err))
		return resp, nil
	}

	var av core_v1alpha.AppVersion
	av.Decode(vr.Entity().Entity())

	// Try to use an existing lease, with retry on stale cached lease
	leaseKey := appId + ":service:" + service
	curLease, err := h.useLease(ctx, leaseKey)
	if err != nil {
		resp.SetError(fmt.Sprintf("error taking lease: %v", err))
		return resp, nil
	}

	// If we got a cached lease, try it — but retry with a fresh one on connection error
	if curLease != nil {
		httpResp, err := h.executeInternalRequest(ctx, curLease, req, method, path, appId)
		if err != nil && isProxyConnectionError(err) {
			// Stale cached lease — invalidate all cached leases for this app service and fall through to acquire fresh
			h.invalidateAppLeases(context.Background(), leaseKey)
			h.Log.Warn("stale lease on internal request, retrying with fresh lease",
				"stale_url", curLease.Lease.URL, "app", appId)
			curLease = nil
		} else if err != nil {
			h.releaseLease(ctx, curLease)
			resp.SetError(fmt.Sprintf("request failed: %v", err))
			return resp, nil
		} else {
			defer httpResp.Body.Close()
			h.releaseLease(ctx, curLease)
			return h.buildInternalResponse(resp, httpResp, appId, method, path, startTime)
		}
	}

	// No cached lease (or stale one was invalidated) — acquire fresh
	actContext, actCancel := context.WithTimeout(context.Background(), leaseAcquisitionTimeout)
	defer actCancel()

	actLease, err := h.aa.AcquireLease(actContext, &av, service)
	if err != nil {
		if errors.Is(err, activator.ErrSandboxDiedEarly) {
			resp.SetError("sandbox died early while acquiring lease")
		} else {
			resp.SetError(fmt.Sprintf("error acquiring lease: %v", err))
		}
		return resp, nil
	}

	if actLease == nil {
		resp.SetError("no lease available for app")
		return resp, nil
	}

	curLease = h.retainLease(ctx, leaseKey, actLease)

	// Execute the HTTP request with fresh lease (no retry)
	httpResp, err := h.executeInternalRequest(ctx, curLease, req, method, path, appId)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			h.releaseLease(ctx, curLease)
		} else if isProxyConnectionError(err) {
			h.invalidateLease(context.Background(), leaseKey, curLease)
		} else {
			h.releaseLease(ctx, curLease)
		}
		resp.SetError(fmt.Sprintf("request failed: %v", err))
		return resp, nil
	}
	defer httpResp.Body.Close()

	// Release the lease on success
	h.releaseLease(ctx, curLease)

	return h.buildInternalResponse(resp, httpResp, appId, method, path, startTime)
}

// TunnelConn represents a resolved connection to an app sandbox. The caller
// is responsible for calling Release when done to return the lease.
type TunnelConn struct {
	// URL is the base URL of the sandbox (e.g., "http://10.0.0.5:8080").
	URL   string
	AppID string

	server *Server
	lease  *lease
}

// Release returns the lease to the pool.
func (tc *TunnelConn) Release() {
	if tc.lease != nil {
		tc.server.releaseLease(context.Background(), tc.lease)
		tc.lease = nil
	}
}

// AcquireTunnel resolves a hostname to an app, acquires a lease to a running
// sandbox, and returns the sandbox URL. This is similar to DoRequest but
// doesn't execute a request — it gives the caller direct access to the
// sandbox URL for protocols that need custom connection handling (e.g.,
// WebSocket tunneling).
//
// The path parameter is checked against blocked paths (e.g., admin endpoints).
// If the route requires OIDC authentication, the tunnel is rejected since
// OIDC flows cannot be performed over tunneled connections.
func (h *Server) AcquireTunnel(ctx context.Context, hostname, path string) (*TunnelConn, error) {
	// Block access to internal miren endpoints (admin, OIDC callbacks, etc.)
	if strings.HasPrefix(path, "/.well-known/miren/") {
		return nil, fmt.Errorf("access to %s is not allowed via tunnel", path)
	}

	onlyHost, _, err := net.SplitHostPort(hostname)
	if err != nil {
		onlyHost = hostname
	}

	// Resolve hostname → app ID via ingress routes
	route, err := h.ingressClient.LookupWithWildcard(ctx, onlyHost)
	if err != nil {
		return nil, fmt.Errorf("route lookup failed for %s: %w", onlyHost, err)
	}

	var targetAppId entity.Id
	var service string
	if route != nil {
		targetAppId = route.App
		service = routeService(route)

		if !entity.Empty(route.AuthProvider) {
			return nil, fmt.Errorf("tunneling not supported for auth-protected routes (host: %s)", onlyHost)
		}
	} else {
		defaultRoute, err := h.ingressClient.LookupDefault(ctx)
		if err != nil {
			return nil, fmt.Errorf("default route lookup failed: %w", err)
		}
		if defaultRoute == nil {
			return nil, fmt.Errorf("no route found for %s", onlyHost)
		}
		if !entity.Empty(defaultRoute.AuthProvider) {
			return nil, fmt.Errorf("tunneling not supported for auth-protected routes (host: %s)", onlyHost)
		}
		targetAppId = defaultRoute.App
		service = routeService(defaultRoute)
	}

	appId := targetAppId.String()

	// Look up the app and its active version
	gr, err := h.eac.Get(ctx, appId)
	if err != nil {
		return nil, fmt.Errorf("app lookup failed for %s: %w", appId, err)
	}

	var appEntity core_v1alpha.App
	appEntity.Decode(gr.Entity().Entity())

	if appEntity.ActiveVersion == "" {
		return nil, fmt.Errorf("no active version for app %s", appId)
	}

	vr, err := h.eac.Get(ctx, appEntity.ActiveVersion.String())
	if err != nil {
		return nil, fmt.Errorf("app version lookup failed: %w", err)
	}

	var av core_v1alpha.AppVersion
	av.Decode(vr.Entity().Entity())

	// Try cached lease first
	leaseKey := appId + ":service:" + service
	curLease, err := h.useLease(ctx, leaseKey)
	if err != nil {
		return nil, fmt.Errorf("lease lookup failed: %w", err)
	}

	if curLease != nil {
		return &TunnelConn{
			URL:    curLease.Lease.URL,
			AppID:  appId,
			server: h,
			lease:  curLease,
		}, nil
	}

	// Acquire a fresh lease
	actContext, actCancel := context.WithTimeout(ctx, leaseAcquisitionTimeout)
	defer actCancel()

	actLease, err := h.aa.AcquireLease(actContext, &av, service)
	if err != nil {
		return nil, fmt.Errorf("lease acquisition failed: %w", err)
	}

	if actLease == nil {
		return nil, fmt.Errorf("no lease available for app %s", appId)
	}

	retained := h.retainLease(ctx, leaseKey, actLease)

	return &TunnelConn{
		URL:    actLease.URL,
		AppID:  appId,
		server: h,
		lease:  retained,
	}, nil
}

// buildInternalResponse populates the InternalHttpResponse from the HTTP response.
func (h *Server) buildInternalResponse(
	resp *httpingress_v1alpha.InternalHttpResponse,
	httpResp *http.Response,
	appId, method, path string,
	startTime time.Time,
) (*httpingress_v1alpha.InternalHttpResponse, error) {
	resp.SetStatusCode(int32(httpResp.StatusCode))

	// Copy response headers
	var respHeaders []*httpingress_v1alpha.HttpHeader
	for key, values := range httpResp.Header {
		for _, value := range values {
			hdr := &httpingress_v1alpha.HttpHeader{}
			hdr.SetKey(key)
			hdr.SetValue(value)
			respHeaders = append(respHeaders, hdr)
		}
	}
	if len(respHeaders) > 0 {
		resp.SetHeaders(respHeaders)
	}

	// Read response body
	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		resp.SetError(fmt.Sprintf("error reading response body: %v", err))
		return resp, nil
	}
	resp.SetBody(&body)

	// Log the internal request
	h.logInternalRequest(appId, method, path, int(httpResp.StatusCode), len(body), startTime)

	return resp, nil
}

// logInternalRequest logs an internal HTTP request in a format similar to public requests
func (h *Server) logInternalRequest(appEntityID, method, path string, statusCode, responseBytes int, startTime time.Time) {
	if h.logWriter == nil {
		return
	}

	duration := time.Since(startTime)

	// logfmt, same reader-first order as public requests, tagged access=internal.
	logMsg := fmt.Sprintf("status=%d method=%s path=\"%s\" access=internal duration_ms=%d response=%d",
		statusCode, method, path, duration.Milliseconds(), responseBytes)

	err := h.logWriter.WriteEntry(appEntityID, observability.LogEntry{
		Timestamp: time.Now(),
		Stream:    observability.UserOOB,
		Body:      logMsg,
		Attributes: map[string]string{
			"source": "router",
			"access": "internal",
			"method": method,
			"path":   path,
		},
	})
	if err != nil {
		h.Log.Error("failed to write internal request log entry", "error", err, "app", appEntityID)
	}
}

// executeInternalRequest performs the actual HTTP request to the sandbox
func (h *Server) executeInternalRequest(
	ctx context.Context,
	lease *lease,
	req *httpingress_v1alpha.InternalHttpRequest,
	method, path, appId string,
) (*http.Response, error) {
	targetURL := lease.Lease.URL + path

	var bodyReader io.Reader
	if req.HasBody() && req.Body() != nil {
		bodyReader = bytes.NewReader(*req.Body())
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, targetURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Copy headers from the request
	for _, hdr := range req.Headers() {
		httpReq.Header.Add(hdr.Key(), hdr.Value())
	}

	// Mark this as an internal request
	httpReq.Header.Set("X-Miren-Access", "internal")

	// Execute the request
	client := &http.Client{
		Timeout: h.config.RequestTimeout,
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		if isProxyConnectionError(err) {
			return nil, err
		}
		return nil, fmt.Errorf("request failed: %w", err)
	}

	return resp, nil
}
