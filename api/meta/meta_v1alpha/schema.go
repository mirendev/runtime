package meta_v1alpha

import (
	entity "miren.dev/runtime/pkg/entity"
	schema "miren.dev/runtime/pkg/entity/schema"
)

const (
	LeasedSessionIdId = entity.Id("db/attr.session")
	LeasedTtlId       = entity.Id("db/entity.ttl")
)

type Leased struct {
	ID        entity.Id `json:"id"`
	SessionId string    `cbor:"session_id,omitempty" json:"session_id,omitempty"`
	Ttl       int64     `cbor:"ttl,omitempty" json:"ttl,omitempty"`
}

func (o *Leased) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(LeasedSessionIdId); ok && a.Value.Kind() == entity.KindString {
		o.SessionId = a.Value.String()
	}
	if a, ok := e.Get(LeasedTtlId); ok && a.Value.Kind() == entity.KindInt64 {
		o.Ttl = a.Value.Int64()
	}
}

func (o *Leased) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindLeased)
}

func (o *Leased) ShortKind() string {
	return "leased"
}

func (o *Leased) Kind() entity.Id {
	return KindLeased
}

func (o *Leased) EntityId() entity.Id {
	return o.ID
}

func (o *Leased) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.SessionId) {
		attrs = append(attrs, entity.String(LeasedSessionIdId, o.SessionId))
	}
	if !entity.Empty(o.Ttl) {
		attrs = append(attrs, entity.Int64(LeasedTtlId, o.Ttl))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindLeased))
	return
}

func (o *Leased) Empty() bool {
	if !entity.Empty(o.SessionId) {
		return false
	}
	if !entity.Empty(o.Ttl) {
		return false
	}
	return true
}

func (o *Leased) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("session_id", "dev.miren.meta/leased.session_id", schema.Doc("The unique identifer for the session bound to this entity"))
	sb.Int64("ttl", "dev.miren.meta/leased.ttl", schema.Doc("The time to live left on the value"))
}

const (
	SessionUniqueIdId = entity.Id("dev.miren.meta/session.unique_id")
	SessionUsageId    = entity.Id("dev.miren.meta/session.usage")
)

type Session struct {
	ID       entity.Id `json:"id"`
	UniqueId string    `cbor:"unique_id,omitempty" json:"unique_id,omitempty"`
	Usage    string    `cbor:"usage,omitempty" json:"usage,omitempty"`
}

func (o *Session) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(SessionUniqueIdId); ok && a.Value.Kind() == entity.KindString {
		o.UniqueId = a.Value.String()
	}
	if a, ok := e.Get(SessionUsageId); ok && a.Value.Kind() == entity.KindString {
		o.Usage = a.Value.String()
	}
}

func (o *Session) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindSession)
}

func (o *Session) ShortKind() string {
	return "session"
}

func (o *Session) Kind() entity.Id {
	return KindSession
}

func (o *Session) EntityId() entity.Id {
	return o.ID
}

func (o *Session) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.UniqueId) {
		attrs = append(attrs, entity.String(SessionUniqueIdId, o.UniqueId))
	}
	if !entity.Empty(o.Usage) {
		attrs = append(attrs, entity.String(SessionUsageId, o.Usage))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindSession))
	return
}

func (o *Session) Empty() bool {
	if !entity.Empty(o.UniqueId) {
		return false
	}
	if !entity.Empty(o.Usage) {
		return false
	}
	return true
}

func (o *Session) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("unique_id", "dev.miren.meta/session.unique_id", schema.Doc("The identifier for the session"))
	sb.String("usage", "dev.miren.meta/session.usage", schema.Doc("What the session is being used for"))
}

var (
	KindLeased  = entity.Id("dev.miren.meta/kind.leased")
	KindSession = entity.Id("dev.miren.meta/kind.session")
	Schema      = entity.Id("dev.miren.meta/schema.v1alpha")
)

func init() {
	schema.Register("dev.miren.meta", "v1alpha", func(sb *schema.SchemaBuilder) {
		(&Leased{}).InitSchema(sb)
		(&Session{}).InitSchema(sb)
	})
	schema.RegisterEncodedSchema("dev.miren.meta", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\x8c\x92MN\xc40\fF\xcf\xc1ς\x13\x04q\xa2ʌ\x9d\x8c\x99\xc4-\xb1[u\xb6\x1c\x05\x10Gd\x8d\xd2t@S\x95\xaa[\xc7\xef9\x9f\x93O\x14H$H\x83K\x9cI\\\"\x830PVn%\fO\x10\xbb#Љ\x05\xf5}\xbc\xbdn{,e\x17\t\x94\xf0\xcbc\x9b\x80ea\x9a\xec\xfd\x02\xab\xc4\xfa\x90o\xef\x99\"\xea\xdbǄ\xbe(iij\x18\xd1\xce\x1dy\xb5\xcc\x12\x9e\x19ǇU\xab\xfb\x03\xaa\xe1`\x16'\xf4\xc0b\x85\xbbY\xe7\xccb\xec2'\xc8\xe7\xa6\xdc\xc4\xd7\xfax\xb7\x96y\x1e\xb2\x15zXp3\xb2+5\xf7¯=\xed\b=[\xdd/P\x05\xd4+\x04Z\xc2\xf7\xff\xc1\xa5\xf9*|\x98\x8fNzl\xb35\xf5\xf9/\x1b\xd9\xf8\x05\x17nsk?\x00\x00\x00\xff\xff"))
}
