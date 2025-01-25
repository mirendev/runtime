package ingress

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"miren.dev/runtime/discovery"
	"miren.dev/runtime/lease"
)

type LeaseHTTP struct {
	Log   *slog.Logger
	Lease *lease.LaunchContainer

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

func (h *LeaseHTTP) DeriveApp(req *http.Request) (string, bool) {
	if strings.HasSuffix(req.Host, h.checkDomain) {
		return strings.TrimSuffix(req.Host, h.checkDomain), true
	}

	return "", false
}

func (h *LeaseHTTP) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	app, ok := h.DeriveApp(req)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	ctx, cancel := context.WithTimeout(req.Context(), h.LookupTimeout)
	defer cancel()

	ctx = namespaces.WithNamespace(ctx, h.Namespace)

	lc, err := h.Lease.Lease(ctx, app, lease.Pool("http"))
	if err != nil {
		h.Log.Error("error looking up endpoint for application", "error", err, "app", app)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	defer func() {
		// We *must* use the top context because the http server will cancel the request
		// context IF AND WHEN the client cancels the request (which, again, super common).
		// So we use the top context so that regardless of what happens, we release the lease.
		li, err := h.Lease.ReleaseLease(h.Top, lc)
		if err != nil {
			h.Log.Error("error releasing lease from http request", "error", err, "app", app)
		} else {
			h.Log.Debug("lease released", "app", app, "usage", li.Usage)
		}
	}()

	cont, err := lc.Obj(ctx)
	if err != nil {
		h.Log.Error("error looking up endpoint for application", "error", err, "app", app)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	endpoint, err := h.extractEndpoint(ctx, cont)
	if err != nil {
		h.Log.Error("error looking up endpoint for application", "error", err, "app", app)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	endpoint.ServeHTTP(w, req)
}

const (
	appLabel       = "miren.dev/app"
	httpHostLabel  = "miren.dev/http_host"
	staticDirLabel = "miren.dev/static_dir"
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
