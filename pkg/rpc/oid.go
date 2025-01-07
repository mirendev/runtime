package rpc

type Capability struct {
	OID     OID    `cbor:"0,keyasint" json:"oid"`
	Address string `cbor:"1,keyasint" json:"address"`
}

type OID string
