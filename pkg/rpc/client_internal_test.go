package rpc

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/require"
)

func TestWebTransportDialHonorsContextWhenServerDoesNotAnswer(t *testing.T) {
	r := require.New(t)

	// Keep a UDP socket open without reading from it. This behaves like a
	// coordinator whose address is routable but whose QUIC server is down: the
	// client gets no ICMP rejection and no handshake response.
	blackhole, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	r.NoError(err)
	t.Cleanup(func() { _ = blackhole.Close() })

	packetConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	r.NoError(err)

	state := &State{StateCommon: &StateCommon{log: slog.Default()}}
	client := &NetworkClient{
		State:     state,
		transport: &quic.Transport{Conn: packetConn},
		tlsCfg:    &tls.Config{InsecureSkipVerify: true}, // test-only endpoint
		remote:    blackhole.LocalAddr().String(),
	}
	client.setupTransport()
	t.Cleanup(func() {
		_ = client.ws.Close()
		_ = client.htr.Close()
		_ = client.transport.Close()
	})

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, _, err := client.ws.Dial(ctx, "https://"+client.remote+"/", nil)
		done <- err
	}()

	select {
	case err := <-done:
		r.Error(err)
		r.ErrorIs(err, context.DeadlineExceeded)
	case <-time.After(time.Second):
		t.Fatal("WebTransport dial remained blocked after its context expired")
	}
}
