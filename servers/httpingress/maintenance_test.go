package httpingress

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

func newTestMaintenanceServer() *Server {
	return &Server{Log: slog.Default()}
}

// runMaintenance drives the middleware for a route with no app reference, so
// the app-name lookup short circuits and no entity client is needed.
func runMaintenance(t *testing.T, maint ingress_v1alpha.Maintenance, accept string) *httptest.ResponseRecorder {
	t.Helper()

	s := newTestMaintenanceServer()
	route := &ingress_v1alpha.HttpRoute{Maintenance: maint}

	next := func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("request reached the app while the route was in maintenance")
	}

	req := httptest.NewRequest("GET", "http://app.example.com/", nil)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}

	rec := httptest.NewRecorder()
	s.maintenanceMiddleware(route, nil, next)(rec, req)

	return rec
}

// TestMaintenancePageNamesTheHostVisited pins the heading to the name the
// visitor typed. The route's own host is the wrong source: a wildcard route
// stores a pattern and the default route stores nothing, so both would put
// something meaningless in front of a person.
func TestMaintenancePageNamesTheHostVisited(t *testing.T) {
	tests := []struct {
		name      string
		routeHost string
		requested string
		want      string
	}{
		{
			name:      "plain route",
			routeHost: "app.example.com",
			requested: "http://app.example.com/",
			want:      "app.example.com",
		},
		{
			name:      "wildcard route names the concrete subdomain",
			routeHost: "*.example.com",
			requested: "http://shop.example.com/",
			want:      "shop.example.com",
		},
		{
			name:      "preview subdomain names itself",
			routeHost: "app.example.com",
			requested: "http://feat-x.app.example.com/",
			want:      "feat-x.app.example.com",
		},
		{
			name:      "default route has no host of its own",
			routeHost: "",
			requested: "http://anything.example.com/",
			want:      "anything.example.com",
		},
		{
			name:      "port is stripped",
			routeHost: "app.example.com",
			requested: "http://app.example.com:8443/",
			want:      "app.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestMaintenanceServer()

			route := &ingress_v1alpha.HttpRoute{
				Host:        tt.routeHost,
				Maintenance: ingress_v1alpha.Maintenance{Reason: "Upgrading the database"},
			}

			next := func(w http.ResponseWriter, r *http.Request) {
				t.Fatal("request reached the app while the route was in maintenance")
			}

			rec := httptest.NewRecorder()
			s.maintenanceMiddleware(route, nil, next)(rec, httptest.NewRequest("GET", tt.requested, nil))

			body := rec.Body.String()
			assert.Contains(t, body, tt.want+" is down for maintenance")
			assert.NotContains(t, body, ":8443", "the port has no business on the page")
		})
	}
}

func TestMaintenanceMiddlewarePassesThroughWhenNotSet(t *testing.T) {
	s := newTestMaintenanceServer()

	route := &ingress_v1alpha.HttpRoute{Host: "app.example.com"}
	called := false
	next := func(w http.ResponseWriter, r *http.Request) { called = true }

	handler := s.maintenanceMiddleware(route, nil, next)

	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest("GET", "http://app.example.com/", nil))

	assert.True(t, called, "next handler should run for a route that isn't in maintenance")
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestMaintenanceMiddlewareServesHoldingPage(t *testing.T) {
	rec := runMaintenance(t, ingress_v1alpha.Maintenance{
		Reason:    "Upgrading the database",
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}, "text/html")

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")

	body := rec.Body.String()
	assert.Contains(t, body, "Down for maintenance")
	assert.Contains(t, body, "Upgrading the database")
}

func TestMaintenanceMiddlewareEscapesOperatorReason(t *testing.T) {
	rec := runMaintenance(t, ingress_v1alpha.Maintenance{
		Reason: `<script>alert("xss")</script>`,
	}, "text/html")

	body := rec.Body.String()
	assert.NotContains(t, body, "<script>alert")
	assert.Contains(t, body, "&lt;script&gt;")
}

func TestMaintenanceMiddlewareRetryAfter(t *testing.T) {
	t.Run("future back_at sets the header", func(t *testing.T) {
		backAt := time.Now().Add(30 * time.Minute).UTC().Format(time.RFC3339)

		rec := runMaintenance(t, ingress_v1alpha.Maintenance{BackAt: backAt}, "text/html")

		secs, err := strconv.Atoi(rec.Header().Get("Retry-After"))
		require.NoError(t, err)
		assert.Greater(t, secs, 1700)
		assert.LessOrEqual(t, secs, 1800)
		assert.Contains(t, rec.Body.String(), "Expected back at")
	})

	t.Run("past back_at omits the header", func(t *testing.T) {
		backAt := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)

		rec := runMaintenance(t, ingress_v1alpha.Maintenance{BackAt: backAt}, "text/html")

		assert.Empty(t, rec.Header().Get("Retry-After"))
	})

	t.Run("unparseable back_at omits the header", func(t *testing.T) {
		rec := runMaintenance(t, ingress_v1alpha.Maintenance{BackAt: "soon"}, "text/html")

		assert.Empty(t, rec.Header().Get("Retry-After"))
		assert.Contains(t, rec.Body.String(), "Please check back shortly")
	})

	t.Run("no back_at omits the header", func(t *testing.T) {
		rec := runMaintenance(t, ingress_v1alpha.Maintenance{Reason: "migrating"}, "text/html")

		assert.Empty(t, rec.Header().Get("Retry-After"))
	})
}

func TestMaintenanceMiddlewareServesJSON(t *testing.T) {
	backAt := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)

	rec := runMaintenance(t, ingress_v1alpha.Maintenance{
		Reason: "Upgrading the database",
		BackAt: backAt,
	}, "application/json")

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")

	var body struct {
		Error  string `json:"error"`
		Reason string `json:"reason"`
		BackAt string `json:"back_at"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

	assert.Equal(t, "maintenance", body.Error)
	assert.Equal(t, "Upgrading the database", body.Reason)
	assert.Equal(t, backAt, body.BackAt)
}

func TestPrefersJSON(t *testing.T) {
	tests := []struct {
		accept string
		want   bool
	}{
		{"", false},
		{"*/*", false},
		{"text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8", false},
		{"application/json", true},
		{"application/json, text/plain, */*", true},
		{"application/vnd.api+json", true},
		{"text/html,application/json;q=0.9", false},
		{"application/json;q=0.9,text/html;q=0.8", true},
		{"application/json;q=bogus", true},
	}

	for _, tt := range tests {
		t.Run(tt.accept, func(t *testing.T) {
			assert.Equal(t, tt.want, prefersJSON(tt.accept))
		})
	}
}

// TestMiddlewareChainOrder pins the two ordering decisions in
// buildRouteHandler. The chain is three adjacent statements, so reordering it
// is a one-line accident that compiles and passes everything else.
func TestMiddlewareChainOrder(t *testing.T) {
	maint := ingress_v1alpha.Maintenance{
		Reason:    "Upgrading the database",
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}

	t.Run("maintenance answers before auth", func(t *testing.T) {
		// A real (empty) entity client, so that if auth did run first this
		// fails on the assertion below rather than panicking on a nil client.
		inmem, cleanup := testutils.NewInMemEntityServer(t)
		defer cleanup()

		s := newTestMaintenanceServer()
		s.eac = inmem.EAC

		// A protected route in maintenance. If auth ran first it would try to
		// resolve this provider and send the visitor to a login, when the
		// honest answer is that the site is deliberately down.
		route := &ingress_v1alpha.HttpRoute{
			AuthProvider: entity.Id("oidc_provider/corp-sso"),
			Maintenance:  maint,
		}

		serve := func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("request reached the app while the route was in maintenance")
		}

		rec := httptest.NewRecorder()
		s.buildRouteHandler(route, nil, serve)(rec, httptest.NewRequest("GET", "http://app.example.com/", nil))

		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
		assert.Contains(t, rec.Body.String(), "Upgrading the database",
			"an unauthenticated visitor should get the holding page, not a login")
	})

	t.Run("WAF still filters a route in maintenance", func(t *testing.T) {
		s := newTestWAFServer()

		route := newTestRoute("waf-l1", 1, s)
		route.Maintenance = maint

		serve := func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("request reached the app while the route was in maintenance")
		}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "http://app.example.com/?id=1%20OR%201=1--", nil)
		s.buildRouteHandler(route, nil, serve)(rec, req)

		assert.Equal(t, http.StatusForbidden, rec.Code,
			"a maintenance window must not become an open window for scanners")
	})
}
