package v1alpha

import (
	entity "miren.dev/runtime/pkg/entity"
	schema "miren.dev/runtime/pkg/entity/schema"
)

var (
	KindNode = entity.MustKeyword("dev.miren.node/Node")
	Schema   = entity.Id("schema.dev.miren.node/v1alpha")
)

const (
	StatusId          = entity.Id("dev.miren.node/status")
	StatusUnknownId   = entity.Id("dev.miren.node/status.unknown")
	StatusReadyId     = entity.Id("dev.miren.node/status.ready")
	StatusDisabledId  = entity.Id("dev.miren.node/status.disabled")
	StatusUnhealthyId = entity.Id("dev.miren.node/status.unhealthy")
)

type Node struct {
	ID     entity.Id `json:"id"`
	Status Status    `json:"status,omitempty"`
}

type Status string

const (
	UNKNOWN   Status = "status.unknown"
	READY     Status = "status.ready"
	DISABLED  Status = "status.disabled"
	UNHEALTHY Status = "status.unhealthy"
)

var statusFromId = map[entity.Id]Status{StatusUnknownId: UNKNOWN, StatusReadyId: READY, StatusDisabledId: DISABLED, StatusUnhealthyId: UNHEALTHY}
var statusToId = map[Status]entity.Id{UNKNOWN: StatusUnknownId, READY: StatusReadyId, DISABLED: StatusDisabledId, UNHEALTHY: StatusUnhealthyId}

func (o *Node) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(StatusId); ok && a.Value.Kind() == entity.KindId {
		o.Status = statusFromId[a.Value.Id()]
	}
}

func (o *Node) Encode() (attrs []entity.Attr) {
	return
}

func (o *Node) InitSchema(sb *schema.SchemaBuilder) {
	sb.Singleton("status.unknown")
	sb.Singleton("status.ready")
	sb.Singleton("status.disabled")
	sb.Singleton("status.unhealthy")
	sb.Ref("status", schema.Doc("The status of the node"), schema.Choices(StatusUnknownId, StatusReadyId, StatusDisabledId, StatusUnhealthyId))
}

func init() {
	schema.Register("dev.miren.node", "v1alpha", (&Node{}).InitSchema)
	schema.RegisterEncodedSchema("dev.miren.node", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xfft\xcdA\x0e\x82@\f\x85\xe1\x83\x18\xf5\x06\x18OdJ\xfa`\x1a\x86B\xe8t\x1c\x96\x9eE\x0ejD\\\x98\xe0\xfa\xff\xf2ޓ\x95z(#W\xbdL\xd0J\aF\x9b1\x99\f\xda\xe6+\xc51P\xd3\b\"\xdbcYqc\x89\x92\x1b\xa7y\x04C\xbd\xaf\x85\xfdw\xe0\xf2!ݻ\xde2E\x87-\xadk\xa7\xc3]\xcbq\xd7V[\xc6\x04\xe2\xb9\x1c\xf6\xd1\x1a\x03\x8bQ\x1d\xc1崯\xbe]\\\x03(\xa60\x97\xf3\xbf\xd3\r\xbc\x00\x00\x00\xff\xff"))
}
