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
	schema.RegisterEncodedSchema("dev.miren.network", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\x94\x94K\xb2\x950\x10\x86\xd7a\xf9,u\x1c\xcb\x15Q!\xdd@\x17\xe4a\x12\x903\xd5*\x17r\x1f\xeeP\xc7V\x12\xe0\x9esC\xc0;a\xc2\xff\x7f\xe9\xfcݝ\aP\\\xe27\xc0\x89I\xb2\xa8\x98B\xff]۾\x9d\xd0:Ҫ\x9d\xbe\xf2\xc1t\x1c{R\xe0\xee\xe6\x0f\x99\xf2K\xf8\xc3P\x81Ѥ\xbc\xfb݀\x96\x9cT\x8e\x8c'ͯs\xc0\xe6\xdd?\xf4o\xd3\x10\x0e\xe0~<FB\xb7\xca\xc1_\f\x92\xd0\xd2h\x85\xca\xd7\x04\xf3\xe7\x03\xf8V\"H\xae.\x7f\x9e\x8cg\x15\xe7P\x96COJ\xbf\x8f\xa8\x9aL,\xbaqޒjC\xc5o\xcb\x1532\xc9\x06F\xdbt[A\xe9\x9e\xef\x0f\\A<\x18K\x92\xdbK\x15\xaa\xd8\x02K\xb4֡\x9dH`\x04\xd6\x04\x81\xf7\xe9(\xb7E\x7fä\xed\xf7\xfc\xae0\x11\x8b\xed,\xddW\xb9}q\x1eG\xfa\xf3\xa1\x14\xe9\x9b\"\x91\x91I\xcdO^\x94܋.\xdaq\xe05\x0e\x85hWw\x92G\xc0\xe3\xb3\xc6\u070e\xe1N$+#:^8\x809\x8e]\xe3\x8es\xfa\xb5\xccP\xfc<Kjg\x17\x03\x90\x05m\xb2\x91ҀU6\x7f;\xaf@2\xae\xea\xd2\xe0\x96N\f\x9fԔ\xceX\xed\xb5\xd0C\xf4\x01\xaaQ\x16ڒ\x8c\x8b\xba\x0f\xc2j\xe2È\xeeNxa\xf6\x9a\xb0\x8a\x99\x17F\x8cp\xac\x19aY\xbf\xdesۢ\xcfS\xf8X\xa8\xe9J\x7f\xff\x94\xfb\xff\x86\x1fd7\xbb\x06\xd9F\xaf+ܻN[_\xa5\xa7\xf9j%O\x1f\xe9\x15p\xb6\xbc\xff\x00\x00\x00\xff\xff"))
}
