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
	if len(o.State) > 0 {
		attrs = append(attrs, entity.Bytes(ActorStateId, o.State))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindActor))
	return
}

func (o *Actor) Empty() bool {
	if !entity.Empty(o.Node) {
		return false
	}
	if len(o.State) > 0 {
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
	schema.RegisterEncodedSchema("dev.miren.actor", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff|\x91\xd1M\xc60\f\x84\xe7\x00\xc4\nEL\xf4\xcb\xc5nk\xb5q\xa2\xc4T\xcd#\x8c\x02\x88\r\xe1\x19\xc5\x11H\x84З\xaaJ\xee\xbb\xdc\xd9o(\xe0\xc8#\xed\x83\xe3H2\xc0\x83\xfa8\xef\x14\x13{\x99\xf7{\xd8\xc2\x02\xb4\xb2`z9\xae\x1a\xdd]9\x1f\xc4#\xbdO\xe8\x1d\xb0\xb4Nf\xaf-Vd\xfd7>\xa7\x89i\xc3\xf4T\x83-$\x18<\x8b\xa2\xe6@S\xd2\xc82\x8f\x8c\xc7m\xcfr\xf8Q;\x90\xfc\xb1\x85\xc8\x0eb\xbe\x14g,\x82\xe3\xba[\xc0~O\x1b<\xb6\xdcɘ\xbe+<\xbf\x1aj\x0f[\xfc\x91\xb1D\xff\x93\xc1\xbe6\xc5JPRЊИ\x95R\xa1n\xfa\x94I\x7f\x15%\xbbX\xd3\xe2\xa3^\xea\xde\xea\xd1Y\xf9:\x9d\xff\xd7\xfb\x05\x00\x00\xff\xff\x01\x00\x00\xff\xffٺ\xf2p'\x02\x00\x00"))
}
