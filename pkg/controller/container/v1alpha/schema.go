package v1alpha

import (
	entity "miren.dev/runtime/pkg/entity"
	schema "miren.dev/runtime/pkg/entity/schema"
)

const (
	CommandId    = entity.Id("dev.miren.container/command")
	EnvId        = entity.Id("dev.miren.container/env")
	ImageId      = entity.Id("dev.miren.container/image")
	LabelId      = entity.Id("dev.miren.container/label")
	MountId      = entity.Id("dev.miren.container/mount")
	NameId       = entity.Id("dev.miren.container/name")
	NetworkId    = entity.Id("dev.miren.container/network")
	PortId       = entity.Id("dev.miren.container/port")
	PrivilegedId = entity.Id("dev.miren.container/privileged")
	RouteId      = entity.Id("dev.miren.container/route")
)

type Container struct {
	ID         string    `json:"id"`
	Command    string    `json:"command,omitempty"`
	Env        []string  `json:"env,omitempty"`
	Image      string    `json:"image"`
	Label      []string  `json:"label,omitempty"`
	Mount      []Mount   `json:"mount,omitempty"`
	Name       string    `json:"name,omitempty"`
	Network    []Network `json:"network,omitempty"`
	Port       []Port    `json:"port,omitempty"`
	Privileged bool      `json:"privileged,omitempty"`
	Route      []Route   `json:"route,omitempty"`
}

func (o *Container) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.String()
	if a, ok := e.Get(CommandId); ok && a.Value.Kind() == entity.KindString {
		o.Command = a.Value.String()
	}
	for _, a := range e.GetAll(EnvId) {
		if a.Value.Kind() == entity.KindString {
			o.Env = append(o.Env, a.Value.String())
		}
	}
	if a, ok := e.Get(ImageId); ok && a.Value.Kind() == entity.KindString {
		o.Image = a.Value.String()
	}
	for _, a := range e.GetAll(LabelId) {
		if a.Value.Kind() == entity.KindString {
			o.Label = append(o.Label, a.Value.String())
		}
	}
	for _, a := range e.GetAll(MountId) {
		if a.Value.Kind() == entity.KindComponent {
			var v Mount
			v.Decode(a.Value.Component())
			o.Mount = append(o.Mount, v)
		}
	}
	if a, ok := e.Get(NameId); ok && a.Value.Kind() == entity.KindString {
		o.Name = a.Value.String()
	}
	for _, a := range e.GetAll(NetworkId) {
		if a.Value.Kind() == entity.KindComponent {
			var v Network
			v.Decode(a.Value.Component())
			o.Network = append(o.Network, v)
		}
	}
	for _, a := range e.GetAll(PortId) {
		if a.Value.Kind() == entity.KindComponent {
			var v Port
			v.Decode(a.Value.Component())
			o.Port = append(o.Port, v)
		}
	}
	if a, ok := e.Get(PrivilegedId); ok && a.Value.Kind() == entity.KindBool {
		o.Privileged = a.Value.Bool()
	}
	for _, a := range e.GetAll(RouteId) {
		if a.Value.Kind() == entity.KindComponent {
			var v Route
			v.Decode(a.Value.Component())
			o.Route = append(o.Route, v)
		}
	}
}

func (o *Container) Encode() (attrs []entity.Attr) {
	attrs = append(attrs, entity.String(CommandId, o.Command))
	for _, v := range o.Env {
		attrs = append(attrs, entity.String(EnvId, v))
	}
	attrs = append(attrs, entity.String(ImageId, o.Image))
	for _, v := range o.Label {
		attrs = append(attrs, entity.String(LabelId, v))
	}
	for _, v := range o.Mount {
		attrs = append(attrs, entity.Component(MountId, v.Encode()))
	}
	attrs = append(attrs, entity.String(NameId, o.Name))
	for _, v := range o.Network {
		attrs = append(attrs, entity.Component(NetworkId, v.Encode()))
	}
	for _, v := range o.Port {
		attrs = append(attrs, entity.Component(PortId, v.Encode()))
	}
	attrs = append(attrs, entity.Bool(PrivilegedId, o.Privileged))
	for _, v := range o.Route {
		attrs = append(attrs, entity.Component(RouteId, v.Encode()))
	}
	return
}

func (o *Container) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("command", schema.Doc("Command to run in the container"))
	sb.String("env", schema.Doc("Environment variable for the container"), schema.Many)
	sb.String("image", schema.Doc("Container image"), schema.Required)
	sb.String("label", schema.Doc("Label for the container"), schema.Many)
	sb.Component("mount", schema.Doc("A mounted directory"), schema.Many)
	(&Mount{}).InitSchema(sb.Builder("mount"))
	sb.String("name", schema.Doc("Container name"))
	sb.Component("network", schema.Doc("Network accessability for the container"), schema.Many)
	(&Network{}).InitSchema(sb.Builder("network"))
	sb.Component("port", schema.Doc("A network port the container declares"), schema.Many)
	(&Port{}).InitSchema(sb.Builder("port"))
	sb.Bool("privileged", schema.Doc("Whether or not the container runs in privileged mode"))
	sb.Component("route", schema.Doc("A network route the container uses"), schema.Many)
	(&Route{}).InitSchema(sb.Builder("route"))
}

const (
	MountDestinationId = entity.Id("dev.miren.container.mount/destination")
	MountSourceId      = entity.Id("dev.miren.container.mount/source")
)

type Mount struct {
	Destination string `json:"destination,omitempty"`
	Source      string `json:"source,omitempty"`
}

func (o *Mount) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(MountDestinationId); ok && a.Value.Kind() == entity.KindString {
		o.Destination = a.Value.String()
	}
	if a, ok := e.Get(MountSourceId); ok && a.Value.Kind() == entity.KindString {
		o.Source = a.Value.String()
	}
}

func (o *Mount) Encode() (attrs []entity.Attr) {
	attrs = append(attrs, entity.String(MountDestinationId, o.Destination))
	attrs = append(attrs, entity.String(MountSourceId, o.Source))
	return
}

func (o *Mount) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("destination", schema.Doc("Mount destination path"))
	sb.String("source", schema.Doc("Mount source path"))
}

const (
	NetworkAddressId = entity.Id("dev.miren.container.network/address")
	NetworkSubnetId  = entity.Id("dev.miren.container.network/subnet")
)

type Network struct {
	Address string `json:"address,omitempty"`
	Subnet  string `json:"subnet,omitempty"`
}

func (o *Network) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(NetworkAddressId); ok && a.Value.Kind() == entity.KindString {
		o.Address = a.Value.String()
	}
	if a, ok := e.Get(NetworkSubnetId); ok && a.Value.Kind() == entity.KindString {
		o.Subnet = a.Value.String()
	}
}

func (o *Network) Encode() (attrs []entity.Attr) {
	attrs = append(attrs, entity.String(NetworkAddressId, o.Address))
	attrs = append(attrs, entity.String(NetworkSubnetId, o.Subnet))
	return
}

func (o *Network) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("address", schema.Doc("A network address to reach the container at"))
	sb.String("subnet", schema.Doc("The subnet that the address is associated with"))
}

const (
	PortNameId        = entity.Id("dev.miren.container.port/name")
	PortPortId        = entity.Id("dev.miren.container.port/port")
	PortProtocolId    = entity.Id("dev.miren.container.port/protocol")
	PortProtocolTcpId = entity.Id("dev.miren.container.port/protocol.tcp")
	PortProtocolUdpId = entity.Id("dev.miren.container.port/protocol.udp")
	PortTypeId        = entity.Id("dev.miren.container.port/type")
)

type Port struct {
	Name     string       `json:"name"`
	Port     int64        `json:"port"`
	Protocol PortProtocol `json:"protocol,omitempty"`
	Type     string       `json:"type,omitempty"`
}

type PortProtocol string

const (
	TCP PortProtocol = "protocol.tcp"
	UDP PortProtocol = "protocol.udp"
)

var protocolFromId = map[entity.Id]PortProtocol{PortProtocolTcpId: TCP, PortProtocolUdpId: UDP}
var protocolToId = map[PortProtocol]entity.Id{TCP: PortProtocolTcpId, UDP: PortProtocolUdpId}

func (o *Port) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(PortNameId); ok && a.Value.Kind() == entity.KindString {
		o.Name = a.Value.String()
	}
	if a, ok := e.Get(PortPortId); ok && a.Value.Kind() == entity.KindInt64 {
		o.Port = a.Value.Int64()
	}
	if a, ok := e.Get(PortProtocolId); ok && a.Value.Kind() == entity.KindId {
		o.Protocol = protocolFromId[a.Value.Id()]
	}
	if a, ok := e.Get(PortTypeId); ok && a.Value.Kind() == entity.KindString {
		o.Type = a.Value.String()
	}
}

func (o *Port) Encode() (attrs []entity.Attr) {
	attrs = append(attrs, entity.String(PortNameId, o.Name))
	attrs = append(attrs, entity.Int64(PortPortId, o.Port))
	attrs = append(attrs, entity.String(PortTypeId, o.Type))
	return
}

func (o *Port) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("name", schema.Doc("Name of the port for reference"), schema.Required)
	sb.Int64("port", schema.Doc("Port number"), schema.Required)
	sb.Singleton("protocol.tcp")
	sb.Singleton("protocol.udp")
	sb.Ref("protocol", schema.Doc("Port protocol"), schema.Choices(PortProtocolTcpId, PortProtocolUdpId))
	sb.String("type", schema.Doc("The highlevel type of the port"))
}

const (
	RouteDestinationId = entity.Id("dev.miren.container.route/destination")
	RouteGatewayId     = entity.Id("dev.miren.container.route/gateway")
)

type Route struct {
	Destination string `json:"destination,omitempty"`
	Gateway     string `json:"gateway,omitempty"`
}

func (o *Route) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(RouteDestinationId); ok && a.Value.Kind() == entity.KindString {
		o.Destination = a.Value.String()
	}
	if a, ok := e.Get(RouteGatewayId); ok && a.Value.Kind() == entity.KindString {
		o.Gateway = a.Value.String()
	}
}

func (o *Route) Encode() (attrs []entity.Attr) {
	attrs = append(attrs, entity.String(RouteDestinationId, o.Destination))
	attrs = append(attrs, entity.String(RouteGatewayId, o.Gateway))
	return
}

func (o *Route) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("destination", schema.Doc("The network destination"))
	sb.String("gateway", schema.Doc("The next hop for the destination"))
}

func init() {
	schema.Register("dev.miren.container", "v1alpha", (&Container{}).InitSchema)
}
