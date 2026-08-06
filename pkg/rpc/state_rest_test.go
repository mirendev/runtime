package rpc_test

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
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

// recordingAuthorizer captures what it was asked and can refuse, so a test can
// assert both that the gateway consults the authorizer at all and that it asks
// the same question the RPC path asks.
type recordingAuthorizer struct {
	deny     error
	resource string
	action   string
	calls    int
}

func (a *recordingAuthorizer) Authorize(ctx context.Context, identity *rpc.Identity, resource, action string) error {
	a.calls++
	a.resource = resource
	a.action = action
	return a.deny
}

func restListenerWithAuthz(t *testing.T, ctx context.Context, authz rpc.Authorizer) string {
	t.Helper()
	r := require.New(t)

	ss, err := rpc.NewState(ctx,
		rpc.WithSkipVerify,
		rpc.WithRESTBindAddr("localhost:0"),
		rpc.WithAuthenticator(&rpc.NoOpAuthenticator{}),
		rpc.WithAuthorizer(authz),
	)
	r.NoError(err)

	ss.Server().ExposeValue("dev.miren.runtime/meter", example.AdaptMeter(&restMeter{}))

	return "https://" + ss.RESTListenAddr()
}

func restGet(t *testing.T, url string) *http.Response {
	t.Helper()

	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}}

	resp, err := client.Get(url)
	require.NoError(t, err)
	return resp
}

// The REST surface must refuse what the RPC surface refuses. Authentication
// alone is not the contract: an identity the authorizer rejects has to be
// rejected here too, or REST becomes a way around per-app roles.
func TestRESTEnforcesAuthorizer(t *testing.T) {
	t.Run("a denied identity gets 403", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		r := require.New(t)

		authz := &recordingAuthorizer{deny: errors.New("role forbids this")}
		base := restListenerWithAuthz(t, ctx, authz)

		resp := restGet(t, base+"/api/v1/meters/room1/temperature")
		defer resp.Body.Close()

		r.Equal(http.StatusForbidden, resp.StatusCode)

		var out struct {
			Error string `json:"error"`
			Code  string `json:"code"`
		}
		r.NoError(json.NewDecoder(resp.Body).Decode(&out))
		r.Equal("forbidden", out.Code)
		r.Contains(out.Error, "role forbids this")
	})

	t.Run("asks the same resource/action the RPC path asks", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		r := require.New(t)

		authz := &recordingAuthorizer{}
		base := restListenerWithAuthz(t, ctx, authz)

		resp := restGet(t, base+"/api/v1/meters/room1/temperature")
		defer resp.Body.Close()

		r.Equal(http.StatusOK, resp.StatusCode)

		// handleCalls lowercases the interface and method names; anything else
		// here would silently miss policies written against the RPC surface.
		r.Equal(1, authz.calls)
		r.Equal("meter", authz.resource)
		r.Equal("readtemperature", authz.action)
	})
}

// The point of the gateway is that REST and RPC dispatch the same handlers, so
// they must also apply the same gate. This runs both transports against one
// server and asserts they agree on allow and on deny — a REST surface that
// answered what QUIC refuses would be a way around per-app roles.
func TestRESTAndRPCAgreeOnAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name     string
		deny     error
		wantREST int
		wantRPC  bool // expect the RPC call to fail
	}{
		{name: "allowed", deny: nil, wantREST: http.StatusOK, wantRPC: false},
		{name: "denied", deny: errors.New("role forbids this"), wantREST: http.StatusForbidden, wantRPC: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			r := require.New(t)

			ss, err := rpc.NewState(ctx,
				rpc.WithSkipVerify,
				rpc.WithRESTBindAddr("localhost:0"),
				rpc.WithAuthenticator(&rpc.NoOpAuthenticator{}),
				rpc.WithAuthorizer(&recordingAuthorizer{deny: tc.deny}),
			)
			r.NoError(err)

			ss.Server().ExposeValue("meter", example.AdaptMeter(&restMeter{}))

			resp := restGet(t, "https://"+ss.RESTListenAddr()+"/api/v1/meters/room1/temperature")
			defer resp.Body.Close()
			r.Equal(tc.wantREST, resp.StatusCode, "REST status")

			cs, err := rpc.NewState(ctx, rpc.WithSkipVerify)
			r.NoError(err)

			client, err := cs.Connect(ss.ListenAddr(), "meter")
			r.NoError(err)

			meter := example.NewMeterClient(client)
			_, rpcErr := meter.ReadTemperature(ctx, "room1")

			if tc.wantRPC {
				r.Error(rpcErr, "RPC should refuse what REST refused")
			} else {
				r.NoError(rpcErr, "RPC should allow what REST allowed")
			}
		})
	}
}
