package rpc

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/webtransport-go"
	"miren.dev/runtime/pkg/packet"
	"miren.dev/runtime/pkg/slogfmt"
)

// InitialPacketSize pins the QUIC Initial at the 1200-byte spec minimum so the
// handshake fits a 1280-MTU path (Tailscale/WireGuard tunnels, the IPv6
// minimum). quic-go's default of 1280 yields a 1308-byte IPv4 datagram with DF
// set, which such tunnels silently drop, hanging the handshake in both
// directions. PMTUD raises the packet size again after the handshake completes,
// so 1500-MTU paths are unaffected. See quic-go#5573, quic-go#5634,
// tailscale#2633.
//
// Every quic.Config we hand to a Dial or Listen must set this. quic-go swallows
// the resulting EMSGSIZE without shrinking the Initial (see sendQueue.Run), so
// forgetting it costs a silent timeout on tunnelled paths and nothing at all on
// ordinary ones.
const InitialPacketSize = 1200

var (
	DefaultQUICConfig quic.Config

	DefaultLogLevel = slog.LevelInfo
)

func init() {
	DefaultQUICConfig = quic.Config{
		InitialPacketSize:              InitialPacketSize,
		EnableDatagrams:                true,
		MaxIncomingStreams:             1000,
		MaxIncomingUniStreams:          1000,
		Allow0RTT:                      true,
		KeepAlivePeriod:                10 * time.Second,
		MaxIdleTimeout:                 30 * time.Second,
		Tracer:                         qlog.DefaultConnectionTracer,
		InitialStreamReceiveWindow:     5 * 1024 * 1024,  // 5MB per stream
		MaxStreamReceiveWindow:         20 * 1024 * 1024, // 20MB max per stream
		InitialConnectionReceiveWindow: 10 * 1024 * 1024, // 10MB total
		MaxConnectionReceiveWindow:     20 * 1024 * 1024, // 20MB total max
	}
}

// closedPacketConn is a stub net.PacketConn that returns net.ErrClosed on all operations.
// Used to trigger webtransport.Server initialization without actually serving connections.
type closedPacketConn struct{}

func (closedPacketConn) ReadFrom([]byte) (int, net.Addr, error) { return 0, nil, net.ErrClosed }
func (closedPacketConn) WriteTo([]byte, net.Addr) (int, error)  { return 0, net.ErrClosed }
func (closedPacketConn) Close() error                           { return nil }
func (closedPacketConn) LocalAddr() net.Addr                    { return &net.UDPAddr{} }
func (closedPacketConn) SetDeadline(time.Time) error            { return nil }
func (closedPacketConn) SetReadDeadline(time.Time) error        { return nil }
func (closedPacketConn) SetWriteDeadline(time.Time) error       { return nil }

type StateCommon struct {
	top context.Context
	log *slog.Logger

	// auditLog is the security audit trail, pinned at an Info floor and tagged
	// module=audit so it stays legible and volume-bounded regardless of the
	// server's -v verbosity. See newAuditLogger.
	auditLog *slog.Logger

	// certAuth collapses repeated cert-auth records for a long-lived peer into
	// one per interval. Nil is valid and means no deduplication. See
	// certAuthDeduper.
	certAuth *certAuthDeduper

	opts *stateOptions

	serverTlsCfg *tls.Config
	clientTlsCfg *tls.Config
	cert         tls.Certificate

	authenticator Authenticator
	authorizer    Authorizer

	privkey ed25519.PrivateKey
	pubkey  ed25519.PublicKey

	qc quic.Config
}

// audit returns the security audit logger, falling back to the general logger
// if one was not wired up (e.g. a hand-constructed StateCommon in a test).
func (s *StateCommon) audit() *slog.Logger {
	if s.auditLog != nil {
		return s.auditLog
	}
	return s.log
}

type State struct {
	*StateCommon

	transport      *quic.Transport
	localTransport *quic.Transport

	defaultEndpoint string

	server *Server
	hs     *http3.Server
	ws     *webtransport.Server
	li     *quic.EarlyListener

	httpSrv *http.Server
	tcpLn   net.Listener
	msgLn   net.Listener
	restSrv *http.Server
	restLn  net.Listener

	// msgContact is the address peers use to reach this State over a message
	// transport, e.g. "mem://name" for an in-process server. It is empty when
	// the State only serves connections supplied by callers, which have no
	// address of their own.
	msgContact string

	localMP *packet.PacketConnMultiplex
}

// contactAddr returns the address embedded in capabilities this server mints,
// so a holder can later reach the issuer.
//
// It is empty when there is nothing to dial — a connection owned by the caller
// is reachable only as itself. Capabilities minted with an empty address are
// therefore scoped to the session they were minted on.
func (s *State) contactAddr() string {
	if s.msgContact != "" {
		return s.msgContact
	}
	if s.transport == nil || s.transport.Conn == nil {
		return ""
	}
	return s.transport.Conn.LocalAddr().String()
}

func (s *State) ListenAddr() string {
	return s.transport.Conn.LocalAddr().String()
}

func (s *State) LoopbackAddr() string {
	addr := s.transport.Conn.LocalAddr().String()
	if _, port, err := net.SplitHostPort(addr); err == nil {
		return "127.0.0.1:" + port
	}

	return addr
}

type stateOptions struct {
	certPath string
	keyPath  string

	certData []byte
	keyData  []byte

	bindAddr     string
	wsBindAddr   string
	tcpBindAddr  string
	restBindAddr string

	memServerName string

	endpoint string

	skipVerify bool
	caCert     []byte

	requireClientCerts bool

	level slog.Level
	log   *slog.Logger

	serverLocalAddr string
	clientLocalAddr string

	authenticator   Authenticator
	authorizer      Authorizer
	bearerToken     string // JWT or other bearer token for authentication
	bearerTokenFunc func() (string, error)
	tlsServerName   string

	httpHandlers []httpHandlerMount
}

// httpHandlerMount is an additional handler to mount alongside the RPC surface.
type httpHandlerMount struct {
	pattern string
	handler http.Handler
}

type StateOption func(*stateOptions)

func WithCert(certPath, keyPath string) StateOption {
	return func(o *stateOptions) {
		o.certPath = certPath
		o.keyPath = keyPath
	}
}

func WithCertPEMs(certData, keyData []byte) StateOption {
	return func(o *stateOptions) {
		o.certData = slices.Clone(certData)
		o.keyData = slices.Clone(keyData)
	}
}

func WithBindAddr(addr string) StateOption {
	return func(o *stateOptions) {
		o.bindAddr = addr
	}
}

// WithWSBindAddr enables an additional TCP listener serving the RPC protocol
// over TLS as a WebSocket message session, bound to addr. Use "host:0" to bind
// an ephemeral port; the chosen address is available via State.WSListenAddr
// after NewState returns. Clients reach it with State.Connect("ws://host:port").
func WithWSBindAddr(addr string) StateOption {
	return func(o *stateOptions) {
		o.wsBindAddr = addr
	}
}

// WithTCPBindAddr enables a raw TLS-over-TCP listener serving the RPC protocol
// as a message session, bound to addr. It carries no HTTP layer at all, which
// makes it the leanest option where a WebSocket handshake buys nothing. Clients
// reach it with State.Connect("tcp://host:port").
func WithTCPBindAddr(addr string) StateOption {
	return func(o *stateOptions) {
		o.tcpBindAddr = addr
	}
}

// WithRESTBindAddr enables a TCP listener serving the REST/JSON gateway over
// TLS, bound to addr. It answers HTTP/2 and HTTP/1.1 (negotiated by ALPN), so
// an ordinary HTTP client reaches the same handlers the QUIC transport serves.
// Use "host:0" to bind an ephemeral port; the chosen address is available via
// State.RESTListenAddr after NewState returns.
//
// Setting this also mounts REST routes for every HTTP-annotated method of the
// interfaces passed to Server.ExposeValue. Without it no REST route is mounted
// at all, so a deployment that has not opted in keeps exactly the surface it
// had before.
func WithRESTBindAddr(addr string) StateOption {
	return func(o *stateOptions) {
		o.restBindAddr = addr
	}
}

// WithMemServer registers this State as an in-process message server under the
// given name. Peers connect with State.Connect("mem://name", ...). Used for
// testing and for bridging RPC onto message-oriented systems.
func WithMemServer(name string) StateOption {
	return func(o *stateOptions) {
		o.memServerName = name
	}
}

func WithSkipVerify(o *stateOptions) {
	o.skipVerify = true
}

func WithLogger(log *slog.Logger) StateOption {
	return func(o *stateOptions) {
		o.log = log
	}
}

func WithLogLevel(level slog.Level) StateOption {
	return func(o *stateOptions) {
		o.level = level
	}
}

func WithLocalServer(addr string) StateOption {
	return func(o *stateOptions) {
		o.serverLocalAddr = addr
	}
}

func WithLocalConnect(addr string) StateOption {
	return func(o *stateOptions) {
		o.clientLocalAddr = addr
	}
}

func WithCertificateVerification(caCert []byte) StateOption {
	return func(o *stateOptions) {
		if caCert != nil {
			o.skipVerify = false
			o.caCert = caCert
		}
	}
}

func WithRequireClientCerts(o *stateOptions) {
	o.requireClientCerts = true
}

func WithEndpoint(endpoint string) StateOption {
	return func(o *stateOptions) {
		o.endpoint = endpoint
	}
}

func WithAuthenticator(auth Authenticator) StateOption {
	return func(o *stateOptions) {
		o.authenticator = auth
	}
}

func WithAuthorizer(authz Authorizer) StateOption {
	return func(o *stateOptions) {
		o.authorizer = authz
	}
}

// WithHTTPHandler mounts an additional handler beside the RPC surface, so a
// cluster-internal service can be reached over the listener that already
// authenticates callers instead of opening a port of its own.
//
// Pattern uses http.ServeMux syntax. Registration happens before the listener
// starts serving, so a mounted route is live for the first request.
//
// Two things about what a handler mounted here does and does not inherit.
// It does inherit authentication: a non-RPC path is refused outright unless the
// authenticator produced an identity, so an anonymous caller never reaches the
// handler. It does not inherit authorization, because the authorizer runs only
// on RPC method dispatch. A handler is responsible for deciding what its caller
// may do, and should not read an identity off the context and assume the
// answer, since a cluster certificate authenticates as a superuser.
func WithHTTPHandler(pattern string, handler http.Handler) StateOption {
	return func(o *stateOptions) {
		o.httpHandlers = append(o.httpHandlers, httpHandlerMount{pattern: pattern, handler: handler})
	}
}

func WithBearerToken(token string) StateOption {
	return func(o *stateOptions) {
		o.bearerToken = token
	}
}

// WithBearerTokenFunc supplies a bearer token per request, for credentials that
// are refreshed out of band and so cannot be captured once at dial time. The
// sandbox workload identity token is the motivating case: it expires hourly and
// is rewritten on disk by the sandbox controller, so a client that read it once
// would start failing after an hour.
//
// Takes precedence over WithBearerToken.
func WithBearerTokenFunc(fn func() (string, error)) StateOption {
	return func(o *stateOptions) {
		o.bearerTokenFunc = fn
	}
}

// WithTLSServerName overrides the name the server certificate is verified
// against, independent of the address dialed. Needed when the dial address
// cannot appear in the certificate — a sandbox reaches the API through its
// bridge router address, which is allocated only after the certificate has been
// issued, and verifies against the API's stable name instead.
//
// This is not a way to skip verification: the certificate must still chain to
// the configured CA and cover this name.
func WithTLSServerName(name string) StateOption {
	return func(o *stateOptions) {
		o.tlsServerName = name
	}
}

func NewState(ctx context.Context, opts ...StateOption) (*State, error) {
	var so stateOptions

	for _, opt := range opts {
		opt(&so)
	}

	if so.bindAddr == "" {
		so.bindAddr = "localhost:0"
	}

	if so.log == nil {
		so.log = slog.Default()
	}

	up, err := net.ResolveUDPAddr("udp", so.bindAddr)
	if err != nil {
		return nil, err
	}

	udpConn, err := net.ListenUDP("udp", up)
	if err != nil {
		return nil, err
	}

	tlsCfg := &tls.Config{
		InsecureSkipVerify: so.skipVerify,
		NextProtos:         []string{http3.NextProtoH3},
		VerifyConnection: func(cs tls.ConnectionState) error {
			return nil
		},
	}

	if so.tlsServerName != "" {
		tlsCfg.ServerName = so.tlsServerName
	}

	if so.caCert != nil {
		tlsCfg.RootCAs = x509.NewCertPool()
		tlsCfg.RootCAs.AppendCertsFromPEM(so.caCert)
		tlsCfg.VerifyConnection = func(cs tls.ConnectionState) error {
			return nil
		}
	}

	var cert tls.Certificate

	if so.certData != nil && so.keyData != nil {
		cert, err = tls.X509KeyPair(so.certData, so.keyData)
		if err != nil {
			return nil, err
		}

		tlsCfg.Certificates = []tls.Certificate{cert}
	} else if so.certPath != "" && so.keyPath != "" {
		cert, err = tls.LoadX509KeyPair(so.certPath, so.keyPath)
		if err != nil {
			return nil, err
		}

		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}

	if so.level == 0 {
		so.level = DefaultLogLevel
	}

	if so.log == nil {
		so.log = slog.New(slogfmt.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: so.level,
		}))
	}

	// Use NoOpAuthenticator if none provided
	authenticator := so.authenticator
	if authenticator == nil {
		authenticator = &NoOpAuthenticator{}
	}

	server := newServer()
	if err := server.mountHTTPHandlers(so.httpHandlers); err != nil {
		return nil, err
	}

	s := &State{
		StateCommon: &StateCommon{
			top:           ctx,
			log:           so.log,
			auditLog:      newAuditLogger(so.log),
			certAuth:      newCertAuthDeduper(),
			opts:          &so,
			clientTlsCfg:  tlsCfg,
			privkey:       priv,
			pubkey:        pub,
			authenticator: authenticator,
			authorizer:    so.authorizer,
		},

		defaultEndpoint: so.endpoint,
		server:          server,
		transport:       &quic.Transport{Conn: udpConn},
	}

	s.qc = DefaultQUICConfig

	err = s.startListener(ctx, &so)
	if err != nil {
		return nil, err
	}

	err = s.setupLocal(ctx)
	if err != nil {
		return nil, err
	}

	if so.serverLocalAddr != "" {
		err := s.startLocalListener(ctx, so.serverLocalAddr)
		if err != nil {
			return nil, err
		}
	}

	if so.wsBindAddr != "" {
		err := s.startWSListener(ctx, so.wsBindAddr)
		if err != nil {
			return nil, err
		}
	}

	if so.tcpBindAddr != "" {
		err := s.startTCPListener(ctx, so.tcpBindAddr)
		if err != nil {
			return nil, err
		}
	}

	if so.restBindAddr != "" {
		err := s.startRESTListener(ctx, so.restBindAddr)
		if err != nil {
			return nil, err
		}
	}

	if so.memServerName != "" {
		s.msgContact = "mem://" + so.memServerName
		defaultMemRegistry.register(ctx, so.memServerName, s)
	}

	return s, nil
}

func (s *State) Server() *Server {
	return s.server
}

func (s *State) setupServerTls(so *stateOptions) error {
	var (
		cert tls.Certificate
		err  error
	)

	if so.certData != nil && so.keyData != nil {
		cert, err = tls.X509KeyPair(so.certData, so.keyData)
		if err != nil {
			return err
		}
	} else if so.certPath != "" && so.keyPath != "" {
		cert, err = tls.LoadX509KeyPair(so.certPath, so.keyPath)
		if err != nil {
			return err
		}

	} else {
		cert, err = generateSelfSignedCert()
		if err != nil {
			return err
		}
	}

	s.cert = cert

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{http3.NextProtoH3},
	}

	if so.caCert != nil {
		tlsCfg.ClientCAs = x509.NewCertPool()
		tlsCfg.ClientCAs.AppendCertsFromPEM(so.caCert)
		if so.requireClientCerts {
			tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert
		} else {
			// VerifyClientCertIfGiven: a client is not required to present a
			// cert (JWT/OIDC callers authenticate via the Authorization header),
			// but if one IS presented it MUST chain to ClientCAs. This ensures
			// r.TLS.PeerCertificates only ever holds a cert that has been
			// verified against the cluster CA, so authenticators that derive an
			// identity from the cert cannot be fooled by a self-signed forgery.
			tlsCfg.ClientAuth = tls.VerifyClientCertIfGiven
		}
	}

	s.serverTlsCfg = tlsCfg

	return nil
}

func (s *State) setupServer(so *stateOptions) error {
	err := s.setupServerTls(so)
	if err != nil {
		return err
	}

	ec, err := s.transport.ListenEarly(s.serverTlsCfg, &s.qc)
	if err != nil {
		return err
	}

	s.li = ec
	s.server.state = s
	s.server.restEnabled = so.restBindAddr != ""

	return nil
}

type connectionKey struct{}

type CurrentConnectionInfo struct {
	PeerSubject string
	// PeerCertificate is the client certificate presented during the mTLS
	// handshake, if any. When a CA is configured the server uses
	// tls.VerifyClientCertIfGiven, so any cert present here has already been
	// verified to chain to the cluster CA (r.TLS.VerifiedChains is non-empty).
	PeerCertificate *x509.Certificate
}

func ConnectionInfo(ctx context.Context) *CurrentConnectionInfo {
	v := ctx.Value(connectionKey{})
	if v == nil {
		return nil
	}

	return v.(*CurrentConnectionInfo)
}

// ContextWithConnectionInfo returns a copy of ctx carrying the given connection
// info, as observed by ConnectionInfo. The server populates this from the mTLS
// handshake; tests use it to exercise handlers that authorize on the peer cert.
func ContextWithConnectionInfo(ctx context.Context, info *CurrentConnectionInfo) context.Context {
	return context.WithValue(ctx, connectionKey{}, info)
}

func (s *State) startListener(ctx context.Context, so *stateOptions) error {
	err := s.setupServer(so)
	if err != nil {
		return err
	}

	s.ws = &webtransport.Server{
		H3: http3.Server{
			Handler:         s.server,
			EnableDatagrams: true,
			QUICConfig:      &s.qc,
			TLSConfig:       s.serverTlsCfg,
			// Use a logger with LevelWarn to suppress noisy debug messages from quic-go
			// while preventing nil pointer panics
			Logger: slog.New(slogfmt.NewTextHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelWarn,
			})).With("module", "http3"),
		},
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	s.hs = &s.ws.H3
	s.server.ws = s.ws

	go func() {
		<-ctx.Done()
		s.hs.Shutdown(context.Background())
	}()

	// Trigger webtransport server initialization by calling Serve with a stub PacketConn.
	// This sets up the stream hijackers on H3 without actually serving connections.
	// The stub returns net.ErrClosed immediately, causing Serve to return after init.
	_ = s.ws.Serve(closedPacketConn{})

	go s.hs.ServeListener(s.li)

	return nil
}

func (s *State) Close() error {
	s.li.Close()
	if s.hs != nil {
		s.hs.Close()
	}

	if s.httpSrv != nil {
		s.httpSrv.Close()
	}

	if s.tcpLn != nil {
		s.tcpLn.Close()
	}

	if s.msgLn != nil {
		s.msgLn.Close()
	}

	if s.restSrv != nil {
		s.restSrv.Close()
	}

	if s.restLn != nil {
		s.restLn.Close()
	}

	return s.transport.Conn.Close()
}

func (s *State) Client(name string) (*NetworkClient, error) {
	if s.defaultEndpoint == "" {
		return nil, fmt.Errorf("no remote address specified")
	}

	return s.Connect(s.defaultEndpoint, name)
}

func (s *State) Connect(remote string, name string) (*NetworkClient, error) {
	var (
		client *NetworkClient
		err    error
	)
	if strings.HasPrefix(remote, "unix:") {
		client, err = s.connectLocal(strings.TrimPrefix(remote, "unix:"))
		if err != nil {
			return nil, err
		}
	} else if isMessageRemote(remote) {
		// mem://, ws://, wss:// and tcp:// all multiplex every operation over one
		// message session; setupMsgTransport picks the connection factory.
		client = &NetworkClient{
			State:         s,
			transportKind: transportMsg,
			tlsCfg:        s.clientTlsCfg,
			remote:        remote,
		}

		client.setupTransport()
	} else if remote == "dial-stdio" {
		shstr := os.Getenv("MIREN_DIAL_PROGRAM")
		if shstr == "" {
			return nil, fmt.Errorf("MIREN_DIAL_PROGRAM not set")
		}

		s.log.Debug("dialing stdio", "command", shstr)

		cmd := exec.Command("sh", "-c", shstr)
		cmd.Env = os.Environ()

		client, err = s.connectProcess(cmd)
		if err != nil {
			return nil, err
		}
	} else {
		client = &NetworkClient{
			State:     s,
			transport: s.transport,
			tlsCfg:    s.clientTlsCfg,
			remote:    remote,
		}

		client.setupTransport()
	}

	err = client.resolveCapability(name)
	if err != nil {
		s.log.Debug("error resolving capability", "error", err)
		return nil, err
	}

	err = client.sendIdentity(s.top)
	if err != nil {
		s.log.Debug("error sending identity", "error", err)
		return nil, err
	}

	return client, nil
}

func (c *NetworkClient) newClientUnder(capa *Capability) *NetworkClient {
	// see if we have the issuer of this capa in our knownAddresses table,
	// and if so, we use that as it's remote address rather than the one
	// in the capability.
	// We do this because the address that client has for itself can be
	// different than the address that this server sees, likely due to NAT.

	addr := capa.Address
	transport := c.State.transport
	tk := c.transportKind

	if strings.HasPrefix(addr, "unix:") {
		transport = c.State.localTransport
	}

	// An empty address means "wherever this capability came from", which for a
	// message transport is the session it arrived on.
	sameSession := addr == "" && c.transportKind == transportMsg

	if addr == "" {
		addr = c.remote
	}

	if isMessageRemote(addr) {
		tk = transportMsg
	}

	newClient := &NetworkClient{
		State:         c.State,
		transportKind: tk,
		transport:     transport,
		tlsCfg:        c.State.clientTlsCfg.Clone(),
		capa:          capa,
		oid:           capa.OID,
		remote:        addr,
	}

	if sameSession {
		// Share the parent's session rather than dialing again. On a connection
		// supplied by the caller there is nothing to dial, and even where there
		// is, a capability reachable through this session should not open a
		// second one.
		newClient.ops = c.ops
	} else {
		newClient.setupTransport()
	}

	return newClient
}

func (s *State) newClientFrom(capa *Capability, peer *x509.Certificate) *NetworkClient {
	transport := s.transport
	tk := transportH3

	if strings.HasPrefix(capa.Address, "unix:") {
		transport = s.localTransport
	}

	if isMessageRemote(capa.Address) {
		tk = transportMsg
	}

	cfg := s.clientTlsCfg.Clone()
	cfg.InsecureSkipVerify = true

	if peer != nil {
		cfg.VerifyPeerCertificate = func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			if bytes.Equal(peer.Raw, rawCerts[0]) {
				return nil
			}

			return fmt.Errorf("certificate mismatch")
		}
	}

	c := &NetworkClient{
		State:         s,
		transportKind: tk,
		transport:     transport,
		tlsCfg:        cfg,
		capa:          capa,
		oid:           capa.OID,
		remote:        capa.Address,
	}

	c.setupTransport()

	return c
}
