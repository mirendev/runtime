package httpingress

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/discovery"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/rpc/stream"
)

type Server struct {
	Log *slog.Logger

	eac *entityserver_v1alpha.EntityAccessClient
}

func NewServer(log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient) *Server {
	return &Server{
		Log: log,
		eac: eac,
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

	ver := core_v1alpha.MD(vr.Entity().Entity()).Name

	var sb compute_v1alpha.Sandbox
	sb.Container = append(sb.Container, compute_v1alpha.Container{
		Name:  "app",
		Image: av.ImageUrl,
		Env: []string{
			"MIREN_APP=" + appMD.Name,
			"MIREN_VERSION=" + ver,
		},
		Port: []compute_v1alpha.Port{
			{
				Port: 80,
				Name: "http",
				Type: "http",
			},
		},
	})

	var rpcE entityserver_v1alpha.Entity
	rpcE.SetAttrs(entity.Attrs(
		(&core_v1alpha.Metadata{
			Name:   ver,
			Labels: types.LabelSet("app", appMD.Name),
		}).Encode,
		sb.Encode,
	))

	pr, err := h.eac.Put(ctx, &rpcE)
	if err != nil {
		h.Log.Error("error creating sandbox", "error", err, "app", hr.App)
		http.Error(w, fmt.Sprintf("error creating sandbox: %s", hr.App), http.StatusInternalServerError)
		return
	}

	h.Log.Debug("created sandbox", "app", hr.App, "sandbox", pr.Id())

	var runningSB compute_v1alpha.Sandbox

	h.Log.Debug("watching sandbox", "app", hr.App, "sandbox", pr.Id())

	h.eac.WatchEntity(ctx, pr.Id(), stream.Callback(func(op *entityserver_v1alpha.EntityOp) error {
		var sb compute_v1alpha.Sandbox

		if op.HasEntity() {
			en := op.Entity().Entity()
			sb.Decode(en)

			if sb.Status == compute_v1alpha.RUNNING {
				runningSB = sb
				// TODO figure out a better way to signal that we're done with the watch.
				return io.EOF
			}
		}

		return nil
	}))

	if runningSB.Status != compute_v1alpha.RUNNING {
		h.Log.Error("error creating sandbox", "app", hr.App, "sandbox", pr.Id())
		http.Error(w, fmt.Sprintf("error creating sandbox: %s", hr.App), http.StatusInternalServerError)
		return
	}

	addr := runningSB.Network[0].Address

	he := &discovery.HTTPEndpoint{
		Host: "http://" + addr,
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
