package rpc

import (
	"context"
	"net"
	"net/http"
	"time"
)

// startRESTListener starts a TCP listener serving the REST/JSON gateway over
// TLS, bound to addr.
//
// Unlike the WebSocket listener, this one serves the full RPC HTTP handler
// rather than a single upgrade route: the same Server.ServeHTTP that HTTP/3
// answers, so a REST request is authenticated by the same chain and dispatched
// to the same handlers. The QUIC listener owns udp/<port> and this owns
// tcp/<port>, so both can share a port number.
//
// ALPN offers h2 ahead of http/1.1, which is the point of the exercise: an
// ordinary HTTP client reaches the API without knowing anything about QUIC. The
// listener is TLS-only, so a plaintext client is rejected at the handshake
// rather than falling back to cleartext HTTP/1.1.
func (s *State) startRESTListener(ctx context.Context, addr string) error {
	tcpLn, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	tlsCfg := s.serverTlsCfg.Clone()
	// Advertise both, best first. Without this the cloned config carries the
	// h3 protos the QUIC listener negotiated, which no TCP client offers.
	tlsCfg.NextProtos = []string{"h2", "http/1.1"}

	srv := &http.Server{
		Handler:   s.restReadinessGate(s.server),
		TLSConfig: tlsCfg,
		// A REST request is request/response and never hijacks its connection,
		// so unlike the WebSocket listener this one can bound the whole
		// exchange rather than only the headers. ReadTimeout is what closes the
		// slow-body hole: MaxBytesReader caps how much a caller may send, not
		// how long it may take, and this listener is reachable from the sandbox
		// bridge.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	s.restSrv = srv
	s.restLn = tcpLn

	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()

	s.log.Info("starting rest/tcp listener", "addr", tcpLn.Addr().String())

	go func() {
		if serr := srv.ServeTLS(tcpLn, "", ""); serr != nil && serr != http.ErrServerClosed {
			s.log.Error("rest/tcp listener stopped", "error", serr)
		}
	}()

	return nil
}

// restReadinessGate answers 503 until the server has a REST route mounted.
//
// The listener starts inside NewState, but routes are mounted from
// Server.ExposeValue, which a caller can only reach once NewState has returned.
// A client that arrives in that window would otherwise get a 404, which is
// indistinguishable from asking for a route that does not exist; 503 says "not
// yet", which is what is actually true. The sibling WithHTTPHandler mounts
// during NewState and so has no such window.
func (s *State) restReadinessGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.server.hasRESTRoutes() {
			w.Header().Set("Retry-After", "1")
			writeRESTStatus(w, http.StatusServiceUnavailable, restErrorBody{
				Error: "rest gateway is starting",
				Code:  "unavailable",
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}

// RESTListenAddr returns the address the REST listener is bound to, or an empty
// string if no such listener is running.
func (s *State) RESTListenAddr() string {
	if s.restLn == nil {
		return ""
	}
	return s.restLn.Addr().String()
}
