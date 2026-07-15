package anywhere

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"

	"miren.dev/runtime/servers/httpingress"
)

const popDataALPN = "pop-data"

// POPManager manages HTTP/3 connections to POP servers. When the cloud
// sends a connection.request, the manager establishes two QUIC connections
// to the POP:
//  1. Control plane (ALPN "h3"): POST /connect with the connect_token
//  2. Data plane (ALPN "pop-data"): authenticated by IP match, then
//     serves incoming HTTP/3 requests forwarded by the POP
type POPManager struct {
	mu    sync.Mutex
	conns map[string]*popConnection // POP XID → connection

	clusterXID string
	ingress    *httpingress.Server
	log        *slog.Logger
}

type popConnection struct {
	popXID string
	cancel context.CancelFunc
}

// NewPOPManager creates a new POP connection manager.
func NewPOPManager(clusterXID string, ingress *httpingress.Server, log *slog.Logger) *POPManager {
	return &POPManager{
		conns:      make(map[string]*popConnection),
		clusterXID: clusterXID,
		ingress:    ingress,
		log:        log,
	}
}

// HandleConnectionRequest processes a connection.request from the cloud.
// It establishes connections to the specified POP if one does not already
// exist.
func (m *POPManager) HandleConnectionRequest(ctx context.Context, req ConnectionRequest) error {
	m.mu.Lock()
	existing, replacing := m.conns[req.POPXID]
	if replacing {
		// Cloud sent a new connection request for a POPXID we already
		// have. The POP was likely recreated (new VM, same hostname).
		// Cancel the old connection so servePOP exits, then replace it.
		existing.cancel()
	}

	connCtx, cancel := context.WithCancel(context.Background())
	pc := &popConnection{
		popXID: req.POPXID,
		cancel: cancel,
	}
	m.conns[req.POPXID] = pc
	m.mu.Unlock()

	if replacing {
		m.log.Info("replacing stale POP connection", "pop_xid", req.POPXID)
	}

	m.log.Info("establishing connection to POP",
		"pop_xid", req.POPXID,
		"pop_address", req.POPAddress,
		"hostname", req.Hostname,
		"request_id", req.RequestID)

	go m.servePOP(connCtx, pc, req)
	return nil
}

// servePOP implements the two-connection protocol:
//  1. Dial POP with ALPN "h3", send POST /connect with the connect_token
//  2. Dial POP again with ALPN "pop-data" on the same quic.Transport
//  3. Serve incoming HTTP/3 requests on connection 2
func (m *POPManager) servePOP(ctx context.Context, pc *popConnection, cr ConnectionRequest) {
	defer func() {
		pc.cancel()
		m.mu.Lock()
		if m.conns[pc.popXID] == pc {
			delete(m.conns, pc.popXID)
		}
		m.mu.Unlock()
		m.log.Info("POP connection closed", "pop_xid", pc.popXID)
	}()

	addr := normalizeAddr(cr.POPAddress)
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
		port = "8443"
	}

	// Resolve the hostname to an IP. The POP address may use names like
	// host.docker.internal that don't resolve in all environments.
	ips, err := net.LookupHost(host)
	if err != nil {
		m.log.Error("failed to resolve POP address",
			"address", addr, "host", host, "error", err)
		return
	}

	resolvedAddr := net.JoinHostPort(ips[0], port)

	// Skip TLS verification for non-global IPs (e.g. private POPs in dev/test).
	resolvedIP := net.ParseIP(ips[0])
	skipVerify := isNonPublicIP(resolvedIP)

	m.log.Info("resolved POP address",
		"original", addr, "resolved", resolvedAddr, "skip_tls_verify", skipVerify)

	// Create a single UDP socket and QUIC transport for both connections.
	udpConn, err := net.ListenUDP("udp", nil)
	if err != nil {
		m.log.Error("failed to create UDP socket", "error", err)
		return
	}
	defer udpConn.Close()

	quicTransport := &quic.Transport{Conn: udpConn}
	defer quicTransport.Close()

	udpAddr, err := net.ResolveUDPAddr("udp", resolvedAddr)
	if err != nil {
		m.log.Error("failed to parse resolved POP address",
			"address", resolvedAddr, "error", err)
		return
	}

	// --- Connection 1: Control plane (ALPN "h3") ---
	controlTLS := &tls.Config{
		NextProtos:         []string{http3.NextProtoH3},
		InsecureSkipVerify: skipVerify,
		ServerName:         host,
	}

	controlConn, err := quicTransport.Dial(ctx, udpAddr, controlTLS, &quic.Config{})
	if err != nil {
		m.log.Error("failed to dial POP control plane",
			"pop_xid", pc.popXID, "address", addr, "error", err)
		return
	}

	// Send /connect handshake
	controlH3 := &http3.Transport{
		Dial: func(_ context.Context, _ string, _ *tls.Config, _ *quic.Config) (*quic.Conn, error) {
			return controlConn, nil
		},
	}

	connectURL := fmt.Sprintf("https://%s/connect", host)
	req, err := http.NewRequestWithContext(ctx, "POST", connectURL, nil)
	if err != nil {
		controlH3.Close()
		controlConn.CloseWithError(0, "request error")
		m.log.Error("failed to create connect request", "error", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+cr.ConnectToken)
	req.Header.Set("X-Cluster-XID", m.clusterXID)

	resp, err := (&http.Client{Transport: controlH3}).Do(req)
	if err != nil {
		controlH3.Close()
		controlConn.CloseWithError(0, "handshake failed")
		m.log.Error("POP connect handshake failed", "pop_xid", pc.popXID, "error", err)
		return
	}

	// Read the full response body before closing the transport.
	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	controlH3.Close()

	if resp.StatusCode != http.StatusOK {
		controlConn.CloseWithError(0, "rejected")
		m.log.Error("POP connect rejected",
			"pop_xid", pc.popXID, "status", resp.StatusCode)
		return
	}

	// Parse the data-plane token from the /connect response.
	var connectResp struct {
		DataPlaneToken string `json:"data_plane_token"`
	}
	if err := json.Unmarshal(respBody, &connectResp); err != nil {
		controlConn.CloseWithError(0, "bad response")
		m.log.Error("failed to parse connect response",
			"pop_xid", pc.popXID, "error", err)
		return
	}
	if connectResp.DataPlaneToken == "" {
		controlConn.CloseWithError(0, "no data plane token")
		m.log.Error("connect response missing data_plane_token",
			"pop_xid", pc.popXID)
		return
	}

	m.log.Info("control plane handshake complete", "pop_xid", pc.popXID)

	// --- Connection 2: Data plane (ALPN "pop-data") ---
	dataTLS := &tls.Config{
		NextProtos:         []string{popDataALPN},
		InsecureSkipVerify: skipVerify,
		ServerName:         host,
	}

	dataConn, err := quicTransport.Dial(ctx, udpAddr, dataTLS, &quic.Config{
		KeepAlivePeriod: 15 * time.Second,
	})
	if err != nil {
		controlConn.CloseWithError(0, "data plane dial failed")
		m.log.Error("failed to dial POP data plane",
			"pop_xid", pc.popXID, "address", addr, "error", err)
		return
	}

	// Send the data-plane token on a unidirectional stream to authenticate.
	tokenStream, err := dataConn.OpenUniStream()
	if err != nil {
		dataConn.CloseWithError(0, "failed to open token stream")
		controlConn.CloseWithError(0, "data plane setup failed")
		m.log.Error("failed to open data plane token stream",
			"pop_xid", pc.popXID, "error", err)
		return
	}
	if _, err := tokenStream.Write([]byte(connectResp.DataPlaneToken)); err != nil {
		dataConn.CloseWithError(0, "failed to send token")
		controlConn.CloseWithError(0, "data plane setup failed")
		m.log.Error("failed to send data plane token",
			"pop_xid", pc.popXID, "error", err)
		return
	}
	tokenStream.Close()

	m.log.Info("data plane connected, serving requests",
		"pop_xid", pc.popXID, "address", addr)

	// Close control connection — it's no longer needed after handshake.
	controlConn.CloseWithError(0, "handshake complete")

	// Serve incoming HTTP/3 requests from the POP on the data plane
	// connection. Requests with Host: pop.internal are POP control
	// traffic (e.g., WebSocket tunneling). All other requests are
	// forwarded to the local HTTP ingress.
	forwardHandler := m.newForwardingHandler(pc.popXID)
	wsHandler := m.newWSTunnelHandler(pc.popXID)

	h3srv := &http3.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Host == "pop.internal" && r.URL.Path == "/_pop/ws" {
				wsHandler.ServeHTTP(w, r)
				return
			}
			forwardHandler.ServeHTTP(w, r)
		}),
	}

	// Close the data-plane connection when our context is cancelled
	// (e.g., this POP entry is replaced by a new connection request).
	// Without this, ServeQUICConn blocks until the peer drops the
	// connection, leaking the old goroutine.
	go func() {
		<-ctx.Done()
		dataConn.CloseWithError(0, "connection replaced")
	}()

	if err := h3srv.ServeQUICConn(dataConn); err != nil && ctx.Err() == nil {
		m.log.Error("POP data plane server error",
			"pop_xid", pc.popXID, "error", err)
	}
}

// newForwardingHandler returns an http.Handler that forwards requests
// received from a POP to the local HTTP ingress. The POP sets
// X-Forwarded-Host to the original hostname, which is used to route
// the request to the correct app.
func (m *POPManager) newForwardingHandler(popXID string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hostname := r.Host
		if fwdHost := r.Header.Get("X-Forwarded-Host"); fwdHost != "" {
			hostname = fwdHost
		}

		if m.ingress == nil {
			http.Error(w, "no ingress configured", http.StatusBadGateway)
			return
		}

		// Forward via the HTTP ingress proxy handler, which handles
		// hostname-based routing, lease management, and app activation.
		proxyReq := r.Clone(r.Context())
		proxyReq.Host = hostname
		proxyReq.URL.Host = hostname
		proxyReq.URL.Scheme = "https"

		m.ingress.ServeHTTP(w, proxyReq)
	})
}

// newWSTunnelHandler returns an http.Handler for POST /_pop/ws that
// bridges a POP WebSocket tunnel to the local app.
//
// The flow:
//  1. Use ingress.AcquireTunnel to resolve hostname → sandbox URL
//  2. Dial WebSocket to the sandbox directly
//  3. Send 200 OK to POP to signal the tunnel is ready
//  4. Bridge messages using tunnel frame encoding between the POP's
//     HTTP/3 POST stream and the local WebSocket connection
func (m *POPManager) newWSTunnelHandler(popXID string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wsPath := r.Header.Get("X-WS-Path")
		wsHostname := r.Header.Get("X-WS-Hostname")
		if wsPath == "" {
			wsPath = "/"
		}

		m.log.Info("WebSocket tunnel request",
			"pop_xid", popXID,
			"hostname", wsHostname,
			"path", wsPath)

		if m.ingress == nil {
			http.Error(w, "no ingress configured", http.StatusBadGateway)
			return
		}

		ctx := r.Context()

		// Resolve hostname → sandbox URL via the ingress routing/lease system.
		tunnel, err := m.ingress.AcquireTunnel(ctx, wsHostname, wsPath)
		if err != nil {
			m.log.Error("failed to acquire tunnel",
				"hostname", wsHostname, "error", err)
			http.Error(w, fmt.Sprintf("tunnel acquisition failed: %v", err), http.StatusBadGateway)
			return
		}
		defer tunnel.Release()

		m.log.Info("tunnel acquired",
			"pop_xid", popXID,
			"hostname", wsHostname,
			"sandbox_url", tunnel.URL,
			"app_id", tunnel.AppID)

		// Dial WebSocket to the sandbox.
		sandboxWSURL := strings.Replace(tunnel.URL, "http://", "ws://", 1) + wsPath

		dialHeaders := http.Header{}
		for _, key := range []string{
			"X-Forwarded-For",
			"X-Forwarded-Proto",
			"X-Forwarded-Host",
		} {
			if v := r.Header.Get(key); v != "" {
				dialHeaders.Set(key, v)
			}
		}
		if origin := r.Header.Get("X-WS-Origin"); origin != "" {
			dialHeaders.Set("Origin", origin)
		} else {
			dialHeaders.Set("Origin", tunnel.URL)
		}

		m.log.Info("dialing sandbox WebSocket",
			"pop_xid", popXID,
			"url", sandboxWSURL)

		localConn, _, err := websocket.Dial(ctx, sandboxWSURL, &websocket.DialOptions{
			HTTPHeader: dialHeaders,
		})
		if err != nil {
			m.log.Error("failed to dial sandbox WebSocket",
				"url", sandboxWSURL, "error", err)
			http.Error(w, fmt.Sprintf("sandbox WebSocket dial failed: %v", err), http.StatusBadGateway)
			return
		}
		defer localConn.CloseNow()

		// 200 OK tells the POP the tunnel is ready
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		// Raise read limit for proxied messages
		localConn.SetReadLimit(16 << 20)

		m.log.Info("WebSocket tunnel established",
			"pop_xid", popXID,
			"hostname", wsHostname,
			"path", wsPath,
			"sandbox_url", sandboxWSURL)

		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		flusher, _ := w.(http.Flusher)

		var wg sync.WaitGroup
		wg.Add(2)

		// POP → App: read tunnel frames from POST body, write WS messages
		go func() {
			defer wg.Done()
			defer cancel()
			var buf []byte
			for {
				msgType, b, err := readTunnelFrame(ctx, r.Body, buf, localConn)
				buf = b
				if err != nil {
					m.log.Info("tunnel POP→app finished",
						"pop_xid", popXID, "error", err)
					return
				}
				if err := localConn.Write(ctx, msgType, buf); err != nil {
					m.log.Info("tunnel POP→app write error",
						"pop_xid", popXID, "error", err)
					return
				}
			}
		}()

		// App → POP: read WS messages, write tunnel frames to response body
		go func() {
			defer wg.Done()
			defer cancel()
			var wbuf []byte
			for {
				msgType, data, err := localConn.Read(ctx)
				if err != nil {
					m.log.Info("tunnel app→POP finished",
						"pop_xid", popXID, "error", err)
					return
				}
				wbuf, err = writeTunnelFrame(w, msgType, data, wbuf)
				if err != nil {
					m.log.Info("tunnel app→POP write error",
						"pop_xid", popXID, "error", err)
					return
				}
				if flusher != nil {
					flusher.Flush()
				}
			}
		}()

		wg.Wait()

		m.log.Info("WebSocket tunnel closed",
			"pop_xid", popXID,
			"hostname", wsHostname,
			"path", wsPath)
	})
}

// Tunnel frame format:
//
//	[1 byte:  message type (1=text, 2=binary)]
//	[4 bytes: payload length, big-endian uint32]
//	[N bytes: payload]

const (
	tunnelFrameHeaderSize = 5
	tunnelFrameKeepalive  = 0
)

// readTunnelFrame reads the next tunnel frame. When a keepalive frame
// (type 0) is received, it pings the local WebSocket connection to
// propagate liveness, then continues reading. The buf argument is
// reused across calls to avoid allocations.
func readTunnelFrame(ctx context.Context, r io.Reader, buf []byte, localConn *websocket.Conn) (websocket.MessageType, []byte, error) {
	if cap(buf) < tunnelFrameHeaderSize {
		buf = make([]byte, 4096)
	}

	for {
		if _, err := io.ReadFull(r, buf[:tunnelFrameHeaderSize]); err != nil {
			return 0, buf, err
		}

		msgType := buf[0]
		length := binary.BigEndian.Uint32(buf[1:tunnelFrameHeaderSize])

		if length > 16<<20 { // 16MB sanity limit
			return 0, buf, fmt.Errorf("tunnel frame too large: %d bytes", length)
		}

		if uint32(cap(buf)) < length {
			buf = make([]byte, length)
		} else {
			buf = buf[:length]
		}

		if length > 0 {
			if _, err := io.ReadFull(r, buf); err != nil {
				return 0, buf, err
			}
		}

		if msgType == tunnelFrameKeepalive {
			localConn.Ping(ctx)
			continue
		}

		return websocket.MessageType(msgType), buf, nil
	}
}

func writeTunnelFrame(w io.Writer, msgType websocket.MessageType, data []byte, buf []byte) ([]byte, error) {
	needed := tunnelFrameHeaderSize + len(data)
	if cap(buf) < needed {
		buf = make([]byte, needed)
	} else {
		buf = buf[:needed]
	}

	buf[0] = byte(msgType)
	binary.BigEndian.PutUint32(buf[1:tunnelFrameHeaderSize], uint32(len(data)))
	copy(buf[tunnelFrameHeaderSize:], data)

	if _, err := w.Write(buf); err != nil {
		return buf, err
	}
	return buf, nil
}

// RemovePOP closes and removes the connection to a specific POP.
func (m *POPManager) RemovePOP(popXID string) {
	m.mu.Lock()
	pc, ok := m.conns[popXID]
	if ok {
		delete(m.conns, popXID)
	}
	m.mu.Unlock()

	if ok {
		pc.cancel()
		m.log.Info("removed POP connection", "pop_xid", popXID)
	}
}

// Close shuts down all POP connections.
func (m *POPManager) Close() {
	m.mu.Lock()
	conns := make(map[string]*popConnection, len(m.conns))
	for k, v := range m.conns {
		conns[k] = v
	}
	m.conns = make(map[string]*popConnection)
	m.mu.Unlock()

	for _, pc := range conns {
		pc.cancel()
	}
}

// ConnectedPOPs returns the XIDs of all currently connected POPs.
func (m *POPManager) ConnectedPOPs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	xids := make([]string, 0, len(m.conns))
	for xid := range m.conns {
		xids = append(xids, xid)
	}
	return xids
}

// isNonPublicIP reports whether ip is nil or not a public address
// (private, loopback, or link-local). Used to decide whether TLS
// certificate verification can be skipped for dev/test POPs.
func isNonPublicIP(ip net.IP) bool {
	return ip == nil || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()
}

// normalizeAddr ensures the address has a port and no scheme prefix.
func normalizeAddr(addr string) string {
	addr = strings.TrimPrefix(addr, "https://")
	addr = strings.TrimPrefix(addr, "http://")
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(addr, "8443")
	}
	return addr
}
