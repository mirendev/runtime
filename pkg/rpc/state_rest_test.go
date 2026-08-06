package rpc_test

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/example"
)

// restListener starts a State serving the REST gateway on an ephemeral port and
// returns its base URL. The NoOp authenticator stands in for the auth chain a
// real deployment wires up: ServeHTTP puts its identity on the context, which is
// what restHandler requires for a non-public method.
func restListener(t *testing.T, ctx context.Context) string {
	t.Helper()
	r := require.New(t)

	ss, err := rpc.NewState(ctx,
		rpc.WithSkipVerify,
		rpc.WithRESTBindAddr("localhost:0"),
		rpc.WithAuthenticator(&rpc.NoOpAuthenticator{}),
	)
	r.NoError(err)

	ss.Server().ExposeValue("dev.miren.runtime/meter", example.AdaptMeter(&restMeter{}))

	return "https://" + ss.RESTListenAddr()
}

func TestRESTListener(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	base := restListener(t, ctx)

	// The gateway has to be reachable by clients that know nothing about QUIC,
	// so both HTTP versions are exercised against the same listener.
	clients := []struct {
		name      string
		transport http.RoundTripper
		wantProto int
	}{
		{
			name:      "http1.1",
			transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
			wantProto: 1,
		},
		{
			name:      "http2",
			transport: &http2.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
			wantProto: 2,
		},
	}

	for _, c := range clients {
		t.Run(c.name, func(t *testing.T) {
			r := require.New(t)

			client := &http.Client{Transport: c.transport}

			resp, err := client.Get(base + "/api/v1/meters/room1/temperature")
			r.NoError(err)
			defer resp.Body.Close()

			r.Equal(http.StatusOK, resp.StatusCode)
			r.Equal(c.wantProto, resp.ProtoMajor)

			var out struct {
				Reading struct {
					Meter       string  `json:"meter"`
					Temperature float32 `json:"temperature"`
				} `json:"reading"`
			}
			r.NoError(json.NewDecoder(resp.Body).Decode(&out))
			r.Equal("room1", out.Reading.Meter)
			r.Equal(float32(21.5), out.Reading.Temperature)
		})
	}

	t.Run("maps a handler error onto its status", func(t *testing.T) {
		r := require.New(t)

		client := &http.Client{Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}}

		resp, err := client.Get(base + "/api/v1/meters/basement/temperature")
		r.NoError(err)
		defer resp.Body.Close()

		r.Equal(http.StatusNotFound, resp.StatusCode)
	})

	t.Run("an un-annotated method is not routed", func(t *testing.T) {
		r := require.New(t)

		client := &http.Client{Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}}

		resp, err := client.Get(base + "/api/v1/meters/room1/setter")
		r.NoError(err)
		defer resp.Body.Close()

		r.Equal(http.StatusNotFound, resp.StatusCode)
	})
}

// Without WithRESTBindAddr nothing is mounted, so a deployment that has not
// opted in does not grow a REST surface on its existing listeners either.
func TestRESTNotMountedWithoutBindAddr(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := require.New(t)

	ss, err := rpc.NewState(ctx, rpc.WithSkipVerify)
	r.NoError(err)

	ss.Server().ExposeValue("dev.miren.runtime/meter", example.AdaptMeter(&restMeter{}))

	r.Empty(ss.RESTListenAddr())

	req, err := http.NewRequest("GET", "/api/v1/meters/room1/temperature", nil)
	r.NoError(err)

	w := httptest.NewRecorder()
	ss.Server().ServeHTTP(w, req)

	r.Equal(http.StatusNotFound, w.Code)
}

// The real app API is the first consumer of the gateway, and its routes are
// spread across several interfaces. ServeMux panics on conflicting patterns, so
// mounting the whole surface is worth proving rather than discovering at
// startup.
func TestRESTMountsAppAPI(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := require.New(t)

	ss, err := rpc.NewState(ctx,
		rpc.WithSkipVerify,
		rpc.WithRESTBindAddr("localhost:0"),
		rpc.WithAuthenticator(&rpc.NoOpAuthenticator{}),
	)
	r.NoError(err)

	// Handlers are never invoked here; ExposeValue only reads the method table.
	r.NotPanics(func() {
		ss.Server().ExposeValue("dev.miren.runtime/app", app_v1alpha.AdaptCrud(nil))
		ss.Server().ExposeValue("dev.miren.runtime/app-status", app_v1alpha.AdaptAppStatus(nil))
		ss.Server().ExposeValue("dev.miren.runtime/disks", app_v1alpha.AdaptDisks(nil))
		ss.Server().ExposeValue("dev.miren.runtime/addons", app_v1alpha.AdaptAddons(nil))
		ss.Server().ExposeValue("dev.miren.runtime/logs", app_v1alpha.AdaptLogs(nil))
	})

	// Re-exposing is legitimate (ExposeValue overwrites), so it must not try to
	// mount the same patterns a second time.
	r.NotPanics(func() {
		ss.Server().ExposeValue("dev.miren.runtime/app", app_v1alpha.AdaptCrud(nil))
	})
}
