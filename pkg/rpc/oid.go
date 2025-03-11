package rpc

import "github.com/mitchellh/mapstructure"

type OID string

type Capability struct {
	OID     OID    `cbor:"0,keyasint" json:"oid"`
	Address string `cbor:"1,keyasint" json:"address"`
	User    []byte `cbor:"2,keyasint" json:"owner"`
	Issuer  []byte `cbor:"3,keyasint" json:"issue"`

	RestoreState *InterfaceState `cbor:"4,keyasint" json:"restore-state"`
}

type InterfaceState struct {
	Category  string `cbor:"0,keyasint" json:"category"`
	Interface string `cbor:"1,keyasint" json:"interface"`
	Data      any    `cbor:"2,keyasint" json:"data"`
}

func (i *InterfaceState) Decode(v any) error {
	return mapstructure.Decode(i.Data, v)
}

type NoRestore struct{}

func (NoRestore) forbidRestore() {}

type ForbidRestore interface {
	forbidRestore()
}
