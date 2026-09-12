package rpc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
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

func TestWebTransportDialHandlesAlreadyCanceledContext(t *testing.T) {
	client := newTestWebTransportClient(t, "127.0.0.1:1")

	// An index watch can begin dialing after its controller has already started
	// shutting down. Exercise cancellation before the shared transport initializes.
	for range 100 {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		_, _, err := client.dialWebTransport(ctx, "https://127.0.0.1:1/", nil)
		require.ErrorIs(t, err, context.Canceled)
	}
}

func TestWebTransportDialClosesConnectionReturnedAfterCancellation(t *testing.T) {
	addr := quietQUICServer(t, t.Context(), func(_ context.Context, conn *quic.Conn) {
		<-conn.Context().Done()
	})

	client := newTestWebTransportClient(t, addr)
	originalDial := client.ws.DialAddr
	dialReturned := make(chan *quic.Conn, 1)
	releaseDial := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseDial) }) }
	t.Cleanup(release)
	client.ws.DialAddr = func(ctx context.Context, addr string, tlsCfg *tls.Config, cfg *quic.Config) (*quic.Conn, error) {
		conn, err := originalDial(ctx, addr, tlsCfg, cfg)
		if err != nil {
			return nil, err
		}
		dialReturned <- conn
		<-releaseDial
		return conn, nil
	}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, _, err := client.dialWebTransport(ctx, "https://"+addr+"/", nil)
		done <- err
	}()

	var dialConn *quic.Conn
	select {
	case dialConn = <-dialReturned:
	case <-time.After(3 * time.Second):
		t.Fatal("QUIC dial did not complete")
	}
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("WebTransport dial did not return after cancellation")
	}

	// The underlying callback returns after cancellation cleanup has already
	// observed no connection. It must close rather than publish that late conn.
	release()
	select {
	case <-dialConn.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("connection returned after cancellation remained open")
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

// Peer death before SETTINGS must surface the peer error immediately, rather
// than waiting for the caller's deadline.
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
		var peerError *quic.ApplicationError
		r.ErrorAs(err, &peerError)
		r.Equal(quic.ApplicationErrorCode(1), peerError.ErrorCode)
		r.True(peerError.Remote)
	case <-time.After(3 * time.Second):
		t.Fatal("WebTransport dial hung after the peer died post-handshake and the caller context expired")
	}
}

// The phase behind the settings wait: a real WebTransport server, so SETTINGS
// arrive, but the handler never answers the CONNECT and the dial sits in
// ReadResponse. Caller cancellation alone does nothing here and keepalives keep the
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
		H3: &http3.Server{
			Handler:    handler,
			TLSConfig:  testServerTLSConfig(t),
			QUICConfig: &quic.Config{EnableDatagrams: true, EnableStreamResetPartialDelivery: true},
		},
		// The test client dials a bare IP:port; accept it.
		CheckOrigin: func(*http.Request) bool { return true },
	}
	go func() { _ = srv.Serve(packetConn) }()
	t.Cleanup(func() { _ = srv.Close() })

	return packetConn.LocalAddr().String()
}

// A completed dial leaves its session usable through the shared transport.
func TestWebTransportDialSucceedsWhenServerAnswers(t *testing.T) {
	r := require.New(t)

	srvPacketConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	r.NoError(err)
	t.Cleanup(func() { _ = srvPacketConn.Close() })
	addr := srvPacketConn.LocalAddr().String()

	var wtSrv *webtransport.Server
	wtSrv = &webtransport.Server{
		H3: &http3.Server{
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
			QUICConfig: &quic.Config{EnableDatagrams: true, EnableStreamResetPartialDelivery: true},
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

	// Returning from the dial must leave the session alive.
	r.NoError(sess.Context().Err())

	// And the wire still works through it.
	str, err := sess.OpenStreamSync(ctx)
	r.NoError(err)
	defer func() { _ = str.Close() }()
	_, err = str.Write([]byte("hi"))
	r.NoError(err)
}

func TestWebTransportDialCancellationLeavesOtherSessionAlive(t *testing.T) {
	packetConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	require.NoError(t, err)
	t.Cleanup(func() { _ = packetConn.Close() })
	blocked := make(chan struct{})
	var server *webtransport.Server
	server = &webtransport.Server{
		H3: &http3.Server{
			TLSConfig: testServerTLSConfig(t),
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/blocked" {
					close(blocked)
					<-r.Context().Done()
					return
				}
				session, err := server.Upgrade(w, r)
				if err != nil {
					return
				}
				str, err := session.AcceptStream(r.Context())
				if err != nil {
					return
				}
				_, _ = io.Copy(str, str)
				_ = str.Close()
			}),
		},
	}
	go func() { _ = server.Serve(packetConn) }()
	t.Cleanup(func() { _ = server.Close() })
	client := newTestWebTransportClient(t, packetConn.LocalAddr().String())
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	blockedCtx, cancelBlocked := context.WithCancel(ctx)
	defer cancelBlocked()
	done := make(chan error, 1)
	go func() {
		_, _, err := client.dialWebTransport(blockedCtx, "https://"+client.remote+"/blocked", nil)
		done <- err
	}()
	select {
	case <-blocked:
	case <-ctx.Done():
		t.Fatal("CONNECT did not reach the server")
	}
	_, session, err := client.dialWebTransport(ctx, "https://"+client.remote+"/echo", nil)
	require.NoError(t, err)
	defer func() { _ = session.CloseWithError(0, "") }()
	cancelBlocked()
	require.ErrorIs(t, <-done, context.Canceled)
	str, err := session.OpenStreamSync(ctx)
	require.NoError(t, err)
	_, err = io.WriteString(str, "still alive")
	require.NoError(t, err)
	require.NoError(t, str.Close())
	body, err := io.ReadAll(str)
	require.NoError(t, err)
	require.Equal(t, "still alive", string(body))
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
		// Caller cancellation releases SETTINGS waits; closing the QUIC
		// transport releases any CONNECT reads still finishing in background.
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

	ln, err := quic.Listen(packetConn, testServerTLSConfig(t), &quic.Config{EnableDatagrams: true, EnableStreamResetPartialDelivery: true})
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

// Cancelling a dial while its handshake is still in flight has to tell the
// server. quic-go's own cancellation destroys the client side without a
// CONNECTION_CLOSE, leaving a server that accepted the connection early to
// discover the peer is gone only at the idle timeout, which is how a
// coordinator's own entity watches held its drain past the deadline.
func TestQUICDialCancelledMidHandshakeClosesServerSide(t *testing.T) {
	r := require.New(t)

	packetConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	r.NoError(err)
	t.Cleanup(func() { _ = packetConn.Close() })
	ln, err := quic.ListenEarly(packetConn, testServerTLSConfig(t), &quic.Config{
		EnableDatagrams:                  true,
		EnableStreamResetPartialDelivery: true,
		// Longer than the assertion window, so the server can only let go
		// because the client told it to.
		HandshakeIdleTimeout: 30 * time.Second,
		MaxIdleTimeout:       30 * time.Second,
	})
	r.NoError(err)
	t.Cleanup(func() { _ = ln.Close() })
	accepted := make(chan *quic.Conn, 1)
	go func() {
		conn, err := ln.Accept(t.Context())
		if err == nil {
			accepted <- conn
		}
	}()

	// Hold the client in certificate verification. By then the server has
	// handed the connection to Accept and is waiting on the client's Finished.
	verifying := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseClient := func() { releaseOnce.Do(func() { close(release) }) }
	client := newTestWebTransportClient(t, packetConn.LocalAddr().String())
	// Registered after the client so it runs before the client's transport
	// is closed; a failing run must not leave that close waiting on a
	// handshake nobody will finish.
	t.Cleanup(releaseClient)
	client.tlsCfg.VerifyPeerCertificate = func([][]byte, [][]*x509.Certificate) error {
		close(verifying)
		<-release
		return nil
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, _, err := client.ws.Dial(ctx, "https://"+client.remote+"/", nil)
		done <- err
	}()

	select {
	case <-verifying:
	case <-time.After(3 * time.Second):
		t.Fatal("client never reached certificate verification")
	}
	var serverConn *quic.Conn
	select {
	case serverConn = <-accepted:
	case <-time.After(3 * time.Second):
		t.Fatal("server did not accept the connection before the handshake finished")
	}

	cancel()
	r.NoError(serverConn.Context().Err(), "server side closed before the client could have told it anything")
	releaseClient()
	select {
	case err := <-done:
		r.ErrorIs(err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("dial did not return after cancellation")
	}

	select {
	case <-serverConn.Context().Done():
	case <-time.After(2 * time.Second):
		t.Fatal("server side of a cancelled dial stayed open")
	}
}
