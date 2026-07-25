package rpc

import (
	"context"
	"net"
	"net/http"
)

// startWSListener starts a TCP listener serving the RPC protocol over TLS.
//
// Unlike the HTTP/3 listener, this one does not serve the RPC HTTP mux: a
// single route performs a WebSocket upgrade, and every operation then rides the
// multiplexed message session established over it. The HTTP layer exists only
// so proxies and browsers see a real WebSocket handshake.
func (s *State) startWSListener(ctx context.Context, addr string) error {
	tcpLn, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	tlsCfg := s.serverTlsCfg.Clone()
	// coder/websocket needs an HTTP/1.1 handshake; do not offer h2.
	tlsCfg.NextProtos = []string{"http/1.1"}

	mux := http.NewServeMux()
	mux.HandleFunc("GET "+wsMessagePath, func(w http.ResponseWriter, r *http.Request) {
		s.serveWSUpgrade(ctx, w, r)
	})

	srv := &http.Server{
		Handler:   mux,
		TLSConfig: tlsCfg,
	}

	s.httpSrv = srv
	s.tcpLn = tcpLn

	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()

	s.log.Debug("starting websocket/tcp listener", "addr", tcpLn.Addr().String())

	go func() {
		if serr := srv.ServeTLS(tcpLn, "", ""); serr != nil && serr != http.ErrServerClosed {
			s.log.Error("websocket/tcp listener stopped", "error", serr)
		}
	}()

	return nil
}

// WSListenAddr returns the address the TCP/WebSocket listener is bound to, or
// an empty string if no such listener is running.
func (s *State) WSListenAddr() string {
	if s.tcpLn == nil {
		return ""
	}
	return s.tcpLn.Addr().String()
}
