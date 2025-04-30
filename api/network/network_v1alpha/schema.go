package network_v1alpha

import (
	entity "miren.dev/runtime/pkg/entity"
	schema "miren.dev/runtime/pkg/entity/schema"
	types "miren.dev/runtime/pkg/entity/types"
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
	(&Endpoint{}).InitSchema(sb.Builder("endpoint"))
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
	(&Port{}).InitSchema(sb.Builder("port"))
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

type PortProtocol string

const (
	TCP PortProtocol = "protocol.tcp"
	UDP PortProtocol = "protocol.udp"
)

var PortprotocolFromId = map[entity.Id]PortProtocol{PortProtocolTcpId: TCP, PortProtocolUdpId: UDP}
var PortprotocolToId = map[PortProtocol]entity.Id{TCP: PortProtocolTcpId, UDP: PortProtocolUdpId}

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
		o.Protocol = PortprotocolFromId[a.Value.Id()]
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
	if !entity.Empty(o.Port) {
		attrs = append(attrs, entity.Int64(PortPortId, o.Port))
	}
	if a, ok := PortprotocolToId[o.Protocol]; ok {
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
	if o.Protocol == "" {
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
	schema.RegisterEncodedSchema("dev.miren.network", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\x94\x94K\xb2\x950\x10\x86\xd7a\xf9,u\x1c\xcb\x15Q!\xdd@\x17\xe4a\x12\x903\xd5*\x17r\x1f\xeeP\xc7V\x12\xe0\x9esC\xc0;a\x00\xff\xff\xa5\xf9\xbb;\x0f\xa0\xb8\xc4o\x80\x13\x93dQ1\x85\xfe\xbb\xb6};\xa1u\xa4U;}\xe5\x83\xe98\xf6\xa4\xc0\xdd\xcd\x1f2\xe5\x97\xf0\x85\xa1\x02\xa3Iy\xf7\xbb\x01-9\xa9\x1c\x19O\x9a_\xe7\x80ͻ\x7f\xe8ߦ!\x1c\xc0\xfdx\x8c\x84n\x95\x83\xbf\x18$\xa1\xa5\xd1\n\x95\xaf\t\xe6\xcf\a\xf0\xadD\x90\\]\xfe<\x19\xcf*Ρ,\x87\x9e\x94~\x1fQ5\x99Xt\xe3\xbc%Ն\x8aߖ+fd\x92\r\x8c\xb6\xe9o\x05\xa5\xff|\x7f\xe0\n\xe2\xc1X\x92\xdc^\xaaP\xc5\x16X\xa2\xb5\x0e\xedD\x02#\xb0&\b\xbcOG\xb9-\xfa\x1b&m\x9f\xe7w\x85\x89Xlg\xe9\xbe\xca\xed\x8b\xf38ҟ\x0f\xa5H\xdf\x14\x89\x8cLj~\xf2\xa2\xe4^tю\x03\xafq(D\xbb\xba\x93<\x02\x1e\x9f5\xe6v\fw\"Y\x19\xd1\xf1\xc2\x01\xccq\xec\x1aw\x9cӯe\x86\xe2\xe3YR;\xbb\x18\x80,h\x93\x8d\x94\x06\xac\xb2\xf9۹\x05\x92qU\x97\x06\xb7tbx\xa4\xa6t\xc6j\xaf\x85\x1e\xa2\x0fP\x8d\xb2Жd\\\xd4}\x10V\x13\x1fFtw\xc2\v\xb3ׄU̼0b\x84c\xcd\b\xcb\xfa\xf5\x9e\xdb\x16}\x9e\xc2\xc7BMW\xfa\xfb\xa7\xdc\xff7\xfc \xbb\xd95\xc86z]\xe1\xdeu\xda\xfa*]\xcd\xeb˳\x85\xbcZ\xdd\xd3\xcb\xfc\x1f\x00\x00\x00\xff\xff"))
}
