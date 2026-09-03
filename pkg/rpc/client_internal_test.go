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

	// Keep a UDP socket open without reading from it. This behaves like a
	// coordinator whose address is routable but whose QUIC server is down: the
	// client gets no ICMP rejection and no handshake response.
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

// TestWebTransportDialHonorsContextAfterHandshakeWithoutSettings is the core
// regression for this bug. A raw QUIC listener completes the handshake and then
// deliberately never speaks HTTP/3, so the client never receives a SETTINGS
// frame. Before the fix, webtransport-go's settings-wait select blocks forever
// (it reacts only to ReceivedSettings and the Dialer's own lifetime context,
// never the caller ctx) and the watch loop wedges until the process restarts.
// After the fix, dialWebTransport races the dial against ctx and closes a
// per-call Dialer to unblock the settings-wait, so the dial aborts on the
// caller's deadline.
func TestWebTransportDialHonorsContextAfterHandshakeWithoutSettings(t *testing.T) {
	r := require.New(t)

	addr := quietQUICServer(t, t.Context(), func(ctx context.Context, conn *quic.Conn) {
		// Handshake completed; never send an HTTP/3 control stream. Hold the
		// connection open so the client really is waiting on SETTINGS rather
		// than on QUIC connection teardown.
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

	// Prove the QUIC handshake really completed, so this test exercises the
	// post-handshake settings-wait rather than the pre-handshake path the
	// blackhole tests already cover. Before the fix this is exactly the state
	// in which the dial should hang.
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

// TestWebTransportDialHonorsContextAfterHandshakeThenConnDeath models a
// coordinator that is killed (SIGKILL / OOM / panic) in the one-RTT window after
// the QUIC handshake completes but before its SETTINGS packet transits. The
// peer tears the connection down with CONNECTION_CLOSE, yet in quic-go that
// teardown does NOT close ReceivedSettings, so webtransport-go's settings-wait
// stays blocked until the Dialer is closed. Before the fix the dial hung
// forever (production never calls Dialer.Close); after the fix the caller's ctx
// releases it.
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

// TestWebTransportDialHonorsContextAfterSettingsWithoutResponse covers the
// phase behind the settings wait. Here the peer is a real WebTransport server,
// so SETTINGS arrive and the dial gets past the wait that the two tests above
// exercise — then the handler never answers the CONNECT, leaving the dial
// blocked in http3.RequestStream.ReadResponse, which takes no context.
//
// Closing the per-call Dialer does nothing for this state, and neither does
// waiting: DefaultQUICConfig keepalives are shorter than its idle timeout, so
// the connection stays healthy indefinitely. Only tearing the connection down
// releases the read. The test asserts both halves of that: the caller returns
// on its deadline, and the connection the dial created is actually closed, which
// is what lets the dial goroutine exit instead of being stranded for the life of
// the process on every reconnect attempt.
func TestWebTransportDialHonorsContextAfterSettingsWithoutResponse(t *testing.T) {
	r := require.New(t)

	// Held open for the whole test so the handler never returns a response.
	// The timer is a backstop: nothing should reach it, but a handler blocked
	// forever would wedge the server's own shutdown rather than fail the test.
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

	// Wrap the client's DialAddr so the test can see the connection the dial
	// creates and assert it gets torn down.
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

// webTransportServer starts a real webtransport.Server on 127.0.0.1 whose
// HTTP/3 handler is handler, and returns the address to dial. Because it is a
// genuine WebTransport server, a client reaches it with SETTINGS negotiated —
// so a handler that never writes a response leaves the dial past the settings
// wait and blocked on the CONNECT response.
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

// TestWebTransportDialSucceedsWhenServerAnswers guards the success path: with a
// real WebTransport server returning HTTP/3 settings and a 2xx CONNECT, the
// per-call Dialer introduced by the fix must still establish a usable session,
// and closing that Dialer on return must not tear the session down (the
// returned Session owns its own context, rooted at context.Background).
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

	// dialWebTransport has already returned, so its per-call Dialer is already
	// closed. The session it handed back must still be alive.
	r.NoError(sess.Context().Err())

	// And the wire must still work: open a stream through the closed-dialer
	// session and round-trip a byte.
	str, err := sess.OpenStreamSync(ctx)
	r.NoError(err)
	defer func() { _ = str.Close() }()
	_, err = str.Write([]byte("hi"))
	r.NoError(err)
}

// newTestWebTransportClient builds a NetworkClient pointed at addr over its own
// UDP socket, wired for the QUIC/HTTP3+WebTransport path, and registers cleanup
// for the transports it creates. Tests dial through this client.
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
		// webtransport.Dialer.Close panics if Dial was never invoked (its initOnce
		// never ran, so ctxCancel is nil). The shared c.ws is only a config
		// holder for dialWebTransport's per-call dialers; guard the close so
		// cleanup is safe whether or not a test dialed through c.ws directly.
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

// quietQUICServer starts a raw QUIC listener (full handshake) on 127.0.0.1 and
// hands the first accepted *quic.Conn to run. It never speaks HTTP/3, so a
// client that dials it completes the QUIC handshake but never receives a
// SETTINGS frame — the post-handshake state this bug lives in. Returns the
// address the client should dial.
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
