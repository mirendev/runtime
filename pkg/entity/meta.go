package entity

import (
	"encoding/json"

	"github.com/fxamacker/cbor/v2"
)

type Meta struct {
	*Entity `json:"entity" cbor:"entity"`

	Revision int64 `json:"version" cbor:"version"`
	Previous int64 `json:"previous" cbor:"previous"`
}

type metaExternal struct {
	Entity   *Entity `json:"entity" cbor:"entity"`
	Revision int64   `json:"version" cbor:"version"`
	Previous int64   `json:"previous" cbor:"previous"`
}

func (m *Meta) MarshalJSON() ([]byte, error) {
	return json.Marshal(metaExternal{
		Entity:   m.Entity,
		Revision: m.Revision,
		Previous: m.Previous,
	})
}

func (m *Meta) UnmarshalJSON(data []byte) error {
	var me metaExternal
	if err := json.Unmarshal(data, &me); err != nil {
		return err
	}
	m.Entity = me.Entity
	m.Revision = me.Revision
	m.Previous = me.Previous
	return nil
}

func (m *Meta) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(metaExternal{
		Entity:   m.Entity,
		Revision: m.Revision,
		Previous: m.Previous,
	})
}

func (m *Meta) UnmarshalCBOR(data []byte) error {
	var me metaExternal
	if err := cbor.Unmarshal(data, &me); err != nil {
		return err
	}
	m.Entity = me.Entity
	m.Revision = me.Revision
	m.Previous = me.Previous
	return nil
}

func (m *Meta) GetRevision() int64 {
	if m.Revision != 0 {
		return m.Revision
	}
	return m.Entity.GetRevision()
}
