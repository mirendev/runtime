package httpingress

import (
	"encoding/json"
	"html/template"
	"math"
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
	display := s.maintenanceAppName(r, appID)
	if appName != nil && *appName == "" {
		*appName = display
	}

	w.Header().Set("Cache-Control", "no-store")

	if secs, ok := retryAfterSeconds(maint.BackAt, time.Now()); ok {
		w.Header().Set("Retry-After", strconv.Itoa(secs))
	}

	if prefersJSON(r.Header.Get("Accept")) {
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

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)

	data := struct {
		AppName string
		Reason  string
		BackAt  string
	}{
		AppName: display,
		Reason:  maint.Reason,
		BackAt:  formatBackAt(maint.BackAt),
	}

	if err := maintenancePage.Execute(w, data); err != nil {
		s.Log.Debug("failed to render maintenance page", "error", err)
	}
}

// maintenanceAppName resolves the app's display name for the holding page. It
// returns an empty string on any failure — the page renders without a name
// rather than turning a planned outage into a 500.
func (s *Server) maintenanceAppName(r *http.Request, appID entity.Id) string {
	if entity.Empty(appID) {
		return ""
	}

	gr, err := s.eac.Get(r.Context(), appID.String())
	if err != nil {
		s.Log.Debug("failed to look up app for maintenance page", "error", err, "app", appID)
		return ""
	}

	var md core_v1alpha.Metadata
	md.Decode(gr.Entity().Entity())

	return md.Name
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

// prefersJSON reports whether the client's Accept header ranks JSON above HTML.
// An absent or unparseable header means HTML, which is what a browser gets.
func prefersJSON(accept string) bool {
	var jsonQ, htmlQ float64

	for _, part := range strings.Split(accept, ",") {
		media, q := parseMediaRange(part)
		if media == "" {
			continue
		}

		switch {
		case media == "application/json" || strings.HasSuffix(media, "+json"):
			jsonQ = math.Max(jsonQ, q)
		case media == "text/html" || media == "application/xhtml+xml":
			htmlQ = math.Max(htmlQ, q)
		}
	}

	return jsonQ > htmlQ
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

// The page is entirely self-contained — inline styles, no external requests —
// because it has to render on a network where the app itself doesn't.
var maintenancePage = template.Must(template.New("maintenance").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Down for maintenance</title>
<style>
  :root { color-scheme: light dark; }
  body {
    margin: 0;
    min-height: 100vh;
    display: flex;
    align-items: center;
    justify-content: center;
    font-family: system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
    background: #f6f7f9;
    color: #1f2430;
  }
  main { max-width: 34rem; padding: 2rem; text-align: center; }
  h1 { font-size: 1.6rem; font-weight: 600; margin: 0 0 1rem; }
  p { font-size: 1.05rem; line-height: 1.6; margin: 0 0 0.75rem; }
  .muted { color: #5b6472; font-size: 0.95rem; }
  @media (prefers-color-scheme: dark) {
    body { background: #14171c; color: #e7eaef; }
    .muted { color: #9aa3b2; }
  }
</style>
</head>
<body>
<main>
<h1>{{if .AppName}}{{.AppName}} is down for maintenance{{else}}Down for maintenance{{end}}</h1>
{{if .Reason}}<p>{{.Reason}}</p>{{end}}
{{if .BackAt}}<p class="muted">Expected back at {{.BackAt}}.</p>{{else}}<p class="muted">Please check back shortly.</p>{{end}}
</main>
</body>
</html>
`))
