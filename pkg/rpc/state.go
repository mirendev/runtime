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
	"miren.dev/runtime/pkg/packet"
	"miren.dev/runtime/pkg/slogfmt"
	"miren.dev/runtime/pkg/webtransport"
)

var (
	DefaultTransport  http3.Transport
	DefaultQUICConfig quic.Config

	DefaultLogLevel = slog.LevelInfo
)

func init() {
	DefaultTransport.EnableDatagrams = true
	DefaultTransport.Logger = slog.Default()
	DefaultTransport.TLSClientConfig = &tls.Config{
		InsecureSkipVerify: true,
	}

	DefaultQUICConfig = quic.Config{
		EnableDatagrams:       true,
		MaxIncomingStreams:    1000,
		MaxIncomingUniStreams: 1000,
		Allow0RTT:             true,
		KeepAlivePeriod:       10 * time.Second,
		MaxIdleTimeout:        30 * time.Second,
	}

	DefaultTransport.QUICConfig = &DefaultQUICConfig
}

type StateCommon struct {
	top context.Context
	log *slog.Logger

	opts *stateOptions

	serverTlsCfg *tls.Config
	clientTlsCfg *tls.Config
	cert         tls.Certificate

	privkey ed25519.PrivateKey
	pubkey  ed25519.PublicKey

	qc quic.Config
}

type State struct {
	*StateCommon

	transport      *quic.Transport
	localTransport *quic.Transport
	localRemote    net.Addr

	server *Server
	hs     *http3.Server
	ws     *webtransport.Server
	li     *quic.EarlyListener

	localMP *packet.PacketConnMultiplex
}

func (s *State) ListenAddr() string {
	return s.transport.Conn.LocalAddr().String()
}

type cachedConn struct {
	ec quic.EarlyConnection
	hc *http3.ClientConn
}

type stateOptions struct {
	certPath string
	keyPath  string

	certData []byte
	keyData  []byte

	bindAddr string

	skipVerify bool
	caCert     []byte

	requireClientCerts bool

	level slog.Level
	log   *slog.Logger

	serverLocalAddr string
	clientLocalAddr string
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

func NewState(ctx context.Context, opts ...StateOption) (*State, error) {
	var so stateOptions

	for _, opt := range opts {
		opt(&so)
	}

	if so.bindAddr == "" {
		so.bindAddr = "localhost:0"
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
			so.log.Debug("connected to unverified peer", "name", cs.ServerName)
			return nil
		},
	}

	if so.caCert != nil {
		so.log.Info("adding CA cert to client TLS config")
		tlsCfg.RootCAs = x509.NewCertPool()
		tlsCfg.RootCAs.AppendCertsFromPEM(so.caCert)
		tlsCfg.VerifyConnection = func(cs tls.ConnectionState) error {
			so.log.Debug("connected to verified peer", "name", cs.ServerName)
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

	s := &State{
		StateCommon: &StateCommon{
			top:          ctx,
			log:          so.log,
			clientTlsCfg: tlsCfg,
			privkey:      priv,
			pubkey:       pub,
		},

		server:    newServer(),
		transport: &quic.Transport{Conn: udpConn},
	}

	qc := quic.Config{
		EnableDatagrams:       true,
		MaxIncomingStreams:    1000,
		MaxIncomingUniStreams: 1000,
		Allow0RTT:             true,
		KeepAlivePeriod:       10 * time.Second,
	}

	s.qc = qc

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
			tlsCfg.ClientAuth = tls.RequestClientCert
		}
		tlsCfg.VerifyConnection = func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				s.log.Warn("client connection has no certificates")
			} else {
				cert := cs.PeerCertificates[0]
				s.log.Info("verified client connection", "subject", cert.Subject)
			}
			return nil
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

	return nil
}

type connectionKey struct{}

type CurrentConnectionInfo struct {
	PeerSubject string
}

func ConnectionInfo(ctx context.Context) *CurrentConnectionInfo {
	v := ctx.Value(connectionKey{})
	if v == nil {
		return nil
	}

	return v.(*CurrentConnectionInfo)
}

func (s *State) startListener(ctx context.Context, so *stateOptions) error {
	err := s.setupServer(so)
	if err != nil {
		return err
	}

	s.ws = &webtransport.Server{
		H3: http3.Server{
			Handler: s.server,
			Logger:  s.log.With("module", "http3"),
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

	err = s.ws.Init()
	if err != nil {
		return err
	}

	go s.hs.ServeListener(s.li)

	return nil
}

func (s *State) Close() error {
	s.li.Close()
	if s.hs != nil {
		s.hs.Close()
	}

	return s.transport.Conn.Close()
}

func (s *State) Connect(remote string, name string) (*Client, error) {
	var (
		client *Client
		err    error
	)
	if strings.HasPrefix(remote, "unix:") {
		client, err = s.connectLocal(strings.TrimPrefix(remote, "unix:"))
		if err != nil {
			return nil, err
		}
	} else if remote == "dial-stdio" {
		shstr := os.Getenv("RUNTIME_DIAL_PROGRAM")
		if shstr == "" {
			return nil, fmt.Errorf("RUNTIME_DIAL_PROGRAM not set")
		}

		s.log.Debug("dialing stdio", "command", shstr)

		cmd := exec.Command("sh", "-c", shstr)
		cmd.Env = os.Environ()

		client, err = s.connectProcess(cmd)
		if err != nil {
			return nil, err
		}
	} else {
		client = &Client{
			State:     s,
			transport: s.transport,
			tlsCfg:    s.clientTlsCfg,
			remote:    remote,
		}

		client.setupTransport()
	}

	err = client.resolveCapability(name)
	if err != nil {
		s.log.Error("error resolving capability", "error", err)
		return nil, err
	}

	err = client.sendIdentity(s.top)
	if err != nil {
		s.log.Error("error sending identity", "error", err)
		return nil, err
	}

	return client, nil
}

func (c *Client) newClientUnder(capa *Capability) *Client {
	// see if we have the issuer of this capa in our knownAddresses table,
	// and if so, we use that as it's remote address rather than the one
	// in the capability.
	// We do this because the address that client has for itself can be
	// different than the address that this server sees, likely due to NAT.

	addr := capa.Address
	transport := c.State.transport

	if strings.HasPrefix(addr, "unix:") {
		transport = c.State.localTransport
	}

	if addr == "" {
		addr = c.remote
	}

	newClient := &Client{
		State:     c.State,
		transport: transport,
		tlsCfg:    c.State.clientTlsCfg.Clone(),
		capa:      capa,
		oid:       capa.OID,
		remote:    addr,
	}

	newClient.setupTransport()

	return newClient
}

func (s *State) newClientFrom(capa *Capability, peer *x509.Certificate) *Client {
	transport := s.transport

	if strings.HasPrefix(capa.Address, "unix:") {
		transport = s.localTransport
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

	c := &Client{
		State:     s,
		transport: transport,
		tlsCfg:    cfg,
		capa:      capa,
		oid:       capa.OID,
		remote:    capa.Address,
	}

	c.setupTransport()

	return c
}
