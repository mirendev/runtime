package httpingress

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestServeIngressError(t *testing.T) {
	for _, tt := range []struct {
		name, accept string
		status       int
		want         string
		contentType  string
	}{
		{"browser missing route", "text/html,application/xhtml+xml", 404, "This page could not be found.", "text/html"},
		{"browser proxy error", "text/html", 502, "This app is temporarily unavailable.", "text/html"},
		{"browser internal error", "text/html", 500, "Something went wrong.", "text/html"},
		{"API client", "application/json", 503, `"error":"service_unavailable"`, "application/json"},
		{"JSON preferred over HTML", "text/html;q=0.2, application/json;q=0.9", 503, `"error":"service_unavailable"`, "application/json"},
		{"HTML preferred over JSON", "application/json;q=0.4, text/html;q=0.9", 503, "This app is temporarily unavailable.", "text/html"},
		{"plain preferred over HTML", "text/html;q=0.1, text/plain;q=1", 503, "private-app-id\n", "text/plain"},
		{"HTML rejected despite XHTML", "text/html;q=0, application/xhtml+xml;q=1", 503, "private-app-id\n", "text/plain"},
		{"HTML explicitly rejected", "text/html;q=0", 503, "private-app-id\n", "text/plain"},
		{"wildcard overridden by HTML rejection", "*/*;q=1, text/html;q=0", 503, "private-app-id\n", "text/plain"},
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
				assert.Equal(t, tt.status != 404, strings.Contains(w.Body.String(), "Try again"))
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
