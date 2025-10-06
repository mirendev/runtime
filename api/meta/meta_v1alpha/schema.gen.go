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
	schema.RegisterEncodedSchema("dev.miren.meta", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\x9c\x92KN\xc40\f\x86\xcf\xc1c\xc1\t\x828Qe\xc6N\xc6L\xe2\x968\xad:[\x8e\x02\x88#\xb2FIZ1D\xa5\x8b\xd9\xe5\xe1\xefK~˟(\x10H\x90&\x138\x92\x98@\t\xe8Ă\xfa>\xdf\xfe=~\xcc\xc7\xc6\x13(\xe1W\xe1Ʀ\xa0\xdeU\xfc\xdbb\x1f\x80\xa5q[\xcb\xe4Q\xdf>\x9e\x19\xe7\x87M\xde(\xa9r/\x1dcy\xe5\xe5b\x9f\xce\x03YM\x91\xc5\x15\xc3Ͷ!%_\xd0C^d\xe6\xc0\x92\xdcD1{\xdc\xf4\x04~8\x82\x1f\"\a\x88\xe7.\xff\xd7Vr\xbe\xdbʼ|\xa0\x86\x9e\x9a\x8a\xe5\xf2\xfaԋ\xc0\x8c¯#\xad\xa9\xf9wۆ\xbe\xffO\xa0\xe0\xa8\xc0T\x97\x17\xe0nx\xb7\x18ڢ\x93\x1e\xfb\x98\xba:\x0ek\x87v\xa6b\xf5\xecv\xf1\a\x00\x00\xff\xff\x01\x00\x00\xff\xffK\x19y\xdfs\x02\x00\x00"))
}
