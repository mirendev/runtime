package rpc

import (
	"context"
	"crypto/tls"
	"errors"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/require"
)

func TestStreamingRPCReleasesConnections(t *testing.T) {
	for _, outcome := range []string{"success", "handler error", "canceled", "rejected"} {
		t.Run(outcome, func(t *testing.T) {
			state, err := NewState(t.Context(), WithSkipVerify)
			require.NoError(t, err)
			t.Cleanup(func() { _ = state.Close() })
			started := make(chan struct{}, 1)
			state.Server().ExposeValue("lifecycle", &Interface{
				name: "Lifecycle",
				methods: map[string]Method{
					"call": {Name: "call", InterfaceName: "Lifecycle", Handler: func(ctx context.Context, call Call) error {
						var args map[string]any
						call.Args(&args)
						if outcome == "canceled" {
							started <- struct{}{}
							<-ctx.Done()
							return ctx.Err()
						}
						if outcome == "handler error" {
							return errors.New("handler failed")
						}
						call.Results(map[string]any{})
						return nil
					}},
				},
			})
			client, err := state.Connect(state.LoopbackAddr(), "lifecycle")
			require.NoError(t, err)
			method := "call"
			if outcome == "rejected" {
				method = "missing"
			}
			for range 20 {
				ctx, cancel := context.WithCancel(t.Context())
				done := make(chan error, 1)
				go func() {
					var result map[string]any
					done <- client.CallWithCaps(ctx, method, map[string]any{}, &result, nil)
				}()
				if outcome == "canceled" {
					select {
					case <-started:
					case <-time.After(5 * time.Second):
						cancel()
						t.Fatal("streaming handler did not start")
					}
					cancel()
				}
				select {
				case err := <-done:
					cancel()
					if outcome == "success" {
						require.NoError(t, err)
					} else {
						require.Error(t, err)
					}
				case <-time.After(5 * time.Second):
					cancel()
					t.Fatal("streaming RPC did not finish")
				}
			}

			// Only the original unary connection should remain. The State tracks
			// live connections; the client must also release its diagnostic refs.
			require.Eventually(t, func() bool {
				state.outboundMu.Lock()
				defer state.outboundMu.Unlock()
				return len(state.outboundConns) == 1
			}, 5*time.Second, 10*time.Millisecond, "completed streaming RPCs left live QUIC connections")
			require.Eventually(t, func() bool {
				client.connMu.Lock()
				defer client.connMu.Unlock()
				return len(client.conns) == 1
			}, time.Second, 10*time.Millisecond, "client retained closed QUIC connections")
		})
	}
}

func TestClientRemembersHandshakeAfterConnectionCloses(t *testing.T) {
	addr := quietQUICServer(t, t.Context(), func(_ context.Context, conn *quic.Conn) {
		<-conn.Context().Done()
	})
	conn, err := quic.DialAddr(t.Context(), addr, &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"h3"},
	}, &quic.Config{})
	require.NoError(t, err)
	client := new(NetworkClient)
	client.trackConn(conn)
	require.NoError(t, conn.CloseWithError(0, "test"))
	require.Eventually(t, func() bool {
		client.connMu.Lock()
		defer client.connMu.Unlock()
		return len(client.conns) == 0
	}, time.Second, time.Millisecond)
	// No diagnostic lookup happened before the connection was removed.
	require.True(t, client.reachedServer())
}
