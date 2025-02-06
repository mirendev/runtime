package rpc

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/mr-tron/base58"
	"github.com/pkg/errors"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
)

type Client struct {
	*State

	transport  *quic.Transport
	capa       *Capability
	remote     string
	remoteAddr net.Addr
	oid        OID

	// This is the remote address that the server
	// observes this client as coming from. We use this address
	// to populate any capabilites that we pass to the server.
	serverObservedAddress string

	cachedConn *cachedConn
}

func (c *Client) String() string {
	return fmt.Sprintf("Client(remote: %s, oid: %s)", c.remote, c.oid)
}

func (c *Client) reexportCapability(origin *Client) (*Capability, error) {
	// We need to re-export the capability held by +cl+ so that it can
	// be used by the entities that we're calling.

	return origin.requestReexportCapability(c.top, origin.capa, c.capa.Issuer)
}

func (c *Client) NewCapability(i *Interface, lower any) *Capability {
	if rc, ok := lower.(interface{ CapabilityClient() *Client }); ok {
		capa, err := c.reexportCapability(rc.CapabilityClient())
		if err != nil {
			panic(err)
		}

		return capa
	} else {
		return c.server.assignCapability(i, c.capa.Issuer, c.serverObservedAddress)
	}
}

func (c *Client) roundTrip(r *http.Request) (*http.Response, error) {
	hc, err := c.conn(context.Background())
	if err != nil {
		return nil, err
	}

	return hc.RoundTrip(r)
}

func (c *Client) sendIdentity(ctx context.Context) error {
	url := "https://" + c.remote + "/_rpc/identify"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("rpc-public-key", base58.Encode(c.pubkey))

	Propagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	ts := time.Now()

	tss := ts.Format(time.RFC3339Nano)

	req.Header.Set("rpc-timestamp", tss)

	var buf bytes.Buffer

	fmt.Fprintf(&buf, "POST %s %s", req.URL.Path, tss)

	sign, err := c.privkey.Sign(rand.Reader, buf.Bytes(), crypto.Hash(0))
	if err != nil {
		return err
	}

	req.Header.Set("rpc-signature", base58.Encode(sign))

	resp, err := c.roundTrip(req)
	if err != nil {
		return err
	}

	c.log.Debug("rpc.identify", "status", resp.StatusCode)

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

func (c *Client) resolveCapability(name string) error {
	c.log.Info("rpc.resolve", "name", name)
	url := "https://" + c.remote + "/_rpc/lookup/" + name
	c.log.Info("rpc.resolve", "url", url)
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("error creating new http request: %w", err)
	}

	c.log.Debug("rpc.resolve", "url", url)

	req.Header.Set("rpc-public-key", base58.Encode(c.pubkey))
	req.Header.Set("rpc-contact-addr", c.remote)

	resp, err := c.roundTrip(req)
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
		return fmt.Errorf("unable to decode response body: %w", err)
	}

	if lr.Error != "" {
		return errors.New(lr.Error)
	}

	c.capa = lr.Capability
	c.oid = lr.Capability.OID

	return nil
}

func (c *Client) requestReexportCapability(ctx context.Context, capa *Capability, target ed25519.PublicKey) (*Capability, error) {
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

func (c *Client) refOID(ctx context.Context, oid OID) error {
	url := "https://" + c.remote + "/_rpc/ref/" + string(oid)
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

func (c *Client) derefOID(ctx context.Context, oid OID) error {
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
	return c.derefOID(c.top, c.oid)
}

func (c *Client) conn(ctx context.Context) (*http3.ClientConn, error) {
	if c.cachedConn != nil {
		return c.cachedConn.hc, nil
	}

	addr := c.remoteAddr

	if addr == nil {
		udpAddr, err := net.ResolveUDPAddr("udp", c.remote)
		if err != nil {
			return nil, err
		}

		addr = udpAddr
	}

	ec, err := c.transport.DialEarly(ctx, addr, c.clientTlsCfg, &DefaultQUICConfig)
	if err != nil {
		return nil, err
	}

	// wait for the handshake to complete. We can't use 0rtt because the request
	// data needs to be secure.
	select {
	case <-ec.HandshakeComplete():
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	hc := DefaultTransport.NewClientConn(ec)

	c.cachedConn = &cachedConn{
		ec: ec,
		hc: hc,
	}

	return hc, nil
}

func (c *Client) prepareRequest(ctx context.Context, req *http.Request) error {
	Propagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	ts := time.Now()

	tss := ts.Format(time.RFC3339Nano)

	req.Header.Set("rpc-timestamp", tss)

	var buf bytes.Buffer

	fmt.Fprintf(&buf, "POST %s %s", req.URL.Path, tss)

	sign, err := c.privkey.Sign(rand.Reader, buf.Bytes(), crypto.Hash(0))
	if err != nil {
		return err
	}

	req.Header.Set("rpc-signature", base58.Encode(sign))

	return nil
}

func (c *Client) Call(ctx context.Context, method string, args, result any) error {
	//c.log.InfoContext(ctx, "rpc.call", "method", method, "oid", string(c.oid))

	ctx, span := Tracer().Start(ctx, "rpc.call."+method)
	defer span.End()

	span.SetAttributes(attribute.String("oid", string(c.oid)))

	hc, err := c.conn(ctx)
	if err != nil {
		return err
	}

	rs, err := hc.OpenRequestStream(ctx)
	if err != nil {
		return err
	}

	data, err := cbor.Marshal(args)
	if err != nil {
		return err
	}

	url := "https://" + c.remote + "/_rpc/call/" + string(c.oid) + "/" + method
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}

	err = c.prepareRequest(ctx, req)
	if err != nil {
		return err
	}

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
