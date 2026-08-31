package network_v1alpha

import (
	entity "miren.dev/runtime/pkg/entity"
	schema "miren.dev/runtime/pkg/entity/schema"
	types "miren.dev/runtime/pkg/entity/types"
)

type PortProtocol string

const (
	PortProtocolTcp PortProtocol = "tcp"
	PortProtocolUdp PortProtocol = "udp"
)

const (
	EndpointsEndpointId = entity.Id("dev.miren.network/endpoints.endpoint")
	EndpointsServiceId  = entity.Id("dev.miren.network/endpoints.service")
)

type Endpoints struct {
	ID       entity.Id  `json:"id"`
	Endpoint []Endpoint `cbor:"endpoint,omitempty" json:"endpoint,omitempty"`
	Service  entity.Id  `cbor:"service,omitempty" json:"service,omitempty"`
}

func (o *Endpoints) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	for _, a := range e.GetAll(EndpointsEndpointId) {
		if a.Value.Kind() == entity.KindComponent {
			var v Endpoint
			v.Decode(a.Value.Component())
			o.Endpoint = append(o.Endpoint, v)
		}
	}
	if a, ok := e.Get(EndpointsServiceId); ok && a.Value.Kind() == entity.KindId {
		o.Service = a.Value.Id()
	}
}

func (o *Endpoints) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindEndpoints)
}

func (o *Endpoints) ShortKind() string {
	return "endpoints"
}

func (o *Endpoints) Kind() entity.Id {
	return KindEndpoints
}

func (o *Endpoints) EntityId() entity.Id {
	return o.ID
}

func (o *Endpoints) Encode() (attrs []entity.Attr) {
	for _, v := range o.Endpoint {
		attrs = append(attrs, entity.Component(EndpointsEndpointId, v.Encode()))
	}
	if !entity.Empty(o.Service) {
		attrs = append(attrs, entity.Ref(EndpointsServiceId, o.Service))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindEndpoints))
	return
}

func (o *Endpoints) Empty() bool {
	if len(o.Endpoint) != 0 {
		return false
	}
	if !entity.Empty(o.Service) {
		return false
	}
	return true
}

func (o *Endpoints) InitSchema(sb *schema.SchemaBuilder) {
	sb.Component("endpoint", "dev.miren.network/endpoints.endpoint", schema.Doc("The endpoint configuration, per endpoint"), schema.Many)
	(&Endpoint{}).InitSchema(sb.Builder("endpoints.endpoint"))
	sb.Ref("service", "dev.miren.network/endpoints.service", schema.Doc("The service that uses these endpoints"), schema.Indexed)
}

const (
	EndpointIpId   = entity.Id("dev.miren.network/endpoint.ip")
	EndpointPortId = entity.Id("dev.miren.network/endpoint.port")
)

type Endpoint struct {
	Ip   string `cbor:"ip,omitempty" json:"ip,omitempty"`
	Port int64  `cbor:"port,omitempty" json:"port,omitempty"`
}

func (o *Endpoint) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(EndpointIpId); ok && a.Value.Kind() == entity.KindString {
		o.Ip = a.Value.String()
	}
	if a, ok := e.Get(EndpointPortId); ok && a.Value.Kind() == entity.KindInt64 {
		o.Port = a.Value.Int64()
	}
}

func (o *Endpoint) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Ip) {
		attrs = append(attrs, entity.String(EndpointIpId, o.Ip))
	}
	if !entity.Empty(o.Port) {
		attrs = append(attrs, entity.Int64(EndpointPortId, o.Port))
	}
	return
}

func (o *Endpoint) Empty() bool {
	if !entity.Empty(o.Ip) {
		return false
	}
	if !entity.Empty(o.Port) {
		return false
	}
	return true
}

func (o *Endpoint) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("ip", "dev.miren.network/endpoint.ip", schema.Doc("The IP of the endpoint"))
	sb.Int64("port", "dev.miren.network/endpoint.port", schema.Doc("The port number"))
}

const (
	ServiceIpId    = entity.Id("dev.miren.network/service.ip")
	ServiceMatchId = entity.Id("dev.miren.network/service.match")
	ServicePortId  = entity.Id("dev.miren.network/service.port")
)

type Service struct {
	ID    entity.Id    `json:"id"`
	Ip    []string     `cbor:"ip,omitempty" json:"ip,omitempty"`
	Match types.Labels `cbor:"match,omitempty" json:"match,omitempty"`
	Port  []Port       `cbor:"port,omitempty" json:"port,omitempty"`
}

func (o *Service) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	for _, a := range e.GetAll(ServiceIpId) {
		if a.Value.Kind() == entity.KindString {
			o.Ip = append(o.Ip, a.Value.String())
		}
	}
	for _, a := range e.GetAll(ServiceMatchId) {
		if a.Value.Kind() == entity.KindLabel {
			o.Match = append(o.Match, a.Value.Label())
		}
	}
	for _, a := range e.GetAll(ServicePortId) {
		if a.Value.Kind() == entity.KindComponent {
			var v Port
			v.Decode(a.Value.Component())
			o.Port = append(o.Port, v)
		}
	}
}

func (o *Service) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindService)
}

func (o *Service) ShortKind() string {
	return "service"
}

func (o *Service) Kind() entity.Id {
	return KindService
}

func (o *Service) EntityId() entity.Id {
	return o.ID
}

func (o *Service) Encode() (attrs []entity.Attr) {
	for _, v := range o.Ip {
		attrs = append(attrs, entity.String(ServiceIpId, v))
	}
	for _, v := range o.Match {
		attrs = append(attrs, entity.Label(ServiceMatchId, v.Key, v.Value))
	}
	for _, v := range o.Port {
		attrs = append(attrs, entity.Component(ServicePortId, v.Encode()))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindService))
	return
}

func (o *Service) Empty() bool {
	if len(o.Ip) != 0 {
		return false
	}
	if len(o.Match) != 0 {
		return false
	}
	if len(o.Port) != 0 {
		return false
	}
	return true
}

func (o *Service) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("ip", "dev.miren.network/service.ip", schema.Doc("The IP allocated to the service"), schema.Many)
	sb.Label("match", "dev.miren.network/service.match", schema.Doc("A label to match against a sandbox"), schema.Many)
	sb.Component("port", "dev.miren.network/service.port", schema.Doc("A network port the service exposes"), schema.Many)
	(&Port{}).InitSchema(sb.Builder("service.port"))
}

const (
	PortNameId        = entity.Id("dev.miren.network/port.name")
	PortNodePortId    = entity.Id("dev.miren.network/port.node_port")
	PortPortId        = entity.Id("dev.miren.network/port.port")
	PortProtocolId    = entity.Id("dev.miren.network/port.protocol")
	PortProtocolTcpId = entity.Id("dev.miren.network/protocol.tcp")
	PortProtocolUdpId = entity.Id("dev.miren.network/protocol.udp")
	PortTargetPortId  = entity.Id("dev.miren.network/port.target_port")
	PortTypeId        = entity.Id("dev.miren.network/port.type")
)

type Port struct {
	Name       string       `cbor:"name" json:"name"`
	NodePort   int64        `cbor:"node_port,omitempty" json:"node_port,omitempty"`
	Port       int64        `cbor:"port" json:"port"`
	Protocol   PortProtocol `cbor:"protocol,omitempty" json:"protocol,omitempty"`
	TargetPort int64        `cbor:"target_port,omitempty" json:"target_port,omitempty"`
	Type       string       `cbor:"type,omitempty" json:"type,omitempty"`
}

const (
	TCP PortProtocol = PortProtocolTcp
	UDP PortProtocol = PortProtocolUdp
)

var PortProtocolFromId = map[entity.Id]PortProtocol{PortProtocolTcpId: PortProtocolTcp, PortProtocolUdpId: PortProtocolUdp}
var PortProtocolToId = map[PortProtocol]entity.Id{PortProtocolTcp: PortProtocolTcpId, PortProtocolUdp: PortProtocolUdpId}

func (o *Port) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(PortNameId); ok && a.Value.Kind() == entity.KindString {
		o.Name = a.Value.String()
	}
	if a, ok := e.Get(PortNodePortId); ok && a.Value.Kind() == entity.KindInt64 {
		o.NodePort = a.Value.Int64()
	}
	if a, ok := e.Get(PortPortId); ok && a.Value.Kind() == entity.KindInt64 {
		o.Port = a.Value.Int64()
	}
	if a, ok := e.Get(PortProtocolId); ok && a.Value.Kind() == entity.KindId {
		o.Protocol = PortProtocolFromId[a.Value.Id()]
	}
	if a, ok := e.Get(PortTargetPortId); ok && a.Value.Kind() == entity.KindInt64 {
		o.TargetPort = a.Value.Int64()
	}
	if a, ok := e.Get(PortTypeId); ok && a.Value.Kind() == entity.KindString {
		o.Type = a.Value.String()
	}
}

func (o *Port) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Name) {
		attrs = append(attrs, entity.String(PortNameId, o.Name))
	}
	if !entity.Empty(o.NodePort) {
		attrs = append(attrs, entity.Int64(PortNodePortId, o.NodePort))
	}
	attrs = append(attrs, entity.Int64(PortPortId, o.Port))
	if a, ok := PortProtocolToId[o.Protocol]; ok {
		attrs = append(attrs, entity.Ref(PortProtocolId, a))
	}
	if !entity.Empty(o.TargetPort) {
		attrs = append(attrs, entity.Int64(PortTargetPortId, o.TargetPort))
	}
	if !entity.Empty(o.Type) {
		attrs = append(attrs, entity.String(PortTypeId, o.Type))
	}
	return
}

func (o *Port) Empty() bool {
	if !entity.Empty(o.Name) {
		return false
	}
	if !entity.Empty(o.NodePort) {
		return false
	}
	if !entity.Empty(o.Port) {
		return false
	}
	if o.Protocol != "" {
		return false
	}
	if !entity.Empty(o.TargetPort) {
		return false
	}
	if !entity.Empty(o.Type) {
		return false
	}
	return true
}

func (o *Port) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("name", "dev.miren.network/port.name", schema.Doc("Name of the port for reference"), schema.Required)
	sb.Int64("node_port", "dev.miren.network/port.node_port", schema.Doc("The port number that should be forwarded from the node to the container"))
	sb.Int64("port", "dev.miren.network/port.port", schema.Doc("Port number to listen on"), schema.Required)
	sb.Singleton("dev.miren.network/protocol.tcp")
	sb.Singleton("dev.miren.network/protocol.udp")
	sb.Ref("protocol", "dev.miren.network/port.protocol", schema.Doc("Port protocol"), schema.Choices(PortProtocolTcpId, PortProtocolUdpId))
	sb.Int64("target_port", "dev.miren.network/port.target_port", schema.Doc("Port number to target on the pod side"))
	sb.String("type", "dev.miren.network/port.type", schema.Doc("The highlevel type of the port"))
}

var (
	KindEndpoints = entity.Id("dev.miren.network/kind.endpoints")
	KindService   = entity.Id("dev.miren.network/kind.service")
	Schema        = entity.Id("dev.miren.network/schema.v1alpha")
)

func init() {
	schema.Register("dev.miren.network", "v1alpha", func(sb *schema.SchemaBuilder) {
		(&Endpoints{}).InitSchema(sb)
		(&Service{}).InitSchema(sb)
	})
	schema.RegisterEncodedSchema("dev.miren.network", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\x8cUݮ\xd30\f~\x0eį\x80\xeb\"\x9ehJc\xb7\xb5\xda8!\xc9\xcav{\x90x\x90s\x0e\xf0\x84p\x8d\xe2ui\xa1M\xb7\x9bju\xfd}\x9f\xe3\xcfΞ\x81\x95\xc1/\x80ce\xc8#W\x8c\xf1\xab\xf5=\xf6\xc4\x10\x1eOoV_>\xa5/U@?\x92Ɵ\x02?\xbdXgM\t\x17\x9e?\rX\xa3\x88\xd7:MC8@\xf8\xf6\\\x13\x9c^\x15i*r`\x14\x9f\x7f\x8b^M\x0e\xe2\xd9a\x13\xa2'n\x05\xfb\xb6\x8c5*\xean\x01\xc7K 1\xe0\xa0j\x1c~$\x82\x8d\x93^\t\x9c\xf5q\x81\ayOp\xd2\xd68\xcb\xc8q\xfe5\xb5dMW-\xe9\xee\xec\xcb\xf7\xa7T\xda\xcbui\x89\xa3\x92b\xe6Ǣ%\x02{W\x82Y\xc0\x83\x9c!\xc1h~M\x04\x9a8\xee\x8af \xfc\x83\xf9U0\xe1\x82\xf16Zm\a@>\x9a\xd3\xc7uV\x8a\v\xf3!\xa7&\x89.\xbf%\x19A\xf7\xe9q\x18\xd5p\xc4\xf0\xa8\xa3v[\xc6]aU\xd4N\x1fa?\xe7\bn\x10R\x83\xa6F\x1f\x1e\xf4\x842\x12E\xd6\x16\x88[\xed\xb1\x91Ƽ/\x1c2*\xdfb\x9c\x1b\xdb/\x03w\xb5V\x0e\x99\xad\\\xfaَ\xe8\x03Yn\xc7\xcfjp\x9d\x1a\x9c'\xa3\xfc\xf9\x90\xc6H\x8c\xd8\xcdh\xa7\xd1\xdb\x1a\tYgdp\x968\x86iz7*\xcc)w\x8e\xee\x83lզ\xd7\x13QV]\xecV\x97c7\xf6kM\\\xad\x89\xef-U\\y].5]?\x1b\x17\xcfSa\xe63\xac\xb0+\xbb^\xe5\x0e\b\xfb\x87\xbd\xfeM\xae\x8a\xc2\xd5b\x11\xa9\tv5(s\xfc\x9fև.m\xe1\xe5\xf6\xcfcs\xe3_`\xa6\xbb=`\x7f\x01\x00\x00\xff\xff\x01\x00\x00\xff\xffVB\x11\aq\x06\x00\x00"))
}
