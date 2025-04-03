package v1alpha

import (
	entity "miren.dev/runtime/pkg/entity"
	schema "miren.dev/runtime/pkg/entity/schema"
)

var (
	KindSandbox = entity.MustKeyword("dev.miren.sandbox/sandbox")
)

const (
	ContainerId   = entity.Id("dev.miren.sandbox/container")
	HostNetworkId = entity.Id("dev.miren.sandbox/hostNetwork")
	LabelsId      = entity.Id("dev.miren.sandbox/labels")
	NetworkId     = entity.Id("dev.miren.sandbox/network")
	PortId        = entity.Id("dev.miren.sandbox/port")
	RouteId       = entity.Id("dev.miren.sandbox/route")
)

type Sandbox struct {
	ID          entity.Id   `json:"id"`
	Container   []Container `json:"container"`
	HostNetwork bool        `json:"hostNetwork,omitempty"`
	Labels      []string    `json:"labels,omitempty"`
	Network     []Network   `json:"network,omitempty"`
	Port        []Port      `json:"port,omitempty"`
	Route       []Route     `json:"route,omitempty"`
}

func (o *Sandbox) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	for _, a := range e.GetAll(ContainerId) {
		if a.Value.Kind() == entity.KindComponent {
			var v Container
			v.Decode(a.Value.Component())
			o.Container = append(o.Container, v)
		}
	}
	if a, ok := e.Get(HostNetworkId); ok && a.Value.Kind() == entity.KindBool {
		o.HostNetwork = a.Value.Bool()
	}
	for _, a := range e.GetAll(LabelsId) {
		if a.Value.Kind() == entity.KindString {
			o.Labels = append(o.Labels, a.Value.String())
		}
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
	for _, a := range e.GetAll(RouteId) {
		if a.Value.Kind() == entity.KindComponent {
			var v Route
			v.Decode(a.Value.Component())
			o.Route = append(o.Route, v)
		}
	}
}

func (o *Sandbox) Encode() (attrs []entity.Attr) {
	for _, v := range o.Container {
		attrs = append(attrs, entity.Component(ContainerId, v.Encode()))
	}
	attrs = append(attrs, entity.Bool(HostNetworkId, o.HostNetwork))
	for _, v := range o.Labels {
		attrs = append(attrs, entity.String(LabelsId, v))
	}
	for _, v := range o.Network {
		attrs = append(attrs, entity.Component(NetworkId, v.Encode()))
	}
	for _, v := range o.Port {
		attrs = append(attrs, entity.Component(PortId, v.Encode()))
	}
	for _, v := range o.Route {
		attrs = append(attrs, entity.Component(RouteId, v.Encode()))
	}
	return
}

func (o *Sandbox) InitSchema(sb *schema.SchemaBuilder) {
	sb.Component("container", schema.Doc("A container running in the sandbox"), schema.Many, schema.Required)
	(&Container{}).InitSchema(sb.Builder("container"))
	sb.Bool("hostNetwork", schema.Doc("Indicates if the container should use the networking of\nnode that it is running on directly\n"))
	sb.String("labels", schema.Doc("Label for the sandbox"), schema.Many)
	sb.Component("network", schema.Doc("Network accessability for the container"), schema.Many)
	(&Network{}).InitSchema(sb.Builder("network"))
	sb.Component("port", schema.Doc("A network port the container declares"), schema.Many)
	(&Port{}).InitSchema(sb.Builder("port"))
	sb.Component("route", schema.Doc("A network route the container uses"), schema.Many)
	(&Route{}).InitSchema(sb.Builder("route"))
}

const (
	ContainerCommandId    = entity.Id("dev.miren.sandbox.container/command")
	ContainerDirectoryId  = entity.Id("dev.miren.sandbox.container/directory")
	ContainerEnvId        = entity.Id("dev.miren.sandbox.container/env")
	ContainerImageId      = entity.Id("dev.miren.sandbox.container/image")
	ContainerMountId      = entity.Id("dev.miren.sandbox.container/mount")
	ContainerNameId       = entity.Id("dev.miren.sandbox.container/name")
	ContainerOomScoreId   = entity.Id("dev.miren.sandbox.container/oom_score")
	ContainerPrivilegedId = entity.Id("dev.miren.sandbox.container/privileged")
)

type Container struct {
	Command    string   `json:"command,omitempty"`
	Directory  string   `json:"directory,omitempty"`
	Env        []string `json:"env,omitempty"`
	Image      string   `json:"image"`
	Mount      []Mount  `json:"mount,omitempty"`
	Name       string   `json:"name,omitempty"`
	OomScore   int64    `json:"oom_score,omitempty"`
	Privileged bool     `json:"privileged,omitempty"`
}

func (o *Container) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(ContainerCommandId); ok && a.Value.Kind() == entity.KindString {
		o.Command = a.Value.String()
	}
	if a, ok := e.Get(ContainerDirectoryId); ok && a.Value.Kind() == entity.KindString {
		o.Directory = a.Value.String()
	}
	for _, a := range e.GetAll(ContainerEnvId) {
		if a.Value.Kind() == entity.KindString {
			o.Env = append(o.Env, a.Value.String())
		}
	}
	if a, ok := e.Get(ContainerImageId); ok && a.Value.Kind() == entity.KindString {
		o.Image = a.Value.String()
	}
	for _, a := range e.GetAll(ContainerMountId) {
		if a.Value.Kind() == entity.KindComponent {
			var v Mount
			v.Decode(a.Value.Component())
			o.Mount = append(o.Mount, v)
		}
	}
	if a, ok := e.Get(ContainerNameId); ok && a.Value.Kind() == entity.KindString {
		o.Name = a.Value.String()
	}
	if a, ok := e.Get(ContainerOomScoreId); ok && a.Value.Kind() == entity.KindInt64 {
		o.OomScore = a.Value.Int64()
	}
	if a, ok := e.Get(ContainerPrivilegedId); ok && a.Value.Kind() == entity.KindBool {
		o.Privileged = a.Value.Bool()
	}
}

func (o *Container) Encode() (attrs []entity.Attr) {
	attrs = append(attrs, entity.String(ContainerCommandId, o.Command))
	attrs = append(attrs, entity.String(ContainerDirectoryId, o.Directory))
	for _, v := range o.Env {
		attrs = append(attrs, entity.String(ContainerEnvId, v))
	}
	attrs = append(attrs, entity.String(ContainerImageId, o.Image))
	for _, v := range o.Mount {
		attrs = append(attrs, entity.Component(ContainerMountId, v.Encode()))
	}
	attrs = append(attrs, entity.String(ContainerNameId, o.Name))
	attrs = append(attrs, entity.Int64(ContainerOomScoreId, o.OomScore))
	attrs = append(attrs, entity.Bool(ContainerPrivilegedId, o.Privileged))
	return
}

func (o *Container) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("command", schema.Doc("Command to run in the container"))
	sb.String("directory", schema.Doc("Directory to start in"))
	sb.String("env", schema.Doc("Environment variable for the container"), schema.Many)
	sb.String("image", schema.Doc("Container image"), schema.Required)
	sb.Component("mount", schema.Doc("A mounted directory"), schema.Many)
	(&Mount{}).InitSchema(sb.Builder("mount"))
	sb.String("name", schema.Doc("Container name"))
	sb.Int64("oom_score", schema.Doc("How to adjust the OOM score for this container"))
	sb.Bool("privileged", schema.Doc("Whether or not the container runs in privileged mode"))
}

const (
	MountDestinationId = entity.Id("dev.miren.sandbox.container.mount/destination")
	MountSourceId      = entity.Id("dev.miren.sandbox.container.mount/source")
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
	NetworkAddressId = entity.Id("dev.miren.sandbox.network/address")
	NetworkSubnetId  = entity.Id("dev.miren.sandbox.network/subnet")
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
	PortNameId        = entity.Id("dev.miren.sandbox.port/name")
	PortNodePortId    = entity.Id("dev.miren.sandbox.port/node_port")
	PortPortId        = entity.Id("dev.miren.sandbox.port/port")
	PortProtocolId    = entity.Id("dev.miren.sandbox.port/protocol")
	PortProtocolTcpId = entity.Id("dev.miren.sandbox.port/protocol.tcp")
	PortProtocolUdpId = entity.Id("dev.miren.sandbox.port/protocol.udp")
	PortTypeId        = entity.Id("dev.miren.sandbox.port/type")
)

type Port struct {
	Name     string       `json:"name"`
	NodePort int64        `json:"node_port,omitempty"`
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
	if a, ok := e.Get(PortNodePortId); ok && a.Value.Kind() == entity.KindInt64 {
		o.NodePort = a.Value.Int64()
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
	attrs = append(attrs, entity.Int64(PortNodePortId, o.NodePort))
	attrs = append(attrs, entity.Int64(PortPortId, o.Port))
	attrs = append(attrs, entity.String(PortTypeId, o.Type))
	return
}

func (o *Port) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("name", schema.Doc("Name of the port for reference"), schema.Required)
	sb.Int64("node_port", schema.Doc("The port number that should be forwarded from the node to the container"))
	sb.Int64("port", schema.Doc("Port number"), schema.Required)
	sb.Singleton("protocol.tcp")
	sb.Singleton("protocol.udp")
	sb.Ref("protocol", schema.Doc("Port protocol"), schema.Choices(PortProtocolTcpId, PortProtocolUdpId))
	sb.String("type", schema.Doc("The highlevel type of the port"))
}

const (
	RouteDestinationId = entity.Id("dev.miren.sandbox.route/destination")
	RouteGatewayId     = entity.Id("dev.miren.sandbox.route/gateway")
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
	schema.Register("dev.miren.sandbox", "v1alpha", (&Sandbox{}).InitSchema)
}
