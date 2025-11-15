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
	SandboxPoolAppId                   = entity.Id("dev.miren.compute/sandbox_pool.app")
	SandboxPoolConsecutiveCrashCountId = entity.Id("dev.miren.compute/sandbox_pool.consecutive_crash_count")
	SandboxPoolCooldownUntilId         = entity.Id("dev.miren.compute/sandbox_pool.cooldown_until")
	SandboxPoolCurrentInstancesId      = entity.Id("dev.miren.compute/sandbox_pool.current_instances")
	SandboxPoolDesiredInstancesId      = entity.Id("dev.miren.compute/sandbox_pool.desired_instances")
	SandboxPoolLastCrashTimeId         = entity.Id("dev.miren.compute/sandbox_pool.last_crash_time")
	SandboxPoolReadyInstancesId        = entity.Id("dev.miren.compute/sandbox_pool.ready_instances")
	SandboxPoolReferencedByVersionsId  = entity.Id("dev.miren.compute/sandbox_pool.referenced_by_versions")
	SandboxPoolSandboxLabelsId         = entity.Id("dev.miren.compute/sandbox_pool.sandbox_labels")
	SandboxPoolSandboxPrefixId         = entity.Id("dev.miren.compute/sandbox_pool.sandbox_prefix")
	SandboxPoolSandboxSpecId           = entity.Id("dev.miren.compute/sandbox_pool.sandbox_spec")
	SandboxPoolServiceId               = entity.Id("dev.miren.compute/sandbox_pool.service")
)

type SandboxPool struct {
	ID                    entity.Id    `json:"id"`
	App                   entity.Id    `cbor:"app,omitempty" json:"app,omitempty"`
	ConsecutiveCrashCount int64        `cbor:"consecutive_crash_count,omitempty" json:"consecutive_crash_count,omitempty"`
	CooldownUntil         time.Time    `cbor:"cooldown_until,omitempty" json:"cooldown_until,omitempty"`
	CurrentInstances      int64        `cbor:"current_instances,omitempty" json:"current_instances,omitempty"`
	DesiredInstances      int64        `cbor:"desired_instances,omitempty" json:"desired_instances,omitempty"`
	LastCrashTime         time.Time    `cbor:"last_crash_time,omitempty" json:"last_crash_time,omitempty"`
	ReadyInstances        int64        `cbor:"ready_instances,omitempty" json:"ready_instances,omitempty"`
	ReferencedByVersions  []entity.Id  `cbor:"referenced_by_versions,omitempty" json:"referenced_by_versions,omitempty"`
	SandboxLabels         types.Labels `cbor:"sandbox_labels,omitempty" json:"sandbox_labels,omitempty"`
	SandboxPrefix         string       `cbor:"sandbox_prefix,omitempty" json:"sandbox_prefix,omitempty"`
	SandboxSpec           SandboxSpec  `cbor:"sandbox_spec,omitempty" json:"sandbox_spec,omitempty"`
	Service               string       `cbor:"service,omitempty" json:"service,omitempty"`
}

func (o *SandboxPool) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(SandboxPoolAppId); ok && a.Value.Kind() == entity.KindId {
		o.App = a.Value.Id()
	}
	if a, ok := e.Get(SandboxPoolConsecutiveCrashCountId); ok && a.Value.Kind() == entity.KindInt64 {
		o.ConsecutiveCrashCount = a.Value.Int64()
	}
	if a, ok := e.Get(SandboxPoolCooldownUntilId); ok && a.Value.Kind() == entity.KindTime {
		o.CooldownUntil = a.Value.Time()
	}
	if a, ok := e.Get(SandboxPoolCurrentInstancesId); ok && a.Value.Kind() == entity.KindInt64 {
		o.CurrentInstances = a.Value.Int64()
	}
	if a, ok := e.Get(SandboxPoolDesiredInstancesId); ok && a.Value.Kind() == entity.KindInt64 {
		o.DesiredInstances = a.Value.Int64()
	}
	if a, ok := e.Get(SandboxPoolLastCrashTimeId); ok && a.Value.Kind() == entity.KindTime {
		o.LastCrashTime = a.Value.Time()
	}
	if a, ok := e.Get(SandboxPoolReadyInstancesId); ok && a.Value.Kind() == entity.KindInt64 {
		o.ReadyInstances = a.Value.Int64()
	}
	for _, a := range e.GetAll(SandboxPoolReferencedByVersionsId) {
		if a.Value.Kind() == entity.KindId {
			o.ReferencedByVersions = append(o.ReferencedByVersions, a.Value.Id())
		}
	}
	for _, a := range e.GetAll(SandboxPoolSandboxLabelsId) {
		if a.Value.Kind() == entity.KindLabel {
			o.SandboxLabels = append(o.SandboxLabels, a.Value.Label())
		}
	}
	if a, ok := e.Get(SandboxPoolSandboxPrefixId); ok && a.Value.Kind() == entity.KindString {
		o.SandboxPrefix = a.Value.String()
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
	if !entity.Empty(o.App) {
		attrs = append(attrs, entity.Ref(SandboxPoolAppId, o.App))
	}
	if !entity.Empty(o.ConsecutiveCrashCount) {
		attrs = append(attrs, entity.Int64(SandboxPoolConsecutiveCrashCountId, o.ConsecutiveCrashCount))
	}
	if !entity.Empty(o.CooldownUntil) {
		attrs = append(attrs, entity.Time(SandboxPoolCooldownUntilId, o.CooldownUntil))
	}
	if !entity.Empty(o.CurrentInstances) {
		attrs = append(attrs, entity.Int64(SandboxPoolCurrentInstancesId, o.CurrentInstances))
	}
	if !entity.Empty(o.DesiredInstances) {
		attrs = append(attrs, entity.Int64(SandboxPoolDesiredInstancesId, o.DesiredInstances))
	}
	if !entity.Empty(o.LastCrashTime) {
		attrs = append(attrs, entity.Time(SandboxPoolLastCrashTimeId, o.LastCrashTime))
	}
	if !entity.Empty(o.ReadyInstances) {
		attrs = append(attrs, entity.Int64(SandboxPoolReadyInstancesId, o.ReadyInstances))
	}
	for _, v := range o.ReferencedByVersions {
		attrs = append(attrs, entity.Ref(SandboxPoolReferencedByVersionsId, v))
	}
	for _, v := range o.SandboxLabels {
		attrs = append(attrs, entity.Label(SandboxPoolSandboxLabelsId, v.Key, v.Value))
	}
	if !entity.Empty(o.SandboxPrefix) {
		attrs = append(attrs, entity.String(SandboxPoolSandboxPrefixId, o.SandboxPrefix))
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
	if !entity.Empty(o.App) {
		return false
	}
	if !entity.Empty(o.ConsecutiveCrashCount) {
		return false
	}
	if !entity.Empty(o.CooldownUntil) {
		return false
	}
	if !entity.Empty(o.CurrentInstances) {
		return false
	}
	if !entity.Empty(o.DesiredInstances) {
		return false
	}
	if !entity.Empty(o.LastCrashTime) {
		return false
	}
	if !entity.Empty(o.ReadyInstances) {
		return false
	}
	if len(o.ReferencedByVersions) != 0 {
		return false
	}
	if len(o.SandboxLabels) != 0 {
		return false
	}
	if !entity.Empty(o.SandboxPrefix) {
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
	sb.Ref("app", "dev.miren.compute/sandbox_pool.app", schema.Doc("Reference to the app this pool belongs to"), schema.Indexed, schema.Tags("dev.miren.app_ref"))
	sb.Int64("consecutive_crash_count", "dev.miren.compute/sandbox_pool.consecutive_crash_count", schema.Doc("Number of consecutive quick crashes (sandboxes that died within 60s of creation)"))
	sb.Time("cooldown_until", "dev.miren.compute/sandbox_pool.cooldown_until", schema.Doc("Timestamp until which new sandbox creation is paused due to crash loop"))
	sb.Int64("current_instances", "dev.miren.compute/sandbox_pool.current_instances", schema.Doc("Current number of sandbox instances (non-STOPPED)"))
	sb.Int64("desired_instances", "dev.miren.compute/sandbox_pool.desired_instances", schema.Doc("Target number of sandbox instances"))
	sb.Time("last_crash_time", "dev.miren.compute/sandbox_pool.last_crash_time", schema.Doc("Timestamp of the most recent quick crash"))
	sb.Int64("ready_instances", "dev.miren.compute/sandbox_pool.ready_instances", schema.Doc("Number of RUNNING sandboxes"))
	sb.Ref("referenced_by_versions", "dev.miren.compute/sandbox_pool.referenced_by_versions", schema.Doc("AppVersions that reference this pool (enables reuse when specs match)"), schema.Many, schema.Indexed)
	sb.Label("sandbox_labels", "dev.miren.compute/sandbox_pool.sandbox_labels", schema.Doc("Labels that will be added to the metadata of sandboxes created from this pool"), schema.Many)
	sb.String("sandbox_prefix", "dev.miren.compute/sandbox_pool.sandbox_prefix", schema.Doc("Prefix used when generating sandbox entity names (e.g., \"myapp-web\" produces \"myapp-web-abc123\")"))
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
	schema.RegisterEncodedSchema("dev.miren.compute", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\xec\\َ\xec4\x13~\x8d\xff\xfc\xff\xcfa\x13\x9b\x10\x19@\xecb;\x02ny\x85\xc8\x1dW\xa7=\x9d\xd89\xb6\xd3\xd3\xcd\x1d $@<\x05g\x867\x84k\xe4%\x89\x93؉\x93\xe6\n\xe5f\x14;\xae\xaf\\\x8bˮ\x94\xa7\xef1E%<\xc5pJJ\u0081&\x19+\xabZ\x02\x1c\t\xc5\xe2\xe1\xfc\xbfћ\x1b\xf5&\xa1\f\xc3\x1f\x9a\xf64\x1e\xa1^\x1a\x80\xbf\xf6\x98\x95\x88\xd01\x83\xfd\x9e@\x81ŏ\xcfv\x04\x9f_\xf2c$\xa8\")\u0098\x83\x10\x9a\xd7\xd1퐗\n\xf6BrB\xf3\xfb)\x90\x8cQ!9\"T\n\\\"z\xf9\xd3@\xb9\xdd\n\n\n\xb4\x83B#=\x17@\x12\x12\xc9\xda\xccdo\x9f\x15%\x06Z\x97G\xf5'=\xa1\xa2\x06q\x0f\x1c\x10\xbe\x9c\x1f\x8fq\fY\xa2\xdf\xe75=RvG\xcf/\x04\xc7\xd9\x11\aL\x04\xda\x15\x80\xcf/\x06\x876CHM\x0f\x80\ny\xb8\xf84\xd2\xe2\xda1\xf9\t\xb8 \x8c\xe6\xa7wPQ\x1dPQqR\"~I\x95\xf9\xb0\x92\xfa\xfc\xff\x80\v\x14\x80\x84\xf5\x81\xbb\xf1\x10\xfdv\x91\x13\xbc\x1a\x00I\n$dz\x00\xc4\xe5\x0e\x90\xd4\f\xe9\xa0O\x9bA\x92\x124\xd2\xf3!\xa4\x8a\xb3[\xc8\fD\xde4\x14\xed\x8e\xe0iJ\x81(ޱ\xb3\xa1l\x1a\x96rR\x87\xa0\xe9}\xae\xa0\x95h\xa1\x8c\x1aϏ<\x063\x03\"5\xf9ۃ\x92\xe2\xe5 \x8cZ\f\x12\x11\n\xdcY\n\xa4\xebT\x12\x11E\xc3(P\xd9=\xd9\xf9\x8d\x81\x93\x11p\xe4L\x7f}\x16\x98i\v\xa4zJ\xa4\xbcP\xe9\xbci8\xab^\xcb\xfa\xda4\x02ݓ<ݓ\x02\x06K\xbf힑\xf8&Bb\x97͵aρJ0\x92H\xcf\x02\xeb'G\xf2\x18\xea\x92a0\xd4\xfai!u\x85\xe4\xc1P\xeb'\x87z\xd2\xdbo\r\x86\x82\xd0\\^\x99\xb2\x0e&\x1c2\xc9\xf8\xc5xa\xd7\x1cFvϪ\xecP\x80\x9e\x1c\xdbf\xaa9\x94\xd5\x134;zR\xa2\xdc(\n\xcc\xe3\xd0\xc3&\xa9KVS\xe9\xf0\a\xd31\xe3Uo\xc4x\x95F\x8a\xf4\xa7\x1fB\xabI\x83$\x18\x84$\x14I¨Y\x01n\xc7P[\x9ePeP\x04\xaby\x06v\xfb3ϱ~aԢ\xe1=\x9b]'\xb3\xf6\xb8\xee\xcfpj\x93\xee\xc4X\x99\x8a\x8cqCK\xba\xa6B\xc9\b\x95\x0f\xb3\xec+\xc6]cbݞ\xb1\xe5\xeb1\xb6T@\x91\xa6\xfcIK\xea9w)\x8c9\x05y\xa43d\fC\xaa\xa5Ѻ隍n&\x99\xb6\x84\xb8G\x13Z\x9b\x86\x863\xc92Vh\xbaC\xdb\xf2\x9e\x97~\xcfdV\xf9\xfc\xae!KdVe5\x9e\x1eS\xe3jR\nͺ\xd5Z\xb4\xebj\x99C\a\x14\xc7\u009c\x9cH\x019\x98\xfd\xea\xd6ikv;ƊI>\xdd.\x1cr\xf5ƭ\x0eL\xc8oA\xde1~4\xab\xd9\xedh\x99\xdd\a\xfc\xa1A\xd1\xc7]\xf7D\xbc\xb7=C\x9f\xf2\xec\xb0\x1d\x86\x90)\xca$9\x11i\xa2x\xd9\xefj\xcfe\xf7\x01\x05\xb6H,\x7f\"%'\xbbZ\xba[u\xd1\xeb\xef\x8e\xe9\xa1p\xe7\xc0}Ce3)\xd25#\x82{\x83A\xadF\xbb\xd9\xe4\xd4Q\xf2DH\x18\x83&\x03\xd0Ea\xdd3G\v\x93\xb8\xc9Q\xeeI\x8cBA\xa1\xa1\x17\xf5\x8e\x82\xb4!\xdd<Ǯ\x8bF\x19\x0f\x81(\xd0H\xccYߤ`:fT8\x06Lz\x80\xd7\xee\x8b\x1adپ\xe8\x91Ѡ\xe4H\xc2\x1d2\xae\x967\x8d\xe8\x9dQc\xdc\a6\xdeFfQAfb\x97~Z\xbc%ݴC\x1a5\xa6\n(R\x8b?k\x1b\xbf\x1b\x8bze\x921\xe6\x93\xcc\xf1Y\x94s|\xb2\\\x8e\xb8T\xe4\xf3U\xc0\xd7f(c\xae\xb3\xeaZ\x9d\xb0|u\x9d\x84s\x19͵\xf03)ϵ\xf0+s\xa2\xf3c\x8b\xad\xa0[\xe4A\xa2\xf4銹E\xe7O\x1f\xac\x00\x8fH\xab>Z\x01;\x9bm\xad\x01]\x97\x84\x8d9\xcd/\x9c\xe59\xd9\xd7k\xe5Y\xb69}\xb1\x9a\xcd\x15Y\xdd\xf9\x91ϳ\xbbT\xef\xc3\x15\x93\x9aIp֬\x93\xb8\xc4p\xcdd\xd7\xe4\x8bc>\xf3n\xb78}\\\xa3\xa6\x98\xfc\xf2\xc9jܨ\x04t\xf5\xb4\xa72\xd4/W\x83.Na\xd7,\xf6\x1e\xab6ѽ\x1e\xa9I\x87W\xebte\xbe|\xfe\x8f/(\xb4I\xf4gk\xa6smn-}S\xd2\xd3y/z:\v\xb2\xee\xf7\xa3Aפ\xbd\xf1\xe7\xf0\xe8,8\x89\x86\\\x97͍\xf1C1oyr\x17\x7f\x9c_\x91\xf3ş\x9c\xfe\x81T\xb0r\xbcT\xc3=,\xf3P!\x91$Y\xaa\xfc\xd2\xcd%\xdc\xee\x19;\x8dy\x85\xec\xe4\x80.\xb2\xd6\xc7k\xa4\xd1kτ\xa2V\n\xd7J\xf1[\xb7\vJ*\r\xb9#U\xb4\x85j\xc7B\x06J!\xe99\xbc\x1d=\a\xcb\xc0\xf8HӰ\xa5Dm\xf1q\xcd)\bŊ\xbat\x97\xe3\xde\xf6,\xafjMr\x884\xf1/\vMl\xc0\x13L\xc41m\xcf\x1c\xa4k\x0e\xed\x1c\xbf\xd4-\xb2J\xaf\xc4EH(\xcd\xc6\xe1\xb4\xd7\xe7H\x16{\xf2\xfb\xa9\x13\xae\xe37\xbc\x06\x18\x90\x80T\x92\x12X-\xedG\xd5^\xd7\xd5j\xd1'\xf4\xb4Mgo\x9d\xf6\x10;>\xfaX\xec\x99\x03d|zg\xf1*\xceN\x04\x03o\x0fa\xa65\xc4]\xect\x1c\x10N\x19-\xec\xde\xd85ۭ|ah\xb1\xb8\x82|\ai\xbe\xb3\x17\x05l\xa39\x8fN\x06\x97\xa7Np1`\x93\xc3\v\x97\xfb\xc3L\xc9\xe0\xfa\xada\f\x9ex\xc0\x17\xed\x05\x81\xeb)\x91A?pi\xe6\x8a\xe8~ۅ\xf4\xb9\xe2I\xecM\xa0\a\x8c\x01y/\x145\xb7v\x00\xe1\xbc\x02\x8a\t\xcd'\xee\x01\xd9\x119\xaf)\x9d\x1eiG\xe4B\xb2\xaa\x82\xa0\x9aj\x91\xd8\x11\x842\x99\x9a\xfbJ\xe1\xfbB\xed\x98P\r\xb5Q\xcc\xda\x1di\f\x99\xf4!c\xbfS\x86\x8a\x92\v\xa3\xb6\xc7bq\x01\xceS\xac\x89\x8ed\x93\u07b9\x8f\x88\t\xcd\xcd$\x9f\x02\xccu\xa3\xec\x00\xb8.쵭\xf3\x7f=\x86\xb4#\"\xf5\xfd}\xb0raq\x92#\x98\x10\x9b\xa9\x87\x19/\x18\xe3$.\u03a2\xe8\xe2\x91\xed\b\x97\xc4\xdckS\x96\xd3O\xee\xe5\xaf\x00\x05m\xbf*\xd3\xe6\xab\xf2ܥ/%\xeb\xe4\x80C#\x96\xaf6\xe5^\fK+Ɗ\xa0vn\xdcQ\xb1WĂ\xb1\xd7\xc1JPe\xe2g\xa6\x1e\\%y\xceI=\u008cQ\x01Y-\xc9\tҌ#qH3\xfd1V_\x13\f\xbd\xec}\x04zk\x96\x03+0\xbb\xa3iM%1\x1fg蠯\x7f\x1d\xd0s4\xef\x03֜\x03\x95)\xa1B\"\x9a\x81\x89\xebO\xc7ݽiΡb\x10\x84\x03\x1e\xa2\x8e\xbb{\xa8\x9eL\xbc\x87\xaa\xcb\xebFuJ>\x8dɆ\x9d}\xf1\xe7 u\\\x1fL\x93\r;{_\xd4<\xdf7\x06\x88{\xe0@3\xc0\xe9\xee\x92\xdau\xe0\x06\xddS`\x84u\xb4\xfb\x187h\x1a\xa3\x88N\ao\x06\x91=\x16\xb7\xe2\xb0'\xe7>\xa2\xed\x1b\xe6\x0eoFB\xb6U\xdc\xde\xd9m\xab\xe6n\xd5ܭ\x9a\xbbUs\xb7j\xeeV\xcdݪ\xb9\xb3\x90[5w\xab\xe6n\xd5ܭ\x9a\xbbUs\xb7j\xeeV\xcdݪ\xb9[5w\xab\xe6n\xd5ܭ\x9a\xfb/\xaa\xe6\x86\xfe۬\xffQ\x11\xf8\x89\xd8T/o\x1a\xb1\xa1\xafp\xa1\x86#\x8f\xe2\xc0\xb8L\xcd/@\x98\xdf\x00\x98\xfa\x19\b\xfb\x1f\xee\x93?\x13\xd0\x16\xa6f\xfe\x0f\xbe\xab\x8b\xccU\xb0z\x12DUQ\xfe\x06\x00\x00\xff\xff\x01\x00\x00\xff\xffUԓ\xe1\xebB\x00\x00"))
}
