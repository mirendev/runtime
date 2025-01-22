package ingress

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"miren.dev/runtime/discovery"
)

type HTTP struct {
	Log    *slog.Logger
	Lookup discovery.Lookup

	HTTPDomain  string `asm:"http_domain"`
	checkDomain string

	LookupTimeout time.Duration `asm:"lookup_timeout"`
}

func (h *HTTP) Populated() error {
	h.checkDomain = "." + h.HTTPDomain
	return nil
}

func (h *HTTP) DeriveApp(req *http.Request) (string, bool) {
	if strings.HasSuffix(req.Host, h.checkDomain) {
		return strings.TrimSuffix(req.Host, h.checkDomain), true
	}

	return "", false
}

func (h *HTTP) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	app, ok := h.DeriveApp(req)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	ctx, cancel := context.WithTimeout(req.Context(), h.LookupTimeout)
	defer cancel()

	ep, ready, err := h.Lookup.Lookup(ctx, app)
	if err != nil {
		h.Log.Error("error looking up endpoint for application", "error", err, "app", app)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if ep == nil && ready == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	if ep != nil {
		ep.ServeHTTP(w, req)
	}

	select {
	case <-ctx.Done():
		w.WriteHeader(http.StatusGatewayTimeout)
	case ep := <-ready:
		if ep.Error != nil {
			h.Log.Error("error looking up endpoint for application in background", "error", ep.Error, "app", app)
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			ep.Endpoint.ServeHTTP(w, req)
		}
	}
}
