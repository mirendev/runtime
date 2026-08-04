package rpc

import (
	"context"
	"crypto/tls"
	"net/http"
	"strings"

	"github.com/coder/websocket"
)

// wsMessagePath is the single route the TCP/WebSocket listener serves. Every
// operation rides the multiplexed session established by the upgrade, so unlike
// the HTTP/3 transport there is no per-operation URL.
const wsMessagePath = "/_rpc/message"

// wsConn is a MessageConn over a WebSocket.
//
// A WebSocket already delivers discrete, ordered, reliable messages — exactly
// what msgmux consumes — so the frames go on the wire as-is. Nothing here
// converts the connection to a byte stream and re-frames it.
type wsConn struct {
	c   *websocket.Conn
	ctx context.Context
}

func (w *wsConn) Send(b []byte) error {
	return w.c.Write(w.ctx, websocket.MessageBinary, b)
}

func (w *wsConn) Recv() ([]byte, error) {
	_, data, err := w.c.Read(w.ctx)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (w *wsConn) Close() error {
	return w.c.Close(websocket.StatusNormalClosure, "")
}

// dialWSMessageConn opens a WebSocket to a "host:port" remote and wraps it as a
// MessageConn. ctx bounds the connection's lifetime, so it must be the State's
// context rather than any one call's.
func dialWSMessageConn(ctx context.Context, remote string, tlsCfg *tls.Config) (MessageConn, error) {
	// coder/websocket does not implement RFC 8441 extended CONNECT, so the
	// handshake must be HTTP/1.1.
	cfg := tlsCfg.Clone()
	cfg.NextProtos = []string{"http/1.1"}

	c, _, err := websocket.Dial(ctx, "wss://"+remote+wsMessagePath, &websocket.DialOptions{
		HTTPClient: &http.Client{
			Transport: &http.Transport{TLSClientConfig: cfg},
		},
	})
	if err != nil {
		return nil, err
	}

	// A single frame may be as large as the session's frame cap, well above
	// coder/websocket's default. Bound it to the frame cap plus framing headroom
	// rather than removing the ceiling, so a peer can't make us buffer without
	// limit before msgmux validates the frame.
	c.SetReadLimit(frameReadLimit(0))

	return &wsConn{c: c, ctx: ctx}, nil
}

// serveWSUpgrade accepts a WebSocket upgrade and serves RPC over it.
//
// The session is bound to lifeCtx (the server's lifetime), not r.Context():
// websocket.Accept hijacks the TCP socket and net/http cancels the request
// context as soon as this handler returns, while the hijacked socket remains
// open and owned by us. Binding to the request context would tear the session
// down immediately.
func (s *State) serveWSUpgrade(lifeCtx context.Context, w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // origin checks are not meaningful for service-to-service RPC
	})
	if err != nil {
		s.log.Error("rpc: websocket upgrade failed", "error", err)
		return
	}

	// Untrusted clients: bound how much we'll buffer per message rather than
	// removing the ceiling. See dialWSMessageConn.
	c.SetReadLimit(frameReadLimit(0))

	go func() {
		conn := &wsConn{c: c, ctx: lifeCtx}
		if serr := s.ServeMessageConn(lifeCtx, conn); serr != nil {
			s.log.Debug("rpc: websocket session ended", "error", serr)
		}
		_ = conn.Close()
	}()
}

// setupWSTransport wires a client for a "ws://host:port" remote onto the
// message transport.
func (c *NetworkClient) setupWSTransport() {
	remote := strings.TrimPrefix(strings.TrimPrefix(c.remote, "wss://"), "ws://")

	tlsCfg := c.tlsCfg
	if tlsCfg == nil {
		tlsCfg = c.State.clientTlsCfg
	}

	c.ops = &msgOpTransport{
		owner: c,
		dial: func() (MessageConn, error) {
			return dialWSMessageConn(c.State.top, remote, tlsCfg)
		},
	}
}
