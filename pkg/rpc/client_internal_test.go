package rpc

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/webtransport-go"
	"github.com/stretchr/testify/require"
)

func TestWebTransportDialHonorsContextWhenServerDoesNotAnswer(t *testing.T) {
	r := require.New(t)

	// A UDP socket nobody reads: like a coordinator whose address is routable
	// but whose QUIC server is down. No rejection, no handshake response.
	blackhole, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	r.NoError(err)
	t.Cleanup(func() { _ = blackhole.Close() })

	client := newTestWebTransportClient(t, blackhole.LocalAddr().String())

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

// The core regression: a raw QUIC listener completes the handshake and then
// never speaks HTTP/3, so SETTINGS never arrive and webtransport-go's wait for
// them has nothing to release it. The dial must still abort on the deadline.
func TestWebTransportDialHonorsContextAfterHandshakeWithoutSettings(t *testing.T) {
	r := require.New(t)

	addr := quietQUICServer(t, t.Context(), func(ctx context.Context, conn *quic.Conn) {
		// Hold the connection open, so the client is waiting on SETTINGS
		// rather than on the connection going away.
		<-ctx.Done()
		_ = conn.CloseWithError(0, "test")
	})

	client := newTestWebTransportClient(t, addr)

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, _, err := client.dialWebTransport(ctx, "https://"+addr+"/", nil)
		done <- err
	}()

	// Prove the handshake completed, so this exercises the settings wait
	// rather than the pre-handshake path the blackhole test covers.
	r.Eventually(func() bool {
		return client.reachedServer()
	}, time.Second, 10*time.Millisecond)

	select {
	case err := <-done:
		r.Error(err)
		r.ErrorIs(err, context.DeadlineExceeded)
	case <-time.After(3 * time.Second):
		t.Fatal("WebTransport dial hung after the caller context expired (handshake completed but HTTP/3 SETTINGS never arrived)")
	}
}

// A coordinator killed in the one-RTT window after the handshake, before its
// SETTINGS packet transits. The peer closes the connection, but quic-go does
// not close ReceivedSettings on teardown, so that alone releases nothing.
func TestWebTransportDialHonorsContextAfterHandshakeThenConnDeath(t *testing.T) {
	r := require.New(t)

	addr := quietQUICServer(t, t.Context(), func(_ context.Context, conn *quic.Conn) {
		// Handshake completed; crash immediately.
		_ = conn.CloseWithError(1, "crash")
	})

	client := newTestWebTransportClient(t, addr)

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, _, err := client.dialWebTransport(ctx, "https://"+addr+"/", nil)
		done <- err
	}()

	r.Eventually(func() bool {
		return client.reachedServer()
	}, time.Second, 10*time.Millisecond)

	select {
	case err := <-done:
		r.Error(err)
		// Conn teardown alone does not unblock the settings-wait; the dial is
		// released by the caller's deadline, exactly as in the no-settings case.
		r.ErrorIs(err, context.DeadlineExceeded)
	case <-time.After(3 * time.Second):
		t.Fatal("WebTransport dial hung after the peer died post-handshake and the caller context expired")
	}
}

// The phase behind the settings wait: a real WebTransport server, so SETTINGS
// arrive, but the handler never answers the CONNECT and the dial sits in
// ReadResponse. Closing the Dialer does nothing here and keepalives keep the
// connection healthy, so only closing the connection releases it. Asserts both
// halves: the caller returns on its deadline, and the connection is closed,
// which is what lets the dial goroutine exit.
func TestWebTransportDialHonorsContextAfterSettingsWithoutResponse(t *testing.T) {
	r := require.New(t)

	// Held open so the handler never returns a response. The timer is a
	// backstop against wedging the server's shutdown instead of failing.
	blocked := make(chan struct{})
	addr := webTransportServer(t, func(http.ResponseWriter, *http.Request) {
		select {
		case <-blocked:
		case <-time.After(30 * time.Second):
		}
	})
	// Registered after the server so cleanup (which runs last-registered
	// first) releases the handler before shutting the server down.
	t.Cleanup(func() { close(blocked) })

	client := newTestWebTransportClient(t, addr)

	// Wrap DialAddr so the test can see the connection the dial creates.
	inner := client.ws.DialAddr
	var (
		mu       sync.Mutex
		dialConn *quic.Conn
	)
	client.ws.DialAddr = func(ctx context.Context, a string, tlsCfg *tls.Config, cfg *quic.Config) (*quic.Conn, error) {
		conn, err := inner(ctx, a, tlsCfg, cfg)
		if err == nil {
			mu.Lock()
			dialConn = conn
			mu.Unlock()
		}
		return conn, err
	}

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, _, err := client.dialWebTransport(ctx, "https://"+addr+"/", nil)
		done <- err
	}()

	select {
	case err := <-done:
		r.Error(err)
		r.ErrorIs(err, context.DeadlineExceeded)
	case <-time.After(3 * time.Second):
		t.Fatal("WebTransport dial hung after SETTINGS arrived but the CONNECT went unanswered")
	}

	mu.Lock()
	conn := dialConn
	mu.Unlock()
	r.NotNil(conn, "the dial must have created a connection to get this far")

	// The abandoned connection must be closed, not left alive by keepalives.
	r.Eventually(func() bool {
		return conn.Context().Err() != nil
	}, time.Second, 10*time.Millisecond,
		"the connection an abandoned dial was reading from must be torn down")
}

// webTransportServer starts a real webtransport.Server on 127.0.0.1 and returns
// the address to dial. Being genuine, it negotiates SETTINGS, so a handler that
// never writes a response leaves the dial blocked on the CONNECT instead.
func webTransportServer(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	r := require.New(t)

	packetConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	r.NoError(err)
	t.Cleanup(func() { _ = packetConn.Close() })

	srv := &webtransport.Server{
		H3: http3.Server{
			Handler:    handler,
			TLSConfig:  testServerTLSConfig(t),
			QUICConfig: &quic.Config{EnableDatagrams: true},
		},
		// The test client dials a bare IP:port; accept it.
		CheckOrigin: func(*http.Request) bool { return true },
	}
	go func() { _ = srv.Serve(packetConn) }()
	t.Cleanup(func() { _ = srv.Close() })

	return packetConn.LocalAddr().String()
}

// The success path: a per-call Dialer must still establish a usable session,
// and closing it on return must not tear that session down.
func TestWebTransportDialSucceedsWhenServerAnswers(t *testing.T) {
	r := require.New(t)

	srvPacketConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	r.NoError(err)
	t.Cleanup(func() { _ = srvPacketConn.Close() })
	addr := srvPacketConn.LocalAddr().String()

	var wtSrv *webtransport.Server
	wtSrv = &webtransport.Server{
		H3: http3.Server{
			Handler: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				sess, err := wtSrv.Upgrade(w, req)
				if err != nil {
					return
				}
				// Hold the session open until the client (or test cleanup)
				// closes it.
				<-sess.Context().Done()
			}),
			TLSConfig:  testServerTLSConfig(t),
			QUICConfig: &quic.Config{EnableDatagrams: true},
		},
		// The test client dials a bare IP:port; accept it.
		CheckOrigin: func(*http.Request) bool { return true },
	}
	go func() { _ = wtSrv.Serve(srvPacketConn) }()
	t.Cleanup(func() { _ = wtSrv.Close() })

	client := newTestWebTransportClient(t, addr)

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()

	hr, sess, err := client.dialWebTransport(ctx, "https://"+addr+"/", nil)
	r.NoError(err)
	r.NotNil(sess)
	r.Equal(http.StatusOK, hr.StatusCode)
	defer func() { _ = sess.CloseWithError(0, "") }()

	// The per-call Dialer is closed by now; the session must still be alive.
	r.NoError(sess.Context().Err())

	// And the wire still works through it.
	str, err := sess.OpenStreamSync(ctx)
	r.NoError(err)
	defer func() { _ = str.Close() }()
	_, err = str.Write([]byte("hi"))
	r.NoError(err)
}

// newTestWebTransportClient builds a NetworkClient pointed at addr over its own
// UDP socket, and registers cleanup for the transports it creates.
func newTestWebTransportClient(t *testing.T, addr string) *NetworkClient {
	t.Helper()
	r := require.New(t)

	packetConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	r.NoError(err)

	state := &State{StateCommon: &StateCommon{log: slog.Default()}}
	client := &NetworkClient{
		State:     state,
		transport: &quic.Transport{Conn: packetConn},
		tlsCfg:    &tls.Config{InsecureSkipVerify: true}, // test-only endpoint
		remote:    addr,
	}
	client.setupTransport()
	t.Cleanup(func() {
		// Dialer.Close panics if Dial never ran, leaving ctxCancel nil. Most
		// tests never dial through c.ws directly, so guard the close.
		func() {
			defer func() { _ = recover() }()
			_ = client.ws.Close()
		}()
		_ = client.htr.Close()
		_ = client.transport.Close()
	})
	return client
}

// testServerTLSConfig builds a self-signed TLS server config offering HTTP/3
// (h3 ALPN), for WebTransport test servers.
func testServerTLSConfig(t *testing.T) *tls.Config {
	t.Helper()
	r := require.New(t)
	cert, err := generateSelfSignedCert()
	r.NoError(err)
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{http3.NextProtoH3},
	}
}

// quietQUICServer starts a raw QUIC listener on 127.0.0.1 and hands the first
// accepted connection to run. It never speaks HTTP/3, so a client completes the
// handshake and then waits on SETTINGS forever. Returns the address to dial.
func quietQUICServer(t *testing.T, parent context.Context, run func(context.Context, *quic.Conn)) string {
	t.Helper()
	r := require.New(t)

	packetConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	r.NoError(err)
	t.Cleanup(func() { _ = packetConn.Close() })

	ln, err := quic.Listen(packetConn, testServerTLSConfig(t), &quic.Config{EnableDatagrams: true})
	r.NoError(err)
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		conn, err := ln.Accept(parent)
		if err != nil {
			return
		}
		run(parent, conn)
	}()

	return packetConn.LocalAddr().String()
}
