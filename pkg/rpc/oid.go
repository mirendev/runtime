package rpc

type OID string

type Capability struct {
	OID     OID    `cbor:"0,keyasint" json:"oid"`
	Address string `cbor:"1,keyasint" json:"address"`
	User    []byte `cbor:"2,keyasint" json:"owner"`
	Issuer  []byte `cbor:"3,keyasint" json:"owner"`
}
