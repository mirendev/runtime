package rpc

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/stretchr/testify/require"
)

func drainTestClient(t *testing.T) *drainingHTTP {
	t.Helper()
	return &drainingHTTP{
		base: &http3.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
		ctx:  t.Context(), prepare: func(context.Context, *http.Request) error { return nil },
	}
}

func drainTestRequest(t *testing.T, client *drainingHTTP, addr string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://"+addr+"/payload", nil)
	require.NoError(t, err)
	resp, err := client.RoundTrip(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func TestHTTPDrainRetiresIdlePoolWithoutGOAWAY(t *testing.T) {
	s, err := NewState(t.Context(), WithSkipVerify,
		WithHTTPHandler("GET /payload", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "ok")
		})))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	s.disableContextShutdown()

	client := drainTestClient(t)
	resp := drainTestRequest(t, client, s.LoopbackAddr())
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "ok", string(body))
	require.NoError(t, resp.Body.Close())

	// Cancel application lifetime without invoking HTTP/3 Shutdown. The
	// notification alone must release a pool that has no more calls to make.
	s.cancel()
	require.Eventually(t, s.li.idle, time.Second, time.Millisecond)
}

func TestHTTPDrainWaitsForResponseConsumption(t *testing.T) {
	release := make(chan struct{})
	payload := strings.Repeat("response", 64*1024)
	s, err := NewState(t.Context(), WithSkipVerify,
		WithHTTPHandler("GET /payload", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_ = http.NewResponseController(w).Flush()
			select {
			case <-release:
				_, _ = io.WriteString(w, payload)
			case <-r.Context().Done():
			}
		})))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	client := drainTestClient(t)
	resp := drainTestRequest(t, client, s.LoopbackAddr())
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	shutdown := make(chan error, 1)
	go func() { shutdown <- s.Shutdown(ctx) }()
	require.Eventually(t, func() bool {
		client.mu.Lock()
		defer client.mu.Unlock()
		return client.current == nil
	}, time.Second, time.Millisecond)
	require.False(t, s.li.idle(), "drain closed a response before the caller read it")

	close(release)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, payload, string(body))
	require.NoError(t, resp.Body.Close())
	require.Eventually(t, s.li.idle, time.Second, time.Millisecond)
	require.NoError(t, <-shutdown)
}

func TestHTTPDrainRejectsNewWorkAndReplacesRetiringPool(t *testing.T) {
	var calls atomic.Int32
	s, err := NewState(t.Context(), WithSkipVerify,
		WithHTTPHandler("GET /payload", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			_, _ = io.WriteString(w, "ok")
		})))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	s.disableContextShutdown()
	client := drainTestClient(t)
	resp := drainTestRequest(t, client, s.LoopbackAddr())
	_, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	s.cancel()
	require.Eventually(t, s.li.idle, time.Second, time.Millisecond)

	// Keep the listener open to exercise the admission gate even when a
	// request arrives after drain. Each request races its pool's notification.
	for range 10 {
		resp := drainTestRequest(t, client, s.LoopbackAddr())
		require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
		_, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
	}
	require.EqualValues(t, 1, calls.Load())
	require.GreaterOrEqual(t, s.li.accepted.Load(), int64(2), "client reused a retired connection")
	require.Eventually(t, s.li.idle, time.Second, time.Millisecond)
}

func TestHTTPDrainOldServerKeepsOrdinaryHTTP3Shutdown(t *testing.T) {
	ln, err := quic.ListenAddrEarly("127.0.0.1:0", testServerTLSConfig(t), nil)
	require.NoError(t, err)
	var probes atomic.Int32
	server := &http3.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == drainPath {
			probes.Add(1)
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, "old server")
	})}
	go func() { _ = server.ServeListener(ln) }()
	t.Cleanup(func() { _ = server.Close() })
	client := drainTestClient(t)
	for range 3 {
		resp := drainTestRequest(t, client, ln.Addr().String())
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, "old server", string(body))
		require.NoError(t, resp.Body.Close())
	}
	require.Eventually(t, func() bool { return probes.Load() == 1 }, time.Second, time.Millisecond)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.NoError(t, server.Shutdown(ctx))
}

func TestHTTPDrainDeadlineClosesUnreadResponse(t *testing.T) {
	s, err := NewState(t.Context(), WithSkipVerify,
		WithHTTPHandler("GET /payload", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_ = http.NewResponseController(w).Flush()
			<-r.Context().Done()
		})))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	client := drainTestClient(t)
	resp := drainTestRequest(t, client, s.LoopbackAddr())
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, s.Shutdown(ctx), context.DeadlineExceeded)
	_, err = io.ReadAll(resp.Body)
	require.Error(t, err)
}

func TestHTTPDrainRetriesWatchAfterPreparationFailure(t *testing.T) {
	s, err := NewState(t.Context(), WithSkipVerify,
		WithHTTPHandler("GET /payload", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "ok")
		})))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	s.disableContextShutdown()

	client := drainTestClient(t)
	var preparations atomic.Int32
	client.prepare = func(context.Context, *http.Request) error {
		if preparations.Add(1) == 1 {
			return errors.New("temporary signing failure")
		}
		return nil
	}
	resp := drainTestRequest(t, client, s.LoopbackAddr())
	require.Eventually(t, func() bool {
		client.mu.Lock()
		defer client.mu.Unlock()
		return client.current == nil
	}, time.Second, time.Millisecond)
	// Failure to establish the watch must not interrupt the response body.
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "ok", string(body))
	require.NoError(t, resp.Body.Close())

	resp = drainTestRequest(t, client, s.LoopbackAddr())
	body, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "ok", string(body))
	require.NoError(t, resp.Body.Close())
	require.Eventually(t, func() bool { return preparations.Load() == 2 }, time.Second, time.Millisecond)
	s.cancel()
	require.Eventually(t, s.li.idle, time.Second, time.Millisecond)
}

// A peer that vanishes without CONNECTION_CLOSE, as an exiting CLI used to,
// leaves the server holding its connection until the 30s idle timeout. The
// drain must not wait for that once nothing is running on it.
func TestHTTPDrainDoesNotWaitOnAVanishedPeer(t *testing.T) {
	s, err := NewState(t.Context(), WithSkipVerify,
		WithHTTPHandler("GET /payload", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "ok")
		})))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	tr := &quic.Transport{Conn: udp}
	client := &http3.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		Dial: func(ctx context.Context, addr string, tlsCfg *tls.Config, cfg *quic.Config) (*quic.Conn, error) {
			raddr, err := net.ResolveUDPAddr("udp", addr)
			if err != nil {
				return nil, err
			}
			return tr.DialEarly(ctx, raddr, tlsCfg, cfg)
		},
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://"+s.LoopbackAddr()+"/payload", nil)
	require.NoError(t, err)
	resp, err := client.RoundTrip(req)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "ok", string(body))

	// Closing the socket out from under the transport is what a process exit
	// does: whatever close the transport attempts never reaches the server.
	require.NoError(t, udp.Close())
	require.False(t, s.li.idle(), "server already saw the peer leave; the test would prove nothing")

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	start := time.Now()
	require.NoError(t, s.Shutdown(ctx))
	require.Less(t, time.Since(start), drainSettle+2*time.Second, "shutdown waited on a peer with nothing running")
}
