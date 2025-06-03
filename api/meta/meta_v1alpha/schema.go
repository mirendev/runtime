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
	schema.RegisterEncodedSchema("dev.miren.meta", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\x8c\x92MN\xc40\fF\xcf\xc1ς\x13\x04q\xa2ʌ\x9d\x8c\x99\xc4-\xb1[u\xb6\x1c\x05\x10Gd\x8d\xd2T\x83\xa6\xeaT\xdd:~\xcf\xf9\x9c|\xa3@\"A\x1a\\\xe2L\xe2\x12\x19\x84\x81\xb2r+ax\x81\xd8\x1d\x81N,\xa8\x9f\xe3\xc3u\xdbs);%-\xbd?\x1e\xdb\x04,\vդ\x1f\x16܌\xac\x8f\xf9\xf5\x9e)\xa2~|M,\xf7\xc2\xef=5\x8ch玼Zf\t\xaf\x8c\xe3Ӻ\xd5]\x80*\xa0^!\xd0\x12~\xbc\x05\x97\xe6\xd8eN\x90\xcfM\xb9P\x98\x8f\xc6\xfb\xb5\xf4\x91@\t\xb7\xc2\xf7\v\xac\x12\xbb\xb2\xbfͣw\x84\xafV\xf7\x0fT\xc3\xc1,N\xe8\x81\xc5\nw\xb7Ιūо\xd6Ozl\xb35\xf5\xf9/\x8b\xd8\xfa\x063\xb8\xb5\xac?\x00\x00\x00\xff\xff"))
}
