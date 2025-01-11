package rpc

import (
	"crypto/ed25519"
	"net/http"

	"github.com/fxamacker/cbor/v2"
)

//go:generate go run ./cmd/rpcgen -pkg=format -input=format/rpc.yml -output=format/rpc.gen.go -pkg=format

type Call struct {
	s      *Server
	r      *http.Request
	oid    OID
	method string

	caller ed25519.PublicKey

	results any
}

func (c *Call) Args(v any) {
	cbor.NewDecoder(c.r.Body).Decode(v)
}

func (c *Call) Results(v any) {
	c.results = v
}

func (c *Call) RemoteAddr() string {
	return c.r.RemoteAddr
}

func (c *Call) NewCapability(i *Interface) *Capability {
	return c.s.assignCapability(i, c.caller, "")
}

func (c *Call) NewClient(capa *Capability) *Client {
	return c.s.NewClient(capa)
}
