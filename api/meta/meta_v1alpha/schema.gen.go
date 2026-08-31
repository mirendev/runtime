package meta_v1alpha

import (
	entity "miren.dev/runtime/pkg/entity"
	schema "miren.dev/runtime/pkg/entity/schema"
)

const (
	CloudExportId = entity.Id("dev.miren.meta/cloud.export")
)

type Cloud struct {
	ID     entity.Id `json:"id"`
	Export bool      `cbor:"export,omitempty" json:"export,omitempty"`
}

func (o *Cloud) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(CloudExportId); ok && a.Value.Kind() == entity.KindBool {
		o.Export = a.Value.Bool()
	}
}

func (o *Cloud) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindCloud)
}

func (o *Cloud) ShortKind() string {
	return "cloud"
}

func (o *Cloud) Kind() entity.Id {
	return KindCloud
}

func (o *Cloud) EntityId() entity.Id {
	return o.ID
}

func (o *Cloud) Encode() (attrs []entity.Attr) {
	attrs = append(attrs, entity.Bool(CloudExportId, o.Export))
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindCloud))
	return
}

func (o *Cloud) Empty() bool {
	return entity.Empty(o.Export)
}

func (o *Cloud) InitSchema(sb *schema.SchemaBuilder) {
	sb.Bool("export", "dev.miren.meta/cloud.export", schema.Doc("Marks an entity as eligible for the generated cloud export contract"), schema.Indexed)
}

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
	KindCloud   = entity.Id("dev.miren.meta/kind.cloud")
	KindLeased  = entity.Id("dev.miren.meta/kind.leased")
	KindSession = entity.Id("dev.miren.meta/kind.session")
	Schema      = entity.Id("dev.miren.meta/schema.v1alpha")
)

func init() {
	schema.Register("dev.miren.meta", "v1alpha", func(sb *schema.SchemaBuilder) {
		(&Cloud{}).InitSchema(sb)
		(&Leased{}).InitSchema(sb)
		(&Session{}).InitSchema(sb)
	})
	schema.RegisterEncodedSchema("dev.miren.meta", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\x9c\x92MN\xc40\f\x85\xcf\xc1ς\x13\x04q\xa2*\xd38\x193\xf9)\x89[u\x96p\x95A\x1c\x915\x8a\x93\x8a!\xb4\x15b\x97\xc4\xf9\x9e߳\xfc\xae\xbct\xe0\x15L\xc2a\x04/\x1c\x90\x84\x13z\x95.\xf3\xcd\xcf\xe7\xc7\xfc,z\x1bF\xf5\xc1\x185u.\x15\xf8S\xab\xe0$\xfaFYk\x04\xab\xd2\xeb\xe5\x80j\xbe[\xc3\x05\xccC\x88\xc4\xfa\xba\x9e\xe9<\x80:\x84`\xcd\x041a\xf0fz\x92v8J;Dt2\x9e\xbb\xdc\x13\x98\x9fo\xd7L[\x90\t\xaa\xeb\xb1\xf9Pj\x7f\xb1\xfdƶ\x1fVy\x91 ek\x1d*\xee\xf2|u\xcf\xfeu\xa2\x88ްB;ת@d\x19\xed\xf3!3=zڍ\xac\v\xf9k\x92\x9c\xb9\x1a(\xa1\xa7\xe6G-\xfe?u\x15\x10\xa3Ǘ\x11\x96\xd4\xf8}mC\xdfo\t$i\x80a(\xc7+p7\xbc\xa9\n\xed\xa7S:\x86H]\xd9\xe1\xba\x14ۛ\xbc\x8cpgm\x96F\xbbc\xfe\x02\x00\x00\xff\xff\x01\x00\x00\xff\xff\x9e8\x17\x82I\x03\x00\x00"))
}
