package cloudrpc_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/cloudauth"
	"miren.dev/runtime/pkg/cloudrpc"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/example"
	"miren.dev/runtime/pkg/uplink"
)

// wsRelayConn is cloud's end of a relayed session, riding the same WebSocket the
// cluster's uplink is on. Frames go out as rpc.data envelopes and come back the
// same way; envelopes belonging to other tenants of the link are skipped.
type wsRelayConn struct {
	c         *websocket.Conn
	ctx       context.Context
	sessionID string
}

func (c *wsRelayConn) Send(b []byte) error {
	raw, err := json.Marshal(cloudrpc.Data{SessionID: c.sessionID, Payload: b})
	if err != nil {
		return err
	}
	return wsjson.Write(c.ctx, c.c, &uplink.Envelope{Type: cloudrpc.TypeData, Data: raw})
}

func (c *wsRelayConn) Recv() ([]byte, error) {
	for {
		var env uplink.Envelope
		if err := wsjson.Read(c.ctx, c.c, &env); err != nil {
			return nil, err
		}

		switch env.Type {
		case cloudrpc.TypeData:
			var msg cloudrpc.Data
			if err := json.Unmarshal(env.Data, &msg); err != nil {
				return nil, err
			}
			if msg.SessionID == c.sessionID {
				return msg.Payload, nil
			}
		case cloudrpc.TypeClose:
			var msg cloudrpc.Close
			if err := json.Unmarshal(env.Data, &msg); err != nil {
				return nil, err
			}
			if msg.SessionID == c.sessionID {
				return nil, io.EOF
			}
		}
	}
}

func (c *wsRelayConn) Close() error { return c.c.Close(websocket.StatusNormalClosure, "") }

// fakeCloud serves the two service-account auth endpoints the uplink
// authenticates with, plus the cluster channel itself. On connect it opens one
// relayed RPC session and hands its end to the test.
func fakeCloud(t *testing.T, ctx context.Context, sessions chan<- rpc.MessageConn) *httptest.Server {
	t.Helper()

	const token = "test-token"

	mux := http.NewServeMux()

	mux.HandleFunc("/auth/service-account/begin", func(w http.ResponseWriter, r *http.Request) {
		//nolint:errcheck // test server
		json.NewEncoder(w).Encode(map[string]string{
			"envelope":  "test-envelope",
			"challenge": "test-challenge",
		})
	})

	mux.HandleFunc("/auth/service-account/complete", func(w http.ResponseWriter, r *http.Request) {
		//nolint:errcheck // test server
		json.NewEncoder(w).Encode(map[string]any{
			"token":      token,
			"expires_in": 3600,
		})
	})

	mux.HandleFunc("/api/v1/cluster-channel/ws", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()

		// Whatever the cluster frames, base64'd, has to fit here.
		c.SetReadLimit(2 << 20)

		raw, err := json.Marshal(cloudrpc.Open{SessionID: "s1"})
		if err != nil {
			return
		}
		if err := wsjson.Write(ctx, c, &uplink.Envelope{Type: cloudrpc.TypeOpen, Data: raw}); err != nil {
			return
		}

		sessions <- &wsRelayConn{c: c, ctx: ctx, sessionID: "s1"}

		<-r.Context().Done()
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

// The end-to-end shape, over a real socket: a cluster holding an uplink, cloud
// opening a session on it, and a caller reaching the cluster's RPC server
// through that session. This is where the envelope encoding earns its keep —
// every frame is base64 inside JSON inside a WebSocket message.
func TestRelayOverARealUplink(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	sessions := make(chan rpc.MessageConn, 1)
	srv := fakeCloud(t, ctx, sessions)

	ss, err := rpc.NewState(ctx, rpc.WithSkipVerify)
	r.NoError(err)
	ss.Server().ExposeValue("meter", example.AdaptMeter(&testMeter{temp: 42}))

	keyPair, err := cloudauth.GenerateKeyPair()
	r.NoError(err)

	authClient, err := cloudauth.NewAuthClient(srv.URL, keyPair)
	r.NoError(err)

	link := uplink.NewClient(srv.URL, authClient, uplink.NewMessageRouter(), slog.Default())
	cloudrpc.New(cloudrpc.Config{Uplink: link, State: ss, Log: slog.Default()})

	go link.Run(ctx) //nolint:errcheck // ends with ctx

	var conn rpc.MessageConn
	select {
	case conn = <-sessions:
	case <-ctx.Done():
		r.Fail("cluster never connected to the fake cloud")
	}

	cs, err := rpc.NewState(ctx, rpc.WithSkipVerify)
	r.NoError(err)

	c, err := cs.ClientFromMessageConn(ctx, conn, "meter")
	r.NoError(err)

	mc := &example.MeterClient{Client: c}

	res, err := mc.ReadTemperature(ctx, "test")
	r.NoError(err)
	r.Equal(float32(42), res.Reading().Temperature())

	// Big enough that both directions cross several envelopes, and — the part
	// that pins the read limit — big enough that the caller, which frames at
	// pkg/rpc's 1 MiB default rather than the cluster's smaller cap, emits a
	// frame whose base64 is half again over 1 MiB. A limit budgeted for the
	// payload instead of its encoding would fail right here.
	big := make([]byte, 3<<20)
	for i := range big {
		big[i] = 'a'
	}

	res, err = mc.ReadTemperature(ctx, string(big))
	r.NoError(err)
	r.Equal(string(big), res.Reading().Meter())
}
