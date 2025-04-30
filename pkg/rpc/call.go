package rpc

import (
	"crypto/ed25519"
	"crypto/x509"
	"fmt"
	"net/http"

	"github.com/fxamacker/cbor/v2"
	"miren.dev/runtime/pkg/webtransport"
)

type Call interface {
	NewClient(capa *Capability) Client
	Args(v any)
	Results(v any)
	NewCapability(i *Interface) *Capability
}

type NetworkCall struct {
	s        *Server
	r        *http.Request
	dec      *cbor.Decoder
	oid      OID
	method   string
	category string

	caller ed25519.PublicKey

	peer *x509.Certificate

	results any

	inline bool

	argData []byte
	local   *localCall

	ctrl      *controlStream
	wsSession *webtransport.Session
}

func (c *NetworkCall) String() string {
	return fmt.Sprintf("<Call %s %s>", c.oid, c.method)
}

func (c *NetworkCall) Args(v any) {
	if c.argData != nil {
		cbor.Unmarshal(c.argData, v)
	} else if c.dec != nil {
		c.dec.Decode(v)
	} else {
		cbor.NewDecoder(c.r.Body).Decode(v)
	}
}

func (c *NetworkCall) Results(v any) {
	c.results = v
}

func (c *NetworkCall) RemoteAddr() string {
	return c.r.RemoteAddr
}

func (c *NetworkCall) NewCapability(i *Interface) *Capability {
	if c.local != nil {
		return c.local.NewCapability(i)
	}

	return c.s.assignCapability(i, c.caller, "", c.category, true)
}

func (c *NetworkCall) NewClient(capa *Capability) Client {
	if c.local != nil {
		return c.local.NewClient(capa)
	}

	client := c.s.state.newClientFrom(capa, c.peer)
	if capa.Inline && c.wsSession != nil {
		client.inlineClient = &inlineClient{
			log:     c.s.state.log,
			oid:     capa.OID,
			ctrl:    c.ctrl,
			session: c.wsSession,
		}
	}

	return client
}
