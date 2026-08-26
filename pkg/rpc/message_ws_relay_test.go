package rpc_test

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/example"
)

// relay is a minimal stand-in for an intermediary that forwards this transport's
// frames without understanding them: it authorizes the handshake, then pumps
// binary messages between the caller and the real server. It is deliberately
// dumb, because that is the property under test — the frames have to survive a
// hop that can read none of them.
type relay struct {
	url    string // the caller-facing "host:port/path" remote
	target string // the server's "host:port"

	mu     sync.Mutex
	tokens []string // bearer tokens seen at the handshake, in order
}

// newRelay starts a relay in front of target. plaintext serves it over plain
// HTTP, the way a local development endpoint with no certificate would.
func newRelay(t *testing.T, ctx context.Context, target string, plaintext bool) *relay {
	t.Helper()

	rl := &relay{target: target}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/relay" {
			http.NotFound(w, r)
			return
		}

		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		rl.mu.Lock()
		rl.tokens = append(rl.tokens, token)
		rl.mu.Unlock()

		caller, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer caller.CloseNow()
		caller.SetReadLimit(-1)

		server, _, err := websocket.Dial(ctx, "wss://"+rl.target+"/_rpc/message", &websocket.DialOptions{
			HTTPClient: &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test relay
				},
			},
		})
		if err != nil {
			return
		}
		defer server.CloseNow()
		server.SetReadLimit(-1)

		pump := func(from, to *websocket.Conn) {
			for {
				typ, data, err := from.Read(ctx)
				if err != nil {
					return
				}
				if err := to.Write(ctx, typ, data); err != nil {
					return
				}
			}
		}

		done := make(chan struct{}, 2)
		go func() { pump(caller, server); done <- struct{}{} }()
		go func() { pump(server, caller); done <- struct{}{} }()
		<-done
	})

	srv, scheme := httptest.NewTLSServer(handler), "wss://"
	if plaintext {
		srv, scheme = httptest.NewUnstartedServer(handler), "ws://"
		srv.Start()
	}
	t.Cleanup(srv.Close)

	u, err := url.Parse(srv.URL)
	require.NoError(t, err)
	rl.url = scheme + u.Host + "/relay"

	return rl
}

func (r *relay) seenTokens() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.tokens...)
}

// A remote naming a path reaches an endpoint that is not ours, and the bearer
// authenticates the handshake so that endpoint can authorize the connection
// before it has forwarded a byte. Together those are what let RPC run through an
// intermediary that cannot read the traffic it is carrying.
func TestWSRelayedRemote(t *testing.T) {
	t.Run("calls survive the hop", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		ss, err := rpc.NewState(ctx, rpc.WithSkipVerify, rpc.WithWSBindAddr("localhost:0"))
		r.NoError(err)
		ss.Server().ExposeValue("meter", example.AdaptMeter(&exampleMeter{temp: 42}))

		rl := newRelay(t, ctx, ss.WSListenAddr(), false)

		cs, err := rpc.NewState(ctx, rpc.WithSkipVerify, rpc.WithBearerToken("user-token"))
		r.NoError(err)

		c, err := cs.Connect(rl.url, "meter")
		r.NoError(err)

		mc := &example.MeterClient{Client: c}

		res, err := mc.ReadTemperature(ctx, "test")
		r.NoError(err)
		r.Equal(float32(42), res.Reading().Temperature())

		// A capability returned by the first call is followed over the same
		// relayed session, so the hop survives more than one round trip.
		set, err := mc.GetSetter(ctx, "test")
		r.NoError(err)

		got, err := set.Setter().SetTemp(ctx, 100)
		r.NoError(err)
		r.Equal(int32(100), got.Temp())

		r.Equal([]string{"user-token"}, rl.seenTokens())
	})

	// A credential that refreshes must be read at dial time. Pinning it when the
	// client was built would send a token that has since expired, and the failure
	// would look like a permissions problem rather than a stale copy.
	t.Run("a refreshing token is resolved per dial", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		ss, err := rpc.NewState(ctx, rpc.WithSkipVerify, rpc.WithWSBindAddr("localhost:0"))
		r.NoError(err)
		ss.Server().ExposeValue("meter", example.AdaptMeter(&exampleMeter{temp: 42}))

		rl := newRelay(t, ctx, ss.WSListenAddr(), false)

		cs, err := rpc.NewState(ctx, rpc.WithSkipVerify,
			rpc.WithBearerToken("stale"),
			rpc.WithBearerTokenFunc(func() (string, error) { return "fresh", nil }),
		)
		r.NoError(err)

		c, err := cs.Connect(rl.url, "meter")
		r.NoError(err)

		_, err = (&example.MeterClient{Client: c}).ReadTemperature(ctx, "test")
		r.NoError(err)

		r.Equal([]string{"fresh"}, rl.seenTokens())
	})

	// Without a bearer the handshake is refused, which is the intermediary doing
	// its job. The client must surface that as a failure to connect rather than
	// producing a client that fails later and elsewhere.
	t.Run("an unauthorized handshake fails the connect", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		ss, err := rpc.NewState(ctx, rpc.WithSkipVerify, rpc.WithWSBindAddr("localhost:0"))
		r.NoError(err)
		ss.Server().ExposeValue("meter", example.AdaptMeter(&exampleMeter{temp: 42}))

		rl := newRelay(t, ctx, ss.WSListenAddr(), false)

		cs, err := rpc.NewState(ctx, rpc.WithSkipVerify)
		r.NoError(err)

		_, err = cs.Connect(rl.url, "meter")
		r.Error(err)
	})

	// A local development endpoint has no certificate, so the scheme has to be
	// able to say so. Without this the only reachable relay is one fronted by
	// TLS, which rules out running the whole thing on a laptop.
	t.Run("a plaintext relay is reachable over ws://", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		ss, err := rpc.NewState(ctx, rpc.WithSkipVerify, rpc.WithWSBindAddr("localhost:0"))
		r.NoError(err)
		ss.Server().ExposeValue("meter", example.AdaptMeter(&exampleMeter{temp: 42}))

		rl := newRelay(t, ctx, ss.WSListenAddr(), true)
		r.True(strings.HasPrefix(rl.url, "ws://"), "expected a plaintext remote, got %q", rl.url)

		cs, err := rpc.NewState(ctx, rpc.WithSkipVerify, rpc.WithBearerToken("user-token"))
		r.NoError(err)

		c, err := cs.Connect(rl.url, "meter")
		r.NoError(err)

		res, err := (&example.MeterClient{Client: c}).ReadTemperature(ctx, "test")
		r.NoError(err)
		r.Equal(float32(42), res.Reading().Temperature())
	})
}
