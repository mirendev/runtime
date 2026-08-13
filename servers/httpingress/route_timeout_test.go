package httpingress

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/entity"
)

// newTimeoutTestServer builds the minimal Server needed to exercise the
// transport/timeout resolution helpers, skipping NewServer's RPC dependencies.
func newTimeoutTestServer(defaultTimeout time.Duration) *Server {
	return &Server{
		Log:        slog.New(slog.DiscardHandler),
		config:     IngressConfig{RequestTimeout: defaultTimeout},
		transport:  newProxyTransport(defaultTimeout),
		transports: make(map[time.Duration]http.RoundTripper),
	}
}

// countingHandler tallies records per level so tests can assert on log volume.
type countingHandler struct {
	slog.Handler
	warns int
}

func (h *countingHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level == slog.LevelWarn {
		h.warns++
	}
	return nil
}

func (h *countingHandler) Enabled(context.Context, slog.Level) bool { return true }

func TestRouteRequestTimeout(t *testing.T) {
	h := newTimeoutTestServer(60 * time.Second)

	tests := []struct {
		name  string
		route *ingress_v1alpha.HttpRoute
		want  time.Duration
	}{
		{"nil route", nil, 0},
		{"unset", &ingress_v1alpha.HttpRoute{}, 0},
		{"minutes", &ingress_v1alpha.HttpRoute{RequestTimeout: "10m"}, 10 * time.Minute},
		{"seconds", &ingress_v1alpha.HttpRoute{RequestTimeout: "300s"}, 5 * time.Minute},
		{"unparseable", &ingress_v1alpha.HttpRoute{RequestTimeout: "10"}, 0},
		{"garbage", &ingress_v1alpha.HttpRoute{RequestTimeout: "forever"}, 0},
		{"zero", &ingress_v1alpha.HttpRoute{RequestTimeout: "0s"}, 0},
		{"negative", &ingress_v1alpha.HttpRoute{RequestTimeout: "-5m"}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, h.routeRequestTimeout(tt.route))
		})
	}
}

func TestRouteRequestTimeoutWarnsOncePerRoute(t *testing.T) {
	counter := &countingHandler{Handler: slog.DiscardHandler}

	h := newTimeoutTestServer(60 * time.Second)
	h.Log = slog.New(counter)
	h.badTimeouts = make(map[entity.Id]string)

	bad := &ingress_v1alpha.HttpRoute{ID: "http_route/bad", Host: "bad.example.com", RequestTimeout: "10"}

	for range 100 {
		require.Zero(t, h.routeRequestTimeout(bad))
	}
	require.Equal(t, 1, counter.warns,
		"a route with a bad value must warn once, not once per request")

	// A different route with its own bad value is its own warning.
	other := &ingress_v1alpha.HttpRoute{ID: "http_route/other", Host: "other.example.com", RequestTimeout: "0s"}
	for range 10 {
		require.Zero(t, h.routeRequestTimeout(other))
	}
	require.Equal(t, 2, counter.warns, "each misconfigured route gets its own warning")

	// Changing the same route to a different bad value is news worth repeating.
	bad.RequestTimeout = "forever"
	for range 10 {
		require.Zero(t, h.routeRequestTimeout(bad))
	}
	require.Equal(t, 3, counter.warns, "a newly-broken value on the same route should warn again")

	// Fixing the route stops the warnings entirely.
	bad.RequestTimeout = "10m"
	for range 10 {
		require.Equal(t, 10*time.Minute, h.routeRequestTimeout(bad))
	}
	require.Equal(t, 3, counter.warns, "a valid value must not warn")
}

func TestRouteRequestTimeoutWarnCapStaysQuiet(t *testing.T) {
	counter := &countingHandler{Handler: slog.DiscardHandler}

	h := newTimeoutTestServer(60 * time.Second)
	h.Log = slog.New(counter)
	h.badTimeouts = make(map[entity.Id]string)

	// Fill the tracking map to its cap, then confirm routes past it stay silent
	// rather than falling back to warning on every request.
	for i := range maxBadTimeoutsTracked {
		route := &ingress_v1alpha.HttpRoute{
			ID:             entity.Id(fmt.Sprintf("http_route/bad-%d", i)),
			RequestTimeout: "nope",
		}
		require.Zero(t, h.routeRequestTimeout(route))
	}
	require.Equal(t, maxBadTimeoutsTracked, counter.warns)

	overflow := &ingress_v1alpha.HttpRoute{ID: "http_route/overflow", RequestTimeout: "nope"}
	for range 50 {
		require.Zero(t, h.routeRequestTimeout(overflow))
	}
	require.Equal(t, maxBadTimeoutsTracked, counter.warns,
		"past the cap we go quiet rather than warn on every request")
	require.Len(t, h.badTimeouts, maxBadTimeoutsTracked, "the map must not grow past its cap")
}

func TestTransportForToleratesNilMap(t *testing.T) {
	// Several existing test helpers build a Server without the transports map;
	// a write to a nil map would panic instead of failing usefully.
	h := &Server{
		Log:       slog.New(slog.DiscardHandler),
		config:    IngressConfig{RequestTimeout: 60 * time.Second},
		transport: newProxyTransport(60 * time.Second),
	}

	require.NotPanics(t, func() {
		got := h.transportFor(10 * time.Minute)
		require.NotSame(t, h.transport, got)
	})
}

func TestTransportForUsesDefault(t *testing.T) {
	h := newTimeoutTestServer(60 * time.Second)

	require.Same(t, h.transport, h.transportFor(0),
		"no override should reuse the shared default transport")
	require.Same(t, h.transport, h.transportFor(60*time.Second),
		"an override equal to the default should reuse the shared transport")
	require.Empty(t, h.transports, "no extra transports should have been built")
}

func TestTransportForMemoizesOverrides(t *testing.T) {
	h := newTimeoutTestServer(60 * time.Second)

	tenMin := h.transportFor(10 * time.Minute)
	require.NotSame(t, h.transport, tenMin, "an override needs its own transport")
	require.Same(t, tenMin, h.transportFor(10*time.Minute),
		"repeated lookups of one duration should return the same transport")

	fiveMin := h.transportFor(5 * time.Minute)
	require.NotSame(t, tenMin, fiveMin, "distinct durations need distinct transports")
	require.Len(t, h.transports, 2)
}

func TestTransportForAppliesOverrideTimeout(t *testing.T) {
	// Backend that never sends response headers.
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer backend.Close()

	// A generous server default: without the override, this request would hang
	// well past the test's patience.
	h := newTimeoutTestServer(30 * time.Second)

	req, err := http.NewRequest(http.MethodGet, backend.URL+"/slow", nil)
	require.NoError(t, err)

	start := time.Now()
	//nolint:bodyclose // the round trip is expected to fail, so there is no body
	_, err = h.transportFor(100 * time.Millisecond).RoundTrip(req)
	elapsed := time.Since(start)

	require.Error(t, err, "the override timeout should have cut the request short")
	require.Less(t, elapsed, 5*time.Second,
		"request should have timed out on the override, not the 30s default")
}
