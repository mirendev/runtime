package actor_v1alpha

import (
	entity "miren.dev/runtime/pkg/entity"
	schema "miren.dev/runtime/pkg/entity/schema"
)

const (
	ActorStateId = entity.Id("dev.miren.actor/actor.state")
)

type Actor struct {
	ID    entity.Id `json:"id"`
	State []byte    `cbor:"state,omitempty" json:"state,omitempty"`
}

func (o *Actor) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
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
	if len(o.State) == 0 {
		attrs = append(attrs, entity.Bytes(ActorStateId, o.State))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindActor))
	return
}

func (o *Actor) Empty() bool {
	if len(o.State) == 0 {
		return false
	}
	return true
}

func (o *Actor) InitSchema(sb *schema.SchemaBuilder) {
	sb.Bytes("state", "dev.miren.actor/actor.state", schema.Doc("The state of an actor"))
}

const (
	ActorLeaseActorId = entity.Id("dev.miren.actor/actor_lease.actor")
	ActorLeaseNodeId  = entity.Id("dev.miren.actor/actor_lease.node")
)

type ActorLease struct {
	ID    entity.Id `json:"id"`
	Actor entity.Id `cbor:"actor,omitempty" json:"actor,omitempty"`
	Node  entity.Id `cbor:"node,omitempty" json:"node,omitempty"`
}

func (o *ActorLease) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(ActorLeaseActorId); ok && a.Value.Kind() == entity.KindId {
		o.Actor = a.Value.Id()
	}
	if a, ok := e.Get(ActorLeaseNodeId); ok && a.Value.Kind() == entity.KindId {
		o.Node = a.Value.Id()
	}
}

func (o *ActorLease) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindActorLease)
}

func (o *ActorLease) ShortKind() string {
	return "actor_lease"
}

func (o *ActorLease) Kind() entity.Id {
	return KindActorLease
}

func (o *ActorLease) EntityId() entity.Id {
	return o.ID
}

func (o *ActorLease) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Actor) {
		attrs = append(attrs, entity.Ref(ActorLeaseActorId, o.Actor))
	}
	if !entity.Empty(o.Node) {
		attrs = append(attrs, entity.Ref(ActorLeaseNodeId, o.Node))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindActorLease))
	return
}

func (o *ActorLease) Empty() bool {
	if !entity.Empty(o.Actor) {
		return false
	}
	if !entity.Empty(o.Node) {
		return false
	}
	return true
}

func (o *ActorLease) InitSchema(sb *schema.SchemaBuilder) {
	sb.Ref("actor", "dev.miren.actor/actor_lease.actor", schema.Doc("The actor that this lease is for"))
	sb.Ref("node", "dev.miren.actor/actor_lease.node", schema.Doc("The node that the actor is running on"))
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
	KindActor      = entity.Id("dev.miren.actor/kind.actor")
	KindActorLease = entity.Id("dev.miren.actor/kind.actor_lease")
	KindNode       = entity.Id("dev.miren.actor/kind.node")
	Schema         = entity.Id("dev.miren.actor/schema.v1alpha")
)

func init() {
	schema.Register("dev.miren.actor", "v1alpha", func(sb *schema.SchemaBuilder) {
		(&Actor{}).InitSchema(sb)
		(&ActorLease{}).InitSchema(sb)
		(&Node{}).InitSchema(sb)
	})
	schema.RegisterEncodedSchema("dev.miren.actor", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\x84\x92aN\x840\x10\x85ϡ\xc6D/P\xe3\x89\xc8\xe0\f0\x81NI;\x12\xf8\xa9WY\xe3\r\xf5\xb7i\xbbٰ\b\xdd\x7f\xa4}\xefu\xbe\xc7|\xa1\x80%\x874\x19˞\xc4\xc0\x9b:\xdfN\xe4\x03;i\xa7W\x18\xc6\x0e\xa8g\xc1p\x9a\xef6\xba\x97xn\xc4!}7\xe8,\xb0l\x93R\xbcnmQ\xb6\xff\xc6o\xd30\r\x18>\xf2`\x1d\t\x8e\x8eEQ\x97\x91\x9a\xa0\x9e\xa5\xad\x19\xe7ǽHsQ[\x90\xe5g\x18=[\xf0K\x15\x931\n\xe6\xfb]\x80\xf4Y$x\xdf\xfa\n5]\x10N\xc9JAA)\xcdO\xf5\xa2\x14\xe2\xf8\x0f\xbby&I\xafƦt1?\x1d\xcf]\r\x04\xa1\xdc\xff\xc1s\xd9Y\x86\xf8<C\xe4\xb0\bQ3F\x82\xe7Bd>\xc9\xce\xd4\xfb\xda\xf8\x0fem\x8c\xe2+\xfe~uۇ\xcey\xad\xf2.\x9e\x8b)\xfcе\xf5v\x81y?\x8e\x17\xfc\x0f\x00\x00\xff\xff"))
}
