package rpc

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"miren.dev/runtime/pkg/packet"
	"miren.dev/runtime/pkg/slogfmt"
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

	bindAddr string

	skipVerify bool

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

	if so.certPath != "" && so.keyPath != "" {
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

func (s *State) startListener(ctx context.Context, so *stateOptions) error {
	err := s.setupServer(so)
	if err != nil {
		return err
	}

	serv := &http3.Server{
		Handler:   s.server,
		TLSConfig: s.clientTlsCfg,
		Logger:    s.log.With("module", "http3"),
	}

	s.hs = serv

	go func() {
		<-ctx.Done()
		serv.Shutdown(context.Background())
	}()

	go serv.ServeListener(s.li)

	return nil
}

func (s *State) Close() error {
	s.li.Close()
	if s.hs != nil {
		s.hs.Close()
	}

	return nil
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
		client = &Client{
			State:     s,
			transport: s.transport,
			remote:    remote,
		}
	}

	err = client.resolveCapability(name)
	if err != nil {
		spew.Config.DisableMethods = true
		spew.Dump(err)
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

func (s *State) NewClient(capa *Capability) *Client {
	transport := s.transport

	if strings.HasPrefix(capa.Address, "unix:") {
		transport = s.localTransport
	}

	return &Client{
		State:     s,
		transport: transport,
		capa:      capa,
		oid:       capa.OID,
		remote:    capa.Address,
	}
}
