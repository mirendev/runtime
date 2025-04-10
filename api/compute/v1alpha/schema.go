package compute_v1alpha

import (
	entity "miren.dev/runtime/pkg/entity"
	schema "miren.dev/runtime/pkg/entity/schema"
	types "miren.dev/runtime/pkg/entity/types"
)

const (
	NodeConstraintsId     = entity.Id("dev.miren.compute/node.constraints")
	NodeStatusId          = entity.Id("dev.miren.compute/node.status")
	NodeStatusUnknownId   = entity.Id("dev.miren.compute/status.unknown")
	NodeStatusReadyId     = entity.Id("dev.miren.compute/status.ready")
	NodeStatusDisabledId  = entity.Id("dev.miren.compute/status.disabled")
	NodeStatusUnhealthyId = entity.Id("dev.miren.compute/status.unhealthy")
)

type Node struct {
	ID          entity.Id    `json:"id"`
	Constraints types.Labels `cbor:"constraints,omitempty" json:"constraints,omitempty"`
	Status      NodeStatus   `cbor:"status,omitempty" json:"status,omitempty"`
}

type NodeStatus string

const (
	UNKNOWN   NodeStatus = "status.unknown"
	READY     NodeStatus = "status.ready"
	DISABLED  NodeStatus = "status.disabled"
	UNHEALTHY NodeStatus = "status.unhealthy"
)

var statusFromId = map[entity.Id]NodeStatus{NodeStatusUnknownId: UNKNOWN, NodeStatusReadyId: READY, NodeStatusDisabledId: DISABLED, NodeStatusUnhealthyId: UNHEALTHY}
var statusToId = map[NodeStatus]entity.Id{UNKNOWN: NodeStatusUnknownId, READY: NodeStatusReadyId, DISABLED: NodeStatusDisabledId, UNHEALTHY: NodeStatusUnhealthyId}

func (o *Node) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	for _, a := range e.GetAll(NodeConstraintsId) {
		if a.Value.Kind() == entity.KindLabel {
			o.Constraints = append(o.Constraints, a.Value.Label())
		}
	}
	if a, ok := e.Get(NodeStatusId); ok && a.Value.Kind() == entity.KindId {
		o.Status = statusFromId[a.Value.Id()]
	}
}

func (o *Node) Encode() (attrs []entity.Attr) {
	for _, v := range o.Constraints {
		attrs = append(attrs, entity.Label(NodeConstraintsId, v.Key, v.Value))
	}
	if a, ok := statusToId[o.Status]; ok {
		attrs = append(attrs, entity.Ref(NodeStatusId, a))
	}
	attrs = append(attrs, entity.Keyword(entity.EntityKind, KindNode))
	attrs = append(attrs, entity.Ref(entity.EntitySchema, Schema))
	return
}

func (o *Node) InitSchema(sb *schema.SchemaBuilder) {
	sb.Label("constraints", "dev.miren.compute/node.constraints", schema.Doc("The label constraints the node has, used for scheduling"), schema.Many)
	sb.Singleton("dev.miren.compute/status.unknown")
	sb.Singleton("dev.miren.compute/status.ready")
	sb.Singleton("dev.miren.compute/status.disabled")
	sb.Singleton("dev.miren.compute/status.unhealthy")
	sb.Ref("status", "dev.miren.compute/node.status", schema.Doc("The status of the node"), schema.Choices(NodeStatusUnknownId, NodeStatusReadyId, NodeStatusDisabledId, NodeStatusUnhealthyId))
}

const (
	SandboxContainerId   = entity.Id("dev.miren.compute/sandbox.container")
	SandboxHostNetworkId = entity.Id("dev.miren.compute/sandbox.hostNetwork")
	SandboxLabelsId      = entity.Id("dev.miren.compute/sandbox.labels")
	SandboxNetworkId     = entity.Id("dev.miren.compute/sandbox.network")
	SandboxPortId        = entity.Id("dev.miren.compute/sandbox.port")
	SandboxRouteId       = entity.Id("dev.miren.compute/sandbox.route")
	SandboxVolumeId      = entity.Id("dev.miren.compute/sandbox.volume")
)

type Sandbox struct {
	ID          entity.Id   `json:"id"`
	Container   []Container `cbor:"container" json:"container"`
	HostNetwork bool        `cbor:"hostNetwork,omitempty" json:"hostNetwork,omitempty"`
	Labels      []string    `cbor:"labels,omitempty" json:"labels,omitempty"`
	Network     []Network   `cbor:"network,omitempty" json:"network,omitempty"`
	Port        []Port      `cbor:"port,omitempty" json:"port,omitempty"`
	Route       []Route     `cbor:"route,omitempty" json:"route,omitempty"`
	Volume      []Volume    `cbor:"volume,omitempty" json:"volume,omitempty"`
}

func (o *Sandbox) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	for _, a := range e.GetAll(SandboxContainerId) {
		if a.Value.Kind() == entity.KindComponent {
			var v Container
			v.Decode(a.Value.Component())
			o.Container = append(o.Container, v)
		}
	}
	if a, ok := e.Get(SandboxHostNetworkId); ok && a.Value.Kind() == entity.KindBool {
		o.HostNetwork = a.Value.Bool()
	}
	for _, a := range e.GetAll(SandboxLabelsId) {
		if a.Value.Kind() == entity.KindString {
			o.Labels = append(o.Labels, a.Value.String())
		}
	}
	for _, a := range e.GetAll(SandboxNetworkId) {
		if a.Value.Kind() == entity.KindComponent {
			var v Network
			v.Decode(a.Value.Component())
			o.Network = append(o.Network, v)
		}
	}
	for _, a := range e.GetAll(SandboxPortId) {
		if a.Value.Kind() == entity.KindComponent {
			var v Port
			v.Decode(a.Value.Component())
			o.Port = append(o.Port, v)
		}
	}
	for _, a := range e.GetAll(SandboxRouteId) {
		if a.Value.Kind() == entity.KindComponent {
			var v Route
			v.Decode(a.Value.Component())
			o.Route = append(o.Route, v)
		}
	}
	for _, a := range e.GetAll(SandboxVolumeId) {
		if a.Value.Kind() == entity.KindComponent {
			var v Volume
			v.Decode(a.Value.Component())
			o.Volume = append(o.Volume, v)
		}
	}
}

func (o *Sandbox) Encode() (attrs []entity.Attr) {
	for _, v := range o.Container {
		attrs = append(attrs, entity.Component(SandboxContainerId, v.Encode()))
	}
	attrs = append(attrs, entity.Bool(SandboxHostNetworkId, o.HostNetwork))
	for _, v := range o.Labels {
		attrs = append(attrs, entity.String(SandboxLabelsId, v))
	}
	for _, v := range o.Network {
		attrs = append(attrs, entity.Component(SandboxNetworkId, v.Encode()))
	}
	for _, v := range o.Port {
		attrs = append(attrs, entity.Component(SandboxPortId, v.Encode()))
	}
	for _, v := range o.Route {
		attrs = append(attrs, entity.Component(SandboxRouteId, v.Encode()))
	}
	for _, v := range o.Volume {
		attrs = append(attrs, entity.Component(SandboxVolumeId, v.Encode()))
	}
	attrs = append(attrs, entity.Keyword(entity.EntityKind, KindSandbox))
	attrs = append(attrs, entity.Ref(entity.EntitySchema, Schema))
	return
}

func (o *Sandbox) InitSchema(sb *schema.SchemaBuilder) {
	sb.Component("container", "dev.miren.compute/sandbox.container", schema.Doc("A container running in the sandbox"), schema.Many, schema.Required)
	(&Container{}).InitSchema(sb.Builder("container"))
	sb.Bool("hostNetwork", "dev.miren.compute/sandbox.hostNetwork", schema.Doc("Indicates if the container should use the networking of\nnode that it is running on directly\n"))
	sb.String("labels", "dev.miren.compute/sandbox.labels", schema.Doc("Label for the sandbox"), schema.Many)
	sb.Component("network", "dev.miren.compute/sandbox.network", schema.Doc("Network accessability for the container"), schema.Many)
	(&Network{}).InitSchema(sb.Builder("network"))
	sb.Component("port", "dev.miren.compute/sandbox.port", schema.Doc("A network port the container declares"), schema.Many)
	(&Port{}).InitSchema(sb.Builder("port"))
	sb.Component("route", "dev.miren.compute/sandbox.route", schema.Doc("A network route the container uses"), schema.Many)
	(&Route{}).InitSchema(sb.Builder("route"))
	sb.Component("volume", "dev.miren.compute/sandbox.volume", schema.Doc("A volume that is available for binding into containers"), schema.Many)
	(&Volume{}).InitSchema(sb.Builder("volume"))
}

const (
	ContainerCommandId    = entity.Id("dev.miren.compute/container.command")
	ContainerDirectoryId  = entity.Id("dev.miren.compute/container.directory")
	ContainerEnvId        = entity.Id("dev.miren.compute/container.env")
	ContainerImageId      = entity.Id("dev.miren.compute/container.image")
	ContainerMountId      = entity.Id("dev.miren.compute/container.mount")
	ContainerNameId       = entity.Id("dev.miren.compute/container.name")
	ContainerOomScoreId   = entity.Id("dev.miren.compute/container.oom_score")
	ContainerPrivilegedId = entity.Id("dev.miren.compute/container.privileged")
)

type Container struct {
	Command    string   `cbor:"command,omitempty" json:"command,omitempty"`
	Directory  string   `cbor:"directory,omitempty" json:"directory,omitempty"`
	Env        []string `cbor:"env,omitempty" json:"env,omitempty"`
	Image      string   `cbor:"image" json:"image"`
	Mount      []Mount  `cbor:"mount,omitempty" json:"mount,omitempty"`
	Name       string   `cbor:"name,omitempty" json:"name,omitempty"`
	OomScore   int64    `cbor:"oom_score,omitempty" json:"oom_score,omitempty"`
	Privileged bool     `cbor:"privileged,omitempty" json:"privileged,omitempty"`
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
	sb.String("command", "dev.miren.compute/container.command", schema.Doc("Command to run in the container"))
	sb.String("directory", "dev.miren.compute/container.directory", schema.Doc("Directory to start in"))
	sb.String("env", "dev.miren.compute/container.env", schema.Doc("Environment variable for the container"), schema.Many)
	sb.String("image", "dev.miren.compute/container.image", schema.Doc("Container image"), schema.Required)
	sb.Component("mount", "dev.miren.compute/container.mount", schema.Doc("A mounted directory"), schema.Many)
	(&Mount{}).InitSchema(sb.Builder("mount"))
	sb.String("name", "dev.miren.compute/container.name", schema.Doc("Container name"))
	sb.Int64("oom_score", "dev.miren.compute/container.oom_score", schema.Doc("How to adjust the OOM score for this container"))
	sb.Bool("privileged", "dev.miren.compute/container.privileged", schema.Doc("Whether or not the container runs in privileged mode"))
}

const (
	MountDestinationId = entity.Id("dev.miren.compute/mount.destination")
	MountSourceId      = entity.Id("dev.miren.compute/mount.source")
)

type Mount struct {
	Destination string `cbor:"destination,omitempty" json:"destination,omitempty"`
	Source      string `cbor:"source,omitempty" json:"source,omitempty"`
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
	sb.String("destination", "dev.miren.compute/mount.destination", schema.Doc("Mount destination path"))
	sb.String("source", "dev.miren.compute/mount.source", schema.Doc("Mount source path"))
}

const (
	NetworkAddressId = entity.Id("dev.miren.compute/network.address")
	NetworkSubnetId  = entity.Id("dev.miren.compute/network.subnet")
)

type Network struct {
	Address string `cbor:"address,omitempty" json:"address,omitempty"`
	Subnet  string `cbor:"subnet,omitempty" json:"subnet,omitempty"`
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
	sb.String("address", "dev.miren.compute/network.address", schema.Doc("A network address to reach the container at"))
	sb.String("subnet", "dev.miren.compute/network.subnet", schema.Doc("The subnet that the address is associated with"))
}

const (
	PortNameId        = entity.Id("dev.miren.compute/port.name")
	PortNodePortId    = entity.Id("dev.miren.compute/port.node_port")
	PortPortId        = entity.Id("dev.miren.compute/port.port")
	PortProtocolId    = entity.Id("dev.miren.compute/port.protocol")
	PortProtocolTcpId = entity.Id("dev.miren.compute/protocol.tcp")
	PortProtocolUdpId = entity.Id("dev.miren.compute/protocol.udp")
	PortTypeId        = entity.Id("dev.miren.compute/port.type")
)

type Port struct {
	Name     string       `cbor:"name" json:"name"`
	NodePort int64        `cbor:"node_port,omitempty" json:"node_port,omitempty"`
	Port     int64        `cbor:"port" json:"port"`
	Protocol PortProtocol `cbor:"protocol,omitempty" json:"protocol,omitempty"`
	Type     string       `cbor:"type,omitempty" json:"type,omitempty"`
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
	if a, ok := protocolToId[o.Protocol]; ok {
		attrs = append(attrs, entity.Ref(PortProtocolId, a))
	}
	attrs = append(attrs, entity.String(PortTypeId, o.Type))
	return
}

func (o *Port) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("name", "dev.miren.compute/port.name", schema.Doc("Name of the port for reference"), schema.Required)
	sb.Int64("node_port", "dev.miren.compute/port.node_port", schema.Doc("The port number that should be forwarded from the node to the container"))
	sb.Int64("port", "dev.miren.compute/port.port", schema.Doc("Port number"), schema.Required)
	sb.Singleton("dev.miren.compute/protocol.tcp")
	sb.Singleton("dev.miren.compute/protocol.udp")
	sb.Ref("protocol", "dev.miren.compute/port.protocol", schema.Doc("Port protocol"), schema.Choices(PortProtocolTcpId, PortProtocolUdpId))
	sb.String("type", "dev.miren.compute/port.type", schema.Doc("The highlevel type of the port"))
}

const (
	RouteDestinationId = entity.Id("dev.miren.compute/route.destination")
	RouteGatewayId     = entity.Id("dev.miren.compute/route.gateway")
)

type Route struct {
	Destination string `cbor:"destination,omitempty" json:"destination,omitempty"`
	Gateway     string `cbor:"gateway,omitempty" json:"gateway,omitempty"`
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
	sb.String("destination", "dev.miren.compute/route.destination", schema.Doc("The network destination"))
	sb.String("gateway", "dev.miren.compute/route.gateway", schema.Doc("The next hop for the destination"))
}

const (
	VolumeLabelsId   = entity.Id("dev.miren.compute/volume.labels")
	VolumeNameId     = entity.Id("dev.miren.compute/volume.name")
	VolumeProviderId = entity.Id("dev.miren.compute/volume.provider")
)

type Volume struct {
	Labels   types.Labels `cbor:"labels,omitempty" json:"labels,omitempty"`
	Name     string       `cbor:"name,omitempty" json:"name,omitempty"`
	Provider string       `cbor:"provider,omitempty" json:"provider,omitempty"`
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
	sb.Label("labels", "dev.miren.compute/volume.labels", schema.Doc("Labels that identify the volume to the provider"), schema.Many)
	sb.String("name", "dev.miren.compute/volume.name", schema.Doc("The name of the volume"))
	sb.String("provider", "dev.miren.compute/volume.provider", schema.Doc("What provider should provide the volume"))
}

const (
	ScheduleKeyId = entity.Id("dev.miren.compute/schedule.key")
)

type Schedule struct {
	ID  entity.Id `json:"id"`
	Key Key       `cbor:"key,omitempty" json:"key,omitempty"`
}

func (o *Schedule) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(ScheduleKeyId); ok && a.Value.Kind() == entity.KindComponent {
		o.Key.Decode(a.Value.Component())
	}
}

func (o *Schedule) Encode() (attrs []entity.Attr) {
	attrs = append(attrs, entity.Component(ScheduleKeyId, o.Key.Encode()))
	attrs = append(attrs, entity.Keyword(entity.EntityKind, KindSchedule))
	attrs = append(attrs, entity.Ref(entity.EntitySchema, Schema))
	return
}

func (o *Schedule) InitSchema(sb *schema.SchemaBuilder) {
	sb.Component("key", "dev.miren.compute/schedule.key", schema.Doc("The scheduling key for an entity"), schema.Indexed)
	(&Key{}).InitSchema(sb.Builder("key"))
}

const (
	KeyKindId = entity.Id("dev.miren.compute/key.kind")
	KeyNodeId = entity.Id("dev.miren.compute/key.node")
)

type Key struct {
	Kind types.Keyword `cbor:"kind,omitempty" json:"kind,omitempty"`
	Node entity.Id     `cbor:"node,omitempty" json:"node,omitempty"`
}

func (o *Key) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(KeyKindId); ok && a.Value.Kind() == entity.KindKeyword {
		o.Kind = a.Value.Keyword()
	}
	if a, ok := e.Get(KeyNodeId); ok && a.Value.Kind() == entity.KindId {
		o.Node = a.Value.Id()
	}
}

func (o *Key) Encode() (attrs []entity.Attr) {
	attrs = append(attrs, entity.Keyword(KeyKindId, o.Kind))
	attrs = append(attrs, entity.Ref(KeyNodeId, o.Node))
	return
}

func (o *Key) InitSchema(sb *schema.SchemaBuilder) {
	sb.Keyword("kind", "dev.miren.compute/key.kind", schema.Doc("The type of entity this is"))
	sb.Ref("node", "dev.miren.compute/key.node", schema.Doc("The node id the entity is scheduled for"))
}

var (
	KindNode     = entity.MustKeyword("dev.miren.compute/kind.node")
	KindSandbox  = entity.MustKeyword("dev.miren.compute/kind.sandbox")
	KindSchedule = entity.MustKeyword("dev.miren.compute/kind.schedule")
	Schema       = entity.Id("schema.dev.miren.compute/v1alpha")
)

func init() {
	schema.Register("dev.miren.compute", "v1alpha", func(sb *schema.SchemaBuilder) {
		(&Node{}).InitSchema(sb)
		(&Sandbox{}).InitSchema(sb)
		(&Schedule{}).InitSchema(sb)
	})
	schema.RegisterEncodedSchema("dev.miren.compute", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\xb4W\xcd\xce\xd3:\x10}\x8f{u\x7f\x00\x81\xc4ƈ\xc7`\xc3+Tn<ML\x12;\xd8N\xda,\x01!x\x0e\xfa\xf1\x88\xb0F\xf6\xd8M\xd2L\xdd\xf0Il\xbah\xce9\x9e9\xf3\x13\xe7A(\xde\xc2{\x01\x03k\xa5\x01\xc5\n\xddv\xbd\x83r\x00c\xa5V\xe5\xf0\x9a7]š\x96J\xd8\xf3\xe9\xef\x15\xf2\x95\x7f\u0094\x16\xf0\xfd\xb7\xb4~\x1e\x0e\x12\x1aa?b\bu\xa1\x95u\x86K\xe5\xacpc\a\xd0\xf0=4{)NO\xd7g\xfa\xe3\u061c\xd1r5\xfe@\xa5\x83u\xdc\xf5(\"@\xf5\xad\xd7\xf8\xe7\x86\x06bk\x0f\xdb\r\xbc\xe9\xc1>\x94\xbd\xaa\x95>\xaa\xd3\xffk\x0e\xc2YD\x80\x01.\xc6ӿ7q\xe1y%\xa4\xe5\xfb\x06\xc4\xe9\xc9M`\x82\xc8^U\xc0\x1bW\x8dT֗\xd3#\xa6\xe9\x8cl\xb9\x19w\xdeP\xe1\xf3\xa1b\t\xf5\xb1\\\x89\xbd>=\xaeD_\x90&\v\xad\x1c\x97\nL\xf0Vz\xaeV\xa0\x9c7\xf8\x19\x11.\x9e\xc9&Z\xa8\xd2\xc4CY\x82\xcaV\xd4|\x80_\xcfA\xa9,t\xdbr%Bx\a\xeb\x8cT\xe5\x8d\xd8.\xc2,rPA\ni\xa0pڌ\xd7\x1a\xcfs\x1a\x17\x16v`\x01j\xb8\xe6\xff\x97\xe3{|\xf0\x06\xa3\x00\xd9\xf2\x12\xae\x15\x88\xee\x99\x14\x02\x03\xfd\x84V\xf7\xca\x11%\xca\n \x89.\xd0\xcb\r\x05B\x81;\xa3\x8e\xe9\xd5\x02\xac\x93\x8a;\xa9ՆR\x05a6\xe3\x9c\xe3\x98\xeb\xde\x14+\x97\x88\x01@\x01\x84/F\x06\x9dB9\xfc\xb9\x12#6\xc0\x94\xb0'ĮѺ\xdd\xd9B\x1b\x14($\xfa\x9dm\x99\v\x05%\xdeuF\x0e\xb2\x81\x12\xb0{\xc5^\xeb\xb0\xfc^\xe4D&\xd2\"\xafiP\xa3ᕶ\xee-\xb8\xa36\xf5R\x9d\b1UvƉ{5,d\xbb\xc1\xa3$\x11\t\xa1\xa9\xb0\x95J5\x8b\xe2nw&\x9dD\xa2\xbbsMdW\xc4M]Yr!\f\xd8U~D\\Q\x96EF\xea\xc7~\xaf\xc0m\xb0'ё\xb0\xa8\\\xf2\aS\x13\x9d6\xd4 S/\x9c\x98q`\xd0>\xadYl\xceʛ\xf493%ĝ\xc0\v\xce\aĿ\x9bv\x97lҀ\x10\xde 1\xa1\xcfW6$\xe2\xad\x13\xfd\x0f6k\xd5\x19\xedt\xa1\x9b\xe55\x80\xd8\xc2H\x8c\xe8\xf9E\xe0[\u128e\xf2:\x81\x99+\xba\xa2\x17yL/\xba\xf3\xe4\xdaV\xeb<l\xf9v\xf7\x7f\xc7\xfdnt\xef\x80h\v\"\xb9T`\xa4\xd0}\xb1\xa6\xb1\x05\xedO\xec\xf4 \xbc\xde\xe9e\xc9\x1d\x1c\xf9\xea\xe5Kd\x86\n\x11\xbf\xdc\xea\xe1\x11&w\x18tӷ\x94Y\x99\xb5\x159\xb4[k\x1e[\xf2\xf2v}Z\xef\xd2\xe9\xa2K\xa4\x89\x9a\x8bM\x9a\x99D\xe2\x96\x1b\x05\xa6Y\xf4\x931H\x11/q\xf9E\x17ɉ\xb1\xb09Z\xbb\\_\xd1\n*\x13\xbc\x86\x16\x15\x88\xbey\xe4\xa7\u0087x\xbd\xaaaܸ\x15\xe3q\xac\x86q\xd3>\x9c\xe17\xb5\xbd\b\xb3\xe9c)k\x18\x8f\xda\b\x1f\xc9_D\xf602\x8fM\xc5\xd3\x02\x8b\xb7\x7f\x93cx\xd8\xc2`\x9f\xfa\xe2\x8f*\x85\\\xdbJ\x1b\xb7\xc3\xcf4\xfc\x16\xc8}\xab]*u\xe7\x83\xe1\xa2\x7f\xb7\xa4\xbf\x00\x00\x00\xff\xff"))
}
