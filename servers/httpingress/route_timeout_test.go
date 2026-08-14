package httpingress

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/ingress/ingress_v1alpha"
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

func TestRouteRequestTimeout(t *testing.T) {
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
			require.Equal(t, tt.want, routeRequestTimeout(tt.route))
		})
	}
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
