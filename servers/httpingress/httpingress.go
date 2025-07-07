package httpingress

import (
	"context"
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

type Server struct {
	Log *slog.Logger

	rpcClient     rpc.Client
	eac           *entityserver_v1alpha.EntityAccessClient
	ingressClient *ingress.Client
	appClient     *app.Client

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
	rpcClient rpc.Client,
	aa activator.AppActivator,
	httpMetrics *metrics.HTTPMetrics,
) *Server {
	eac := entityserver_v1alpha.NewEntityAccessClient(rpcClient)

	serv := &Server{
		Log:           log.With("module", "httpingress"),
		rpcClient:     rpcClient,
		eac:           eac,
		ingressClient: ingress.NewClient(log, rpcClient),
		appClient:     app.NewClient(log, rpcClient),
		aa:            aa,
		httpMetrics:   httpMetrics,
		apps:          make(map[string]*appUsage),
	}

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

	h.Log.Debug("starting check lease routine")
	for {
		select {
		case <-ctx.Done():
			h.Log.Debug("context done, stopping lease checker")
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
			h.Log.Debug("App still has leases", "app", app, "leaseCount", len(newLeases))
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
	// Start tracking request time
	start := time.Now()

	// Wrap response writer to capture status and size
	rw := &responseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK, // default if not explicitly set
	}

	// Variable to capture app name from serveHTTPWithMetrics
	var appName string

	// Ensure we record metrics at the end
	defer func() {
		if h.httpMetrics != nil && appName != "" {
			h.recordHTTPMetrics(appName, req, rw, start)
		}
	}()

	h.serveHTTPWithMetrics(rw, req, &appName)
}

func (h *Server) recordHTTPMetrics(appName string, req *http.Request, rw *responseWriter, start time.Time) {
	duration := time.Since(start)
	err := h.httpMetrics.RecordRequest(req.Context(), metrics.HTTPRequest{
		Timestamp:    start,
		App:          appName,
		Method:       req.Method,
		Path:         req.URL.Path,
		StatusCode:   rw.statusCode,
		DurationMs:   duration.Milliseconds(),
		ResponseSize: int64(rw.bytesWritten),
	})
	if err != nil {
		h.Log.Error("Failed to record HTTP request", "error", err, "app", appName)
	}
}

func (h *Server) serveHTTPWithMetrics(w http.ResponseWriter, req *http.Request, appName *string) {
	onlyHost, _, err := net.SplitHostPort(req.Host)
	if err != nil {
		onlyHost = req.Host
	}

	ctx := req.Context()

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

	actLease, err := h.aa.AcquireLease(ctx, &av, "http", "web")
	if err != nil {
		h.Log.Error("error acquiring lease", "error", err, "app", targetAppId)
		http.Error(w, fmt.Sprintf("error acquiring lease: %s", targetAppId), http.StatusInternalServerError)
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

const (
	appLabel       = "runtime.computer/app"
	httpHostLabel  = "runtime.computer/http_host"
	staticDirLabel = "runtime.computer/static_dir"
)

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
