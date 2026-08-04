package rpc

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/mr-tron/base58"
	"github.com/pkg/errors"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/webtransport-go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"miren.dev/runtime/pkg/cond"
)

type Client interface {
	CallWithCaps(ctx context.Context, method string, args, result any, caps map[OID]*InlineCapability) error
	Call(ctx context.Context, method string, args, result any) error
	NewInlineCapability(i *Interface, lower any) (*InlineCapability, OID, *Capability)
	NewClient(capa *Capability) Client
	Close() error
}

type NetworkClient struct {
	State *State

	transport  *quic.Transport
	htr        http3.Transport
	ws         webtransport.Dialer
	qc         quic.Config
	capa       *Capability
	remote     string
	remoteAddr net.Addr
	oid        OID

	tlsCfg *tls.Config

	// This is the remote address that the server
	// observes this client as coming from. We use this address
	// to populate any capabilites that we pass to the server.
	serverObservedAddress string

	//cachedConn *cachedConn

	// conns records every QUIC connection this client has dialed, so a failed
	// request can be classified by whether a handshake ever completed. That
	// distinction is not recoverable from the error value: quic-go raises the
	// same IdleTimeoutError ("timeout: no recent network activity") both for a
	// connection that never got a packet back and for one that completed its
	// handshake and later went quiet. Those are different problems with
	// different fixes, and we own the Dial hook, so we track it ourselves.
	connMu sync.Mutex
	conns  []*quic.Conn

	inlineClient *inlineClient
	localClient  *localClient
}

func setTLSConfigServerName(tlsConf *tls.Config, addr net.Addr, host string) {
	// If no ServerName is set, infer the ServerName from the host we're connecting to.
	if tlsConf.ServerName != "" {
		return
	}
	if host == "" {
		if udpAddr, ok := addr.(*net.UDPAddr); ok {
			tlsConf.ServerName = udpAddr.IP.String()
			return
		}
	}
	h, _, err := net.SplitHostPort(host)
	if err != nil { // This happens if the host doesn't contain a port number.
		tlsConf.ServerName = host
		return
	}
	tlsConf.ServerName = h
}

func (c *NetworkClient) setupTransport() {
	c.htr.Logger = c.State.log.With("module", "rpc-call")
	c.htr.TLSClientConfig = c.tlsCfg

	// A per-client copy rather than the shared default: how long to wait for a
	// handshake depends on where we're pointed, and DefaultQUICConfig is shared
	// with the server listener.
	c.qc = DefaultQUICConfig
	c.qc.HandshakeIdleTimeout = handshakeTimeoutFor(c.remote)
	c.htr.QUICConfig = &c.qc
	c.htr.Dial = func(ctx context.Context, addr string, tlsCfg *tls.Config, cfg *quic.Config) (*quic.Conn, error) {
		uaddr, err := resolveUDPAddr(ctx, "udp", addr)
		if err != nil {
			return nil, err
		}

		setTLSConfigServerName(tlsCfg, uaddr, addr)

		conn, err := c.transport.DialEarly(ctx, uaddr, tlsCfg, cfg)
		if err != nil {
			return nil, err
		}
		c.trackConn(conn)
		return conn, nil
	}

	c.ws.TLSClientConfig = c.tlsCfg
	c.ws.QUICConfig = &c.qc
	c.ws.DialAddr = c.htr.Dial
}

func (c *NetworkClient) trackConn(conn *quic.Conn) {
	c.connMu.Lock()
	c.conns = append(c.conns, conn)
	c.connMu.Unlock()
}

// reachedServer reports whether any connection this client dialed finished its
// QUIC handshake, which is the difference between "nothing ever answered" and
// "we were talking to it and then it stopped".
//
// Dial success alone is not the answer: DialEarly returns as soon as the
// connection object exists so 0-RTT data can flow, well before the handshake
// completes. HandshakeComplete's channel is only closed on success, so a
// non-blocking receive is an accurate "did we ever get this far" test.
func (c *NetworkClient) reachedServer() bool {
	c.connMu.Lock()
	conns := slices.Clone(c.conns)
	c.connMu.Unlock()

	for _, conn := range conns {
		select {
		case <-conn.HandshakeComplete():
			return true
		default:
		}
	}
	return false
}

// classifyTransportError turns a transport failure during capability
// resolution into a ResolveError whose kind reflects what actually happened.
//
// Timeouts are the interesting case. quic-go reports a pre-handshake silence
// and a post-handshake silence with the identical IdleTimeoutError, so we
// separate them using our own record of whether a handshake ever completed.
// Non-timeout failures keep the generic kind; they already carry a useful
// message of their own.
func (c *NetworkClient) classifyTransportError(name string, elapsed time.Duration, err error) error {
	return classifyTransportError(name, c.remote, elapsed, err, c.reachedServer())
}

// classifyTransportError is the decision itself, split out from the client so
// it can be tested directly: reached is whether a QUIC handshake ever completed.
func classifyTransportError(name, remote string, elapsed time.Duration, err error, reached bool) error {
	netErr, ok := stderrors.AsType[net.Error](err)
	if !ok || !netErr.Timeout() {
		return NewResolveHTTPError(err, "error performing http request to %s for %q: %v", remote, name, err)
	}

	// Nothing ever answered, so we never got far enough to say anything about
	// the server itself. This is the common case: it isn't running, the
	// address is wrong, or the traffic is being dropped.
	if !reached {
		return NewResolveUnreachableError(name, remote, elapsed, err)
	}

	// Our own lookup deadline fired while the connection was still healthy:
	// the server is up and the transport is fine, it just never replied.
	if errors.Is(err, context.DeadlineExceeded) {
		return NewResolveNoAnswerError(name, remote, elapsed, err)
	}

	// We had a working connection and it stopped responding partway through.
	return NewResolveWentSilentError(name, remote, elapsed, err)
}

func (c *NetworkClient) NewClient(capa *Capability) Client {
	if c.localClient != nil {
		return c.localClient.NewClient(capa)
	}

	return c.newClientUnder(capa)
}

func (c *NetworkClient) String() string {
	return fmt.Sprintf("Client(remote: %s, oid: %s)", c.remote, c.oid)
}

func (c *NetworkClient) reexportCapability(oc Client) (*Capability, error) {
	origin, ok := oc.(*NetworkClient)
	if !ok {
		return nil, errors.New("origin client is not a network client")
	}

	// We need to re-export the capability held by +cl+ so that it can
	// be used by the entities that we're calling.

	return origin.requestReexportCapability(c.State.top, origin.capa, c.capa.Issuer)
}

func (c *NetworkClient) NewCapability(i *Interface, lower any) *Capability {
	if rc, ok := lower.(interface{ CapabilityClient() Client }); ok {
		capa, err := c.reexportCapability(rc.CapabilityClient())
		if err != nil {
			panic(err)
		}

		return capa
	} else if c.localClient != nil {
		return c.localClient.NewCapability(i)
	} else {
		return c.State.server.assignCapability(i, c.capa.Issuer, c.serverObservedAddress, "", true)
	}
}

func (c *NetworkClient) NewInlineCapability(i *Interface, lower any) (*InlineCapability, OID, *Capability) {
	capa := c.NewCapability(i, lower)

	ic := &InlineCapability{
		Capability: capa,
		Interface:  i,
	}

	return ic, capa.OID, capa
}

func (c *NetworkClient) roundTrip(r *http.Request) (*http.Response, error) {
	return c.htr.RoundTrip(r)
}

func (c *NetworkClient) sendIdentity(ctx context.Context) error {
	url := "https://" + c.remote + "/_rpc/identify"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("rpc-public-key", base58.Encode(c.State.pubkey))

	// Add bearer token if configured
	c.addBearerToken(req)

	Propagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	ts := time.Now()

	tss := ts.Format(time.RFC3339Nano)

	req.Header.Set("rpc-timestamp", tss)

	var buf bytes.Buffer

	fmt.Fprintf(&buf, "POST %s %s", req.URL.Path, tss)

	sign, err := c.State.privkey.Sign(rand.Reader, buf.Bytes(), crypto.Hash(0))
	if err != nil {
		return err
	}

	req.Header.Set("rpc-signature", base58.Encode(sign))

	resp, err := c.roundTrip(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var lr identifyResponse

	err = cbor.NewDecoder(resp.Body).Decode(&lr)
	if err != nil {
		return err
	}

	if lr.Error != "" {
		return errors.New(lr.Error)
	}

	if !lr.Ok {
		return errors.New("identity rejected")
	}

	c.serverObservedAddress = lr.Address

	return nil
}

// resolveCapability looks up a named capability on the remote.
//
// The deadline matters as much as the request: without one, a server that
// accepts the connection and then never answers hangs the CLI forever with no
// output at all, because the transport's keepalives keep proving the connection
// is alive so no idle timeout ever fires. Verified against a server whose
// lookup handler blocked for two minutes — the client waited the entire time
// and printed nothing. See lookupTimeoutFor for how the budget is chosen.
func (c *NetworkClient) resolveCapability(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), lookupTimeoutFor(c.remote))
	defer cancel()

	url := "https://" + c.remote + "/_rpc/lookup/" + url.PathEscape(name)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return NewResolveHTTPError(err, "error creating http request for %q at %s: %v", name, c.remote, err)
	}

	req.Header.Set("rpc-public-key", base58.Encode(c.State.pubkey))

	// Add bearer token if configured
	c.addBearerToken(req)
	req.Header.Set("rpc-contact-addr", c.remote)

	started := time.Now()
	resp, err := c.roundTrip(req)
	if err != nil {
		return c.classifyTransportError(name, time.Since(started), err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return NewResolveStatusErrorWithReason(name, c.remote, resp.StatusCode,
			resp.Header.Get("rpc-status"), resp.Header.Get("rpc-error"))
	}

	var lr lookupResponse

	err = cbor.NewDecoder(resp.Body).Decode(&lr)
	if err != nil {
		return NewResolveDecodeError(name, c.remote, err)
	}

	if lr.Error != "" {
		return NewResolveLookupError(name, c.remote, lr.Error)
	}

	c.capa = lr.Capability
	c.oid = lr.Capability.OID

	c.State.log.Debug("resolve name into capability", "name", name, "oid", string(c.oid))

	return nil
}

// reresolveName describes what we're re-resolving, for error messages. The
// interface name is the closest thing to the capability name the user would
// recognize; without it the message would name nothing at all.
func reresolveName(rs *InterfaceState) string {
	if rs != nil && rs.Interface != "" {
		return rs.Interface
	}
	return "reresolve"
}

func (c *NetworkClient) reresolveCapability(rs *InterfaceState) error {
	// Same unbounded-wait hazard as resolveCapability: a server that holds the
	// connection open without answering would hang here forever.
	ctx, cancel := context.WithTimeout(context.Background(), lookupTimeoutFor(c.remote))
	defer cancel()

	url := "https://" + c.remote + "/_rpc/reresolve"
	c.State.log.Debug("reresolving capability from state", "url", url)

	var buf bytes.Buffer
	err := cbor.NewEncoder(&buf).Encode(rs)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return fmt.Errorf("error creating new http request: %w", err)
	}

	req.Header.Set("rpc-public-key", base58.Encode(c.State.pubkey))
	req.Header.Set("rpc-contact-addr", c.remote)

	// Add bearer token if configured
	c.addBearerToken(req)

	// Reconnecting deserves the same diagnosis as connecting. Returning the
	// raw transport error here would put the original "timeout: no recent
	// network activity" back in front of anyone whose session had to
	// re-resolve, which is the failure this change exists to remove.
	started := time.Now()
	resp, err := c.roundTrip(req)
	if err != nil {
		return c.classifyTransportError(reresolveName(rs), time.Since(started), err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return NewResolveStatusError(reresolveName(rs), c.remote, resp.StatusCode)
	}

	var lr lookupResponse

	err = cbor.NewDecoder(resp.Body).Decode(&lr)
	if err != nil {
		return fmt.Errorf("unable to decode response body: %w", err)
	}

	if lr.Error != "" {
		return errors.New(lr.Error)
	}

	c.capa = lr.Capability
	c.oid = lr.Capability.OID

	return nil
}

// fetchMethods queries the server's method-introspection endpoint. Returns an
// error if the server doesn't support introspection (old servers).
func (c *NetworkClient) fetchMethods(ctx context.Context) (methodsResponse, error) {
	if c.localClient != nil {
		return c.localClient.listMethods(), nil
	}

	url := "https://" + c.remote + "/_rpc/methods/" + string(c.oid)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return methodsResponse{}, err
	}

	c.addBearerToken(req)

	resp, err := c.roundTrip(req)
	if err != nil {
		return methodsResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return methodsResponse{}, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result methodsResponse
	if err := cbor.NewDecoder(resp.Body).Decode(&result); err != nil {
		return methodsResponse{}, err
	}

	if result.Error != "" {
		return methodsResponse{}, errors.New(result.Error)
	}

	return result, nil
}

// ListMethods returns the list of methods available on this capability.
// Returns an error if the server doesn't support method introspection (old servers).
func (c *NetworkClient) ListMethods(ctx context.Context) ([]string, error) {
	result, err := c.fetchMethods(ctx)
	if err != nil {
		return nil, err
	}
	return result.Methods, nil
}

// HasMethod checks if the remote interface supports a given method.
// Returns false if the method doesn't exist or if the server doesn't support introspection.
func (c *NetworkClient) HasMethod(ctx context.Context, method string) bool {
	methods, err := c.ListMethods(ctx)
	if err != nil {
		return false
	}
	for _, m := range methods {
		if m == method {
			return true
		}
	}
	return false
}

// HasMethodParam reports whether the remote interface's method accepts a given
// parameter. This distinguishes servers that have a method from servers that
// have a newer revision of it with an added parameter. Returns false if the
// method or parameter is absent, or if the server is too old to report
// parameters at all (introspection predates this, or the method itself).
func (c *NetworkClient) HasMethodParam(ctx context.Context, method, param string) bool {
	result, err := c.fetchMethods(ctx)
	if err != nil {
		return false
	}
	for _, p := range result.Params[method] {
		if p == param {
			return true
		}
	}
	return false
}

func (c *NetworkClient) requestReexportCapability(ctx context.Context, capa *Capability, target ed25519.PublicKey) (*Capability, error) {
	url := "https://" + c.remote + "/_rpc/reexport/" + string(capa.OID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return nil, err
	}

	err = c.prepareRequest(ctx, req)
	if err != nil {
		return nil, err
	}

	req.Header.Set("rpc-target-public-key", base58.Encode(target))
	req.Header.Set("rpc-contact-addr", c.remote)

	resp, err := c.roundTrip(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

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

func (c *NetworkClient) derefOID(ctx context.Context, oid OID) error {
	if c.inlineClient != nil {
		return c.inlineClient.derefOID(ctx, oid)
	}

	url := "https://" + c.remote + "/_rpc/deref/" + string(oid)
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return err
	}

	err = c.prepareRequest(ctx, req)
	if err != nil {
		return err
	}

	resp, err := c.roundTrip(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

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

func (c *NetworkClient) Close() error {
	// Close inline client if present
	if c.inlineClient != nil {
		_ = c.inlineClient.Close()
	}

	return c.derefOID(c.State.top, c.oid)
}

// addBearerToken safely adds a bearer token to the request header if configured
func (c *NetworkClient) addBearerToken(req *http.Request) {
	if c.State == nil || c.State.opts == nil {
		return
	}

	if fn := c.State.opts.bearerTokenFunc; fn != nil {
		token, err := fn()
		if err != nil || token == "" {
			// Send the request unauthenticated and let the server reject it.
			// The caller's retry then picks up a token that has since been
			// refreshed, which a hard failure here would not.
			return
		}
		req.Header.Set("Authorization", "Bearer "+token)
		return
	}

	if c.State.opts.bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.State.opts.bearerToken)
	}
}

func (c *NetworkClient) prepareRequest(ctx context.Context, req *http.Request) error {
	Propagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	// Add bearer token if configured
	c.addBearerToken(req)

	ts := time.Now()

	tss := ts.Format(time.RFC3339Nano)

	req.Header.Set("rpc-timestamp", tss)

	var buf bytes.Buffer

	fmt.Fprintf(&buf, "%s %s %s", req.Method, req.URL.Path, tss)

	sign, err := c.State.privkey.Sign(rand.Reader, buf.Bytes(), crypto.Hash(0))
	if err != nil {
		return err
	}

	req.Header.Set("rpc-contact-addr", c.remote)
	req.Header.Set("rpc-signature", base58.Encode(sign))

	return nil
}

func resolveUDPAddr(ctx context.Context, network, addr string) (*net.UDPAddr, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	port, err := net.LookupPort(network, portStr)
	if err != nil {
		return nil, err
	}
	resolver := net.DefaultResolver
	ipAddrs, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	addrs := addrList(ipAddrs)
	ip := addrs.forResolve(network, addr)
	return &net.UDPAddr{IP: ip.IP, Port: port, Zone: ip.Zone}, nil
}

func (c *NetworkClient) Call(ctx context.Context, method string, args, result any) error {
	if c.localClient != nil {
		return c.localClient.Call(ctx, method, args, result)
	}

	if c.inlineClient != nil && c.capa.Inline {
		return c.inlineClient.Call(ctx, method, args, result)
	}

	ctx, span := Tracer().Start(ctx, "rpc.call."+method)
	defer span.End()

	data, err := cbor.Marshal(args)
	if err != nil {
		return err
	}

request:
	for {
		span.SetAttributes(attribute.String("oid", string(c.oid)))

		url := "https://" + c.remote + "/_rpc/call/" + string(c.oid) + "/" + method
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
		if err != nil {
			return err
		}

		err = c.prepareRequest(ctx, req)
		if err != nil {
			return err
		}

		hr, err := c.htr.RoundTrip(req)
		if err != nil {
			if _, ok := err.(*quic.ApplicationError); ok {
				c.State.log.Info("rpc.call retrying", "oid", string(c.oid), "error", err)
				continue request
			}

			return fmt.Errorf("error performing http request to %s: %w", url, err)
		}

		defer hr.Body.Close()

		if hr.StatusCode == http.StatusOK {
			err = cbor.NewDecoder(hr.Body).Decode(result)
		} else {
			et, _ := io.ReadAll(hr.Body)
			err = fmt.Errorf("unexpected status code: %d: %s", hr.StatusCode, et)
		}

		// We perform this draining read because quic/http3 populates the trailers
		// as part of the body read.
		io.Copy(io.Discard, hr.Body)

		switch hr.Trailer.Get("rpc-status") {
		case "ok", "":
			// The remote side thought everything was fine, so use our ability to parse
			// the response as the error.
			return err
		case "unknown-capability":
			if c.capa.RestoreState != nil {
				// We have a resolution, let's try to resolve it and update our capability.
				rerr := c.reresolveCapability(c.capa.RestoreState)
				if rerr != nil {
					return cond.NotFound("capability", c.capa.OID)
				}

				continue request
			}

			return cond.NotFound("capability", c.capa.OID)
		case "error":
			code := hr.Trailer.Get("rpc-error-code")
			category := hr.Trailer.Get("rpc-error-category")
			errs := hr.Trailer.Get("rpc-error")
			return cond.RemoteError(category, code, errs)
		case "panic":
			errs := hr.Trailer.Get("rpc-error")
			return cond.Panic(errs)
		}

		return err
	}
}

type InlineCapability struct {
	*Capability
	*Interface
}

func (c *NetworkClient) CallWithCaps(ctx context.Context, method string, args, result any, caps map[OID]*InlineCapability) error {
	if c.localClient != nil {
		return c.localClient.Call(ctx, method, args, result)
	}

	ctx, span := Tracer().Start(ctx, "rpc.call."+method)
	defer span.End()

	data, err := cbor.Marshal(args)
	if err != nil {
		return err
	}

request:
	for {
		span.SetAttributes(attribute.String("oid", string(c.oid)))

		url := "https://" + c.remote + "/_rpc/callstream/" + string(c.oid) + "/" + method
		req, err := http.NewRequestWithContext(ctx, http.MethodConnect, url, bytes.NewReader(data))
		if err != nil {
			return err
		}

		err = c.prepareRequest(ctx, req)
		if err != nil {
			return err
		}

		hr, sess, err := c.ws.Dial(ctx, url, req.Header)
		if err != nil {
			if _, ok := err.(*quic.ApplicationError); ok {
				c.State.log.Info("rpc.call retrying", "oid", string(c.oid), "error", err)
				continue request
			}

			return fmt.Errorf("error performing http request to %s: %w", url, err)
		}

		retry, err := c.handleCallStream(ctx, hr, sess, method, args, result, caps)
		if err != nil {
			return err
		}

		if retry {
			continue request
		}

		return nil
	}
}

func (c *NetworkClient) handleCallStream(
	ctx context.Context,
	hr *http.Response,
	sess *webtransport.Session,
	method string,
	args, result any,
	caps map[OID]*InlineCapability,
) (bool, error) {
	if hr.StatusCode != http.StatusOK {
		status := hr.Trailer.Get("rpc-status")

		err := cond.Errorf("unexpected status code: %d", hr.StatusCode)

		switch status {
		case "ok", "":
			// The remote side thought everything was fine, so use our ability to parse
			// the response as the error.
			return false, err
		case "unknown-capability":
			if c.capa.RestoreState != nil {
				// Try to re-resolve and, if successful, signal caller to retry the request.
				if rerr := c.reresolveCapability(c.capa.RestoreState); rerr == nil {
					return true, nil
				}
			}
			err = cond.NotFound("capability", c.capa.OID)
		case "error":
			errs := hr.Trailer.Get("rpc-error")
			err = cond.RemoteError("generic", "unknown", errs)
		case "panic":
			errs := hr.Trailer.Get("rpc-error")
			err = cond.Panic(errs)
		}

		return false, err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		for {
			str, err := sess.AcceptStream(ctx)
			if err != nil {
				break
			}

			go func() {
				defer str.Close()

				dec := cbor.NewDecoder(str)
				enc := cbor.NewEncoder(str)

				// Loop to handle multiple requests on the same stream
				for {
					var rs streamRequest

					err = dec.Decode(&rs)
					if err != nil {
						if err != io.EOF {
							c.State.log.Error("rpc.callstream call: error decoding stream request", "error", err)
						}
						return
					}

					switch rs.Kind {
					case "call":
						iface, ok := caps[rs.OID]
						if !ok {
							_ = enc.Encode(refResponse{
								Status: "error",
								Error:  "unknown capability",
							})
							continue
						}

						mm := iface.methods[rs.Method]
						if mm.Handler == nil {
							enc.Encode(refResponse{
								Status: "error",
								Error:  "unknown method",
							})
							continue
						}

						ctx, cancel := context.WithCancel(ctx)
						err := c.callInline(ctx, mm, rs.OID, rs.Method, iface.Interface, enc, dec)
						cancel()
						if err != nil {
							if !errors.Is(err, context.Canceled) && !errors.Is(err, cond.ErrClosed{}) {
								c.State.log.Error("rpc.callstream: error calling inline", "error", err)
							}
							return
						}
					default:
						c.State.log.Error("rpc.callstream: unknown call stream request", "kind", rs.Kind)
					}
				}
			}()
		}
	}()

	// Open the control stream
	ctrl, err := sess.OpenStreamSync(ctx)
	if err != nil {
		c.State.log.Error("rpc.callstream ctrl: error opening control stream", "error", err)
		return false, err
	}

	enc := cbor.NewEncoder(ctrl)
	enc.Encode(args)

	dec := cbor.NewDecoder(ctrl)

	const cancelCode = webtransport.StreamErrorCode(quic.FlowControlError)

	// If the context is canceled, then we bail ASAP on trying to complete the RPC.
	// Because we have a local ctx with a local cancel also, when this method turns, this
	// goroutine will automatically get cleaned up.
	go func() {
		<-ctx.Done()
		ctrl.CancelRead(cancelCode)
	}()

loop:
	for {
		var rs streamRequest

		err = dec.Decode(&rs)
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = nil
			} else if se, ok := err.(*webtransport.StreamError); ok && se.ErrorCode == cancelCode {
				err = cond.Closed("rpc call terminated before getting response")
			}

			break
		}

		switch rs.Kind {
		case "result":
			err = dec.Decode(result)
			break loop
		case "deref":
			c.State.server.Deref(rs.OID)
		case "error":
			err = cond.RemoteError(rs.Category, rs.Code, rs.Error)
			break loop
		case "panic":
			err = cond.Panic(rs.Error)
			break loop
		default:
			c.State.log.Error("rpc.callstream: unknown control stream request", "kind", rs.Kind)
		}
	}

	return false, err
}

func (c *NetworkClient) callInline(
	ctx context.Context,
	mm Method,
	oid OID,
	method string,
	iface *Interface,
	enc *cbor.Encoder,
	dec *cbor.Decoder,
) error {
	call := &NetworkCall{
		oid:    oid,
		method: method,
		dec:    dec,
		caller: c.capa.User,
		inline: true,
	}

	err := cond.Wrap(mm.Handler(ctx, call))

	// Defensively consume args if the handler didn't read them.
	// This prevents leftover args from being interpreted as the next stream request.
	call.SkipArgs()

	if err != nil {
		var msg, category, code string

		if emsg, ok := err.(ErrorMessage); ok {
			msg = emsg.ErrorMessage()
		} else {
			msg = err.Error()
		}

		if ecat, ok := err.(ErrorCategory); ok {
			category = ecat.ErrorCategory()
		}

		if ecode, ok := err.(ErrorCode); ok {
			code = ecode.ErrorCode()
		}

		enc.Encode(refResponse{
			Status:   "error",
			Error:    msg,
			Category: category,
			Code:     code,
		})
		return err
	}

	enc.Encode(refResponse{
		Status: "ok",
	})

	return enc.Encode(call.results)
}
