package entity

type Meta struct {
	*Entity `json:"entity" cbor:"entity"`

	Revision int64 `json:"version" cbor:"version"`
	Previous int64 `json:"previous" cbor:"previous"`
}

func (m *Meta) GetRevision() int64 {
	if m.Revision != 0 {
		return m.Revision
	}
	if m.Entity != nil {
		return m.Entity.GetRevision()
	}
	return 0
}
