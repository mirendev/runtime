package rpc

import (
	"context"
	"crypto/tls"
	"net"
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

// wsRemoteURL turns a remote into the URL to dial.
//
// The scheme means what it says: wss:// is TLS, ws:// is plaintext. A remote
// naming neither is assumed to be one of our own listeners, which are always
// TLS. Plaintext exists for a development endpoint that has no certificate —
// a local cloud on http://, say — and should not appear anywhere else.
//
// A bare "host:port" gets the listener's own route appended, which is what a
// cluster's own RPC endpoint wants. A remote that already names a path keeps it
// verbatim, so it can point at somebody else's endpoint — a relay forwarding
// these frames on, say — without that endpoint having to mimic our route.
func wsRemoteURL(remote string) string {
	scheme := "wss"
	if rest, ok := strings.CutPrefix(remote, "ws://"); ok {
		scheme, remote = "ws", rest
	} else {
		remote = strings.TrimPrefix(remote, "wss://")
	}

	host, path, _ := strings.Cut(remote, "/")
	if path == "" {
		return scheme + "://" + host + wsMessagePath
	}
	return scheme + "://" + host + "/" + path
}

// bearerTravelsSafely reports whether a credential may be put on the handshake
// to this URL.
//
// Encrypted, always. Unencrypted, only to this machine — which is what ws://
// exists for, a development endpoint with no certificate. A configured remote
// like ws://relay.example would otherwise put a reusable credential on the
// network in the clear, and the caller would never know it happened.
//
// A caller that meant to reach a remote plaintext endpoint gets no header and
// an authentication failure from the far end, which is the right way round: a
// broken connection is recoverable, a leaked token is not.
func bearerTravelsSafely(url string) bool {
	rest, ok := strings.CutPrefix(url, "ws://")
	if !ok {
		return true // wss://, so the handshake is encrypted
	}

	host, _, _ := strings.Cut(rest, "/")
	if isLoopbackAddr(host) {
		return true
	}

	// isLoopbackAddr only knows addresses, and a development endpoint is
	// usually reached by name. Handled here rather than there, because that
	// helper also decides an audit record's level and this is not a reason to
	// change what it means.
	name, _, err := net.SplitHostPort(host)
	if err != nil {
		name = host
	}
	return strings.EqualFold(name, "localhost")
}

// dialWSMessageConn opens a WebSocket to a "host:port" or "host:port/path"
// remote and wraps it as a MessageConn. ctx bounds the connection's lifetime, so
// it must be the State's context rather than any one call's.
//
// bearer, when set, authenticates the handshake itself. On this transport the
// operation frames carry their own bearer, but those are opaque to anything
// standing between us and the server, so an intermediary that must authorize
// the connection before it can forward a byte has nothing else to go on.
func dialWSMessageConn(ctx context.Context, remote string, tlsCfg *tls.Config, bearer string) (MessageConn, error) {
	// coder/websocket does not implement RFC 8441 extended CONNECT, so the
	// handshake must be HTTP/1.1.
	cfg := tlsCfg.Clone()
	cfg.NextProtos = []string{"http/1.1"}

	url := wsRemoteURL(remote)

	opts := &websocket.DialOptions{
		HTTPClient: &http.Client{
			Transport: &http.Transport{TLSClientConfig: cfg},
		},
	}
	if bearer != "" && bearerTravelsSafely(url) {
		opts.HTTPHeader = http.Header{"Authorization": []string{"Bearer " + bearer}}
	}

	c, _, err := websocket.Dial(ctx, url, opts)
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

// setupWSTransport wires a client for a "wss://host:port" remote onto the
// message transport. The remote is passed through whole, scheme and all, since
// that is what decides the URL to dial — see wsRemoteURL.
func (c *NetworkClient) setupWSTransport() {
	remote := c.remote

	tlsCfg := c.tlsCfg
	if tlsCfg == nil {
		tlsCfg = c.State.clientTlsCfg
	}

	c.ops = &msgOpTransport{
		owner: c,
		dial: func() (MessageConn, error) {
			// Resolved per dial, not once here: a refreshing credential must not
			// be pinned to whatever it happened to be when the client was built.
			return dialWSMessageConn(c.State.top, remote, tlsCfg, c.bearer())
		},
	}
}
