package v1alpha

import (
	entity "miren.dev/runtime/pkg/entity"
	schema "miren.dev/runtime/pkg/entity/schema"
	types "miren.dev/runtime/pkg/entity/types"
)

const (
	ContainerId   = entity.Id("dev.miren.sandbox/container")
	HostNetworkId = entity.Id("dev.miren.sandbox/hostNetwork")
	LabelsId      = entity.Id("dev.miren.sandbox/labels")
	NetworkId     = entity.Id("dev.miren.sandbox/network")
	PortId        = entity.Id("dev.miren.sandbox/port")
	RouteId       = entity.Id("dev.miren.sandbox/route")
	VolumeId      = entity.Id("dev.miren.sandbox/volume")
)

type Sandbox struct {
	ID          entity.Id   `json:"id"`
	Container   []Container `json:"container"`
	HostNetwork bool        `json:"hostNetwork,omitempty"`
	Labels      []string    `json:"labels,omitempty"`
	Network     []Network   `json:"network,omitempty"`
	Port        []Port      `json:"port,omitempty"`
	Route       []Route     `json:"route,omitempty"`
	Volume      []Volume    `json:"volume,omitempty"`
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
	for _, a := range e.GetAll(VolumeId) {
		if a.Value.Kind() == entity.KindComponent {
			var v Volume
			v.Decode(a.Value.Component())
			o.Volume = append(o.Volume, v)
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
	for _, v := range o.Volume {
		attrs = append(attrs, entity.Component(VolumeId, v.Encode()))
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
	sb.Component("volume", schema.Doc("A volume that is available for binding into containers"), schema.Many)
	(&Volume{}).InitSchema(sb.Builder("volume"))
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

const (
	VolumeLabelsId   = entity.Id("dev.miren.sandbox.volume/labels")
	VolumeNameId     = entity.Id("dev.miren.sandbox.volume/name")
	VolumeProviderId = entity.Id("dev.miren.sandbox.volume/provider")
)

type Volume struct {
	Labels   types.Labels `json:"labels,omitempty"`
	Name     string       `json:"name,omitempty"`
	Provider string       `json:"provider,omitempty"`
}

func (o *Volume) Decode(e entity.AttrGetter) {
	for _, a := range e.GetAll(VolumeLabelsId) {
		if a.Value.Kind() == entity.KindLabel {
			o.Labels = append(o.Labels, a.Value.Label())
		}
	}
	if a, ok := e.Get(VolumeNameId); ok && a.Value.Kind() == entity.KindString {
		o.Name = a.Value.String()
	}
	if a, ok := e.Get(VolumeProviderId); ok && a.Value.Kind() == entity.KindString {
		o.Provider = a.Value.String()
	}
}

func (o *Volume) Encode() (attrs []entity.Attr) {
	for _, v := range o.Labels {
		attrs = append(attrs, entity.Label(VolumeLabelsId, v.Key, v.Value))
	}
	attrs = append(attrs, entity.String(VolumeNameId, o.Name))
	attrs = append(attrs, entity.String(VolumeProviderId, o.Provider))
	return
}

func (o *Volume) InitSchema(sb *schema.SchemaBuilder) {
	sb.Label("labels", schema.Doc("Labels that identify the volume to the provider"), schema.Many)
	sb.String("name", schema.Doc("The name of the volume"))
	sb.String("provider", schema.Doc("What provider should provide the volume"))
}

var (
	KindSandbox   = entity.MustKeyword("dev.miren.sandbox/kind.sandbox")
	KindContainer = entity.MustKeyword("dev.miren.sandbox/kind.container")
	KindMount     = entity.MustKeyword("dev.miren.sandbox/kind.mount")
	KindNetwork   = entity.MustKeyword("dev.miren.sandbox/kind.network")
	KindPort      = entity.MustKeyword("dev.miren.sandbox/kind.port")
	KindRoute     = entity.MustKeyword("dev.miren.sandbox/kind.route")
	KindVolume    = entity.MustKeyword("dev.miren.sandbox/kind.volume")
	Schema        = entity.Id("schema.dev.miren.sandbox/v1alpha")
)

func init() {
	schema.Register("dev.miren.sandbox", "v1alpha", (&Sandbox{}).InitSchema)
	schema.RegisterEncodedSchema("dev.miren.sandbox", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\xa4VQ\xae\xd3:\x14\xdc\xc6{B\b\x10\x02\xf1C*6\xc2\x16*\xd7>qMc\x9f`;\xe9\xed/\b]\xd6Aa\x89\xf0\x8d\xec\xe34M\xec\xfaV\xe2'j\x95\x99\x89\xcfx<\xc9/a\x98\x86\xcf\x02\xc6F+\v\xa6q̈\x1d>\xc8\x11\xacSh\xe4\xf8\x81u\xfd\x9e\xc1A\x19\xe1\x1ee\xba\xad8\x1aϔ\x01\v\x1a\a\xe3\xa5\x01\x7fD{\x10=Z\x0f\x16\a\x0f\xed\x88ݠ\xa1m\x15t\xc2=҃f\x9e\xf0\xa7>\xfc\xd5=\x1a0~\xa7\xc4ól\x15\x9b\x19\xae\x999\xfd\x9e\xf1$W\xa04\x17Jy\x84?iA\xdf\xcfQArԚ\x19\x11\x97\xd3:o\x95\x91a-\xaf+\u009b\xc4!\x05%\x94\x05\xeeў\xd6\x1aoj\x1a\x17\xd6Ϩ\xc2\xc1\x8ck\xfe\x8b\x1a?\xe0\xa3'\xb4\nP\x9aIX+\xbc\xaa)D\x06\xf9H\x9bXؒ\xaa\x00\x91\xca\x1bS#6\x14\x99\xea\xf6|\xa1\xb1\x0e\x02\x9cW\x86y\x85f=\xdc\xfb'\x1f\xb1\xb9b\x93^\xebp\xb0<\xf3\xe9\xdd\xd3RD\xecz\xab4\xb3\xa7mX,\xb9F\xc2tYɾ\xac\xb9\x17\b)A\x88z\xeb8Z\x12\xe0\x8a\xbc\xaf\xc6\xe7B!\x89O\xbdU\xa3\xea@\x02%Y\xec\x10\xbb \xf2\xb6&2\x93\x16s͇4m\xc2\x1e\x9d\xff\x98\x0e\xf8B\xfdy~b\xaf\xb0\x94\xec\xb6c;\xe8\xdcڛ\xffrj\x02\xc6@Q\x8c.\xb5\x92'\xf3\xff\x9c?\x81ˉ\xcc\tM\"ܕDɄ\xb0\xe0\xb29\nAO\xb2\x9bĘ\x927\xec\f\xf8;\"2щ\xb0ؙ\xc9\x0f\x1a)\x96mn͘;\x13\x81E[rp\x13\xc0uK\xbeU2_h\xe4 x\x1dw\x83\x02\xb6\x97\xb5Oq/8A\xc4\t}^\r=\x11o=1\\(\x82\xfbޢG\x8e\x1d\xa5\x17̠o\xf4+\x11\x13\xfa\x10\x80ۑu\x03\xb8\x1f\xdc\xf3\xbe\xf4VX0\x1a\xcf{>\x88;\x80\x83\xe8ϳ\x7f\xf7\x9a\x18`\x8b@D7R\x87Ƿn\x1e\x87c\x1e\aB\x16\U000d08db\x88\xfe\xe7\xb6.8\x12\x85\U000ce592y8\xb2\xecuZ\xd8.RH\xf8e7\xc7[4S\xfa\x12)\x94H\xa1\x84\x12\xb6\xdc!9\xbe!|ݞ\xafy\x13B\xfc}c,\xd2\\\xf4a\xe5\xc4\xe5%<\t\xccg.\x9c\x80Q\x89\xf4\xd9U\xaf\xafD\x9e\x18\v[\x93\x95\xcbRJĿ\x00\x00\x00\xff\xff"))
}
