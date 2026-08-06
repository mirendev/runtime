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
// ALPN offers h2 ahead of http/1.1. A client that speaks neither (or sends a
// plaintext request) still gets HTTP/1.1, which is the point: an ordinary HTTP
// client can reach the API without knowing anything about QUIC.
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
		Handler:   s.server,
		TLSConfig: tlsCfg,
		// A REST request is request/response, so unlike the WebSocket listener
		// this one can bound the whole exchange rather than just the headers.
		ReadHeaderTimeout: 10 * time.Second,
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

// RESTListenAddr returns the address the REST listener is bound to, or an empty
// string if no such listener is running.
func (s *State) RESTListenAddr() string {
	if s.restLn == nil {
		return ""
	}
	return s.restLn.Addr().String()
}
