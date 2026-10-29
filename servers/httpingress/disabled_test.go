package httpingress

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/components/activator"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
)

// A disabled app is checked before the lease cache. A warm app holds cached
// leases to sandboxes that keep running until the pool scales them down, so a
// check on the activator's scale-up path alone would leave it serving.
func TestDisabledAppStopsServingFromCachedLease(t *testing.T) {
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("from the app"))
	}))
	defer upstream.Close()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()
	ctx := context.Background()

	appID, err := inmem.Client.Create(ctx, "shop", &core_v1alpha.App{})
	require.NoError(t, err)
	cvID, err := inmem.Client.Create(ctx, "cfg", &core_v1alpha.ConfigVersion{App: appID})
	require.NoError(t, err)
	verID, err := inmem.Client.Create(ctx, "ver", &core_v1alpha.AppVersion{App: appID, ConfigVersion: cvID})
	require.NoError(t, err)
	require.NoError(t, inmem.Client.Update(ctx, &core_v1alpha.App{ID: appID, ActiveVersion: verID}))
	_, err = inmem.Client.Create(ctx, "shop.test", &ingress_v1alpha.HttpRoute{Host: "shop.test", App: appID})
	require.NoError(t, err)

	s := &Server{Log: slog.Default(), eac: inmem.EAC, aa: stubActivator{}, apps: make(map[string]*appUsage)}
	s.ingressClient = ingress.NewClient(s.Log, rpc.LocalClient(entityserver_v1alpha.AdaptEntityAccess(inmem.Server)))
	s.retainLease(ctx, leaseCacheKey(appID, "web", "", false), &activator.Lease{Size: 10, URL: upstream.URL})

	do := func(accept string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "http://shop.test/", nil)
		req.Header.Set("Accept", accept)
		var appName string
		s.serveHTTPWithMetrics(rec, req, &appName)
		return rec
	}

	rec := do("text/html")
	require.Equal(t, http.StatusOK, rec.Code, "the cached lease serves while the app is enabled")
	require.Equal(t, int32(1), hits.Load())

	_, err = inmem.EAC.Patch(ctx, entity.New(
		entity.Ref(entity.DBId, appID),
		entity.Time(core_v1alpha.AppDisabledAtId, time.Now()),
		entity.String(core_v1alpha.AppDisabledReasonId, "moved to the new cluster"),
	).Attrs(), 0)
	require.NoError(t, err)

	rec = do("text/html")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	assert.Contains(t, rec.Body.String(), "shop.test is unavailable")
	assert.NotContains(t, rec.Body.String(), "moved to the new cluster", "the disable reason is an operator note, not visitor copy")
	assert.NotContains(t, rec.Body.String(), "Try again")

	rec = do("application/json")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.JSONEq(t, `{"error":"disabled"}`, rec.Body.String())

	rec = do("text/plain")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, "Unavailable\n", rec.Body.String())

	assert.Equal(t, int32(1), hits.Load(), "no request reached the app after it was disabled")
}

// A cluster template written for maintenance keys on .Maintenance. A disabled
// app sets it too, so that template still renders a holding page rather than
// falling through to its generic error branch.
func TestDisabledPageRendersThroughAMaintenanceTemplate(t *testing.T) {
	page, err := parseErrorTemplate("{{if .Maintenance}}holding{{if .Disabled}} (disabled){{end}}{{else}}error{{end}}")
	require.NoError(t, err)
	s := newTestMaintenanceServer()
	s.config.ErrorPageTemplate = page

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "http://shop.test/", nil)
	req.Header.Set("Accept", "text/html")
	s.serveDisabled(rec, req, &core_v1alpha.App{})

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, "holding (disabled)", rec.Body.String())
}
