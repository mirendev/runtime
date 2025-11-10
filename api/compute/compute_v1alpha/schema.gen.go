package compute_v1alpha

import (
	"time"

	entity "miren.dev/runtime/pkg/entity"
	schema "miren.dev/runtime/pkg/entity/schema"
	types "miren.dev/runtime/pkg/entity/types"
)

const (
	SandboxSpecContainerId    = entity.Id("dev.miren.compute/component.sandbox_spec.container")
	SandboxSpecHostNetworkId  = entity.Id("dev.miren.compute/component.sandbox_spec.hostNetwork")
	SandboxSpecLogAttributeId = entity.Id("dev.miren.compute/component.sandbox_spec.logAttribute")
	SandboxSpecLogEntityId    = entity.Id("dev.miren.compute/component.sandbox_spec.logEntity")
	SandboxSpecRouteId        = entity.Id("dev.miren.compute/component.sandbox_spec.route")
	SandboxSpecStaticHostId   = entity.Id("dev.miren.compute/component.sandbox_spec.static_host")
	SandboxSpecVersionId      = entity.Id("dev.miren.compute/component.sandbox_spec.version")
	SandboxSpecVolumeId       = entity.Id("dev.miren.compute/component.sandbox_spec.volume")
)

type SandboxSpec struct {
	Container    []SandboxSpecContainer  `cbor:"container" json:"container"`
	HostNetwork  bool                    `cbor:"hostNetwork,omitempty" json:"hostNetwork,omitempty"`
	LogAttribute types.Labels            `cbor:"logAttribute,omitempty" json:"logAttribute,omitempty"`
	LogEntity    string                  `cbor:"logEntity,omitempty" json:"logEntity,omitempty"`
	Route        []SandboxSpecRoute      `cbor:"route,omitempty" json:"route,omitempty"`
	StaticHost   []SandboxSpecStaticHost `cbor:"static_host,omitempty" json:"static_host,omitempty"`
	Version      entity.Id               `cbor:"version,omitempty" json:"version,omitempty"`
	Volume       []SandboxSpecVolume     `cbor:"volume,omitempty" json:"volume,omitempty"`
}

func (o *SandboxSpec) Decode(e entity.AttrGetter) {
	for _, a := range e.GetAll(SandboxSpecContainerId) {
		if a.Value.Kind() == entity.KindComponent {
			var v SandboxSpecContainer
			v.Decode(a.Value.Component())
			o.Container = append(o.Container, v)
		}
	}
	if a, ok := e.Get(SandboxSpecHostNetworkId); ok && a.Value.Kind() == entity.KindBool {
		o.HostNetwork = a.Value.Bool()
	}
	for _, a := range e.GetAll(SandboxSpecLogAttributeId) {
		if a.Value.Kind() == entity.KindLabel {
			o.LogAttribute = append(o.LogAttribute, a.Value.Label())
		}
	}
	if a, ok := e.Get(SandboxSpecLogEntityId); ok && a.Value.Kind() == entity.KindString {
		o.LogEntity = a.Value.String()
	}
	for _, a := range e.GetAll(SandboxSpecRouteId) {
		if a.Value.Kind() == entity.KindComponent {
			var v SandboxSpecRoute
			v.Decode(a.Value.Component())
			o.Route = append(o.Route, v)
		}
	}
	for _, a := range e.GetAll(SandboxSpecStaticHostId) {
		if a.Value.Kind() == entity.KindComponent {
			var v SandboxSpecStaticHost
			v.Decode(a.Value.Component())
			o.StaticHost = append(o.StaticHost, v)
		}
	}
	if a, ok := e.Get(SandboxSpecVersionId); ok && a.Value.Kind() == entity.KindId {
		o.Version = a.Value.Id()
	}
	for _, a := range e.GetAll(SandboxSpecVolumeId) {
		if a.Value.Kind() == entity.KindComponent {
			var v SandboxSpecVolume
			v.Decode(a.Value.Component())
			o.Volume = append(o.Volume, v)
		}
	}
}

func (o *SandboxSpec) Encode() (attrs []entity.Attr) {
	for _, v := range o.Container {
		attrs = append(attrs, entity.Component(SandboxSpecContainerId, v.Encode()))
	}
	attrs = append(attrs, entity.Bool(SandboxSpecHostNetworkId, o.HostNetwork))
	for _, v := range o.LogAttribute {
		attrs = append(attrs, entity.Label(SandboxSpecLogAttributeId, v.Key, v.Value))
	}
	if !entity.Empty(o.LogEntity) {
		attrs = append(attrs, entity.String(SandboxSpecLogEntityId, o.LogEntity))
	}
	for _, v := range o.Route {
		attrs = append(attrs, entity.Component(SandboxSpecRouteId, v.Encode()))
	}
	for _, v := range o.StaticHost {
		attrs = append(attrs, entity.Component(SandboxSpecStaticHostId, v.Encode()))
	}
	if !entity.Empty(o.Version) {
		attrs = append(attrs, entity.Ref(SandboxSpecVersionId, o.Version))
	}
	for _, v := range o.Volume {
		attrs = append(attrs, entity.Component(SandboxSpecVolumeId, v.Encode()))
	}
	return
}

func (o *SandboxSpec) Empty() bool {
	if len(o.Container) != 0 {
		return false
	}
	if !entity.Empty(o.HostNetwork) {
		return false
	}
	if len(o.LogAttribute) != 0 {
		return false
	}
	if !entity.Empty(o.LogEntity) {
		return false
	}
	if len(o.Route) != 0 {
		return false
	}
	if len(o.StaticHost) != 0 {
		return false
	}
	if !entity.Empty(o.Version) {
		return false
	}
	if len(o.Volume) != 0 {
		return false
	}
	return true
}

func (o *SandboxSpec) InitSchema(sb *schema.SchemaBuilder) {
	sb.Component("container", "dev.miren.compute/component.sandbox_spec.container", schema.Doc("Container specification"), schema.Many, schema.Required)
	(&SandboxSpecContainer{}).InitSchema(sb.Builder("component.sandbox_spec.container"))
	sb.Bool("hostNetwork", "dev.miren.compute/component.sandbox_spec.hostNetwork", schema.Doc("Whether to use host networking"))
	sb.Label("logAttribute", "dev.miren.compute/component.sandbox_spec.logAttribute", schema.Doc("Labels for log entries"), schema.Many)
	sb.String("logEntity", "dev.miren.compute/component.sandbox_spec.logEntity", schema.Doc("Entity to associate log output with"))
	sb.Component("route", "dev.miren.compute/component.sandbox_spec.route", schema.Doc("Network route configuration"), schema.Many)
	(&SandboxSpecRoute{}).InitSchema(sb.Builder("component.sandbox_spec.route"))
	sb.Component("static_host", "dev.miren.compute/component.sandbox_spec.static_host", schema.Doc("Static host-to-IP mapping"), schema.Many)
	(&SandboxSpecStaticHost{}).InitSchema(sb.Builder("component.sandbox_spec.static_host"))
	sb.Ref("version", "dev.miren.compute/component.sandbox_spec.version", schema.Doc("Application version reference"), schema.Indexed)
	sb.Component("volume", "dev.miren.compute/component.sandbox_spec.volume", schema.Doc("Volume configuration"), schema.Many)
	(&SandboxSpecVolume{}).InitSchema(sb.Builder("component.sandbox_spec.volume"))
}

const (
	SandboxSpecContainerCommandId    = entity.Id("dev.miren.compute/component.sandbox_spec.container.command")
	SandboxSpecContainerConfigFileId = entity.Id("dev.miren.compute/component.sandbox_spec.container.config_file")
	SandboxSpecContainerDirectoryId  = entity.Id("dev.miren.compute/component.sandbox_spec.container.directory")
	SandboxSpecContainerEnvId        = entity.Id("dev.miren.compute/component.sandbox_spec.container.env")
	SandboxSpecContainerImageId      = entity.Id("dev.miren.compute/component.sandbox_spec.container.image")
	SandboxSpecContainerMountId      = entity.Id("dev.miren.compute/component.sandbox_spec.container.mount")
	SandboxSpecContainerNameId       = entity.Id("dev.miren.compute/component.sandbox_spec.container.name")
	SandboxSpecContainerOomScoreId   = entity.Id("dev.miren.compute/component.sandbox_spec.container.oom_score")
	SandboxSpecContainerPortId       = entity.Id("dev.miren.compute/component.sandbox_spec.container.port")
	SandboxSpecContainerPrivilegedId = entity.Id("dev.miren.compute/component.sandbox_spec.container.privileged")
)

type SandboxSpecContainer struct {
	Command    string                           `cbor:"command,omitempty" json:"command,omitempty"`
	ConfigFile []SandboxSpecContainerConfigFile `cbor:"config_file,omitempty" json:"config_file,omitempty"`
	Directory  string                           `cbor:"directory,omitempty" json:"directory,omitempty"`
	Env        []string                         `cbor:"env,omitempty" json:"env,omitempty"`
	Image      string                           `cbor:"image" json:"image"`
	Mount      []SandboxSpecContainerMount      `cbor:"mount,omitempty" json:"mount,omitempty"`
	Name       string                           `cbor:"name,omitempty" json:"name,omitempty"`
	OomScore   int64                            `cbor:"oom_score,omitempty" json:"oom_score,omitempty"`
	Port       []SandboxSpecContainerPort       `cbor:"port,omitempty" json:"port,omitempty"`
	Privileged bool                             `cbor:"privileged,omitempty" json:"privileged,omitempty"`
}

func (o *SandboxSpecContainer) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(SandboxSpecContainerCommandId); ok && a.Value.Kind() == entity.KindString {
		o.Command = a.Value.String()
	}
	for _, a := range e.GetAll(SandboxSpecContainerConfigFileId) {
		if a.Value.Kind() == entity.KindComponent {
			var v SandboxSpecContainerConfigFile
			v.Decode(a.Value.Component())
			o.ConfigFile = append(o.ConfigFile, v)
		}
	}
	if a, ok := e.Get(SandboxSpecContainerDirectoryId); ok && a.Value.Kind() == entity.KindString {
		o.Directory = a.Value.String()
	}
	for _, a := range e.GetAll(SandboxSpecContainerEnvId) {
		if a.Value.Kind() == entity.KindString {
			o.Env = append(o.Env, a.Value.String())
		}
	}
	if a, ok := e.Get(SandboxSpecContainerImageId); ok && a.Value.Kind() == entity.KindString {
		o.Image = a.Value.String()
	}
	for _, a := range e.GetAll(SandboxSpecContainerMountId) {
		if a.Value.Kind() == entity.KindComponent {
			var v SandboxSpecContainerMount
			v.Decode(a.Value.Component())
			o.Mount = append(o.Mount, v)
		}
	}
	if a, ok := e.Get(SandboxSpecContainerNameId); ok && a.Value.Kind() == entity.KindString {
		o.Name = a.Value.String()
	}
	if a, ok := e.Get(SandboxSpecContainerOomScoreId); ok && a.Value.Kind() == entity.KindInt64 {
		o.OomScore = a.Value.Int64()
	}
	for _, a := range e.GetAll(SandboxSpecContainerPortId) {
		if a.Value.Kind() == entity.KindComponent {
			var v SandboxSpecContainerPort
			v.Decode(a.Value.Component())
			o.Port = append(o.Port, v)
		}
	}
	if a, ok := e.Get(SandboxSpecContainerPrivilegedId); ok && a.Value.Kind() == entity.KindBool {
		o.Privileged = a.Value.Bool()
	}
}

func (o *SandboxSpecContainer) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Command) {
		attrs = append(attrs, entity.String(SandboxSpecContainerCommandId, o.Command))
	}
	for _, v := range o.ConfigFile {
		attrs = append(attrs, entity.Component(SandboxSpecContainerConfigFileId, v.Encode()))
	}
	if !entity.Empty(o.Directory) {
		attrs = append(attrs, entity.String(SandboxSpecContainerDirectoryId, o.Directory))
	}
	for _, v := range o.Env {
		attrs = append(attrs, entity.String(SandboxSpecContainerEnvId, v))
	}
	if !entity.Empty(o.Image) {
		attrs = append(attrs, entity.String(SandboxSpecContainerImageId, o.Image))
	}
	for _, v := range o.Mount {
		attrs = append(attrs, entity.Component(SandboxSpecContainerMountId, v.Encode()))
	}
	if !entity.Empty(o.Name) {
		attrs = append(attrs, entity.String(SandboxSpecContainerNameId, o.Name))
	}
	if !entity.Empty(o.OomScore) {
		attrs = append(attrs, entity.Int64(SandboxSpecContainerOomScoreId, o.OomScore))
	}
	for _, v := range o.Port {
		attrs = append(attrs, entity.Component(SandboxSpecContainerPortId, v.Encode()))
	}
	attrs = append(attrs, entity.Bool(SandboxSpecContainerPrivilegedId, o.Privileged))
	return
}

func (o *SandboxSpecContainer) Empty() bool {
	if !entity.Empty(o.Command) {
		return false
	}
	if len(o.ConfigFile) != 0 {
		return false
	}
	if !entity.Empty(o.Directory) {
		return false
	}
	if len(o.Env) != 0 {
		return false
	}
	if !entity.Empty(o.Image) {
		return false
	}
	if len(o.Mount) != 0 {
		return false
	}
	if !entity.Empty(o.Name) {
		return false
	}
	if !entity.Empty(o.OomScore) {
		return false
	}
	if len(o.Port) != 0 {
		return false
	}
	if !entity.Empty(o.Privileged) {
		return false
	}
	return true
}

func (o *SandboxSpecContainer) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("command", "dev.miren.compute/component.sandbox_spec.container.command", schema.Doc("Command to run"))
	sb.Component("config_file", "dev.miren.compute/component.sandbox_spec.container.config_file", schema.Doc("File to write into container"), schema.Many)
	(&SandboxSpecContainerConfigFile{}).InitSchema(sb.Builder("component.sandbox_spec.container.config_file"))
	sb.String("directory", "dev.miren.compute/component.sandbox_spec.container.directory", schema.Doc("Working directory"))
	sb.String("env", "dev.miren.compute/component.sandbox_spec.container.env", schema.Doc("Environment variable"), schema.Many)
	sb.String("image", "dev.miren.compute/component.sandbox_spec.container.image", schema.Doc("Container image"), schema.Required)
	sb.Component("mount", "dev.miren.compute/component.sandbox_spec.container.mount", schema.Doc("Mounted directory"), schema.Many)
	(&SandboxSpecContainerMount{}).InitSchema(sb.Builder("component.sandbox_spec.container.mount"))
	sb.String("name", "dev.miren.compute/component.sandbox_spec.container.name", schema.Doc("Container name"))
	sb.Int64("oom_score", "dev.miren.compute/component.sandbox_spec.container.oom_score", schema.Doc("OOM score adjustment"))
	sb.Component("port", "dev.miren.compute/component.sandbox_spec.container.port", schema.Doc("Network port declaration"), schema.Many)
	(&SandboxSpecContainerPort{}).InitSchema(sb.Builder("component.sandbox_spec.container.port"))
	sb.Bool("privileged", "dev.miren.compute/component.sandbox_spec.container.privileged", schema.Doc("Whether container runs in privileged mode"))
}

const (
	SandboxSpecContainerConfigFileDataId = entity.Id("dev.miren.compute/component.sandbox_spec.container.config_file.data")
	SandboxSpecContainerConfigFileModeId = entity.Id("dev.miren.compute/component.sandbox_spec.container.config_file.mode")
	SandboxSpecContainerConfigFilePathId = entity.Id("dev.miren.compute/component.sandbox_spec.container.config_file.path")
)

type SandboxSpecContainerConfigFile struct {
	Data string `cbor:"data,omitempty" json:"data,omitempty"`
	Mode string `cbor:"mode,omitempty" json:"mode,omitempty"`
	Path string `cbor:"path,omitempty" json:"path,omitempty"`
}

func (o *SandboxSpecContainerConfigFile) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(SandboxSpecContainerConfigFileDataId); ok && a.Value.Kind() == entity.KindString {
		o.Data = a.Value.String()
	}
	if a, ok := e.Get(SandboxSpecContainerConfigFileModeId); ok && a.Value.Kind() == entity.KindString {
		o.Mode = a.Value.String()
	}
	if a, ok := e.Get(SandboxSpecContainerConfigFilePathId); ok && a.Value.Kind() == entity.KindString {
		o.Path = a.Value.String()
	}
}

func (o *SandboxSpecContainerConfigFile) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Data) {
		attrs = append(attrs, entity.String(SandboxSpecContainerConfigFileDataId, o.Data))
	}
	if !entity.Empty(o.Mode) {
		attrs = append(attrs, entity.String(SandboxSpecContainerConfigFileModeId, o.Mode))
	}
	if !entity.Empty(o.Path) {
		attrs = append(attrs, entity.String(SandboxSpecContainerConfigFilePathId, o.Path))
	}
	return
}

func (o *SandboxSpecContainerConfigFile) Empty() bool {
	if !entity.Empty(o.Data) {
		return false
	}
	if !entity.Empty(o.Mode) {
		return false
	}
	if !entity.Empty(o.Path) {
		return false
	}
	return true
}

func (o *SandboxSpecContainerConfigFile) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("data", "dev.miren.compute/component.sandbox_spec.container.config_file.data", schema.Doc("File contents"))
	sb.String("mode", "dev.miren.compute/component.sandbox_spec.container.config_file.mode", schema.Doc("File mode"))
	sb.String("path", "dev.miren.compute/component.sandbox_spec.container.config_file.path", schema.Doc("File path in container"))
}

const (
	SandboxSpecContainerMountDestinationId = entity.Id("dev.miren.compute/component.sandbox_spec.container.mount.destination")
	SandboxSpecContainerMountSourceId      = entity.Id("dev.miren.compute/component.sandbox_spec.container.mount.source")
)

type SandboxSpecContainerMount struct {
	Destination string `cbor:"destination,omitempty" json:"destination,omitempty"`
	Source      string `cbor:"source,omitempty" json:"source,omitempty"`
}

func (o *SandboxSpecContainerMount) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(SandboxSpecContainerMountDestinationId); ok && a.Value.Kind() == entity.KindString {
		o.Destination = a.Value.String()
	}
	if a, ok := e.Get(SandboxSpecContainerMountSourceId); ok && a.Value.Kind() == entity.KindString {
		o.Source = a.Value.String()
	}
}

func (o *SandboxSpecContainerMount) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Destination) {
		attrs = append(attrs, entity.String(SandboxSpecContainerMountDestinationId, o.Destination))
	}
	if !entity.Empty(o.Source) {
		attrs = append(attrs, entity.String(SandboxSpecContainerMountSourceId, o.Source))
	}
	return
}

func (o *SandboxSpecContainerMount) Empty() bool {
	if !entity.Empty(o.Destination) {
		return false
	}
	if !entity.Empty(o.Source) {
		return false
	}
	return true
}

func (o *SandboxSpecContainerMount) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("destination", "dev.miren.compute/component.sandbox_spec.container.mount.destination", schema.Doc("Mount destination path"))
	sb.String("source", "dev.miren.compute/component.sandbox_spec.container.mount.source", schema.Doc("Mount source path"))
}

const (
	SandboxSpecContainerPortNameId        = entity.Id("dev.miren.compute/component.sandbox_spec.container.port.name")
	SandboxSpecContainerPortNodePortId    = entity.Id("dev.miren.compute/component.sandbox_spec.container.port.node_port")
	SandboxSpecContainerPortPortId        = entity.Id("dev.miren.compute/component.sandbox_spec.container.port.port")
	SandboxSpecContainerPortProtocolId    = entity.Id("dev.miren.compute/component.sandbox_spec.container.port.protocol")
	SandboxSpecContainerPortProtocolTcpId = entity.Id("dev.miren.compute/component.sandbox_spec.container.port.protocol.tcp")
	SandboxSpecContainerPortProtocolUdpId = entity.Id("dev.miren.compute/component.sandbox_spec.container.port.protocol.udp")
	SandboxSpecContainerPortTypeId        = entity.Id("dev.miren.compute/component.sandbox_spec.container.port.type")
)

type SandboxSpecContainerPort struct {
	Name     string                           `cbor:"name" json:"name"`
	NodePort int64                            `cbor:"node_port,omitempty" json:"node_port,omitempty"`
	Port     int64                            `cbor:"port" json:"port"`
	Protocol SandboxSpecContainerPortProtocol `cbor:"protocol,omitempty" json:"protocol,omitempty"`
	Type     string                           `cbor:"type,omitempty" json:"type,omitempty"`
}

type SandboxSpecContainerPortProtocol string

const (
	SandboxSpecContainerPortTCP SandboxSpecContainerPortProtocol = "component.sandbox_spec.container.port.protocol.tcp"
	SandboxSpecContainerPortUDP SandboxSpecContainerPortProtocol = "component.sandbox_spec.container.port.protocol.udp"
)

var SandboxSpecContainerPortprotocolFromId = map[entity.Id]SandboxSpecContainerPortProtocol{SandboxSpecContainerPortProtocolTcpId: SandboxSpecContainerPortTCP, SandboxSpecContainerPortProtocolUdpId: SandboxSpecContainerPortUDP}
var SandboxSpecContainerPortprotocolToId = map[SandboxSpecContainerPortProtocol]entity.Id{SandboxSpecContainerPortTCP: SandboxSpecContainerPortProtocolTcpId, SandboxSpecContainerPortUDP: SandboxSpecContainerPortProtocolUdpId}

func (o *SandboxSpecContainerPort) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(SandboxSpecContainerPortNameId); ok && a.Value.Kind() == entity.KindString {
		o.Name = a.Value.String()
	}
	if a, ok := e.Get(SandboxSpecContainerPortNodePortId); ok && a.Value.Kind() == entity.KindInt64 {
		o.NodePort = a.Value.Int64()
	}
	if a, ok := e.Get(SandboxSpecContainerPortPortId); ok && a.Value.Kind() == entity.KindInt64 {
		o.Port = a.Value.Int64()
	}
	if a, ok := e.Get(SandboxSpecContainerPortProtocolId); ok && a.Value.Kind() == entity.KindId {
		o.Protocol = SandboxSpecContainerPortprotocolFromId[a.Value.Id()]
	}
	if a, ok := e.Get(SandboxSpecContainerPortTypeId); ok && a.Value.Kind() == entity.KindString {
		o.Type = a.Value.String()
	}
}

func (o *SandboxSpecContainerPort) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Name) {
		attrs = append(attrs, entity.String(SandboxSpecContainerPortNameId, o.Name))
	}
	if !entity.Empty(o.NodePort) {
		attrs = append(attrs, entity.Int64(SandboxSpecContainerPortNodePortId, o.NodePort))
	}
	attrs = append(attrs, entity.Int64(SandboxSpecContainerPortPortId, o.Port))
	if a, ok := SandboxSpecContainerPortprotocolToId[o.Protocol]; ok {
		attrs = append(attrs, entity.Ref(SandboxSpecContainerPortProtocolId, a))
	}
	if !entity.Empty(o.Type) {
		attrs = append(attrs, entity.String(SandboxSpecContainerPortTypeId, o.Type))
	}
	return
}

func (o *SandboxSpecContainerPort) Empty() bool {
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
	if !entity.Empty(o.Type) {
		return false
	}
	return true
}

func (o *SandboxSpecContainerPort) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("name", "dev.miren.compute/component.sandbox_spec.container.port.name", schema.Doc("Port name"), schema.Required)
	sb.Int64("node_port", "dev.miren.compute/component.sandbox_spec.container.port.node_port", schema.Doc("The port number that should be forwarded from the node to the container"))
	sb.Int64("port", "dev.miren.compute/component.sandbox_spec.container.port.port", schema.Doc("Port number"), schema.Required)
	sb.Singleton("dev.miren.compute/component.sandbox_spec.container.port.protocol.tcp")
	sb.Singleton("dev.miren.compute/component.sandbox_spec.container.port.protocol.udp")
	sb.Ref("protocol", "dev.miren.compute/component.sandbox_spec.container.port.protocol", schema.Doc("Port protocol"), schema.Choices(SandboxSpecContainerPortProtocolTcpId, SandboxSpecContainerPortProtocolUdpId))
	sb.String("type", "dev.miren.compute/component.sandbox_spec.container.port.type", schema.Doc("High-level port type (e.g., http)"))
}

const (
	SandboxSpecRouteDestinationId = entity.Id("dev.miren.compute/component.sandbox_spec.route.destination")
	SandboxSpecRouteGatewayId     = entity.Id("dev.miren.compute/component.sandbox_spec.route.gateway")
)

type SandboxSpecRoute struct {
	Destination string `cbor:"destination,omitempty" json:"destination,omitempty"`
	Gateway     string `cbor:"gateway,omitempty" json:"gateway,omitempty"`
}

func (o *SandboxSpecRoute) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(SandboxSpecRouteDestinationId); ok && a.Value.Kind() == entity.KindString {
		o.Destination = a.Value.String()
	}
	if a, ok := e.Get(SandboxSpecRouteGatewayId); ok && a.Value.Kind() == entity.KindString {
		o.Gateway = a.Value.String()
	}
}

func (o *SandboxSpecRoute) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Destination) {
		attrs = append(attrs, entity.String(SandboxSpecRouteDestinationId, o.Destination))
	}
	if !entity.Empty(o.Gateway) {
		attrs = append(attrs, entity.String(SandboxSpecRouteGatewayId, o.Gateway))
	}
	return
}

func (o *SandboxSpecRoute) Empty() bool {
	if !entity.Empty(o.Destination) {
		return false
	}
	if !entity.Empty(o.Gateway) {
		return false
	}
	return true
}

func (o *SandboxSpecRoute) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("destination", "dev.miren.compute/component.sandbox_spec.route.destination", schema.Doc("Network destination"))
	sb.String("gateway", "dev.miren.compute/component.sandbox_spec.route.gateway", schema.Doc("Next hop for destination"))
}

const (
	SandboxSpecStaticHostHostId = entity.Id("dev.miren.compute/component.sandbox_spec.static_host.host")
	SandboxSpecStaticHostIpId   = entity.Id("dev.miren.compute/component.sandbox_spec.static_host.ip")
)

type SandboxSpecStaticHost struct {
	Host string `cbor:"host,omitempty" json:"host,omitempty"`
	Ip   string `cbor:"ip,omitempty" json:"ip,omitempty"`
}

func (o *SandboxSpecStaticHost) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(SandboxSpecStaticHostHostId); ok && a.Value.Kind() == entity.KindString {
		o.Host = a.Value.String()
	}
	if a, ok := e.Get(SandboxSpecStaticHostIpId); ok && a.Value.Kind() == entity.KindString {
		o.Ip = a.Value.String()
	}
}

func (o *SandboxSpecStaticHost) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Host) {
		attrs = append(attrs, entity.String(SandboxSpecStaticHostHostId, o.Host))
	}
	if !entity.Empty(o.Ip) {
		attrs = append(attrs, entity.String(SandboxSpecStaticHostIpId, o.Ip))
	}
	return
}

func (o *SandboxSpecStaticHost) Empty() bool {
	if !entity.Empty(o.Host) {
		return false
	}
	if !entity.Empty(o.Ip) {
		return false
	}
	return true
}

func (o *SandboxSpecStaticHost) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("host", "dev.miren.compute/component.sandbox_spec.static_host.host", schema.Doc("Hostname"))
	sb.String("ip", "dev.miren.compute/component.sandbox_spec.static_host.ip", schema.Doc("IP address"))
}

const (
	SandboxSpecVolumeDiskNameId     = entity.Id("dev.miren.compute/component.sandbox_spec.volume.disk_name")
	SandboxSpecVolumeFilesystemId   = entity.Id("dev.miren.compute/component.sandbox_spec.volume.filesystem")
	SandboxSpecVolumeLabelsId       = entity.Id("dev.miren.compute/component.sandbox_spec.volume.labels")
	SandboxSpecVolumeLeaseTimeoutId = entity.Id("dev.miren.compute/component.sandbox_spec.volume.lease_timeout")
	SandboxSpecVolumeMountPathId    = entity.Id("dev.miren.compute/component.sandbox_spec.volume.mount_path")
	SandboxSpecVolumeNameId         = entity.Id("dev.miren.compute/component.sandbox_spec.volume.name")
	SandboxSpecVolumeProviderId     = entity.Id("dev.miren.compute/component.sandbox_spec.volume.provider")
	SandboxSpecVolumeReadOnlyId     = entity.Id("dev.miren.compute/component.sandbox_spec.volume.read_only")
	SandboxSpecVolumeSizeGbId       = entity.Id("dev.miren.compute/component.sandbox_spec.volume.size_gb")
)

type SandboxSpecVolume struct {
	DiskName     string       `cbor:"disk_name,omitempty" json:"disk_name,omitempty"`
	Filesystem   string       `cbor:"filesystem,omitempty" json:"filesystem,omitempty"`
	Labels       types.Labels `cbor:"labels,omitempty" json:"labels,omitempty"`
	LeaseTimeout string       `cbor:"lease_timeout,omitempty" json:"lease_timeout,omitempty"`
	MountPath    string       `cbor:"mount_path,omitempty" json:"mount_path,omitempty"`
	Name         string       `cbor:"name,omitempty" json:"name,omitempty"`
	Provider     string       `cbor:"provider,omitempty" json:"provider,omitempty"`
	ReadOnly     bool         `cbor:"read_only,omitempty" json:"read_only,omitempty"`
	SizeGb       int64        `cbor:"size_gb,omitempty" json:"size_gb,omitempty"`
}

func (o *SandboxSpecVolume) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(SandboxSpecVolumeDiskNameId); ok && a.Value.Kind() == entity.KindString {
		o.DiskName = a.Value.String()
	}
	if a, ok := e.Get(SandboxSpecVolumeFilesystemId); ok && a.Value.Kind() == entity.KindString {
		o.Filesystem = a.Value.String()
	}
	for _, a := range e.GetAll(SandboxSpecVolumeLabelsId) {
		if a.Value.Kind() == entity.KindLabel {
			o.Labels = append(o.Labels, a.Value.Label())
		}
	}
	if a, ok := e.Get(SandboxSpecVolumeLeaseTimeoutId); ok && a.Value.Kind() == entity.KindString {
		o.LeaseTimeout = a.Value.String()
	}
	if a, ok := e.Get(SandboxSpecVolumeMountPathId); ok && a.Value.Kind() == entity.KindString {
		o.MountPath = a.Value.String()
	}
	if a, ok := e.Get(SandboxSpecVolumeNameId); ok && a.Value.Kind() == entity.KindString {
		o.Name = a.Value.String()
	}
	if a, ok := e.Get(SandboxSpecVolumeProviderId); ok && a.Value.Kind() == entity.KindString {
		o.Provider = a.Value.String()
	}
	if a, ok := e.Get(SandboxSpecVolumeReadOnlyId); ok && a.Value.Kind() == entity.KindBool {
		o.ReadOnly = a.Value.Bool()
	}
	if a, ok := e.Get(SandboxSpecVolumeSizeGbId); ok && a.Value.Kind() == entity.KindInt64 {
		o.SizeGb = a.Value.Int64()
	}
}

func (o *SandboxSpecVolume) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.DiskName) {
		attrs = append(attrs, entity.String(SandboxSpecVolumeDiskNameId, o.DiskName))
	}
	if !entity.Empty(o.Filesystem) {
		attrs = append(attrs, entity.String(SandboxSpecVolumeFilesystemId, o.Filesystem))
	}
	for _, v := range o.Labels {
		attrs = append(attrs, entity.Label(SandboxSpecVolumeLabelsId, v.Key, v.Value))
	}
	if !entity.Empty(o.LeaseTimeout) {
		attrs = append(attrs, entity.String(SandboxSpecVolumeLeaseTimeoutId, o.LeaseTimeout))
	}
	if !entity.Empty(o.MountPath) {
		attrs = append(attrs, entity.String(SandboxSpecVolumeMountPathId, o.MountPath))
	}
	if !entity.Empty(o.Name) {
		attrs = append(attrs, entity.String(SandboxSpecVolumeNameId, o.Name))
	}
	if !entity.Empty(o.Provider) {
		attrs = append(attrs, entity.String(SandboxSpecVolumeProviderId, o.Provider))
	}
	attrs = append(attrs, entity.Bool(SandboxSpecVolumeReadOnlyId, o.ReadOnly))
	if !entity.Empty(o.SizeGb) {
		attrs = append(attrs, entity.Int64(SandboxSpecVolumeSizeGbId, o.SizeGb))
	}
	return
}

func (o *SandboxSpecVolume) Empty() bool {
	if !entity.Empty(o.DiskName) {
		return false
	}
	if !entity.Empty(o.Filesystem) {
		return false
	}
	if len(o.Labels) != 0 {
		return false
	}
	if !entity.Empty(o.LeaseTimeout) {
		return false
	}
	if !entity.Empty(o.MountPath) {
		return false
	}
	if !entity.Empty(o.Name) {
		return false
	}
	if !entity.Empty(o.Provider) {
		return false
	}
	if !entity.Empty(o.ReadOnly) {
		return false
	}
	if !entity.Empty(o.SizeGb) {
		return false
	}
	return true
}

func (o *SandboxSpecVolume) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("disk_name", "dev.miren.compute/component.sandbox_spec.volume.disk_name", schema.Doc("Name of the disk to attach (for disk provider)"))
	sb.String("filesystem", "dev.miren.compute/component.sandbox_spec.volume.filesystem", schema.Doc("Filesystem type for auto-creation (for disk provider)"))
	sb.Label("labels", "dev.miren.compute/component.sandbox_spec.volume.labels", schema.Doc("Labels identifying the volume"), schema.Many)
	sb.String("lease_timeout", "dev.miren.compute/component.sandbox_spec.volume.lease_timeout", schema.Doc("Timeout for acquiring disk lease (for disk provider)"))
	sb.String("mount_path", "dev.miren.compute/component.sandbox_spec.volume.mount_path", schema.Doc("Path where disk should be mounted (for disk provider)"))
	sb.String("name", "dev.miren.compute/component.sandbox_spec.volume.name", schema.Doc("Volume name"))
	sb.String("provider", "dev.miren.compute/component.sandbox_spec.volume.provider", schema.Doc("Volume provider"))
	sb.Bool("read_only", "dev.miren.compute/component.sandbox_spec.volume.read_only", schema.Doc("Whether to mount disk as read-only (for disk provider)"))
	sb.Int64("size_gb", "dev.miren.compute/component.sandbox_spec.volume.size_gb", schema.Doc("Disk size in GB for auto-creation (for disk provider)"))
}

const (
	LeaseLastHeartbeatId = entity.Id("dev.miren.compute/lease.last_heartbeat")
	LeaseProjectId       = entity.Id("dev.miren.compute/lease.project")
	LeaseSandboxId       = entity.Id("dev.miren.compute/lease.sandbox")
)

type Lease struct {
	ID            entity.Id `json:"id"`
	LastHeartbeat time.Time `cbor:"last_heartbeat,omitempty" json:"last_heartbeat,omitempty"`
	Project       entity.Id `cbor:"project,omitempty" json:"project,omitempty"`
	Sandbox       entity.Id `cbor:"sandbox,omitempty" json:"sandbox,omitempty"`
}

func (o *Lease) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(LeaseLastHeartbeatId); ok && a.Value.Kind() == entity.KindTime {
		o.LastHeartbeat = a.Value.Time()
	}
	if a, ok := e.Get(LeaseProjectId); ok && a.Value.Kind() == entity.KindId {
		o.Project = a.Value.Id()
	}
	if a, ok := e.Get(LeaseSandboxId); ok && a.Value.Kind() == entity.KindId {
		o.Sandbox = a.Value.Id()
	}
}

func (o *Lease) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindLease)
}

func (o *Lease) ShortKind() string {
	return "lease"
}

func (o *Lease) Kind() entity.Id {
	return KindLease
}

func (o *Lease) EntityId() entity.Id {
	return o.ID
}

func (o *Lease) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.LastHeartbeat) {
		attrs = append(attrs, entity.Time(LeaseLastHeartbeatId, o.LastHeartbeat))
	}
	if !entity.Empty(o.Project) {
		attrs = append(attrs, entity.Ref(LeaseProjectId, o.Project))
	}
	if !entity.Empty(o.Sandbox) {
		attrs = append(attrs, entity.Ref(LeaseSandboxId, o.Sandbox))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindLease))
	return
}

func (o *Lease) Empty() bool {
	if !entity.Empty(o.LastHeartbeat) {
		return false
	}
	if !entity.Empty(o.Project) {
		return false
	}
	if !entity.Empty(o.Sandbox) {
		return false
	}
	return true
}

func (o *Lease) InitSchema(sb *schema.SchemaBuilder) {
	sb.Time("last_heartbeat", "dev.miren.compute/lease.last_heartbeat", schema.Doc("The last time the lease was updated"))
	sb.Ref("project", "dev.miren.compute/lease.project", schema.Doc("Which project currently holds the lease"), schema.Indexed)
	sb.Ref("sandbox", "dev.miren.compute/lease.sandbox", schema.Doc("The sandbox that is leased"), schema.Indexed)
}

const (
	NodeApiAddressId      = entity.Id("dev.miren.compute/node.api_address")
	NodeConstraintsId     = entity.Id("dev.miren.compute/node.constraints")
	NodeStatusId          = entity.Id("dev.miren.compute/node.status")
	NodeStatusUnknownId   = entity.Id("dev.miren.compute/status.unknown")
	NodeStatusReadyId     = entity.Id("dev.miren.compute/status.ready")
	NodeStatusDisabledId  = entity.Id("dev.miren.compute/status.disabled")
	NodeStatusUnhealthyId = entity.Id("dev.miren.compute/status.unhealthy")
)

type Node struct {
	ID          entity.Id    `json:"id"`
	ApiAddress  string       `cbor:"api_address,omitempty" json:"api_address,omitempty"`
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

var nodestatusFromId = map[entity.Id]NodeStatus{NodeStatusUnknownId: UNKNOWN, NodeStatusReadyId: READY, NodeStatusDisabledId: DISABLED, NodeStatusUnhealthyId: UNHEALTHY}
var nodestatusToId = map[NodeStatus]entity.Id{UNKNOWN: NodeStatusUnknownId, READY: NodeStatusReadyId, DISABLED: NodeStatusDisabledId, UNHEALTHY: NodeStatusUnhealthyId}

func (o *Node) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(NodeApiAddressId); ok && a.Value.Kind() == entity.KindString {
		o.ApiAddress = a.Value.String()
	}
	for _, a := range e.GetAll(NodeConstraintsId) {
		if a.Value.Kind() == entity.KindLabel {
			o.Constraints = append(o.Constraints, a.Value.Label())
		}
	}
	if a, ok := e.Get(NodeStatusId); ok && a.Value.Kind() == entity.KindId {
		o.Status = nodestatusFromId[a.Value.Id()]
	}
}

func (o *Node) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindNode)
}

func (o *Node) ShortKind() string {
	return "node"
}

func (o *Node) Kind() entity.Id {
	return KindNode
}

func (o *Node) EntityId() entity.Id {
	return o.ID
}

func (o *Node) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.ApiAddress) {
		attrs = append(attrs, entity.String(NodeApiAddressId, o.ApiAddress))
	}
	for _, v := range o.Constraints {
		attrs = append(attrs, entity.Label(NodeConstraintsId, v.Key, v.Value))
	}
	if a, ok := nodestatusToId[o.Status]; ok {
		attrs = append(attrs, entity.Ref(NodeStatusId, a))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindNode))
	return
}

func (o *Node) Empty() bool {
	if !entity.Empty(o.ApiAddress) {
		return false
	}
	if len(o.Constraints) != 0 {
		return false
	}
	if o.Status != "" {
		return false
	}
	return true
}

func (o *Node) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("api_address", "dev.miren.compute/node.api_address", schema.Doc("The address to connect the node at"))
	sb.Label("constraints", "dev.miren.compute/node.constraints", schema.Doc("The label constraints the node has, used for scheduling"), schema.Many)
	sb.Singleton("dev.miren.compute/status.unknown")
	sb.Singleton("dev.miren.compute/status.ready")
	sb.Singleton("dev.miren.compute/status.disabled")
	sb.Singleton("dev.miren.compute/status.unhealthy")
	sb.Ref("status", "dev.miren.compute/node.status", schema.Doc("The status of the node"), schema.Session, schema.Choices(NodeStatusUnknownId, NodeStatusReadyId, NodeStatusDisabledId, NodeStatusUnhealthyId))
}

const (
	SandboxContainerId      = entity.Id("dev.miren.compute/sandbox.container")
	SandboxHostNetworkId    = entity.Id("dev.miren.compute/sandbox.hostNetwork")
	SandboxLabelsId         = entity.Id("dev.miren.compute/sandbox.labels")
	SandboxLastActivityId   = entity.Id("dev.miren.compute/sandbox.last_activity")
	SandboxLogAttributeId   = entity.Id("dev.miren.compute/sandbox.logAttribute")
	SandboxLogEntityId      = entity.Id("dev.miren.compute/sandbox.logEntity")
	SandboxNetworkId        = entity.Id("dev.miren.compute/sandbox.network")
	SandboxRouteId          = entity.Id("dev.miren.compute/sandbox.route")
	SandboxSpecId           = entity.Id("dev.miren.compute/sandbox.spec")
	SandboxStaticHostId     = entity.Id("dev.miren.compute/sandbox.static_host")
	SandboxStatusId         = entity.Id("dev.miren.compute/sandbox.status")
	SandboxStatusPendingId  = entity.Id("dev.miren.compute/status.pending")
	SandboxStatusNotReadyId = entity.Id("dev.miren.compute/status.not_ready")
	SandboxStatusRunningId  = entity.Id("dev.miren.compute/status.running")
	SandboxStatusStoppedId  = entity.Id("dev.miren.compute/status.stopped")
	SandboxStatusDeadId     = entity.Id("dev.miren.compute/status.dead")
	SandboxVolumeId         = entity.Id("dev.miren.compute/sandbox.volume")
)

type Sandbox struct {
	ID           entity.Id     `json:"id"`
	Container    []Container   `cbor:"container" json:"container"`
	HostNetwork  bool          `cbor:"hostNetwork,omitempty" json:"hostNetwork,omitempty"`
	Labels       []string      `cbor:"labels,omitempty" json:"labels,omitempty"`
	LastActivity time.Time     `cbor:"last_activity,omitempty" json:"last_activity,omitempty"`
	LogAttribute types.Labels  `cbor:"logAttribute,omitempty" json:"logAttribute,omitempty"`
	LogEntity    string        `cbor:"logEntity,omitempty" json:"logEntity,omitempty"`
	Network      []Network     `cbor:"network,omitempty" json:"network,omitempty"`
	Route        []Route       `cbor:"route,omitempty" json:"route,omitempty"`
	Spec         SandboxSpec   `cbor:"spec,omitempty" json:"spec,omitempty"`
	StaticHost   []StaticHost  `cbor:"static_host,omitempty" json:"static_host,omitempty"`
	Status       SandboxStatus `cbor:"status,omitempty" json:"status,omitempty"`
	Volume       []Volume      `cbor:"volume,omitempty" json:"volume,omitempty"`
}

type SandboxStatus string

const (
	PENDING   SandboxStatus = "status.pending"
	NOT_READY SandboxStatus = "status.not_ready"
	RUNNING   SandboxStatus = "status.running"
	STOPPED   SandboxStatus = "status.stopped"
	DEAD      SandboxStatus = "status.dead"
)

var sandboxstatusFromId = map[entity.Id]SandboxStatus{SandboxStatusPendingId: PENDING, SandboxStatusNotReadyId: NOT_READY, SandboxStatusRunningId: RUNNING, SandboxStatusStoppedId: STOPPED, SandboxStatusDeadId: DEAD}
var sandboxstatusToId = map[SandboxStatus]entity.Id{PENDING: SandboxStatusPendingId, NOT_READY: SandboxStatusNotReadyId, RUNNING: SandboxStatusRunningId, STOPPED: SandboxStatusStoppedId, DEAD: SandboxStatusDeadId}

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
	if a, ok := e.Get(SandboxLastActivityId); ok && a.Value.Kind() == entity.KindTime {
		o.LastActivity = a.Value.Time()
	}
	for _, a := range e.GetAll(SandboxLogAttributeId) {
		if a.Value.Kind() == entity.KindLabel {
			o.LogAttribute = append(o.LogAttribute, a.Value.Label())
		}
	}
	if a, ok := e.Get(SandboxLogEntityId); ok && a.Value.Kind() == entity.KindString {
		o.LogEntity = a.Value.String()
	}
	for _, a := range e.GetAll(SandboxNetworkId) {
		if a.Value.Kind() == entity.KindComponent {
			var v Network
			v.Decode(a.Value.Component())
			o.Network = append(o.Network, v)
		}
	}
	for _, a := range e.GetAll(SandboxRouteId) {
		if a.Value.Kind() == entity.KindComponent {
			var v Route
			v.Decode(a.Value.Component())
			o.Route = append(o.Route, v)
		}
	}
	if a, ok := e.Get(SandboxSpecId); ok && a.Value.Kind() == entity.KindComponent {
		o.Spec.Decode(a.Value.Component())
	}
	for _, a := range e.GetAll(SandboxStaticHostId) {
		if a.Value.Kind() == entity.KindComponent {
			var v StaticHost
			v.Decode(a.Value.Component())
			o.StaticHost = append(o.StaticHost, v)
		}
	}
	if a, ok := e.Get(SandboxStatusId); ok && a.Value.Kind() == entity.KindId {
		o.Status = sandboxstatusFromId[a.Value.Id()]
	}
	for _, a := range e.GetAll(SandboxVolumeId) {
		if a.Value.Kind() == entity.KindComponent {
			var v Volume
			v.Decode(a.Value.Component())
			o.Volume = append(o.Volume, v)
		}
	}
}

func (o *Sandbox) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindSandbox)
}

func (o *Sandbox) ShortKind() string {
	return "sandbox"
}

func (o *Sandbox) Kind() entity.Id {
	return KindSandbox
}

func (o *Sandbox) EntityId() entity.Id {
	return o.ID
}

func (o *Sandbox) Encode() (attrs []entity.Attr) {
	for _, v := range o.Container {
		attrs = append(attrs, entity.Component(SandboxContainerId, v.Encode()))
	}
	attrs = append(attrs, entity.Bool(SandboxHostNetworkId, o.HostNetwork))
	for _, v := range o.Labels {
		attrs = append(attrs, entity.String(SandboxLabelsId, v))
	}
	if !entity.Empty(o.LastActivity) {
		attrs = append(attrs, entity.Time(SandboxLastActivityId, o.LastActivity))
	}
	for _, v := range o.LogAttribute {
		attrs = append(attrs, entity.Label(SandboxLogAttributeId, v.Key, v.Value))
	}
	if !entity.Empty(o.LogEntity) {
		attrs = append(attrs, entity.String(SandboxLogEntityId, o.LogEntity))
	}
	for _, v := range o.Network {
		attrs = append(attrs, entity.Component(SandboxNetworkId, v.Encode()))
	}
	for _, v := range o.Route {
		attrs = append(attrs, entity.Component(SandboxRouteId, v.Encode()))
	}
	if !o.Spec.Empty() {
		attrs = append(attrs, entity.Component(SandboxSpecId, o.Spec.Encode()))
	}
	for _, v := range o.StaticHost {
		attrs = append(attrs, entity.Component(SandboxStaticHostId, v.Encode()))
	}
	if a, ok := sandboxstatusToId[o.Status]; ok {
		attrs = append(attrs, entity.Ref(SandboxStatusId, a))
	}
	for _, v := range o.Volume {
		attrs = append(attrs, entity.Component(SandboxVolumeId, v.Encode()))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindSandbox))
	return
}

func (o *Sandbox) Empty() bool {
	if len(o.Container) != 0 {
		return false
	}
	if !entity.Empty(o.HostNetwork) {
		return false
	}
	if len(o.Labels) != 0 {
		return false
	}
	if !entity.Empty(o.LastActivity) {
		return false
	}
	if len(o.LogAttribute) != 0 {
		return false
	}
	if !entity.Empty(o.LogEntity) {
		return false
	}
	if len(o.Network) != 0 {
		return false
	}
	if len(o.Route) != 0 {
		return false
	}
	if !o.Spec.Empty() {
		return false
	}
	if len(o.StaticHost) != 0 {
		return false
	}
	if o.Status != "" {
		return false
	}
	if len(o.Volume) != 0 {
		return false
	}
	return true
}

func (o *Sandbox) InitSchema(sb *schema.SchemaBuilder) {
	sb.Component("container", "dev.miren.compute/sandbox.container", schema.Doc("A container running in the sandbox"), schema.Many, schema.Required)
	(&Container{}).InitSchema(sb.Builder("sandbox.container"))
	sb.Bool("hostNetwork", "dev.miren.compute/sandbox.hostNetwork", schema.Doc("Indicates if the container should use the networking of\nnode that it is running on directly\n"))
	sb.String("labels", "dev.miren.compute/sandbox.labels", schema.Doc("Label for the sandbox"), schema.Many)
	sb.Time("last_activity", "dev.miren.compute/sandbox.last_activity", schema.Doc("Last lease activity (throttled updates, ~30s granularity for scale-down)"))
	sb.Label("logAttribute", "dev.miren.compute/sandbox.logAttribute", schema.Doc("Labels that will be associated with the log entries generated by the sandbox"), schema.Many)
	sb.String("logEntity", "dev.miren.compute/sandbox.logEntity", schema.Doc("The entity to associate the log output of the sandbox with"))
	sb.Component("network", "dev.miren.compute/sandbox.network", schema.Doc("Network accessability for the container"), schema.Many)
	(&Network{}).InitSchema(sb.Builder("sandbox.network"))
	sb.Component("route", "dev.miren.compute/sandbox.route", schema.Doc("A network route the container uses"), schema.Many)
	(&Route{}).InitSchema(sb.Builder("sandbox.route"))
	sb.Component("spec", "dev.miren.compute/sandbox.spec", schema.Doc("Immutable sandbox configuration"))
	sb.Component("static_host", "dev.miren.compute/sandbox.static_host", schema.Doc("A name to ip mapping configured staticly for the sandbox"), schema.Many)
	(&StaticHost{}).InitSchema(sb.Builder("sandbox.static_host"))
	sb.Singleton("dev.miren.compute/status.pending")
	sb.Singleton("dev.miren.compute/status.not_ready")
	sb.Singleton("dev.miren.compute/status.running")
	sb.Singleton("dev.miren.compute/status.stopped")
	sb.Singleton("dev.miren.compute/status.dead")
	sb.Ref("status", "dev.miren.compute/sandbox.status", schema.Doc("The status of the pod"), schema.Choices(SandboxStatusPendingId, SandboxStatusNotReadyId, SandboxStatusRunningId, SandboxStatusStoppedId, SandboxStatusDeadId))
	sb.Component("volume", "dev.miren.compute/sandbox.volume", schema.Doc("A volume that is available for binding into containers"), schema.Many)
	(&Volume{}).InitSchema(sb.Builder("sandbox.volume"))
}

const (
	ContainerCommandId    = entity.Id("dev.miren.compute/container.command")
	ContainerConfigFileId = entity.Id("dev.miren.compute/container.config_file")
	ContainerDirectoryId  = entity.Id("dev.miren.compute/container.directory")
	ContainerEnvId        = entity.Id("dev.miren.compute/container.env")
	ContainerImageId      = entity.Id("dev.miren.compute/container.image")
	ContainerMountId      = entity.Id("dev.miren.compute/container.mount")
	ContainerNameId       = entity.Id("dev.miren.compute/container.name")
	ContainerOomScoreId   = entity.Id("dev.miren.compute/container.oom_score")
	ContainerPortId       = entity.Id("dev.miren.compute/container.port")
	ContainerPrivilegedId = entity.Id("dev.miren.compute/container.privileged")
)

type Container struct {
	Command    string       `cbor:"command,omitempty" json:"command,omitempty"`
	ConfigFile []ConfigFile `cbor:"config_file,omitempty" json:"config_file,omitempty"`
	Directory  string       `cbor:"directory,omitempty" json:"directory,omitempty"`
	Env        []string     `cbor:"env,omitempty" json:"env,omitempty"`
	Image      string       `cbor:"image" json:"image"`
	Mount      []Mount      `cbor:"mount,omitempty" json:"mount,omitempty"`
	Name       string       `cbor:"name,omitempty" json:"name,omitempty"`
	OomScore   int64        `cbor:"oom_score,omitempty" json:"oom_score,omitempty"`
	Port       []Port       `cbor:"port,omitempty" json:"port,omitempty"`
	Privileged bool         `cbor:"privileged,omitempty" json:"privileged,omitempty"`
}

func (o *Container) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(ContainerCommandId); ok && a.Value.Kind() == entity.KindString {
		o.Command = a.Value.String()
	}
	for _, a := range e.GetAll(ContainerConfigFileId) {
		if a.Value.Kind() == entity.KindComponent {
			var v ConfigFile
			v.Decode(a.Value.Component())
			o.ConfigFile = append(o.ConfigFile, v)
		}
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
	for _, a := range e.GetAll(ContainerPortId) {
		if a.Value.Kind() == entity.KindComponent {
			var v Port
			v.Decode(a.Value.Component())
			o.Port = append(o.Port, v)
		}
	}
	if a, ok := e.Get(ContainerPrivilegedId); ok && a.Value.Kind() == entity.KindBool {
		o.Privileged = a.Value.Bool()
	}
}

func (o *Container) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Command) {
		attrs = append(attrs, entity.String(ContainerCommandId, o.Command))
	}
	for _, v := range o.ConfigFile {
		attrs = append(attrs, entity.Component(ContainerConfigFileId, v.Encode()))
	}
	if !entity.Empty(o.Directory) {
		attrs = append(attrs, entity.String(ContainerDirectoryId, o.Directory))
	}
	for _, v := range o.Env {
		attrs = append(attrs, entity.String(ContainerEnvId, v))
	}
	if !entity.Empty(o.Image) {
		attrs = append(attrs, entity.String(ContainerImageId, o.Image))
	}
	for _, v := range o.Mount {
		attrs = append(attrs, entity.Component(ContainerMountId, v.Encode()))
	}
	if !entity.Empty(o.Name) {
		attrs = append(attrs, entity.String(ContainerNameId, o.Name))
	}
	if !entity.Empty(o.OomScore) {
		attrs = append(attrs, entity.Int64(ContainerOomScoreId, o.OomScore))
	}
	for _, v := range o.Port {
		attrs = append(attrs, entity.Component(ContainerPortId, v.Encode()))
	}
	attrs = append(attrs, entity.Bool(ContainerPrivilegedId, o.Privileged))
	return
}

func (o *Container) Empty() bool {
	if !entity.Empty(o.Command) {
		return false
	}
	if len(o.ConfigFile) != 0 {
		return false
	}
	if !entity.Empty(o.Directory) {
		return false
	}
	if len(o.Env) != 0 {
		return false
	}
	if !entity.Empty(o.Image) {
		return false
	}
	if len(o.Mount) != 0 {
		return false
	}
	if !entity.Empty(o.Name) {
		return false
	}
	if !entity.Empty(o.OomScore) {
		return false
	}
	if len(o.Port) != 0 {
		return false
	}
	if !entity.Empty(o.Privileged) {
		return false
	}
	return true
}

func (o *Container) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("command", "dev.miren.compute/container.command", schema.Doc("Command to run in the container"))
	sb.Component("config_file", "dev.miren.compute/container.config_file", schema.Doc("A file to write into the container before starting"), schema.Many)
	(&ConfigFile{}).InitSchema(sb.Builder("container.config_file"))
	sb.String("directory", "dev.miren.compute/container.directory", schema.Doc("Directory to start in"))
	sb.String("env", "dev.miren.compute/container.env", schema.Doc("Environment variable for the container"), schema.Many)
	sb.String("image", "dev.miren.compute/container.image", schema.Doc("Container image"), schema.Required)
	sb.Component("mount", "dev.miren.compute/container.mount", schema.Doc("A mounted directory"), schema.Many)
	(&Mount{}).InitSchema(sb.Builder("container.mount"))
	sb.String("name", "dev.miren.compute/container.name", schema.Doc("Container name"))
	sb.Int64("oom_score", "dev.miren.compute/container.oom_score", schema.Doc("How to adjust the OOM score for this container"))
	sb.Component("port", "dev.miren.compute/container.port", schema.Doc("A network port the container declares"), schema.Many)
	(&Port{}).InitSchema(sb.Builder("container.port"))
	sb.Bool("privileged", "dev.miren.compute/container.privileged", schema.Doc("Whether or not the container runs in privileged mode"))
}

const (
	ConfigFileDataId = entity.Id("dev.miren.compute/config_file.data")
	ConfigFileModeId = entity.Id("dev.miren.compute/config_file.mode")
	ConfigFilePathId = entity.Id("dev.miren.compute/config_file.path")
)

type ConfigFile struct {
	Data string `cbor:"data,omitempty" json:"data,omitempty"`
	Mode string `cbor:"mode,omitempty" json:"mode,omitempty"`
	Path string `cbor:"path,omitempty" json:"path,omitempty"`
}

func (o *ConfigFile) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(ConfigFileDataId); ok && a.Value.Kind() == entity.KindString {
		o.Data = a.Value.String()
	}
	if a, ok := e.Get(ConfigFileModeId); ok && a.Value.Kind() == entity.KindString {
		o.Mode = a.Value.String()
	}
	if a, ok := e.Get(ConfigFilePathId); ok && a.Value.Kind() == entity.KindString {
		o.Path = a.Value.String()
	}
}

func (o *ConfigFile) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Data) {
		attrs = append(attrs, entity.String(ConfigFileDataId, o.Data))
	}
	if !entity.Empty(o.Mode) {
		attrs = append(attrs, entity.String(ConfigFileModeId, o.Mode))
	}
	if !entity.Empty(o.Path) {
		attrs = append(attrs, entity.String(ConfigFilePathId, o.Path))
	}
	return
}

func (o *ConfigFile) Empty() bool {
	if !entity.Empty(o.Data) {
		return false
	}
	if !entity.Empty(o.Mode) {
		return false
	}
	if !entity.Empty(o.Path) {
		return false
	}
	return true
}

func (o *ConfigFile) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("data", "dev.miren.compute/config_file.data", schema.Doc("The configuration data"))
	sb.String("mode", "dev.miren.compute/config_file.mode", schema.Doc("The file mode to set the configuration to"))
	sb.String("path", "dev.miren.compute/config_file.path", schema.Doc("The path in the container to write the data"))
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
	if !entity.Empty(o.Destination) {
		attrs = append(attrs, entity.String(MountDestinationId, o.Destination))
	}
	if !entity.Empty(o.Source) {
		attrs = append(attrs, entity.String(MountSourceId, o.Source))
	}
	return
}

func (o *Mount) Empty() bool {
	if !entity.Empty(o.Destination) {
		return false
	}
	if !entity.Empty(o.Source) {
		return false
	}
	return true
}

func (o *Mount) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("destination", "dev.miren.compute/mount.destination", schema.Doc("Mount destination path"))
	sb.String("source", "dev.miren.compute/mount.source", schema.Doc("Mount source path"))
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
	if a, ok := PortprotocolToId[o.Protocol]; ok {
		attrs = append(attrs, entity.Ref(PortProtocolId, a))
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
	if !entity.Empty(o.Type) {
		return false
	}
	return true
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
	if !entity.Empty(o.Address) {
		attrs = append(attrs, entity.String(NetworkAddressId, o.Address))
	}
	if !entity.Empty(o.Subnet) {
		attrs = append(attrs, entity.String(NetworkSubnetId, o.Subnet))
	}
	return
}

func (o *Network) Empty() bool {
	if !entity.Empty(o.Address) {
		return false
	}
	if !entity.Empty(o.Subnet) {
		return false
	}
	return true
}

func (o *Network) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("address", "dev.miren.compute/network.address", schema.Doc("A network address to reach the container at"))
	sb.String("subnet", "dev.miren.compute/network.subnet", schema.Doc("The subnet that the address is associated with"))
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
	if !entity.Empty(o.Destination) {
		attrs = append(attrs, entity.String(RouteDestinationId, o.Destination))
	}
	if !entity.Empty(o.Gateway) {
		attrs = append(attrs, entity.String(RouteGatewayId, o.Gateway))
	}
	return
}

func (o *Route) Empty() bool {
	if !entity.Empty(o.Destination) {
		return false
	}
	if !entity.Empty(o.Gateway) {
		return false
	}
	return true
}

func (o *Route) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("destination", "dev.miren.compute/route.destination", schema.Doc("The network destination"))
	sb.String("gateway", "dev.miren.compute/route.gateway", schema.Doc("The next hop for the destination"))
}

const (
	StaticHostHostId = entity.Id("dev.miren.compute/static_host.host")
	StaticHostIpId   = entity.Id("dev.miren.compute/static_host.ip")
)

type StaticHost struct {
	Host string `cbor:"host,omitempty" json:"host,omitempty"`
	Ip   string `cbor:"ip,omitempty" json:"ip,omitempty"`
}

func (o *StaticHost) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(StaticHostHostId); ok && a.Value.Kind() == entity.KindString {
		o.Host = a.Value.String()
	}
	if a, ok := e.Get(StaticHostIpId); ok && a.Value.Kind() == entity.KindString {
		o.Ip = a.Value.String()
	}
}

func (o *StaticHost) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Host) {
		attrs = append(attrs, entity.String(StaticHostHostId, o.Host))
	}
	if !entity.Empty(o.Ip) {
		attrs = append(attrs, entity.String(StaticHostIpId, o.Ip))
	}
	return
}

func (o *StaticHost) Empty() bool {
	if !entity.Empty(o.Host) {
		return false
	}
	if !entity.Empty(o.Ip) {
		return false
	}
	return true
}

func (o *StaticHost) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("host", "dev.miren.compute/static_host.host", schema.Doc("The hostname"))
	sb.String("ip", "dev.miren.compute/static_host.ip", schema.Doc("The IP"))
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
	if !entity.Empty(o.Name) {
		attrs = append(attrs, entity.String(VolumeNameId, o.Name))
	}
	if !entity.Empty(o.Provider) {
		attrs = append(attrs, entity.String(VolumeProviderId, o.Provider))
	}
	return
}

func (o *Volume) Empty() bool {
	if len(o.Labels) != 0 {
		return false
	}
	if !entity.Empty(o.Name) {
		return false
	}
	if !entity.Empty(o.Provider) {
		return false
	}
	return true
}

func (o *Volume) InitSchema(sb *schema.SchemaBuilder) {
	sb.Label("labels", "dev.miren.compute/volume.labels", schema.Doc("Labels that identify the volume to the provider"), schema.Many)
	sb.String("name", "dev.miren.compute/volume.name", schema.Doc("The name of the volume"))
	sb.String("provider", "dev.miren.compute/volume.provider", schema.Doc("What provider should provide the volume"))
}

const (
	SandboxPoolCurrentInstancesId     = entity.Id("dev.miren.compute/sandbox_pool.current_instances")
	SandboxPoolDesiredInstancesId     = entity.Id("dev.miren.compute/sandbox_pool.desired_instances")
	SandboxPoolReadyInstancesId       = entity.Id("dev.miren.compute/sandbox_pool.ready_instances")
	SandboxPoolReferencedByVersionsId = entity.Id("dev.miren.compute/sandbox_pool.referenced_by_versions")
	SandboxPoolSandboxSpecId          = entity.Id("dev.miren.compute/sandbox_pool.sandbox_spec")
	SandboxPoolServiceId              = entity.Id("dev.miren.compute/sandbox_pool.service")
)

type SandboxPool struct {
	ID                   entity.Id   `json:"id"`
	CurrentInstances     int64       `cbor:"current_instances,omitempty" json:"current_instances,omitempty"`
	DesiredInstances     int64       `cbor:"desired_instances,omitempty" json:"desired_instances,omitempty"`
	ReadyInstances       int64       `cbor:"ready_instances,omitempty" json:"ready_instances,omitempty"`
	ReferencedByVersions []entity.Id `cbor:"referenced_by_versions,omitempty" json:"referenced_by_versions,omitempty"`
	SandboxSpec          SandboxSpec `cbor:"sandbox_spec,omitempty" json:"sandbox_spec,omitempty"`
	Service              string      `cbor:"service,omitempty" json:"service,omitempty"`
}

func (o *SandboxPool) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(SandboxPoolCurrentInstancesId); ok && a.Value.Kind() == entity.KindInt64 {
		o.CurrentInstances = a.Value.Int64()
	}
	if a, ok := e.Get(SandboxPoolDesiredInstancesId); ok && a.Value.Kind() == entity.KindInt64 {
		o.DesiredInstances = a.Value.Int64()
	}
	if a, ok := e.Get(SandboxPoolReadyInstancesId); ok && a.Value.Kind() == entity.KindInt64 {
		o.ReadyInstances = a.Value.Int64()
	}
	for _, a := range e.GetAll(SandboxPoolReferencedByVersionsId) {
		if a.Value.Kind() == entity.KindId {
			o.ReferencedByVersions = append(o.ReferencedByVersions, a.Value.Id())
		}
	}
	if a, ok := e.Get(SandboxPoolSandboxSpecId); ok && a.Value.Kind() == entity.KindComponent {
		o.SandboxSpec.Decode(a.Value.Component())
	}
	if a, ok := e.Get(SandboxPoolServiceId); ok && a.Value.Kind() == entity.KindString {
		o.Service = a.Value.String()
	}
}

func (o *SandboxPool) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindSandboxPool)
}

func (o *SandboxPool) ShortKind() string {
	return "sandbox_pool"
}

func (o *SandboxPool) Kind() entity.Id {
	return KindSandboxPool
}

func (o *SandboxPool) EntityId() entity.Id {
	return o.ID
}

func (o *SandboxPool) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.CurrentInstances) {
		attrs = append(attrs, entity.Int64(SandboxPoolCurrentInstancesId, o.CurrentInstances))
	}
	if !entity.Empty(o.DesiredInstances) {
		attrs = append(attrs, entity.Int64(SandboxPoolDesiredInstancesId, o.DesiredInstances))
	}
	if !entity.Empty(o.ReadyInstances) {
		attrs = append(attrs, entity.Int64(SandboxPoolReadyInstancesId, o.ReadyInstances))
	}
	for _, v := range o.ReferencedByVersions {
		attrs = append(attrs, entity.Ref(SandboxPoolReferencedByVersionsId, v))
	}
	if !o.SandboxSpec.Empty() {
		attrs = append(attrs, entity.Component(SandboxPoolSandboxSpecId, o.SandboxSpec.Encode()))
	}
	if !entity.Empty(o.Service) {
		attrs = append(attrs, entity.String(SandboxPoolServiceId, o.Service))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindSandboxPool))
	return
}

func (o *SandboxPool) Empty() bool {
	if !entity.Empty(o.CurrentInstances) {
		return false
	}
	if !entity.Empty(o.DesiredInstances) {
		return false
	}
	if !entity.Empty(o.ReadyInstances) {
		return false
	}
	if len(o.ReferencedByVersions) != 0 {
		return false
	}
	if !o.SandboxSpec.Empty() {
		return false
	}
	if !entity.Empty(o.Service) {
		return false
	}
	return true
}

func (o *SandboxPool) InitSchema(sb *schema.SchemaBuilder) {
	sb.Int64("current_instances", "dev.miren.compute/sandbox_pool.current_instances", schema.Doc("Current number of sandbox instances (non-STOPPED)"))
	sb.Int64("desired_instances", "dev.miren.compute/sandbox_pool.desired_instances", schema.Doc("Target number of sandbox instances"))
	sb.Int64("ready_instances", "dev.miren.compute/sandbox_pool.ready_instances", schema.Doc("Number of RUNNING sandboxes"))
	sb.Ref("referenced_by_versions", "dev.miren.compute/sandbox_pool.referenced_by_versions", schema.Doc("AppVersions that reference this pool (enables reuse when specs match)"), schema.Many, schema.Indexed)
	sb.Component("sandbox_spec", "dev.miren.compute/sandbox_pool.sandbox_spec", schema.Doc("Complete sandbox specification template (includes version ref to AppVersion)"))
	sb.String("service", "dev.miren.compute/sandbox_pool.service", schema.Doc("Service name (e.g., web, worker) - pool identifier"), schema.Indexed)
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

func (o *Schedule) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindSchedule)
}

func (o *Schedule) ShortKind() string {
	return "schedule"
}

func (o *Schedule) Kind() entity.Id {
	return KindSchedule
}

func (o *Schedule) EntityId() entity.Id {
	return o.ID
}

func (o *Schedule) Encode() (attrs []entity.Attr) {
	if !o.Key.Empty() {
		attrs = append(attrs, entity.Component(ScheduleKeyId, o.Key.Encode()))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindSchedule))
	return
}

func (o *Schedule) Empty() bool {
	if !o.Key.Empty() {
		return false
	}
	return true
}

func (o *Schedule) InitSchema(sb *schema.SchemaBuilder) {
	sb.Component("key", "dev.miren.compute/schedule.key", schema.Doc("The scheduling key for an entity"), schema.Indexed)
	(&Key{}).InitSchema(sb.Builder("schedule.key"))
}

const (
	KeyKindId = entity.Id("dev.miren.compute/key.kind")
	KeyNodeId = entity.Id("dev.miren.compute/key.node")
)

type Key struct {
	Kind entity.Id `cbor:"kind,omitempty" json:"kind,omitempty"`
	Node entity.Id `cbor:"node,omitempty" json:"node,omitempty"`
}

func (o *Key) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(KeyKindId); ok && a.Value.Kind() == entity.KindId {
		o.Kind = a.Value.Id()
	}
	if a, ok := e.Get(KeyNodeId); ok && a.Value.Kind() == entity.KindId {
		o.Node = a.Value.Id()
	}
}

func (o *Key) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Kind) {
		attrs = append(attrs, entity.Ref(KeyKindId, o.Kind))
	}
	if !entity.Empty(o.Node) {
		attrs = append(attrs, entity.Ref(KeyNodeId, o.Node))
	}
	return
}

func (o *Key) Empty() bool {
	if !entity.Empty(o.Kind) {
		return false
	}
	if !entity.Empty(o.Node) {
		return false
	}
	return true
}

func (o *Key) InitSchema(sb *schema.SchemaBuilder) {
	sb.Ref("kind", "dev.miren.compute/key.kind", schema.Doc("The type of entity this is"))
	sb.Ref("node", "dev.miren.compute/key.node", schema.Doc("The node id the entity is scheduled for"))
}

var (
	KindLease       = entity.Id("dev.miren.compute/kind.lease")
	KindNode        = entity.Id("dev.miren.compute/kind.node")
	KindSandbox     = entity.Id("dev.miren.compute/kind.sandbox")
	KindSandboxPool = entity.Id("dev.miren.compute/kind.sandbox_pool")
	KindSchedule    = entity.Id("dev.miren.compute/kind.schedule")
	Schema          = entity.Id("dev.miren.compute/schema.v1alpha")
)

func init() {
	schema.Register("dev.miren.compute", "v1alpha", func(sb *schema.SchemaBuilder) {
		(&SandboxSpec{}).InitSchema(sb)
		(&Lease{}).InitSchema(sb)
		(&Node{}).InitSchema(sb)
		(&Sandbox{}).InitSchema(sb)
		(&SandboxPool{}).InitSchema(sb)
		(&Schedule{}).InitSchema(sb)
	})
	schema.RegisterEncodedSchema("dev.miren.compute", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\xec\\ˎ\xec4\x13~\x8d\xff\xfc\xff\xcf\xe1&nB\xca\x00\xe2.nG\xc0\x96W\x88\xdcqu\xdaӉ\x9dc;=\xdd\xec\x00\x81\x00\xf1\x14\x9c\x19\xde\x10\xd6(\xb6\x938\x89\x9d8nvd3\x8aݮ\xaf\\\x17\x97])g\xee1E%<\xc5pJJ\u0081&\x19+\xabZ\x02\x1c\t\xc5\xe2\xe1\xfc\xbf\xc9/7\xcd/\te\x18\xfeP\xb4\xa7\xe9\x88\xe6G\r\xf0\xd7\x1e\xb3\x12\x11:e\xb0\xdf\x13(\xb0\xf8\xe1َ\xe0\xf3Kn\x8c\x04U$E\x18s\x10B\xf1:\xda\x1d\xf2R\xc1^HNh~?\a\x921*$G\x84J\x81KD/\x7fj(\xbb\xbb\x81\x82\x02\xed\xa0PH\xcfy\x90\x84D\xb2\xd63ٛ\xe7\x86\x12\x03\xad\xcbc\xf3'=\xa1\xa2\x06q\x0f\x1c\x10\xbe\x9c\x1fOq4Y\xa2~\xcfkz\xa4쎞_\xf0\x8e3#\x0e\x98\b\xb4+\x00\x9f_\xf4\x0em\x87\x90\x9a\x1e\x00\x15\xf2pqi\xa4\xc35c\xf2\x13pA\x18\xcdOo\xa3\xa2:\xa0\xa2\xe2\xa4D\xfc\x926\xe6Í\xd4\xe7\xff{\\\xa0\x00$\x8c\x0f\xdcM\x87\xa8_W9\xc1\xab\x1e\x90\xa4@B\xa6\a@\\\xee\x00IŐ\x8e\xfa\x94\x19$)A!=\xefC\xaa8\xbb\x85LC\xe4m\xa3\xa1\xdd\x11<O)\x10\xc5;v֔m\xc3P\xce\xea\x10\x14\xbd\xcb\x15\x94\x12\r\x94V\xe3\xf9\x91\xc3`z@\xa0&\x7f{h\xa4x\xd9\v\xd3,\x06\x89\b\x05n-\x05\xd2w6\x12\x91\x86\x86Q\xa0\xb2\x7f2\xf3\x9b\x02'\x13\xe0\xc0\x99\xfe\xfa\xcc3\xd3\x0e\xa8\xe9)Qㅍ\xceۆ\xb5ꕬ\xaf\xcd#\xd0=\xc9\xd3=)`\xb4\xf4\xbb\xee\x05\x89o\x02$\xb6\xd9\\\x1b\xf6,\xa8\x04#\x89\xd4,\xb0z\xb2$\x0f\xa1.\x19\x06M\xad\x9eVRWH\x1e4\xb5z\xb2\xa8g\xbd\xfdVc4\x10\x8a\xcb+s\xd6\xc1\x84C&\x19\xbfh/\xec\x9b\xe3\xc8\xeeX\x95=\nГe۬i\x8eeu\x04͞\x9e\x94(\u05ca\x02\xfd8\xf6\xb0Y\xea\x92\xd5TZ\xfcAw,x\xd5\x1b!^\xa5\x90\x02\xfd\xe9{\xdfjR \t\x06!\tE\x920\xaaW\x80\xdd1֖#Ti\x14\xc1j\x9e\x81\xd9\xfe\xf4s\xa8_h\xb5(x\xc7f\xd7ˬ<\xae\xff3\x9eڬ;1V\xa6\"c\\Ӓ\xbe٠d\x84ʇE\xf6\x15\xe3\xb61\xb1j/\xd8\xf2\xf5\x10[6@\x81\xa6\xfcQI\xea8w5\x18K\nrH\xa7\xc9\x18\x86TI\xa3t\xd37[\xdd\xcc2\xed\b\xf1\x80Ʒ65\rg\x92e\xacPt\x87\xae\xe5</\xfd\x9eɬr\xf9]K\x96Ȭ\xcaj<?\xa6\xc6լ\x14\x8au\xa7\xb5`\xd7U2\xfb\x0e(\x96\x8599\x91\x02r\xd0\xfbխ\xd5V\xecv\x8c\x15\xb3|\xfa]\xd8\xe7\xea\xad[\x1d\x98\x90߀\xbcc\xfc\xa8W\xb3\xdd\xd11\xbb\xf7\xf8C\x8b\xa2\x8e\xbb\xf6\x89xoz\xc6>\xe5\xd8a{\f!S\x94Ir\"RG\xf1r\xd8՝\xcb\xee=\n\xec\x90X\xfeDJNv\xb5\xb4\xb7\xeab\xd0\xdf\x1f\xd3}\xe1\u0382\xfb\x9a\xcavR\xa4o\x06\x04\xf7\x16\x83\x1a\x8d\xf6\xb3ɩ\xa5䙐0\x05MF\xa0\xabºc\x8e\x06&\xb1\x93\xa3ܑ\x18\xf9\x82BK/\xea\x1d\x05iB\xba~\x0e]\x17\xad2\x1e<Q\xa0\x95\x98\xb3\xa1IAw,\xa8p\n\x98\f\x00\xaf\xdd\x17\x15Ⱥ}\xd1!\xa3Fɑ\x84;\xa4]-o\x1b\xc1;\xa3¸\xf7l\xbc\xad̢\x82L\xc7.\xf5\xb4zK\xba醴jL\x1b\xa0@-\xfe\xacl\xfcN(\xea\x95IƔO\xb2\xc4gU\xce\xf1\xf1z9\xc2R\x91Ϣ\x80\xaf\xcdP\xa6\\\x17\xd5\x15\x9d\xb0|y\x9d\x84K\x19͵\xf0\v)ϵ\xf0\x919\xd1\xf9\xb1\xc1n\xa0;\xe4Q\xa2\xf4I\xc4܂\xf3\xa7\xf7#\xc0\x03Ҫ\x0f#`\x17\xb3\xad\x18и$l\xcaiy\xe1\xac\xcfɾ\x8a\x95g\xdd\xe6\xf4y4\x9b+\xb2\xba\xf3#\x97g\xf7\xa9\xde\a\x11\x93ZHpb\xd6IXb\x183٘|q\xcag\xd9\xedV\xa7\x8f1j\n\xc9/\x9fD\xe3\x06%\xa0\xd1Ӟ\xcbP\xbf\x88\x06]\x9d\xc2\xc6,\xf6\x01\xab.ѽ\x1e\xa9M\x87\xa3u\x1a\x99/\x9f\xff\xe3\n\n]\x12\xfdi\xcct\xaeͭ\xa5kJj:\xef\x06OgE\xd6\xfd^0hL\xda\x1b~\x0e\x0f\u0382\x93`ȸln\x8a\xef\x8by듻\xf0\xe3|D\xce\x17~r\xfa\aR\xc1\xca\xf2R\x05\xf7\xb0\xceC\x85D\x92di\xe3\x97v.aw/\xd8i\xca\xcbg'\vt\x95\xb5>\x8a\x91F\xad=\x1d\x8a:)l+\x85o\xdd6(\xa9\x14\xe4\x8eT\xc1\x16\xaa-\vi\xa8\x06I\xcd\xe1\xad\xe09\x18\x06\xdaGچ)%*\x8bOkN^(Vԥ\xbd\x1c\xf7\xa6g}Uk\x96C\xa0\x89\x7fYib\r\x9e`\"\x8eiw\xe6 }sl\xe7\xf0\xa5n\x90\x9b\xf4J\\\x84\x84Ro\x1cV;>G2س\xefO\xadp\x1d\xbe\xe1\xb5\xc0\x80\x04\xa4\x92\x94\xc0ji^\xaa\x0e\xba\xaeV\x8b:\xa1\xa7]:{k\xb5\xc7\xd8\xe1\xd1\xc7`/\x1c \xc3\xd3;\x83Wqv\"\x18xw\bӭ1\xeej\xa7\xe3\x80p\xcaha\xf6ƾ\xd9m\xe5+C\x8b\xc1\x15\xe4[H\xf3\x9d\xb9(`\x1a\xedyt6\xb8<\xb5\x82\x8b\x06\x9b\x1d^\xd8\xdc\x1f\x16J\x06\xd7o\rS\xf0\xc4\x01\xbej/\xf0\\O\t\f\xfa\x9eK3WD\xf7\xdb>\xa4/\x15OBo\x02=`\f\xc8y\xa1\xa8\xbd\xb5\x03\b\xe7\x15PLh>s\x0fȌ\xc8yM\xe9\xfcH3\"\x17\x92U\x15x\xd5T\x8bČ \x94\xc9T\xdfW\xf2\xdf\x17\xea\xc6\xf8j\xa8\xadbbw\xa4)d2\x84\f}O\xe9+J\xae\x8c\xda\x0e\x8b\x85\x058G\xb1&8\x92\xcdz\xe7> &\xb47\x93\\\n\xd0\u05cd\xb2\x03\xe0\xba0\u05f6\xce\xffu\x18Ҍ\b\xd4\xf7w\xdeʅ\xc1I\x8e\xa0Cl\xd6<,x\xc1\x14'\xb1qVE\x17\x87lG\xb8$\xfa^[c9\xf5d_\xfe\xf2P\xd0\xee\xad2m\xdf*/]\xfajd\x9d\x1dph\xc5rզ\xec\x8bai\xc5X\xe1\xd5\u038d=*P;?\xf9\x8e\xab6V\x92՜\x03\x95)\xa1B\"\x9a\x81\x8euO\xa7݃\xf77K\xa8\x18\x04\xe1\x80Ǩ\xd3\xee\x01\xaa#;\x1d\xa0\xaa\xc04\xc2d\xe3\xce\xc1+!G\x82>B\xdc\x03\a\x9a\x01Nw\x97\xd4\x18Ҏ\x1a'\xcf\b\xe3\x1c\x8aɛ\vL\xec\xfd[g\xfd\x83\x9e\xadƷ\xd5\xf8\xb6\x1a\xdfV\xe3\xdbj|[\x8do\xab\xf1m5\xbe\xadƷ\xd5\xf8\xb6\x1a\xdfV\xe3\xdbj|[\x8do\xab\xf1m5\xbe\xadƷ\xd5\xf8\xb6\x1a߿\xa8\xc6\xe7\xfb\x06i\xf8R\x11\xf8\x89\x98T/o\x1b\xa1\xa1\xaf\xb0\xa1\xc6#\x8f\xe2\xc0\xb8L\xf5\xff\x05\xd0_\x86\xcf\xfds\x00\xf3\xdd\xf3\xec\xc7\xe3]\xb9b\xe1\xeb\xe8\xfem\xf9R]c Aл\xf5\xbf\x01\x00\x00\xff\xff\x01\x00\x00\xff\xff\x87\xfa4m\x01A\x00\x00"))
}
