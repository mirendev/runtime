package actor_v1alpha

import (
	entity "miren.dev/runtime/pkg/entity"
	schema "miren.dev/runtime/pkg/entity/schema"
)

const (
	ActorNodeId  = entity.Id("dev.miren.actor/actor.node")
	ActorStateId = entity.Id("dev.miren.actor/actor.state")
)

type Actor struct {
	ID    entity.Id `json:"id"`
	Node  entity.Id `cbor:"node,omitempty" json:"node,omitempty"`
	State []byte    `cbor:"state,omitempty" json:"state,omitempty"`
}

func (o *Actor) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(ActorNodeId); ok && a.Value.Kind() == entity.KindId {
		o.Node = a.Value.Id()
	}
	if a, ok := e.Get(ActorStateId); ok && a.Value.Kind() == entity.KindBytes {
		o.State = a.Value.Bytes()
	}
}

func (o *Actor) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindActor)
}

func (o *Actor) ShortKind() string {
	return "actor"
}

func (o *Actor) Kind() entity.Id {
	return KindActor
}

func (o *Actor) EntityId() entity.Id {
	return o.ID
}

func (o *Actor) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Node) {
		attrs = append(attrs, entity.Ref(ActorNodeId, o.Node))
	}
	if len(o.State) == 0 {
		attrs = append(attrs, entity.Bytes(ActorStateId, o.State))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindActor))
	return
}

func (o *Actor) Empty() bool {
	if !entity.Empty(o.Node) {
		return false
	}
	if len(o.State) == 0 {
		return false
	}
	return true
}

func (o *Actor) InitSchema(sb *schema.SchemaBuilder) {
	sb.Ref("node", "dev.miren.actor/actor.node", schema.Doc("The node that is serving the actor"))
	sb.Bytes("state", "dev.miren.actor/actor.state", schema.Doc("The state of an actor"))
}

const (
	NodeEndpointId = entity.Id("dev.miren.actor/node.endpoint")
)

type Node struct {
	ID       entity.Id `json:"id"`
	Endpoint []string  `cbor:"endpoint,omitempty" json:"endpoint,omitempty"`
}

func (o *Node) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	for _, a := range e.GetAll(NodeEndpointId) {
		if a.Value.Kind() == entity.KindString {
			o.Endpoint = append(o.Endpoint, a.Value.String())
		}
	}
}

func (o *Node) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindNode)
}

func (o *Node) ShortKind() string {
	return "node"
}

func (o *Node) Kind() entity.Id {
	return KindNode
}

func (o *Node) EntityId() entity.Id {
	return o.ID
}

func (o *Node) Encode() (attrs []entity.Attr) {
	for _, v := range o.Endpoint {
		attrs = append(attrs, entity.String(NodeEndpointId, v))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindNode))
	return
}

func (o *Node) Empty() bool {
	if len(o.Endpoint) != 0 {
		return false
	}
	return true
}

func (o *Node) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("endpoint", "dev.miren.actor/node.endpoint", schema.Doc("The address to dial for the node"), schema.Many)
}

var (
	KindActor = entity.Id("dev.miren.actor/kind.actor")
	KindNode  = entity.Id("dev.miren.actor/kind.node")
	Schema    = entity.Id("dev.miren.actor/schema.v1alpha")
)

func init() {
	schema.Register("dev.miren.actor", "v1alpha", func(sb *schema.SchemaBuilder) {
		(&Actor{}).InitSchema(sb)
		(&Node{}).InitSchema(sb)
	})
	schema.RegisterEncodedSchema("dev.miren.actor", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff|\x91QN\xc40\fD\xcf\x01\x88+\x14q\xa2\x95\x8b\xdd\xd6\xdaƩ\x12Sm>\xe1(\x80\xb8!|#;\b\x89l\x95\x9f\xaaJ\xe6\x8d=\x99\x0f\x14\b\x14\x91\xf6!p\"\x19\xe0Ic\x9awJ\x99\xa3\xcc\xfb#\xac\xdb\x02tf\xc1\xfcv\xb9mt\x0fv^\x7f?'\x8c\x01XZ+\xf7\x7fn\xb9Δ\xefibZ1\xbf\xbe;\x8a\xe6\x88Z6\x1a\x19G\xc6\xeb\x1d\xfc;\x98\xac\x12\x94\x15\xb4\"4\x16\xa5l\xd4\xdd1\xe5\xd2uK\x1c \x95\x93\xcd'\xbf\xb8\xdc\x1c&\xb5!ݠ\xdab&\xeb\xe7|\xa9\x15,$\xb8E\x16\xf5ŧ\xac\x89e\xb6\xcd\xef\x8f,\x87?u\x00)_\xff\x12\xf8\x8b\x9d\xf3\x12\x93\x9ejo\xbf\x99:\xed9\xd3\t\xfd\x03\x00\x00\xff\xff"))
}
