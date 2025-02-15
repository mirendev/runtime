package ingress

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"miren.dev/runtime/app"
	"miren.dev/runtime/discovery"
	"miren.dev/runtime/lease"
)

type LeaseHTTP struct {
	Log   *slog.Logger
	Lease *lease.LaunchContainer

	App *app.AppAccess

	CC        *containerd.Client
	Namespace string `asm:"namespace"`

	HTTPDomain  string `asm:"http_domain"`
	checkDomain string

	LookupTimeout time.Duration `asm:"lookup_timeout"`

	Top context.Context `asm:"top_context"`
}

func (h *LeaseHTTP) Populated() error {
	h.checkDomain = "." + h.HTTPDomain
	return nil
}

func (h *LeaseHTTP) DeriveApp(host string) (string, bool) {
	if host == "" {
		return "", false
	}

	_, err := netip.ParseAddr(host)
	if err != nil {
		return "", false
	}

	if app, _, ok := strings.Cut(host, "."); ok {
		return app, true
	}

	// Ok, it's JUST a name, so let's try it.
	return host, true
}

func (h *LeaseHTTP) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	onlyHost, _, err := net.SplitHostPort(req.Host)
	if err != nil {
		onlyHost = req.Host
	}

	ac, err := h.App.LoadApplicationForHost(req.Context(), onlyHost)
	if err != nil {
		h.Log.Error("error looking up application by host", "error", err, "host", req.Host)
	}

	if ac == nil {
		h.Log.Debug("no application found by host route", "host", req.Host)
		app, ok := h.DeriveApp(onlyHost)
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		ac, err = h.App.LoadApp(req.Context(), app)
		if err != nil {
			h.Log.Error("error looking up application", "error", err, "app", app)
			http.Error(w, fmt.Sprintf("application not found: %s", app), http.StatusNotFound)
			return
		}
	}

	ctx, cancel := context.WithTimeout(req.Context(), h.LookupTimeout)
	defer cancel()

	ctx = namespaces.WithNamespace(ctx, h.Namespace)

	lc, err := h.Lease.Lease(ctx, ac.Xid, lease.Pool("http"))
	if err != nil {
		h.Log.Error("error looking up endpoint for application", "error", err, "app", ac.Name)
		http.Error(w, fmt.Sprintf("error accessing application: %s", ac.Name), http.StatusInternalServerError)
		return
	}

	defer func() {
		// We *must* use the top context because the http server will cancel the request
		// context IF AND WHEN the client cancels the request (which, again, super common).
		// So we use the top context so that regardless of what happens, we release the lease.
		li, err := h.Lease.ReleaseLease(h.Top, lc)
		if err != nil {
			h.Log.Error("error releasing lease from http request", "error", err, "app", ac.Name)
		} else {
			h.Log.Debug("lease released", "app", ac.Name, "usage", li.Usage)
		}
	}()

	cont, err := lc.Obj(ctx)
	if err != nil {
		h.Log.Error("error looking up endpoint for application", "error", err, "app", ac.Name)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	endpoint, err := h.extractEndpoint(ctx, cont)
	if err != nil {
		h.Log.Error("error looking up endpoint for application", "error", err, "app", ac.Name)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	endpoint.ServeHTTP(w, req)
}

const (
	appLabel       = "runtime.computer/app"
	httpHostLabel  = "runtime.computer/http_host"
	staticDirLabel = "runtime.computer/static_dir"
)

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
