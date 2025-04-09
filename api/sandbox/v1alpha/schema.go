package v1alpha

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
	schema.RegisterEncodedSchema("dev.miren.compute", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\xb4\x97ݎ\xd3:\x10\xc7\xdf\xe3\x1c\x9d\x0f@\x80\x10\x92+\x1e\x83\x1b^\xa1r\xedib\x92\xd8\xc1v\xd2\xe6\x12\x10\x82\xe7\xa0\xcb#\xc25\xb2\xc7n\x93\xda\xf5fW\xe2\xa6\x17\xdd\xf9\xfd3\xf3\x9f\x8ff︤\x1d|\xe00\x92Nh\x90\x84\xa9\xae\x1f,T#h#\x94\xac\xc67\xb4\xedk\n\x8d\x90ܜ\xb8T\x1c~<\x88\xf9\xb5\xdf\vh\xb9\xf9\x84\x8fj\x98\x92\xc6j*\xa45\xdcN=@Kw\xd0\xee\x04?>M\x147\xeeqdNtTN?Qio,\xb5\x03\x8ap\x90C\xe74\xfe\xb9\xa1\x81\xb1\x8d\vێ\xb4\x1d\xc0܁\x06ʧ㳄 \x8e\xd8 A|Pͅ\xa1\xbb\x16\xf8\xf1E9:ƉA\xd6@[[OǗe\xe2\x1cX\r\xb2\x91\xea \x8f\xcf\xef\x03|X\xdbk\xd1Q=m\x9dɾ-\x95\xa1\x92\xef\xd4\xf1q\xed\xf9\x8a\x98`JZ*$h\xef\xabp\xac\x92 \xad37\xb5j\x13\x9eI.\x98\xefЅCٜ\xcb\tZN\xf0\xdb\xc9+ULu\x1d\x95ܧ\xb77V\vY\xdd\xc8\xed,L\x02\x83\n\x82\v\r\xcc*=]k\xa4\xce\xcf4\xce\x14N\x1f\x039^\xf3\xff\x95x\x17\xef\xbd\xc1,@t\xb4\x82k\x85'%\x05O\xa0\x9fЩA\xdaL\x8b\x8a\x02\b\xe5\x1b\xf4jE\x83P\xe0\x9e5\xc7\xf2\x1a\x0e\xc6\nI\xadPrE\xab\xbc0\x991\xa7\xb0\xe2j\xd0,q\xe9\xdf[\x02\x18\xbeX\rt\n\xe5\xf0\xe3J\xec\xff\x92c\x0e\bS\xa3T\xb75Li\x14`\x02\xfd.\x8e\xcc\x19A\x89\xf7\xbd\x16\xa3h\xa1\x02\x9c^\xbeS\xca\x1f\xbe\xf4\xa8\xccD.Т\xaeˢ\x06\xc3ke\xec;\xb0\a\xa5\x9b\xa5z&\xc5\xd8\xd9\x19\x13n\xaa?\xc6f\x85GQ\"\x00~\xa8p\x94*9\xcb\xe2\xde\xe9\x8c:\x11\xcaOg\n\x92+p\xd5TV\x94s\r&\xa9/\x93W\x90%\x81\x88\xf38\xec$\xd8\x15\xf6D\x1c\x81E\xe7\xa2?X\x1a\xef\x95\xce-rf\xc6cŞ\xc8\xfb\x94RdN\x95M\xfaRؒ\xbf\xd3t\x9c\xe0|A\xdco\xd0\xf6\\M\\\x90\x8c7\b\xc6\xe8ӕ\r\x11\xbc\xf5D\xf7\x81\xc3Z\xf7ZY\xc5T\xbb|\x05\xc8\\a\x04C\xf4\xfc%\xe0;\xb3\xac?\xbe.\xbb\xb6\x89$\xb1\xacg\x03\x7f\x000\xf0\xfet\xf1s\xad\xa9.l\xf9\xfb\xee\xbe\x0e\x97_\xab\xc1Bf`2eǜ\x10\xc9OL\x8a\x91\x05\xf6'\xae\xbd\x17N\xaf}UQ\v\a\x9a\xfc,g*C\x85\x10\xbf\xbc\xf7\xfeOX\xdc~T\xed\xd0\xe5\xcc*\x1c\xb4\xc0\xe4\xddJ9\xb2\xe4\xcav}N\xaf\xec\xe5\xf57S&j.nlaG3\xef\xbeAಥngF\xc1\xc3\xeb]\xf9\x04\x068\x12\v\x9b\x83\xb5\xcb\xc3\x16\xac\xa8\r\xab\x81\x0f\xed#\xffQ\xf8\x18^\xb0\x1a\x98V\xde\xc5\xf08\xd2\xc0\xb4\xea\"\xce\xe2W\x8d7\xf7;\xe8r\xa9\x1a\x98\x0eJs\x97\xc9_i&\rL\xc4\xc5\xc6&)\x8eMڽ-\x11.la\xa4+}\xf1\xc5\xd9\xd1\xc6\xd4J\xdb-&\xf9\x1b\x00\x00\xff\xff"))
}
