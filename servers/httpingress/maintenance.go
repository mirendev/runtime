package httpingress

import (
	"context"
	"encoding/json"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/entity"
)

// maintenanceMiddleware short-circuits requests for a route an operator has
// taken out of service, serving a holding page instead of proxying to the app.
//
// It sits between the WAF filter and the auth wrapper. WAF stays outermost so a
// maintenance window doesn't become an open window for scanners. Maintenance
// runs ahead of auth because a holding page is public information: otherwise a
// visitor completes a full round trip to an identity provider only to be told
// the site is down, and an API client gets a 401 when the honest answer is 503.
func (s *Server) maintenanceMiddleware(route *ingress_v1alpha.HttpRoute, appName *string, next http.HandlerFunc) http.HandlerFunc {
	if route == nil || route.Maintenance.Empty() {
		return next
	}

	maint := route.Maintenance
	appID := route.App

	return func(w http.ResponseWriter, r *http.Request) {
		s.serveMaintenance(w, r, appID, appName, maint)
	}
}

func (s *Server) serveMaintenance(w http.ResponseWriter, r *http.Request, appID entity.Id, appName *string, maint ingress_v1alpha.Maintenance) {
	// Metrics want the app; the visitor wants the site they opened. The app's
	// name is an operator's identifier, so "payments-api is down" means nothing
	// to someone who typed shop.example.com.
	var app core_v1alpha.App
	if !entity.Empty(appID) && s.eac != nil {
		if gr, err := s.eac.Get(r.Context(), appID.String()); err == nil {
			app.Decode(gr.Entity().Entity())
			if appName != nil && *appName == "" {
				var md core_v1alpha.Metadata
				md.Decode(gr.Entity().Entity())
				*appName = md.Name
			}
		} else {
			s.Log.Debug("failed to look up app for maintenance page", "error", err, "app", appID)
		}
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Add("Vary", "Accept")

	if secs, ok := retryAfterSeconds(maint.BackAt, time.Now()); ok {
		w.Header().Set("Retry-After", strconv.Itoa(secs))
	}

	accept := r.Header.Get("Accept")
	representation := errorRepresentation(accept)
	if representation == "text/plain" && accept != "" && accept != "*/*" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("Down for maintenance\n"))
		if maint.Reason != "" {
			_, _ = w.Write([]byte(maint.Reason + "\n"))
		}
		return
	}

	if representation == "application/json" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)

		body := struct {
			Error  string `json:"error"`
			Reason string `json:"reason,omitempty"`
			BackAt string `json:"back_at,omitempty"`
		}{
			Error:  "maintenance",
			Reason: maint.Reason,
			BackAt: maint.BackAt,
		}

		if err := json.NewEncoder(w).Encode(body); err != nil {
			s.Log.Debug("failed to write maintenance JSON response", "error", err)
		}
		return
	}

	data := errorPageData{
		Status:      http.StatusServiceUnavailable,
		Site:        visitorHost(r),
		Reason:      maint.Reason,
		BackAt:      formatBackAt(maint.BackAt),
		Maintenance: true,
	}

	page := s.htmlErrorTemplate(r)
	// Maintenance runs before target preparation. Resolve the active version
	// only for the optional HTML page; failure never blocks the holding page.
	if !entity.Empty(app.ActiveVersion) {
		if resolved, err := s.resolveVersionConfig(r.Context(), app.ActiveVersion, nil); err == nil {
			target := &resolvedIngressTarget{version: resolved.version, config: &resolved.config}
			page = s.htmlErrorTemplate(r.WithContext(context.WithValue(r.Context(), errorPageTargetKey{}, target)))
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write(renderErrorPage(page, s.config.ErrorPageTemplate, data))
}

// visitorHost is the hostname the visitor actually opened, without its port.
//
// It reads the request rather than the route because the route's host can be a
// pattern or nothing at all: a wildcard route stores "*.example.com" and the
// default route stores no host, while the request always carries the concrete
// name. That also makes preview subdomains name themselves correctly.
func visitorHost(r *http.Request) string {
	host := r.Host

	if stripped, _, err := net.SplitHostPort(host); err == nil {
		host = stripped
	}

	return host
}

// retryAfterSeconds converts a stored RFC 3339 return time into the
// delta-seconds form of Retry-After. A time that doesn't parse, or that has
// already passed, yields no header at all rather than a misleading one.
func retryAfterSeconds(backAt string, now time.Time) (int, bool) {
	if backAt == "" {
		return 0, false
	}

	t, err := time.Parse(time.RFC3339, backAt)
	if err != nil {
		return 0, false
	}

	d := t.Sub(now)
	if d <= 0 {
		return 0, false
	}

	return int(math.Ceil(d.Seconds())), true
}

// formatBackAt renders the return time for the holding page. Visitors can be
// anywhere, so it's shown in UTC and labeled as such rather than silently
// rendered in the server's zone.
func formatBackAt(backAt string) string {
	if backAt == "" {
		return ""
	}

	t, err := time.Parse(time.RFC3339, backAt)
	if err != nil {
		return ""
	}

	return t.UTC().Format("15:04 UTC on 2 January 2006")
}

func parseMediaRange(part string) (string, float64) {
	fields := strings.Split(strings.TrimSpace(part), ";")

	media := strings.ToLower(strings.TrimSpace(fields[0]))
	if media == "" {
		return "", 0
	}

	q := 1.0
	for _, param := range fields[1:] {
		name, value, ok := strings.Cut(param, "=")
		if !ok || strings.ToLower(strings.TrimSpace(name)) != "q" {
			continue
		}
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
			q = parsed
		}
	}

	return media, q
}
