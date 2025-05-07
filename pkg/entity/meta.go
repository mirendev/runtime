package entity

type Meta struct {
	*Entity `json:"entity" cbor:"entity"`

	Revision int64 `json:"version" cbor:"version"`
	Previous int64 `json:"previous" cbor:"previous"`
}
