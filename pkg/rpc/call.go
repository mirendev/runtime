package rpc

import (
	"crypto/ed25519"
	"crypto/x509"
	"fmt"
	"net/http"

	"github.com/fxamacker/cbor/v2"
)

//go:generate go run ./cmd/rpcgen -pkg=format -input=format/rpc.yml -output=format/rpc.gen.go -pkg=format

type Call struct {
	s        *Server
	r        *http.Request
	oid      OID
	method   string
	category string

	caller ed25519.PublicKey

	peer *x509.Certificate

	results any

	argData []byte
	local   *localCall
}

func (c *Call) String() string {
	return fmt.Sprintf("<Call %s %s>", c.oid, c.method)
}

func (c *Call) Args(v any) {
	if c.argData != nil {
		cbor.Unmarshal(c.argData, v)
	} else {
		cbor.NewDecoder(c.r.Body).Decode(v)
	}
}

func (c *Call) Results(v any) {
	c.results = v
}

func (c *Call) RemoteAddr() string {
	return c.r.RemoteAddr
}

func (c *Call) NewCapability(i *Interface) *Capability {
	if c.local != nil {
		return c.local.NewCapability(i)
	}

	return c.s.assignCapability(i, c.caller, "", c.category)
}

func (c *Call) NewClient(capa *Capability) *Client {
	if c.local != nil {
		return c.local.NewClient(capa)
	}

	return c.s.state.newClientFrom(capa, c.peer)
}
