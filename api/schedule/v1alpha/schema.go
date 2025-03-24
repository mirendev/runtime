package v1alpha

import (
	entity "miren.dev/runtime/pkg/entity"
	schema "miren.dev/runtime/pkg/entity/schema"
	types "miren.dev/runtime/pkg/entity/types"
)

const (
	KeyId         = entity.Id("dev.miren.schedule/key")
	SchedulableId = entity.Id("dev.miren.schedule/schedulable")
)

type Schedule struct {
	ID          entity.Id `json:"id"`
	Key         Key       `json:"key,omitempty"`
	Schedulable bool      `json:"schedulable,omitempty"`
}

func (o *Schedule) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(KeyId); ok && a.Value.Kind() == entity.KindComponent {
		o.Key.Decode(a.Value.Component())
	}
	if a, ok := e.Get(SchedulableId); ok && a.Value.Kind() == entity.KindBool {
		o.Schedulable = a.Value.Bool()
	}
}

func (o *Schedule) Encode() (attrs []entity.Attr) {
	attrs = append(attrs, entity.Component(KeyId, o.Key.Encode()))
	attrs = append(attrs, entity.Bool(SchedulableId, o.Schedulable))
	return
}

func (o *Schedule) InitSchema(sb *schema.SchemaBuilder) {
	sb.Component("key", schema.Doc("The scheduling key for an entity"), schema.Indexed)
	(&Key{}).InitSchema(sb.Builder("key"))
	sb.Bool("schedulable", schema.Doc("Indicates that the entity should be considered for scheduling"))
}

const (
	KeyKindId = entity.Id("dev.miren.schedule.key/kind")
	KeyNodeId = entity.Id("dev.miren.schedule.key/node")
)

type Key struct {
	Kind types.Keyword `json:"kind,omitempty"`
	Node string        `json:"node,omitempty"`
}

func (o *Key) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(KeyKindId); ok && a.Value.Kind() == entity.KindKeyword {
		o.Kind = a.Value.Keyword()
	}
	if a, ok := e.Get(KeyNodeId); ok && a.Value.Kind() == entity.KindString {
		o.Node = a.Value.String()
	}
}

func (o *Key) Encode() (attrs []entity.Attr) {
	attrs = append(attrs, entity.Keyword(KeyKindId, o.Kind))
	attrs = append(attrs, entity.String(KeyNodeId, o.Node))
	return
}

func (o *Key) InitSchema(sb *schema.SchemaBuilder) {
	sb.Keyword("kind", schema.Doc("The type of entity this is"))
	sb.String("node", schema.Doc("The node id the entity is scheduled for"))
}

func init() {
	schema.Register("dev.miren.schedule", "v1alpha", (&Schedule{}).InitSchema)
}
