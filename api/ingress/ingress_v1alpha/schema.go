package ingress_v1alpha

import (
	entity "miren.dev/runtime/pkg/entity"
	schema "miren.dev/runtime/pkg/entity/schema"
)

const (
	HttpRouteAppId  = entity.Id("dev.miren.ingress/http_route.app")
	HttpRouteHostId = entity.Id("dev.miren.ingress/http_route.host")
)

type HttpRoute struct {
	ID   entity.Id `json:"id"`
	App  entity.Id `cbor:"app,omitempty" json:"app,omitempty"`
	Host string    `cbor:"host,omitempty" json:"host,omitempty"`
}

func (o *HttpRoute) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(HttpRouteAppId); ok && a.Value.Kind() == entity.KindId {
		o.App = a.Value.Id()
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
	if !entity.Empty(o.Host) {
		return false
	}
	return true
}

func (o *HttpRoute) InitSchema(sb *schema.SchemaBuilder) {
	sb.Ref("app", "dev.miren.ingress/http_route.app", schema.Doc("The application to route to"))
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
	schema.RegisterEncodedSchema("dev.miren.ingress", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\x8c\x90An\xc3 \x10E/\xd2E{\x01\xaa\x9e\xc8\u009d\x01\xa660\x9d!\x96\xbd\xcdM\xa2D9b\xd6\x11\xf6\u0096lE\xd9\xf2y\xefÿA\xb2\x11\xff\x01\a\x13I0\x19J^P\xd5\x0f(J9\xf9\xe1\xc7\xf6\x1c,v\x94@/\xe3\xd7\xee\xe6wML(\x85\x1bɧ\x82w\a9ZJ{\xe7\\5~\xec\r+|\\\xfbp\x8e\xb0\a=_gůe\x8621\xb6\x04-\xc1\xf8\xf9\xcah,\xf3\x82A\xc8Zf\xcei\x11J\xbe\xb2\a\xffٰ\x95\xe8Y(Z\x99\x9a\xfa\x94\xbf5\xec4d)Ͳ\xcb\xe6\xfc\x8d\x89\x9e\x00\x00\x00\xff\xff"))
}
