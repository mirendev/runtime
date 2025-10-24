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
	schema.RegisterEncodedSchema("dev.miren.network", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\x8cT\xddҔ0\f}\x0e\xc7\xdfQ\xaf\xeb\xf8D;\xdd&@\x06\xfac[p\xf7Vg|\x90\xef[}C\xbdv\x1a\u0602BYn\x18\xda朤礹\x81\x91\x1a\xbf\x00\x0eB\x93G#\fƯַؒ\x81\xf0ty\xb3:\xf9\x94ND@?\x90\xc2_\f\xbf\xbcXGM\x01#ϟ\n\xac\x96d\xd6y\xaa\x8a\xb0\x83\xf0\xfdv&\xb8\xbc*\xd2\br\xa0\xa5\xb9\xfe\xe6|gr\x10\xaf\x0e\xab\x10=\x99\x9a\xb1o\xcbX-\xa3j\x16p\x1c7\x12\x03v\xf2\x8c\xdd\xcfD\xb0q\xd3;\x81\xb3>.\xf0\xc0\xeb\x04'e\xb5\xb3\x06M\x9c\xff&I\xd6tbIwP\x97\x1fϩ\xb4\x97\xeb\xd2\x12\x87\xe0b\xe6\xcfB\x12\x86\xbd+\xc1,\xe0\x89\xef\x90`4/\x13\x81\"\x13w\x93f \xfc\x83)\x990b\xbc\x8dVَqM^%,\xa0\xe9u\x9b>\xa7Av=\x86'\x15\x95\xdbr\xe3\x0e\x13Q9\xd5\xc3~L\x0f\x8eo\xf1\xbePQ\x94\xbe\xc68\xab\xd0.7\x0e\xe9\xc0\xc5gݗ\xe2\xd7\x03\xfa@\xd6\xd4\xc3gٹFvΓ\x96\xfezJ\x9e\xb3j\xbb\x11\xf5\xd4'[\xfe\xf1\xdbC\x03Β\x89aj\xb5\x8d\ns\xc8\xc1>\xfb\xc6O\xe0\xe3\x0eQκx\bM\xde{\xf0\x18\xd6\xc4bM|\xb4Tv\xe5u\xb9\xd44+6\xa6\xc4s\xa1A3\xac\xd0ػ^e\x05\x98\xfdÞ~\x93\xab\x9c\xe1n1'9\x13\xec\xe6\xa0\xcc\xf1\x7fX\x1b\x1a\xeb\xe3i\x1cչm\x1e\x8c\xec\x99\xeeq\x83\xfd\x05\x00\x00\xff\xff\x01\x00\x00\xff\xff\xa7\xb2\x19\xcc\x1e\x06\x00\x00"))
}
