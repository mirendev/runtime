package rpc

import (
	"net/http"

	"github.com/fxamacker/cbor/v2"
)

//go:generate go run ./cmd/rpcgen -pkg=format -input=format/rpc.yml -output=format/rpc.gen.go -pkg=format

type Call struct {
	s      *Server
	r      *http.Request
	oid    OID
	method string

	results any
}

func (c *Call) Args(v any) {
	cbor.NewDecoder(c.r.Body).Decode(v)
}

func (c *Call) Results(v any) {
	c.results = v
}

func (c *Call) NewOID(i *Interface) OID {
	return c.s.AssignOID(i)
}
