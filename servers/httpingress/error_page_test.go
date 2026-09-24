package httpingress

import (
	"context"
	"encoding/json"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/errorpage"
)

func TestCustomErrorPagePrecedence(t *testing.T) {
	cluster, err := parseErrorTemplate("<h1>cluster {{.Status}} {{.Title}}</h1>")
	require.NoError(t, err)
	app := &fakeStaticFiles{template: []byte("<h1>app {{.Status}} {{.Title}}</h1>")}
	h := &Server{Log: testutils.TestLogger(t), config: IngressConfig{ErrorPageTemplate: cluster}, staticFiles: app}
	target := &resolvedIngressTarget{config: &core_v1alpha.ConfigSpec{StaticErrorPage: "errors/page.html"}}

	for _, tt := range []struct {
		name, accept, appTemplate, want, notWant string
		target                                   bool
	}{
		{"app HTML", "text/html", "<h1>app {{.Status}} {{.Title}}</h1>", "app 502", "cluster", true},
		{"unknown host", "text/html", "", "cluster 502", "app 502", false},
		{"missing app template", "text/html", "", "cluster 502", "app 502", true},
		{"malformed app template", "text/html", "{{if", "cluster 502", "app 502", true},
		{"app execution fails", "text/html", "{{index .Site 0}}", "cluster 502", "app 502", true},
		{"JSON unaffected", "application/json", "<h1>app</h1>", `"error":"bad_gateway"`, "<h1>app", true},
		{"plain unaffected", "text/plain", "<h1>app</h1>", "private-id\n", "<h1>app", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			app.template = []byte(tt.appTemplate)
			if tt.name == "missing app template" {
				app.template = nil
			}
			r := httptest.NewRequest("GET", "http://app.test/", nil)
			r.Header.Set("Accept", tt.accept)
			if tt.target {
				r = r.WithContext(context.WithValue(r.Context(), errorPageTargetKey{}, target))
			}
			w := httptest.NewRecorder()
			h.serveIngressError(w, r, "private-id", http.StatusBadGateway)
			assert.Equal(t, http.StatusBadGateway, w.Code)
			assert.Contains(t, w.Body.String(), tt.want)
			assert.NotContains(t, w.Body.String(), tt.notWant)
			if tt.accept == "text/html" {
				assert.NotContains(t, w.Body.String(), "private-id")
			}
		})
	}
}

func TestErrorTemplateRenderLimitFallsBack(t *testing.T) {
	cluster, err := parseErrorTemplate("<h1>cluster</h1>")
	require.NoError(t, err)
	oversized, err := parseErrorTemplate(strings.Repeat("{{brandLogo}}", 30))
	require.NoError(t, err)
	assert.Equal(t, "<h1>cluster</h1>", string(renderErrorPage(oversized, cluster, errorPageData{Status: 502})))
	fallback := string(renderErrorPage(oversized, oversized, errorPageData{Status: 502}))
	assert.Contains(t, fallback, "Powered by Miren")
	assert.Equal(t, 1, strings.Count(fallback, "<svg"))

	withinLimit, err := parseErrorTemplate(strings.Repeat("{{brandLogo}}", 2))
	require.NoError(t, err)
	assert.Equal(t, 2, strings.Count(string(renderErrorPage(withinLimit, cluster, errorPageData{Status: 502})), "<svg"))
}

func TestAppErrorTemplateRejectsUnboundedWork(t *testing.T) {
	cluster, err := parseErrorTemplate("<h1>cluster</h1>")
	require.NoError(t, err)
	for _, source := range []string{
		`{{define "empty"}}{{end}}{{template "empty"}}`,
		`{{range .Description}}{{.}}{{end}}`,
		`{{printf "%1000000000s" "x"}}`,
		`{{if printf "%1000000000s" "x"}}hi{{end}}`,
	} {
		_, err := errorpage.ParseApp(source, brandLogo)
		assert.ErrorContains(t, err, "cannot use define, block, template, range, or printf")
		files := &fakeStaticFiles{template: []byte(source)}
		h := &Server{Log: testutils.TestLogger(t), config: IngressConfig{ErrorPageTemplate: cluster}, staticFiles: files}
		target := &resolvedIngressTarget{config: &core_v1alpha.ConfigSpec{StaticErrorPage: "error.html"}}
		r := httptest.NewRequest("GET", "http://app.test/", nil)
		r.Header.Set("Accept", "text/html")
		r = r.WithContext(context.WithValue(r.Context(), errorPageTargetKey{}, target))
		w := httptest.NewRecorder()
		h.serveIngressError(w, r, "private-id", http.StatusBadGateway)
		assert.Equal(t, "<h1>cluster</h1>", w.Body.String())
	}
	page, err := errorpage.ParseApp("{{if .Maintenance}}Later{{else}}{{with .Title}}{{.}}{{end}}{{end}}", brandLogo)
	require.NoError(t, err)
	assert.Equal(t, "Unavailable", string(renderErrorPage(page, cluster, errorPageData{Title: "Unavailable"})))
}

func TestAppErrorTemplateCacheUsesDigestAndPath(t *testing.T) {
	files := &fakeStaticFiles{template: []byte("app {{.Status}}")}
	cache, err := lru.New[string, *template.Template](2)
	require.NoError(t, err)
	h := &Server{Log: testutils.TestLogger(t), staticFiles: files, errorTemplates: cache}
	target := &resolvedIngressTarget{version: core_v1alpha.AppVersion{StaticArtifact: "sha256:one"},
		config: &core_v1alpha.ConfigSpec{StaticErrorPage: "error.html"}}
	r := httptest.NewRequest("GET", "http://app.test/", nil).WithContext(context.WithValue(context.Background(), errorPageTargetKey{}, target))
	for i := 0; i < 2; i++ {
		assert.Contains(t, string(renderErrorPage(h.htmlErrorTemplate(r), nil, errorPageData{Status: 502})), "app 502")
	}
	assert.Equal(t, 1, files.reads)
	target.version.StaticArtifact = "sha256:two"
	h.htmlErrorTemplate(r)
	target.config.StaticErrorPage = "other.html"
	h.htmlErrorTemplate(r)
	assert.Equal(t, 3, files.reads)
	assert.Equal(t, 2, cache.Len())
}

func TestLoadErrorPageTemplate(t *testing.T) {
	_, err := LoadErrorPageTemplate("relative/error.html")
	assert.ErrorContains(t, err, "absolute path")
	file := filepath.Join(t.TempDir(), "error.html")
	require.NoError(t, os.WriteFile(file, []byte("<p>{{.Status}} {{.Description}}</p>"), 0600))
	page, err := LoadErrorPageTemplate(file)
	require.NoError(t, err)
	assert.NotNil(t, page)
	require.NoError(t, os.WriteFile(file, []byte("{{if"), 0600))
	_, err = LoadErrorPageTemplate(file)
	require.Error(t, err)
	require.NoError(t, os.WriteFile(file, []byte(strings.Repeat("x", maxErrorTemplateSize+1)), 0600))
	_, err = LoadErrorPageTemplate(file)
	assert.ErrorContains(t, err, "exceeds")
}

func TestDocumentedErrorPageTemplate(t *testing.T) {
	doc, err := os.ReadFile("../../docs/docs/server-config.md")
	require.NoError(t, err)
	_, after, ok := strings.Cut(string(doc), "Copy this into `/etc/miren/error.html`")
	require.True(t, ok)
	_, after, ok = strings.Cut(after, "```html\n")
	require.True(t, ok)
	source, _, ok := strings.Cut(after, "\n```")
	require.True(t, ok)
	page, err := parseErrorTemplate(source)
	require.NoError(t, err)
	_, err = errorpage.ParseApp(source, brandLogo)
	require.NoError(t, err)

	for _, tt := range []struct {
		name string
		data errorPageData
		want string
	}{
		{"ordinary error", errorPageData{Status: 502, Title: "Unavailable", Description: "Try later"}, "<h1>Unavailable</h1>"},
		{"maintenance", errorPageData{Status: 503, Site: "shop.test", Reason: "Upgrading", BackAt: "12:00 UTC", Maintenance: true}, "shop.test is down for maintenance"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := string(renderErrorPage(page, nil, tt.data))
			assert.Contains(t, body, tt.want)
			assert.NotContains(t, body, "{{")
			assert.NotContains(t, body, "Powered by Miren")
		})
	}
}

func TestAppErrorPageUsedAfterPreparationBeforeAuth(t *testing.T) {
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()
	h := &Server{Log: testutils.TestLogger(t), eac: inmem.EAC,
		staticFiles: &fakeStaticFiles{template: []byte("<h1>app error {{.Status}}</h1>")}}
	target := &resolvedIngressTarget{config: &core_v1alpha.ConfigSpec{StaticErrorPage: "error.html"}}
	prepare := func(_ http.ResponseWriter, r *http.Request) *http.Request {
		return r.WithContext(context.WithValue(r.Context(), errorPageTargetKey{}, target))
	}
	route := &ingress_v1alpha.HttpRoute{AuthProvider: entity.Id("missing-auth-provider")}
	r := httptest.NewRequest("GET", "http://app.test/", nil)
	r.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	h.buildRouteHandler(route, nil, prepare, func(http.ResponseWriter, *http.Request) {
		t.Fatal("auth failure reached app")
	})(w, r)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Contains(t, w.Body.String(), "app error 503")
}

func TestServeIngressError(t *testing.T) {
	for _, tt := range []struct {
		name, accept string
		status       int
		want         string
		contentType  string
	}{
		{"browser missing route", "text/html,application/xhtml+xml", 404, "This page could not be found.", "text/html"},
		{"browser boot failure", "text/html", 408, "check its logs with miren logs", "text/html"},
		{"browser proxy error", "text/html", 502, "This app is temporarily unavailable.", "text/html"},
		{"browser internal error", "text/html", 500, "Something went wrong.", "text/html"},
		{"API client", "application/json", 503, `"error":"service_unavailable"`, "application/json"},
		{"vendor JSON client", "application/problem+json", 503, `"error":"service_unavailable"`, "application/json"},
		{"JSON preferred over HTML", "text/html;q=0.2, application/json;q=0.9", 503, `"error":"service_unavailable"`, "application/json"},
		{"HTML preferred over JSON", "application/json;q=0.4, text/html;q=0.9", 503, "This app is temporarily unavailable.", "text/html"},
		{"plain preferred over HTML", "text/html;q=0.1, text/plain;q=1", 503, "private-app-id\n", "text/plain"},
		{"HTML rejected despite XHTML", "text/html;q=0, application/xhtml+xml;q=1", 503, "private-app-id\n", "text/plain"},
		{"HTML explicitly rejected", "text/html;q=0", 503, "private-app-id\n", "text/plain"},
		{"wildcard overridden by HTML rejection", "*/*;q=1, text/html;q=0", 503, "private-app-id\n", "text/plain"},
		{"plain rejected despite wildcard", "text/plain;q=0, */*", 503, "This app is temporarily unavailable.", "text/html"},
		{"plain and HTML rejected despite wildcard", "text/plain;q=0, text/html;q=0, */*", 503, `"error":"service_unavailable"`, "application/json"},
		{"HTML explicit over wildcard", "*/*;q=1, text/html;q=1", 404, "This page could not be found.", "text/html"},
		{"wildcard fallback", "*/*", 404, "private-app-id\n", "text/plain"},
		{"unspecified accept", "", 404, "private-app-id\n", "text/plain"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "http://example.test/", nil)
			r.Header.Set("Accept", tt.accept)
			w := httptest.NewRecorder()
			serveIngressError(w, r, "private-app-id", tt.status)

			assert.Equal(t, tt.status, w.Code)
			assert.Contains(t, w.Header().Get("Content-Type"), tt.contentType)
			assert.Contains(t, w.Body.String(), tt.want)
			assert.Equal(t, "Accept", w.Header().Get("Vary"))
			switch tt.contentType {
			case "text/html":
				assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
				assert.NotContains(t, w.Body.String(), "private-app-id")
				assert.Contains(t, w.Body.String(), "Powered by Miren")
				assert.Contains(t, w.Body.String(), `viewBox="0 0 230 54"`)
				assert.Contains(t, w.Body.String(), `fill="currentColor"`)
				assert.Equal(t, tt.status != 404 && tt.status != 408, strings.Contains(w.Body.String(), "Try again"))
			case "application/json":
				assert.NotContains(t, w.Body.String(), "private-app-id")
				var body struct {
					Error   string `json:"error"`
					Message string `json:"message"`
				}
				assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
				assert.Equal(t, "service_unavailable", body.Error)
				assert.Equal(t, "The app couldn't respond right now. Please try again in a few moments.", body.Message)
			}
		})
	}
}

func TestProxyErrorServesPageToBrowser(t *testing.T) {
	h := &Server{Log: newTestMaintenanceServer().Log}
	r := httptest.NewRequest("GET", "http://app.example.com/", nil)
	r.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()

	err := h.proxyToLease(w, r, "http://127.0.0.1:1", "app/private", "test", true, 0)
	assert.Error(t, err)
	assert.Equal(t, http.StatusBadGateway, w.Code)
	assert.Contains(t, w.Body.String(), "This app is temporarily unavailable.")
	assert.NotContains(t, w.Body.String(), "127.0.0.1")
}

func TestProxyErrorRetainsEmptyPlainResponseAndSupportsJSON(t *testing.T) {
	h := &Server{Log: newTestMaintenanceServer().Log}
	for _, tt := range []struct {
		accept, contentType, body string
	}{
		{"", "", ""},
		{"text/plain", "", ""},
		{"application/json", "application/json", `"error":"bad_gateway"`},
		{"text/plain;q=0, */*", "text/html", "This app is temporarily unavailable."},
	} {
		t.Run(tt.accept, func(t *testing.T) {
			r := httptest.NewRequest("GET", "http://app.example.com/", nil)
			r.Header.Set("Accept", tt.accept)
			w := httptest.NewRecorder()

			err := h.proxyToLease(w, r, "http://127.0.0.1:1", "app/private", "test", true, 0)
			assert.Error(t, err)
			assert.Equal(t, http.StatusBadGateway, w.Code)
			assert.Contains(t, w.Header().Get("Content-Type"), tt.contentType)
			assert.Contains(t, w.Body.String(), tt.body)
			if tt.contentType == "" {
				assert.Empty(t, w.Body.String())
				assert.Empty(t, w.Header().Get("Content-Type"))
			} else {
				assert.NotContains(t, w.Body.String(), "app/private")
			}
		})
	}
}
