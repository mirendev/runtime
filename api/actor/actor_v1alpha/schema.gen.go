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
	schema.RegisterEncodedSchema("dev.miren.actor", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\x84\x91AN\xc40\fE\xcf\x01\x88+\x14q\xa2\x91\x8b\x9d֚Ɖ\x12S5K8\n\x8c\xb8!\xacQ\x1c\t\xa1P:\x9b(\xb2\xfdl\xff\xef\v\nx\nH\xeb\xe09\x91\f\xf0\xa4!љ\x05\xf3\xdbv\xd3\xc5\x1fj|\x90\x80\xf4a\x9c\xf6\xf9\x9aj\xf0\x97\xc3\xe0\x81\xa5o\xed\x1cӂ\xf9\xe522n\xf7{\xfc@\x821\xb0(z\x90\xf2i\x83柘\x96H.kb\x99\xa6\x95R\xe6 \xd3\xfa\bK\x9ca\x89\x89=\xa4r\xaa\v`m\xb5\xdd\xee\n\xb0oS\xf0\xdc\x17\xfc\xd2\x7fE\xc2\xeb{\x95\xf0g\x82\xbd\xe6\x91\r\xb0=l\xeb\x91ш\xbb}\"+hC\xa8}+CcQʇB\xc9\xf0\xbe\xe4\x9c\xe7\x90\xf4\xd4\xeeؼ\xf8\xff\x98\xadőY\xdf\x00\x00\x00\xff\xff\x01\x00\x00\xff\xff\xa8č\xe2'\x02\x00\x00"))
}
