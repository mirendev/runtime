package httpingress

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strings"
)

type errorPageData struct {
	Status      int
	Title       string
	Description string
	Site        string
	Reason      string
	BackAt      string
	Maintenance bool
}

// serveIngressError negotiates public error responses without exposing internal
// identifiers or failure details in HTML or JSON.
func serveIngressError(w http.ResponseWriter, r *http.Request, message string, status int) {
	w.Header().Add("Vary", "Accept")
	data := errorPageData{Status: status}
	switch status {
	case http.StatusNotFound:
		data.Title = "This page could not be found."
		data.Description = "The page you're looking for isn't available. Check the address and try again."
	case http.StatusRequestTimeout, http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusGatewayTimeout:
		data.Title = "This app is temporarily unavailable."
		data.Description = "The app couldn't respond right now. Please try again in a few moments."
	default:
		data.Title = "Something went wrong."
		data.Description = "We couldn't complete your request. Please try again in a few moments."
	}

	switch errorRepresentation(r.Header.Get("Accept")) {
	case "application/json":
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}{strings.ToLower(strings.ReplaceAll(http.StatusText(status), " ", "_")), data.Description})
		return
	case "text/plain":
		if message == "" {
			w.WriteHeader(status)
		} else {
			http.Error(w, message, status)
		}
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = errorPage.Execute(w, data)
}

// Choose among the representations we actually emit. A more specific media
// range sets each representation's quality, even when a wildcard appears later.
// Unspecified or unsupported Accept headers retain the historical text fallback.
func errorRepresentation(accept string) string {
	mediaTypes := []string{"text/html", "application/json", "text/plain"}
	type preference struct {
		q           float64
		specificity int
	}
	preferences := make([]preference, len(mediaTypes))
	for _, part := range strings.Split(accept, ",") {
		media, q := parseMediaRange(part)
		if q < 0 || q > 1 || q != q {
			continue
		}
		for i, offered := range mediaTypes {
			specificity := 0
			switch media {
			case offered:
				specificity = 3
			case strings.SplitN(offered, "/", 2)[0] + "/*":
				specificity = 2
			case "*/*":
				specificity = 1
			}
			if specificity > preferences[i].specificity || (specificity != 0 && specificity == preferences[i].specificity && q > preferences[i].q) {
				preferences[i] = preference{q, specificity}
			}
		}
	}
	best := -1
	for i, p := range preferences {
		if p.q > 0 && (best < 0 || p.q > preferences[best].q || (p.q == preferences[best].q && p.specificity > preferences[best].specificity)) {
			best = i
		}
	}
	if best < 0 || (preferences[best].specificity == 1 && preferences[2].q > 0) {
		return "text/plain"
	}
	return mediaTypes[best]
}

// Inline assets let the page render even when the app and its static files are down.
var errorPage = template.Must(template.New("ingress-error").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{if .Maintenance}}Down for maintenance{{else}}{{.Status}} · {{.Title}}{{end}} — Miren</title>
<style>
  :root { color-scheme: light; }
  * { box-sizing: border-box; }
  body {
    margin: 0; min-height: 100vh; color: #1b1f27;
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    background-color: #fdfaf2;
    background-image: radial-gradient(#e8d9ce 1px, transparent 1px);
    background-size: 24px 24px;
  }
  .shell { min-height: 100vh; display: flex; flex-direction: column; padding: 0 clamp(24px, 7vw, 112px); }
  header { height: 92px; display: flex; align-items: center; border-bottom: 1px solid #eadfd6; }
  .brand { display: inline-flex; align-items: center; gap: 12px; font-family: Georgia, serif; font-size: 27px; letter-spacing: -.04em; }
  .mark { width: 34px; height: 34px; color: #0056ff; flex: none; }
  main { flex: 1; display: flex; align-items: center; padding: 72px 0; }
  .content { max-width: 760px; }
  .eyebrow { color: #545868; font-size: 13px; font-weight: 700; letter-spacing: .16em; text-transform: uppercase; }
  h1 { margin: 22px 0 24px; font-size: clamp(42px, 6.5vw, 76px); line-height: 1.06; letter-spacing: -.055em; font-weight: 800; overflow-wrap: anywhere; }
  h1 em { color: #0056ff; font-style: normal; }
  p { max-width: 590px; margin: 0 0 18px; color: #545868; font-size: clamp(18px, 2vw, 22px); line-height: 1.6; }
  .reason { color: #1b1f27; overflow-wrap: anywhere; white-space: pre-wrap; }
  .action { display: inline-block; margin-top: 24px; padding: 15px 24px; border-radius: 8px; background: #0056ff; color: white; font-size: 16px; font-weight: 650; text-decoration: none; }
  .action:hover, .action:focus-visible { background: #0844c5; }
  .action:focus-visible { outline: 3px solid #1b1f27; outline-offset: 3px; }
  footer { border-top: 1px solid #eadfd6; padding: 26px 0; color: #767989; font-size: 13px; }
  @media (max-width: 600px) {
    header { height: 76px; }
    main { padding: 64px 0; }
    h1 { letter-spacing: -.04em; }
  }
  @media (prefers-color-scheme: dark) {
    :root { color-scheme: dark; }
    body { background-color: #151a23; background-image: radial-gradient(#344052 1px, transparent 1px); color: #f4f5f5; }
    header, footer { border-color: #393e48; }
    .eyebrow, p, footer { color: #b6bac1; }
    .reason { color: #f4f5f5; }
    .action:focus-visible { outline-color: #f4f5f5; }
  }
</style>
</head>
<body>
<div class="shell">
  <header><div class="brand"><svg class="mark" viewBox="0 0 40 40" aria-hidden="true"><g fill="none" stroke="currentColor" stroke-width="1.8"><path d="M20 1v38M1 20h38M6.6 6.6l26.8 26.8M33.4 6.6 6.6 33.4M12.7 2.4l14.6 35.2M2.4 12.7l35.2 14.6M27.3 2.4 12.7 37.6M37.6 12.7 2.4 27.3"/></g></svg>Miren</div></header>
  <main><div class="content">
    <div class="eyebrow">{{if .Maintenance}}Scheduled maintenance{{else}}Error {{.Status}}{{end}}</div>
    <h1>{{if .Maintenance}}{{if .Site}}{{.Site}} is down for maintenance{{else}}Down for maintenance{{end}}{{else}}{{.Title}}{{end}}</h1>
    {{if .Maintenance}}
      {{if .Reason}}<p class="reason">{{.Reason}}</p>{{else}}<p>This site is taking a short break for maintenance.</p>{{end}}
      {{if .BackAt}}<p>Expected back at {{.BackAt}}.</p>{{else}}<p>Please check back shortly.</p>{{end}}
    {{else}}<p>{{.Description}}</p>{{end}}
    {{if not (eq .Status 404)}}<a class="action" href="">Try again &rarr;</a>{{end}}
  </div></main>
  <footer>Powered by Miren</footer>
</div>
</body>
</html>
`))
