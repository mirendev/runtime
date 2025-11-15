package ingress_v1alpha

import (
	entity "miren.dev/runtime/pkg/entity"
	schema "miren.dev/runtime/pkg/entity/schema"
)

const (
	HttpRouteAppId     = entity.Id("dev.miren.ingress/http_route.app")
	HttpRouteDefaultId = entity.Id("dev.miren.ingress/http_route.default")
	HttpRouteHostId    = entity.Id("dev.miren.ingress/http_route.host")
)

type HttpRoute struct {
	ID      entity.Id `json:"id"`
	App     entity.Id `cbor:"app,omitempty" json:"app,omitempty"`
	Default bool      `cbor:"default,omitempty" json:"default,omitempty"`
	Host    string    `cbor:"host,omitempty" json:"host,omitempty"`
}

func (o *HttpRoute) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(HttpRouteAppId); ok && a.Value.Kind() == entity.KindId {
		o.App = a.Value.Id()
	}
	if a, ok := e.Get(HttpRouteDefaultId); ok && a.Value.Kind() == entity.KindBool {
		o.Default = a.Value.Bool()
	}
	if a, ok := e.Get(HttpRouteHostId); ok && a.Value.Kind() == entity.KindString {
		o.Host = a.Value.String()
	}
}

func (o *HttpRoute) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindHttpRoute)
}

func (o *HttpRoute) ShortKind() string {
	return "http_route"
}

func (o *HttpRoute) Kind() entity.Id {
	return KindHttpRoute
}

func (o *HttpRoute) EntityId() entity.Id {
	return o.ID
}

func (o *HttpRoute) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.App) {
		attrs = append(attrs, entity.Ref(HttpRouteAppId, o.App))
	}
	attrs = append(attrs, entity.Bool(HttpRouteDefaultId, o.Default))
	if !entity.Empty(o.Host) {
		attrs = append(attrs, entity.String(HttpRouteHostId, o.Host))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindHttpRoute))
	return
}

func (o *HttpRoute) Empty() bool {
	if !entity.Empty(o.App) {
		return false
	}
	if !entity.Empty(o.Default) {
		return false
	}
	if !entity.Empty(o.Host) {
		return false
	}
	return true
}

func (o *HttpRoute) InitSchema(sb *schema.SchemaBuilder) {
	sb.Ref("app", "dev.miren.ingress/http_route.app", schema.Doc("The application to route to"), schema.Indexed, schema.Tags("dev.miren.app_ref"))
	sb.Bool("default", "dev.miren.ingress/http_route.default", schema.Doc("Whether this is the default route for routing"), schema.Indexed)
	sb.String("host", "dev.miren.ingress/http_route.host", schema.Doc("The hostname to match on for the application"), schema.Indexed)
}

var (
	KindHttpRoute = entity.Id("dev.miren.ingress/kind.http_route")
	Schema        = entity.Id("dev.miren.ingress/schema.v1alpha")
)

func init() {
	schema.Register("dev.miren.ingress", "v1alpha", func(sb *schema.SchemaBuilder) {
		(&HttpRoute{}).InitSchema(sb)
	})
	schema.RegisterEncodedSchema("dev.miren.ingress", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\x8c\x90MN\xc40\fF/\xc2\x02$\xd6A\x9c\xa8J\xb1\x93\x98\xe6\x0f;\xad\xda5'A \x8e\xc8\x1a%ը\xd5tT\xcdΖ\xbf\xf7,\xfb\a\xa2\x0e\xf8\x018\xa9@\x8cQQ\xb4\x8c\"8P\x04\xf9\x9a\x9f\x0e\x93\x97:Q\xae\x94\xdcq\x1a\v\xfe6\xc3\xfcp\fn\x99\xd5\xf6g \x05M\xf1\xb8\xcd\x18B\x0f\xf2\xf9\xdd\x13̏g&\xa5sn\v\xdfjQ\x96\x8c=AÞO1@\xa3G_\x1aj/MšO\xc97\xc1\x8dSw\x02\x97d\xa5\xa1U\x155R\x98\xa2\xb5\x13\xb2P\x8avz\xd5>;\xed3Sмt\xf5\xe8\xf7Mq\x9d\x1b\xc4%.\xdd\xfa\xe8]\ue39f\xff\x03\x00\x00\xff\xff\x01\x00\x00\xff\xff\xad\xbe\xa7\xab\xb6\x01\x00\x00"))
}
