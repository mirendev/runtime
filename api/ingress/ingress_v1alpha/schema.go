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
	sb.Ref("app", "dev.miren.ingress/http_route.app", schema.Doc("The application to route to"))
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
	schema.RegisterEncodedSchema("dev.miren.ingress", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\x8c\xd0\xcfJ\x041\f\x06\xf0\x17\xf1\xa0\xe0\xb9\xe2\x13\r\x19\x93\xb6q\xfb'&\xdda\xf6쓈\xe2#z\x96N\x85]\x98e\xf1\xda\xf4\xf7\x85|_X \xd3\x1b\xd2\xe22+\x15\xc7%(\x99\x85\x85Ը\x96\xb0<C\x92\bt\xe0\x82\xf6\xb1>\xec~>\xf5\x89\x8b\xadɤ\xf5\xd8\xe8\xdbc\xcd\xc0e\x9f\xb9\xadZ\xef\xf6\tg|}\xed\x8f\xf7L\t\xed\xfds\x8bx\x01\x11l'\xa1\x99qf\\\xefo%:\x10\x19, y8\xa6\xb6Q\x9ckM\x1d?\xde\xc4\x7fd\x04`\xac6\xb4\xb7\xa6\\B\xf7W\n\xb9\xf0]$QΠ\xa7\xa9\xdf\xf2z\x1e\x1e,Vm\xd3(\xf6\xe2\xfd\x1f\x1d\xff\x02\x00\x00\xff\xff\x01\x00\x00\xff\xffÃW]\xb6\x01\x00\x00"))
}
