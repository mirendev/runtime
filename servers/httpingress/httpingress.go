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

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/components/activator"
	"miren.dev/runtime/discovery"
	"miren.dev/runtime/pkg/entity"
)

type Server struct {
	Log *slog.Logger

	eac *entityserver_v1alpha.EntityAccessClient

	aa activator.AppActivator

	mu   sync.Mutex
	apps map[string]*appUsage
}

type appUsage struct {
	leases []*lease
}

func NewServer(
	ctx context.Context,
	log *slog.Logger,
	eac *entityserver_v1alpha.EntityAccessClient,
	aa activator.AppActivator,
) *Server {
	serv := &Server{
		Log:  log.With("module", "httpingress"),
		eac:  eac,
		aa:   aa,
		apps: make(map[string]*appUsage),
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
	onlyHost, _, err := net.SplitHostPort(req.Host)
	if err != nil {
		onlyHost = req.Host
	}

	ctx := req.Context()

	ia := entity.String(ingress_v1alpha.HttpRouteHostId, onlyHost)

	resp, err := h.eac.List(ctx, ia)
	if err != nil {
		h.Log.Error("error looking up http route", "error", err, "host", onlyHost)
		http.Error(w, fmt.Sprintf("error looking up http route: %s", onlyHost), http.StatusInternalServerError)
		return
	}

	vals := resp.Values()

	if len(vals) == 0 {
		h.Log.Debug("no http route found", "host", onlyHost)
		http.Error(w, fmt.Sprintf("no http route found: %s", onlyHost), http.StatusNotFound)
		return
	}

	var hr ingress_v1alpha.HttpRoute
	hr.Decode(vals[0].Entity())

	curLease, err := h.useLease(ctx, hr.App.String())
	if err != nil {
		h.Log.Error("error taking lease", "error", err, "app", hr.App)
		http.Error(w, fmt.Sprintf("error taking lease: %s", hr.App), http.StatusInternalServerError)
		return
	}

	if curLease != nil {
		h.Log.Info("using existing lease", "app", hr.App, "url", curLease.Lease.URL)
		h.forwardToLease(ctx, w, req, curLease)
		return
	}

	gr, err := h.eac.Get(ctx, hr.App.String())
	if err != nil {
		h.Log.Error("error looking up application", "error", err, "app", hr.App)
		http.Error(w, fmt.Sprintf("error looking up application: %s", hr.App), http.StatusInternalServerError)
		return
	}

	var app core_v1alpha.App
	app.Decode(gr.Entity().Entity())

	var appMD core_v1alpha.Metadata
	appMD.Decode(gr.Entity().Entity())

	if app.ActiveVersion == "" {
		h.Log.Debug("no active version for app", "app", hr.App)
		http.Error(w, fmt.Sprintf("no active version for app: %s", hr.App), http.StatusNotFound)
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
		h.Log.Error("error acquiring lease", "error", err, "app", hr.App)
		http.Error(w, fmt.Sprintf("error acquiring lease: %s", hr.App), http.StatusInternalServerError)
		return
	}

	if actLease == nil {
		h.Log.Debug("no lease available for app", "app", hr.App)
		http.Error(w, fmt.Sprintf("no lease available for app: %s", hr.App), http.StatusServiceUnavailable)
		return
	}

	localLease := h.retainLease(ctx, hr.App.String(), actLease)

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
