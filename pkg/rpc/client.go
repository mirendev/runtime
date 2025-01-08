package rpc

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/mr-tron/base58"
	"github.com/pkg/errors"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"miren.dev/runtime/pkg/slogfmt"
)

var (
	DefaultTransport  http3.Transport
	DefaultQUICConfig quic.Config
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

type State struct {
	log       *slog.Logger
	transport *quic.Transport

	tlsCfg *tls.Config

	server *Server
	li     *quic.EarlyListener
	cert   tls.Certificate

	privkey ed25519.PrivateKey
	pubkey  ed25519.PublicKey
}

func (s *State) ListenAddr() string {
	return s.transport.Conn.LocalAddr().String()
}

type Client struct {
	*State

	capa   *Capability
	remote string
	oid    OID
}

func (c *Client) String() string {
	return fmt.Sprintf("Client(remote: %s, oid: %s)", c.remote, c.oid)
}

func NewState(ctx context.Context, addr string) (*State, error) {
	if addr == "" {
		addr = "localhost:0"
	}

	up, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}

	udpConn, err := net.ListenUDP("udp", up)
	if err != nil {
		return nil, err
	}

	tlsCfg := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{http3.NextProtoH3},
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}

	log := slog.New(slogfmt.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	s := &State{
		log:       log,
		transport: &quic.Transport{Conn: udpConn},
		tlsCfg:    tlsCfg,
		server:    newServer(),
		privkey:   priv,
		pubkey:    pub,
	}

	err = s.startListener(ctx)
	if err != nil {
		return nil, err
	}

	return s, nil
}

func (s *State) Server() *Server {
	return s.server
}

func (s *State) setupServer() error {
	cert, err := generateSelfSignedCert()
	if err != nil {
		return err
	}

	s.cert = cert

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{http3.NextProtoH3},
	}

	ec, err := s.transport.ListenEarly(tlsCfg, &DefaultQUICConfig)
	if err != nil {
		return err
	}

	s.li = ec
	s.server.state = s

	return nil
}

func (s *State) startListener(ctx context.Context) error {
	err := s.setupServer()
	if err != nil {
		return err
	}

	serv := &http3.Server{
		Handler:   s.server,
		TLSConfig: s.tlsCfg,
		Logger:    s.log,
	}

	go func() {
		<-ctx.Done()
		serv.Shutdown(context.Background())
	}()

	go serv.ServeListener(s.li)

	return nil
}

func (s *State) Connect(remote string, name string) (*Client, error) {
	c := &Client{
		State:  s,
		remote: remote,
	}

	err := c.resolveCapability(name)
	if err != nil {
		c.log.Error("rpc.connect", "error", err)
		return nil, err
	}

	return c, nil
}

func (s *State) NewClient(capa *Capability) *Client {
	return &Client{
		State:  s,
		capa:   capa,
		oid:    capa.OID,
		remote: capa.Address,
	}
}

type CallResult struct {
	err error
	res chan *CallResult
}

type Future[T any] struct {
	done chan struct{}

	mu       sync.Mutex
	resolved bool
	err      error
	result   *T
}

func (f *Future[T]) Wait() {
	<-f.done
}

func (f *Future[T]) Done() <-chan struct{} {
	return f.done
}

func (f *Future[T]) Result() (*T, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if !f.resolved {
		return nil, errors.New("future not resolved")
	}

	return f.result, f.err
}

func (f *Future[T]) processResult(hr *http.Response) {
	defer close(f.done)

	var v T

	err := cbor.NewDecoder(hr.Body).Decode(&v)

	f.mu.Lock()
	defer f.mu.Unlock()

	f.resolved = true

	if err != nil {
		f.err = err
	} else {
		f.result = &v
	}
}

func (f *Future[T]) processError(err error) {
	defer close(f.done)

	f.mu.Lock()
	defer f.mu.Unlock()

	f.resolved = true
	f.err = err
}

type ResultProcessor interface {
	processError(err error)
	processResult(hr *http.Response)
}

func (c *Client) reexportCapability(cl *Client) (*Capability, error) {
	// We need to re-export the capability held by +cl+ so that it can
	// be used by the entities that we're calling.

	return cl.requestReexportCapability(cl.capa, c.capa.Issuer)
}

func (c *Client) NewCapability(i *Interface, lower any) *Capability {
	if rc, ok := lower.(interface{ CapabilityClient() *Client }); ok {
		capa, err := c.reexportCapability(rc.CapabilityClient())
		if err != nil {
			panic(err)
		}

		return capa
	} else {
		return c.server.assignCapability(i, c.capa.Issuer)
	}
}

func (c *Client) CallFuture(ctx context.Context, oid, method string, args any, rp ResultProcessor) {
	udpAddr, err := net.ResolveUDPAddr("udp", c.remote)
	if err != nil {
		rp.processError(err)
		return
	}

	ec, err := c.transport.DialEarly(ctx, udpAddr, c.tlsCfg, &DefaultQUICConfig)
	if err != nil {
		rp.processError(err)
		return
	}

	// wait for the handshake to complete. We can't use 0rtt because the request
	// data needs to be secure.
	select {
	case <-ec.HandshakeComplete():
	case <-ctx.Done():
		rp.processError(ctx.Err())
		return
	}

	hc := DefaultTransport.NewClientConn(ec)

	rs, err := hc.OpenRequestStream(ctx)
	if err != nil {
		rp.processError(err)
		return
	}

	url := "https://" + c.remote + "/_rpc/call/" + oid + "/" + method
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		rp.processError(err)
		return
	}

	err = rs.SendRequestHeader(req)
	if err != nil {
		rp.processError(err)
		return
	}

	err = cbor.NewEncoder(rs).Encode(args)
	if err != nil {
		rp.processError(err)
		return
	}

	err = rs.Close()

	go func() {
		num1xx := 0               // number of informational 1xx headers received
		const max1xxResponses = 5 // arbitrary bound on number of informational responses

		var (
			hr  *http.Response
			err error
		)

		for {
			hr, err = rs.ReadResponse()
			if err != nil {
				rp.processError(err)
				return
			}

			// Dup'd from http3/client.go
			resCode := hr.StatusCode
			is1xx := 100 <= resCode && resCode <= 199
			// treat 101 as a terminal status, see https://github.com/golang/go/issues/26161
			is1xxNonTerminal := is1xx && resCode != http.StatusSwitchingProtocols
			if is1xxNonTerminal {
				num1xx++
				if num1xx > max1xxResponses {
					err = errors.New("http: too many 1xx informational responses")
					rp.processError(err)
					return
				}
				continue
			}
			break
		}

		defer hr.Body.Close()

		if hr.StatusCode != http.StatusOK {
			err = errors.Errorf("unexpected status code: %d", hr.StatusCode)
			rp.processError(err)
			return
		}

		rp.processResult(hr)
	}()
}

func (c *Client) resolveCapability(name string) error {
	c.log.Info("rpc.resolve", "name", name)
	url := "https://" + c.remote + "/_rpc/lookup/" + name
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("rpc-public-key", base58.Encode(c.pubkey))

	resp, err := DefaultTransport.RoundTrip(req)
	if err != nil {
		return err
	}

	c.log.Debug("rpc.resolve", "status", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		return errors.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var lr lookupResponse

	err = cbor.NewDecoder(resp.Body).Decode(&lr)
	if err != nil {
		return err
	}

	if lr.Error != "" {
		return errors.New(lr.Error)
	}

	c.capa = lr.Capability
	c.oid = lr.Capability.OID

	return nil
}

func (c *Client) requestReexportCapability(capa *Capability, target ed25519.PublicKey) (*Capability, error) {
	url := "https://" + c.remote + "/_rpc/reexport/" + string(capa.OID)
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("rpc-target-public-key", base58.Encode(target))

	resp, err := DefaultTransport.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var lr lookupResponse

	err = cbor.NewDecoder(resp.Body).Decode(&lr)
	if err != nil {
		return nil, err
	}

	if lr.Error != "" {
		return nil, errors.New(lr.Error)
	}

	return lr.Capability, nil
}

func (c *Client) refOID(oid OID) error {
	url := "https://" + c.remote + "/_rpc/ref/" + string(oid)
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return err
	}

	resp, err := DefaultTransport.RoundTrip(req)
	if err != nil {
		return err
	}

	var lr refResponse

	err = json.NewDecoder(resp.Body).Decode(&lr)
	if err != nil {
		return err
	}

	if lr.Error != "" {
		return errors.New(lr.Error)
	}

	return nil
}

func (c *Client) derefOID(oid OID) error {
	url := "https://" + c.remote + "/_rpc/deref/" + string(oid)
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return err
	}

	resp, err := DefaultTransport.RoundTrip(req)
	if err != nil {
		return err
	}

	var lr refResponse

	err = json.NewDecoder(resp.Body).Decode(&lr)
	if err != nil {
		return err
	}

	if lr.Error != "" {
		return errors.New(lr.Error)
	}

	return nil
}

func (c *Client) Close() error {
	return c.derefOID(c.oid)
}

func (c *Client) Call(ctx context.Context, method string, args, result any) error {
	c.log.InfoContext(ctx, "rpc.call", "method", method, "oid", string(c.oid))

	ctx, span := Tracer().Start(ctx, "rpc.call."+method)
	defer span.End()

	span.SetAttributes(attribute.String("oid", string(c.oid)))

	udpAddr, err := net.ResolveUDPAddr("udp", c.remote)
	if err != nil {
		return err
	}

	ec, err := c.transport.DialEarly(ctx, udpAddr, c.tlsCfg, &DefaultQUICConfig)
	if err != nil {
		return err
	}

	// wait for the handshake to complete. We can't use 0rtt because the request
	// data needs to be secure.
	select {
	case <-ec.HandshakeComplete():
	case <-ctx.Done():
		return ctx.Err()
	}

	hc := DefaultTransport.NewClientConn(ec)

	rs, err := hc.OpenRequestStream(ctx)
	if err != nil {
		return err
	}

	data, err := cbor.Marshal(args)
	if err != nil {
		return err
	}

	url := "https://" + c.remote + "/_rpc/call/" + string(c.oid) + "/" + method
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}

	Propagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	ts := time.Now()

	tss := ts.Format(time.RFC3339Nano)

	req.Header.Set("rpc-timestamp", tss)

	var buf bytes.Buffer

	fmt.Fprintf(&buf, "POST /_rpc/call/%s/%s %s", c.oid, method, tss)

	sign, err := c.privkey.Sign(rand.Reader, buf.Bytes(), crypto.Hash(0))
	if err != nil {
		return err
	}

	req.Header.Set("rpc-signature", base58.Encode(sign))

	err = rs.SendRequestHeader(req)
	if err != nil {
		return err
	}

	_, err = rs.Write(data)
	if err != nil {
		return err
	}

	err = rs.Close()

	num1xx := 0               // number of informational 1xx headers received
	const max1xxResponses = 5 // arbitrary bound on number of informational responses

	var hr *http.Response

	for {
		hr, err = rs.ReadResponse()
		if err != nil {
			return err
		}

		// Dup'd from http3/client.go
		resCode := hr.StatusCode
		is1xx := 100 <= resCode && resCode <= 199
		// treat 101 as a terminal status, see https://github.com/golang/go/issues/26161
		is1xxNonTerminal := is1xx && resCode != http.StatusSwitchingProtocols
		if is1xxNonTerminal {
			num1xx++
			if num1xx > max1xxResponses {
				return errors.New("http: too many 1xx informational responses")
			}
			continue
		}
		break
	}

	defer hr.Body.Close()

	if hr.StatusCode != http.StatusOK {
		return errors.Errorf("unexpected status code: %d", hr.StatusCode)
	}

	err = cbor.NewDecoder(hr.Body).Decode(result)

	// We perform this draining read because quic/http3 populates the trailers
	// as part of the body read.
	io.Copy(io.Discard, hr.Body)

	switch hr.Trailer.Get("rpc-status") {
	case "ok", "":
		// The remote side thought everything was fine, so use our ability to parse
		// the response as the error.
		return err
	case "error":
		errs := hr.Trailer.Get("rpc-error")
		return fmt.Errorf("remote error: %s", errs)
	case "panic":
		errs := hr.Trailer.Get("rpc-error")
		return fmt.Errorf("remote panic: %s", errs)
	}

	return err
}
