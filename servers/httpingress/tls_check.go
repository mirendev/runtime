package httpingress

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	ephemeralx "miren.dev/runtime/pkg/ephemeral"
)

// tlsCheckTimeout bounds the ask request to the app. It runs inside a TLS
// handshake that is already waiting on it, so it has to be short.
const tlsCheckTimeout = 5 * time.Second

// AllowCertificate reports whether host deserves an on-demand certificate
// even though no route names it exactly. The name has to sit one label under
// a route, either through a wildcard ("foo.example.com" under "*.example.com")
// or as an ephemeral subdomain of a normal route ("pr-3.app.example.com" under
// "app.example.com"), and then one of two things has to vouch for it: a live
// ephemeral deploy of the route's app with that label, or a 200 from the
// route's tls_check path. Anything else is a name nothing will serve, so the
// answer is no.
//
// A non-nil error means the answer is unknown, not that the name was refused:
// the entity store failed, or the app gave no clear answer (a 5xx or a
// timeout, as when it is still booting from zero). Callers should ask again
// rather than remember a refusal.
func (h *Server) AllowCertificate(ctx context.Context, host string) (bool, error) {
	host = strings.ToLower(host)

	route, err := h.ingressClient.LookupWithWildcard(ctx, host)
	if err != nil {
		return false, err
	}

	var label string
	switch {
	case route != nil && strings.EqualFold(route.Host, host):
		return true, nil
	case route != nil:
		label = ingress.ExtractSubdomainLabel(host, route.Host)
	default:
		label, route, err = h.lookupEphemeralRoute(ctx, host)
		if err != nil {
			return false, err
		}
	}

	if route == nil || label == "" {
		return false, nil
	}

	ev, err := ephemeralx.LookupByLabel(ctx, h.eac, route.App, label)
	if err != nil {
		return false, err
	}
	if ev != nil {
		return true, nil
	}

	if route.TlsCheck == "" {
		return false, nil
	}

	return h.askTLSCheck(ctx, route, host)
}

// askTLSCheck sends GET <tls_check>?domain=<host> to the route's app. A 200 is
// a yes. A 5xx or no response at all is an error, so a slow or booting app
// doesn't get a refusal remembered on its behalf; any other status is a no.
//
// The request goes straight to the app's active version, skipping WAF,
// maintenance and auth: the app is being asked a question by its own
// platform, and an auth-protected route would otherwise never be able to say
// yes.
func (h *Server) askTLSCheck(ctx context.Context, route *ingress_v1alpha.HttpRoute, host string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, tlsCheckTimeout)
	defer cancel()

	// The name being vouched for will be served over HTTPS, so the ask says so.
	// Otherwise the app sees X-Forwarded-Proto: http, and one that redirects
	// plain HTTP would answer with a redirect instead of its verdict.
	req, err := http.NewRequestWithContext(WithOriginScheme(ctx, "https"), http.MethodGet, route.TlsCheck, nil)
	if err != nil {
		h.Log.Warn("invalid tls_check path on route", "route", route.Host, "tls_check", route.TlsCheck, "error", err)
		return false, nil
	}
	req.URL.RawQuery = url.Values{"domain": {host}}.Encode()
	req.Host = host
	req.RemoteAddr = "127.0.0.1:0"

	rec := &statusRecorder{header: http.Header{}}

	if target, ok := h.resolveIngressTarget(rec, req, route.App, "", ingress.IsWildcardHost(route.Host)); ok {
		appName := target.appMetadata.Name
		h.serveAuthenticatedRequest(rec, req, route.App, routeService(route), "tls_check", target, &appName, tlsCheckTimeout)
	}

	h.Log.Debug("tls_check answered", "route", route.Host, "host", host, "status", rec.status)
	switch {
	case rec.status == http.StatusOK:
		return true, nil
	case rec.status == 0 || rec.status >= 500:
		return false, fmt.Errorf("tls_check %s on route %s gave no answer for %s (status %d)", route.TlsCheck, route.Host, host, rec.status)
	default:
		return false, nil
	}
}

// statusRecorder is a ResponseWriter that keeps only the status code. The
// tls_check answer is the status; the body is discarded so an app can't make
// the ingress buffer something large.
type statusRecorder struct {
	header http.Header
	status int
}

func (r *statusRecorder) Header() http.Header { return r.header }

func (r *statusRecorder) WriteHeader(code int) {
	// The proxy relays informational responses (103 Early Hints) through
	// WriteHeader ahead of the final one; only the final status is the answer.
	if code < 200 {
		return
	}
	if r.status == 0 {
		r.status = code
	}
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return len(b), nil
}
