package session_v1alpha

import (
	"time"

	entity "miren.dev/runtime/pkg/entity"
	schema "miren.dev/runtime/pkg/entity/schema"
	types "miren.dev/runtime/pkg/entity/types"
)

const (
	SandboxSpecContainerId           = entity.Id("dev.miren.session/component.sandbox_spec.container")
	SandboxSpecHostNetworkId         = entity.Id("dev.miren.session/component.sandbox_spec.hostNetwork")
	SandboxSpecLogAttributeId        = entity.Id("dev.miren.session/component.sandbox_spec.logAttribute")
	SandboxSpecLogEntityId           = entity.Id("dev.miren.session/component.sandbox_spec.logEntity")
	SandboxSpecPortWaitTimeoutId     = entity.Id("dev.miren.session/component.sandbox_spec.port_wait_timeout")
	SandboxSpecRestartPolicyId       = entity.Id("dev.miren.session/component.sandbox_spec.restart_policy")
	SandboxSpecRestartPolicyAlwaysId = entity.Id("dev.miren.session/component.sandbox_spec.restart_policy.always")
	SandboxSpecRestartPolicyNeverId  = entity.Id("dev.miren.session/component.sandbox_spec.restart_policy.never")
	SandboxSpecRouteId               = entity.Id("dev.miren.session/component.sandbox_spec.route")
	SandboxSpecStaticHostId          = entity.Id("dev.miren.session/component.sandbox_spec.static_host")
	SandboxSpecVersionId             = entity.Id("dev.miren.session/component.sandbox_spec.version")
	SandboxSpecVolumeId              = entity.Id("dev.miren.session/component.sandbox_spec.volume")
)

type SandboxSpec struct {
	Container       []SandboxSpecContainer   `cbor:"container" json:"container"`
	HostNetwork     bool                     `cbor:"hostNetwork,omitempty" json:"hostNetwork,omitempty"`
	LogAttribute    types.Labels             `cbor:"logAttribute,omitempty" json:"logAttribute,omitempty"`
	LogEntity       string                   `cbor:"logEntity,omitempty" json:"logEntity,omitempty"`
	PortWaitTimeout string                   `cbor:"port_wait_timeout,omitempty" json:"port_wait_timeout,omitempty"`
	RestartPolicy   SandboxSpecRestartPolicy `cbor:"restart_policy,omitempty" json:"restart_policy,omitempty"`
	Route           []SandboxSpecRoute       `cbor:"route,omitempty" json:"route,omitempty"`
	StaticHost      []SandboxSpecStaticHost  `cbor:"static_host,omitempty" json:"static_host,omitempty"`
	Version         entity.Id                `cbor:"version,omitempty" json:"version,omitempty"`
	Volume          []SandboxSpecVolume      `cbor:"volume,omitempty" json:"volume,omitempty"`
}

type SandboxSpecRestartPolicy string

const (
	SandboxSpecALWAYS SandboxSpecRestartPolicy = "component.sandbox_spec.restart_policy.always"
	SandboxSpecNEVER  SandboxSpecRestartPolicy = "component.sandbox_spec.restart_policy.never"
)

var sandbox_specrestart_policyFromId = map[entity.Id]SandboxSpecRestartPolicy{SandboxSpecRestartPolicyAlwaysId: SandboxSpecALWAYS, SandboxSpecRestartPolicyNeverId: SandboxSpecNEVER}
var sandbox_specrestart_policyToId = map[SandboxSpecRestartPolicy]entity.Id{SandboxSpecALWAYS: SandboxSpecRestartPolicyAlwaysId, SandboxSpecNEVER: SandboxSpecRestartPolicyNeverId}

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
	if a, ok := e.Get(SandboxSpecPortWaitTimeoutId); ok && a.Value.Kind() == entity.KindString {
		o.PortWaitTimeout = a.Value.String()
	}
	if a, ok := e.Get(SandboxSpecRestartPolicyId); ok && a.Value.Kind() == entity.KindId {
		o.RestartPolicy = sandbox_specrestart_policyFromId[a.Value.Id()]
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
	if !entity.Empty(o.PortWaitTimeout) {
		attrs = append(attrs, entity.String(SandboxSpecPortWaitTimeoutId, o.PortWaitTimeout))
	}
	if a, ok := sandbox_specrestart_policyToId[o.RestartPolicy]; ok {
		attrs = append(attrs, entity.Ref(SandboxSpecRestartPolicyId, a))
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
	if !entity.Empty(o.PortWaitTimeout) {
		return false
	}
	if o.RestartPolicy != "" {
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
	sb.Component("container", "dev.miren.session/component.sandbox_spec.container", schema.Doc("Container specification"), schema.Many, schema.Required)
	(&SandboxSpecContainer{}).InitSchema(sb.Builder("component.sandbox_spec.container"))
	sb.Bool("hostNetwork", "dev.miren.session/component.sandbox_spec.hostNetwork", schema.Doc("Whether to use host networking"))
	sb.Label("logAttribute", "dev.miren.session/component.sandbox_spec.logAttribute", schema.Doc("Labels for log entries"), schema.Many)
	sb.String("logEntity", "dev.miren.session/component.sandbox_spec.logEntity", schema.Doc("Entity to associate log output with"))
	sb.String("port_wait_timeout", "dev.miren.session/component.sandbox_spec.port_wait_timeout", schema.Doc("Maximum time to wait for container ports to bind"))
	sb.Singleton("dev.miren.session/component.sandbox_spec.restart_policy.always")
	sb.Singleton("dev.miren.session/component.sandbox_spec.restart_policy.never")
	sb.Ref("restart_policy", "dev.miren.session/component.sandbox_spec.restart_policy", schema.Doc("Whether the sandbox controller may restart this sandbox's containers"), schema.Choices(SandboxSpecRestartPolicyAlwaysId, SandboxSpecRestartPolicyNeverId))
	sb.Component("route", "dev.miren.session/component.sandbox_spec.route", schema.Doc("Network route configuration"), schema.Many)
	(&SandboxSpecRoute{}).InitSchema(sb.Builder("component.sandbox_spec.route"))
	sb.Component("static_host", "dev.miren.session/component.sandbox_spec.static_host", schema.Doc("Static host-to-IP mapping"), schema.Many)
	(&SandboxSpecStaticHost{}).InitSchema(sb.Builder("component.sandbox_spec.static_host"))
	sb.Ref("version", "dev.miren.session/component.sandbox_spec.version", schema.Doc("Application version reference"))
	sb.Component("volume", "dev.miren.session/component.sandbox_spec.volume", schema.Doc("Volume configuration"), schema.Many)
	(&SandboxSpecVolume{}).InitSchema(sb.Builder("component.sandbox_spec.volume"))
}

const (
	SandboxSpecContainerArgsId            = entity.Id("dev.miren.session/component.sandbox_spec.container.args")
	SandboxSpecContainerCommandId         = entity.Id("dev.miren.session/component.sandbox_spec.container.command")
	SandboxSpecContainerConfigFileId      = entity.Id("dev.miren.session/component.sandbox_spec.container.config_file")
	SandboxSpecContainerDirectoryId       = entity.Id("dev.miren.session/component.sandbox_spec.container.directory")
	SandboxSpecContainerEnvId             = entity.Id("dev.miren.session/component.sandbox_spec.container.env")
	SandboxSpecContainerImageId           = entity.Id("dev.miren.session/component.sandbox_spec.container.image")
	SandboxSpecContainerMountId           = entity.Id("dev.miren.session/component.sandbox_spec.container.mount")
	SandboxSpecContainerNameId            = entity.Id("dev.miren.session/component.sandbox_spec.container.name")
	SandboxSpecContainerOomScoreId        = entity.Id("dev.miren.session/component.sandbox_spec.container.oom_score")
	SandboxSpecContainerPortId            = entity.Id("dev.miren.session/component.sandbox_spec.container.port")
	SandboxSpecContainerPrivilegedId      = entity.Id("dev.miren.session/component.sandbox_spec.container.privileged")
	SandboxSpecContainerShutdownTimeoutId = entity.Id("dev.miren.session/component.sandbox_spec.container.shutdown_timeout")
	SandboxSpecContainerStdinId           = entity.Id("dev.miren.session/component.sandbox_spec.container.stdin")
	SandboxSpecContainerTtyId             = entity.Id("dev.miren.session/component.sandbox_spec.container.tty")
)

type SandboxSpecContainer struct {
	Args            []string                         `cbor:"args,omitempty" json:"args,omitempty"`
	Command         string                           `cbor:"command,omitempty" json:"command,omitempty"`
	ConfigFile      []SandboxSpecContainerConfigFile `cbor:"config_file,omitempty" json:"config_file,omitempty"`
	Directory       string                           `cbor:"directory,omitempty" json:"directory,omitempty"`
	Env             []string                         `cbor:"env,omitempty" json:"env,omitempty"`
	Image           string                           `cbor:"image" json:"image"`
	Mount           []SandboxSpecContainerMount      `cbor:"mount,omitempty" json:"mount,omitempty"`
	Name            string                           `cbor:"name,omitempty" json:"name,omitempty"`
	OomScore        int64                            `cbor:"oom_score,omitempty" json:"oom_score,omitempty"`
	Port            []SandboxSpecContainerPort       `cbor:"port,omitempty" json:"port,omitempty"`
	Privileged      bool                             `cbor:"privileged,omitempty" json:"privileged,omitempty"`
	ShutdownTimeout string                           `cbor:"shutdown_timeout,omitempty" json:"shutdown_timeout,omitempty"`
	Stdin           bool                             `cbor:"stdin,omitempty" json:"stdin,omitempty"`
	Tty             bool                             `cbor:"tty,omitempty" json:"tty,omitempty"`
}

func (o *SandboxSpecContainer) Decode(e entity.AttrGetter) {
	for _, a := range e.GetAll(SandboxSpecContainerArgsId) {
		if a.Value.Kind() == entity.KindString {
			o.Args = append(o.Args, a.Value.String())
		}
	}
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
	if a, ok := e.Get(SandboxSpecContainerShutdownTimeoutId); ok && a.Value.Kind() == entity.KindString {
		o.ShutdownTimeout = a.Value.String()
	}
	if a, ok := e.Get(SandboxSpecContainerStdinId); ok && a.Value.Kind() == entity.KindBool {
		o.Stdin = a.Value.Bool()
	}
	if a, ok := e.Get(SandboxSpecContainerTtyId); ok && a.Value.Kind() == entity.KindBool {
		o.Tty = a.Value.Bool()
	}
}

func (o *SandboxSpecContainer) Encode() (attrs []entity.Attr) {
	for _, v := range o.Args {
		attrs = append(attrs, entity.String(SandboxSpecContainerArgsId, v))
	}
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
	if !entity.Empty(o.ShutdownTimeout) {
		attrs = append(attrs, entity.String(SandboxSpecContainerShutdownTimeoutId, o.ShutdownTimeout))
	}
	attrs = append(attrs, entity.Bool(SandboxSpecContainerStdinId, o.Stdin))
	attrs = append(attrs, entity.Bool(SandboxSpecContainerTtyId, o.Tty))
	return
}

func (o *SandboxSpecContainer) Empty() bool {
	if len(o.Args) != 0 {
		return false
	}
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
	if !entity.Empty(o.ShutdownTimeout) {
		return false
	}
	if !entity.Empty(o.Stdin) {
		return false
	}
	if !entity.Empty(o.Tty) {
		return false
	}
	return true
}

func (o *SandboxSpecContainer) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("args", "dev.miren.session/component.sandbox_spec.container.args", schema.Doc("Arguments that replace the image CMD while preserving its ENTRYPOINT"), schema.Many)
	sb.String("command", "dev.miren.session/component.sandbox_spec.container.command", schema.Doc("Command to run"))
	sb.Component("config_file", "dev.miren.session/component.sandbox_spec.container.config_file", schema.Doc("File to write into the container"), schema.Many)
	(&SandboxSpecContainerConfigFile{}).InitSchema(sb.Builder("component.sandbox_spec.container.config_file"))
	sb.String("directory", "dev.miren.session/component.sandbox_spec.container.directory", schema.Doc("Working directory"))
	sb.String("env", "dev.miren.session/component.sandbox_spec.container.env", schema.Doc("Environment variable"), schema.Many)
	sb.String("image", "dev.miren.session/component.sandbox_spec.container.image", schema.Doc("Container image"), schema.Required)
	sb.Component("mount", "dev.miren.session/component.sandbox_spec.container.mount", schema.Doc("Mounted directory"), schema.Many)
	(&SandboxSpecContainerMount{}).InitSchema(sb.Builder("component.sandbox_spec.container.mount"))
	sb.String("name", "dev.miren.session/component.sandbox_spec.container.name", schema.Doc("Container name"))
	sb.Int64("oom_score", "dev.miren.session/component.sandbox_spec.container.oom_score", schema.Doc("OOM score adjustment"))
	sb.Component("port", "dev.miren.session/component.sandbox_spec.container.port", schema.Doc("Network port declaration"), schema.Many)
	(&SandboxSpecContainerPort{}).InitSchema(sb.Builder("component.sandbox_spec.container.port"))
	sb.Bool("privileged", "dev.miren.session/component.sandbox_spec.container.privileged", schema.Doc("Whether the container runs in privileged mode"))
	sb.String("shutdown_timeout", "dev.miren.session/component.sandbox_spec.container.shutdown_timeout", schema.Doc("Time to wait for graceful shutdown before force-killing"))
	sb.Bool("stdin", "dev.miren.session/component.sandbox_spec.container.stdin", schema.Doc("Keep stdin open for the container"))
	sb.Bool("tty", "dev.miren.session/component.sandbox_spec.container.tty", schema.Doc("Allocate a TTY for the container"))
}

const (
	SandboxSpecContainerConfigFileDataId = entity.Id("dev.miren.session/component.sandbox_spec.container.config_file.data")
	SandboxSpecContainerConfigFileModeId = entity.Id("dev.miren.session/component.sandbox_spec.container.config_file.mode")
	SandboxSpecContainerConfigFilePathId = entity.Id("dev.miren.session/component.sandbox_spec.container.config_file.path")
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
	sb.String("data", "dev.miren.session/component.sandbox_spec.container.config_file.data", schema.Doc("File contents"))
	sb.String("mode", "dev.miren.session/component.sandbox_spec.container.config_file.mode", schema.Doc("File mode"))
	sb.String("path", "dev.miren.session/component.sandbox_spec.container.config_file.path", schema.Doc("File path in the container"))
}

const (
	SandboxSpecContainerMountDestinationId = entity.Id("dev.miren.session/component.sandbox_spec.container.mount.destination")
	SandboxSpecContainerMountSourceId      = entity.Id("dev.miren.session/component.sandbox_spec.container.mount.source")
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
	sb.String("destination", "dev.miren.session/component.sandbox_spec.container.mount.destination", schema.Doc("Mount destination path"))
	sb.String("source", "dev.miren.session/component.sandbox_spec.container.mount.source", schema.Doc("Mount source path"))
}

const (
	SandboxSpecContainerPortNameId        = entity.Id("dev.miren.session/component.sandbox_spec.container.port.name")
	SandboxSpecContainerPortNodePortId    = entity.Id("dev.miren.session/component.sandbox_spec.container.port.node_port")
	SandboxSpecContainerPortPortId        = entity.Id("dev.miren.session/component.sandbox_spec.container.port.port")
	SandboxSpecContainerPortProtocolId    = entity.Id("dev.miren.session/component.sandbox_spec.container.port.protocol")
	SandboxSpecContainerPortProtocolTcpId = entity.Id("dev.miren.session/component.sandbox_spec.container.port.protocol.tcp")
	SandboxSpecContainerPortProtocolUdpId = entity.Id("dev.miren.session/component.sandbox_spec.container.port.protocol.udp")
	SandboxSpecContainerPortTypeId        = entity.Id("dev.miren.session/component.sandbox_spec.container.port.type")
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
	sb.String("name", "dev.miren.session/component.sandbox_spec.container.port.name", schema.Doc("Port name"), schema.Required)
	sb.Int64("node_port", "dev.miren.session/component.sandbox_spec.container.port.node_port", schema.Doc("The port number forwarded from the node to the container"))
	sb.Int64("port", "dev.miren.session/component.sandbox_spec.container.port.port", schema.Doc("Port number"), schema.Required)
	sb.Singleton("dev.miren.session/component.sandbox_spec.container.port.protocol.tcp")
	sb.Singleton("dev.miren.session/component.sandbox_spec.container.port.protocol.udp")
	sb.Ref("protocol", "dev.miren.session/component.sandbox_spec.container.port.protocol", schema.Doc("Port protocol"), schema.Choices(SandboxSpecContainerPortProtocolTcpId, SandboxSpecContainerPortProtocolUdpId))
	sb.String("type", "dev.miren.session/component.sandbox_spec.container.port.type", schema.Doc("High-level port type (e.g., http)"))
}

const (
	SandboxSpecRouteDestinationId = entity.Id("dev.miren.session/component.sandbox_spec.route.destination")
	SandboxSpecRouteGatewayId     = entity.Id("dev.miren.session/component.sandbox_spec.route.gateway")
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
	sb.String("destination", "dev.miren.session/component.sandbox_spec.route.destination", schema.Doc("Network destination"))
	sb.String("gateway", "dev.miren.session/component.sandbox_spec.route.gateway", schema.Doc("Next hop for destination"))
}

const (
	SandboxSpecStaticHostHostId = entity.Id("dev.miren.session/component.sandbox_spec.static_host.host")
	SandboxSpecStaticHostIpId   = entity.Id("dev.miren.session/component.sandbox_spec.static_host.ip")
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
	sb.String("host", "dev.miren.session/component.sandbox_spec.static_host.host", schema.Doc("Hostname"))
	sb.String("ip", "dev.miren.session/component.sandbox_spec.static_host.ip", schema.Doc("IP address"))
}

const (
	SandboxSpecVolumeDbFileId       = entity.Id("dev.miren.session/component.sandbox_spec.volume.db_file")
	SandboxSpecVolumeDiskNameId     = entity.Id("dev.miren.session/component.sandbox_spec.volume.disk_name")
	SandboxSpecVolumeFilesystemId   = entity.Id("dev.miren.session/component.sandbox_spec.volume.filesystem")
	SandboxSpecVolumeLabelsId       = entity.Id("dev.miren.session/component.sandbox_spec.volume.labels")
	SandboxSpecVolumeLeaseTimeoutId = entity.Id("dev.miren.session/component.sandbox_spec.volume.lease_timeout")
	SandboxSpecVolumeMountPathId    = entity.Id("dev.miren.session/component.sandbox_spec.volume.mount_path")
	SandboxSpecVolumeNameId         = entity.Id("dev.miren.session/component.sandbox_spec.volume.name")
	SandboxSpecVolumeOwnerId        = entity.Id("dev.miren.session/component.sandbox_spec.volume.owner")
	SandboxSpecVolumeProviderId     = entity.Id("dev.miren.session/component.sandbox_spec.volume.provider")
	SandboxSpecVolumeReadOnlyId     = entity.Id("dev.miren.session/component.sandbox_spec.volume.read_only")
	SandboxSpecVolumeSizeGbId       = entity.Id("dev.miren.session/component.sandbox_spec.volume.size_gb")
	SandboxSpecVolumeSqliteIdId     = entity.Id("dev.miren.session/component.sandbox_spec.volume.sqlite_id")
)

type SandboxSpecVolume struct {
	DbFile       string       `cbor:"db_file,omitempty" json:"db_file,omitempty"`
	DiskName     string       `cbor:"disk_name,omitempty" json:"disk_name,omitempty"`
	Filesystem   string       `cbor:"filesystem,omitempty" json:"filesystem,omitempty"`
	Labels       types.Labels `cbor:"labels,omitempty" json:"labels,omitempty"`
	LeaseTimeout string       `cbor:"lease_timeout,omitempty" json:"lease_timeout,omitempty"`
	MountPath    string       `cbor:"mount_path,omitempty" json:"mount_path,omitempty"`
	Name         string       `cbor:"name,omitempty" json:"name,omitempty"`
	Owner        string       `cbor:"owner,omitempty" json:"owner,omitempty"`
	Provider     string       `cbor:"provider,omitempty" json:"provider,omitempty"`
	ReadOnly     bool         `cbor:"read_only,omitempty" json:"read_only,omitempty"`
	SizeGb       int64        `cbor:"size_gb,omitempty" json:"size_gb,omitempty"`
	SqliteId     string       `cbor:"sqlite_id,omitempty" json:"sqlite_id,omitempty"`
}

func (o *SandboxSpecVolume) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(SandboxSpecVolumeDbFileId); ok && a.Value.Kind() == entity.KindString {
		o.DbFile = a.Value.String()
	}
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
	if a, ok := e.Get(SandboxSpecVolumeOwnerId); ok && a.Value.Kind() == entity.KindString {
		o.Owner = a.Value.String()
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
	if a, ok := e.Get(SandboxSpecVolumeSqliteIdId); ok && a.Value.Kind() == entity.KindString {
		o.SqliteId = a.Value.String()
	}
}

func (o *SandboxSpecVolume) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.DbFile) {
		attrs = append(attrs, entity.String(SandboxSpecVolumeDbFileId, o.DbFile))
	}
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
	if !entity.Empty(o.Owner) {
		attrs = append(attrs, entity.String(SandboxSpecVolumeOwnerId, o.Owner))
	}
	if !entity.Empty(o.Provider) {
		attrs = append(attrs, entity.String(SandboxSpecVolumeProviderId, o.Provider))
	}
	attrs = append(attrs, entity.Bool(SandboxSpecVolumeReadOnlyId, o.ReadOnly))
	if !entity.Empty(o.SizeGb) {
		attrs = append(attrs, entity.Int64(SandboxSpecVolumeSizeGbId, o.SizeGb))
	}
	if !entity.Empty(o.SqliteId) {
		attrs = append(attrs, entity.String(SandboxSpecVolumeSqliteIdId, o.SqliteId))
	}
	return
}

func (o *SandboxSpecVolume) Empty() bool {
	if !entity.Empty(o.DbFile) {
		return false
	}
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
	if !entity.Empty(o.Owner) {
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
	if !entity.Empty(o.SqliteId) {
		return false
	}
	return true
}

func (o *SandboxSpecVolume) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("db_file", "dev.miren.session/component.sandbox_spec.volume.db_file", schema.Doc("Database filename inside the disk directory"))
	sb.String("disk_name", "dev.miren.session/component.sandbox_spec.volume.disk_name", schema.Doc("Name of the disk to attach"))
	sb.String("filesystem", "dev.miren.session/component.sandbox_spec.volume.filesystem", schema.Doc("Filesystem type for auto-creation"))
	sb.Label("labels", "dev.miren.session/component.sandbox_spec.volume.labels", schema.Doc("Labels identifying the volume"), schema.Many)
	sb.String("lease_timeout", "dev.miren.session/component.sandbox_spec.volume.lease_timeout", schema.Doc("Timeout for acquiring the disk lease"))
	sb.String("mount_path", "dev.miren.session/component.sandbox_spec.volume.mount_path", schema.Doc("Path where the disk should be mounted"))
	sb.String("name", "dev.miren.session/component.sandbox_spec.volume.name", schema.Doc("Volume name"))
	sb.String("owner", "dev.miren.session/component.sandbox_spec.volume.owner", schema.Doc("Ownership policy for the mounted disk"))
	sb.String("provider", "dev.miren.session/component.sandbox_spec.volume.provider", schema.Doc("Volume provider"))
	sb.Bool("read_only", "dev.miren.session/component.sandbox_spec.volume.read_only", schema.Doc("Whether to mount the disk as read-only"))
	sb.Int64("size_gb", "dev.miren.session/component.sandbox_spec.volume.size_gb", schema.Doc("Disk size in GB for auto-creation"))
	sb.String("sqlite_id", "dev.miren.session/component.sandbox_spec.volume.sqlite_id", schema.Doc("Identity of the database this volume attaches to"))
}

const (
	BindingAcknowledgedAtId = entity.Id("dev.miren.session/binding.acknowledged_at")
	BindingDeletedAtId      = entity.Id("dev.miren.session/binding.deleted_at")
	BindingDetachedAtId     = entity.Id("dev.miren.session/binding.detached_at")
	BindingSandboxId        = entity.Id("dev.miren.session/binding.sandbox")
	BindingSessionId        = entity.Id("dev.miren.session/binding.session")
)

type Binding struct {
	ID             entity.Id `json:"id"`
	AcknowledgedAt time.Time `cbor:"acknowledged_at,omitempty" json:"acknowledged_at"`
	DeletedAt      time.Time `cbor:"deleted_at,omitempty" json:"deleted_at"`
	DetachedAt     time.Time `cbor:"detached_at,omitempty" json:"detached_at"`
	Sandbox        string    `cbor:"sandbox,omitempty" json:"sandbox,omitempty"`
	Session        string    `cbor:"session,omitempty" json:"session,omitempty"`
}

func (o *Binding) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(BindingAcknowledgedAtId); ok && a.Value.Kind() == entity.KindTime {
		o.AcknowledgedAt = a.Value.Time()
	}
	if a, ok := e.Get(BindingDeletedAtId); ok && a.Value.Kind() == entity.KindTime {
		o.DeletedAt = a.Value.Time()
	}
	if a, ok := e.Get(BindingDetachedAtId); ok && a.Value.Kind() == entity.KindTime {
		o.DetachedAt = a.Value.Time()
	}
	if a, ok := e.Get(BindingSandboxId); ok && a.Value.Kind() == entity.KindString {
		o.Sandbox = a.Value.String()
	}
	if a, ok := e.Get(BindingSessionId); ok && a.Value.Kind() == entity.KindString {
		o.Session = a.Value.String()
	}
}

func (o *Binding) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindBinding)
}

func (o *Binding) ShortKind() string {
	return "binding"
}

func (o *Binding) Kind() entity.Id {
	return KindBinding
}

func (o *Binding) EntityId() entity.Id {
	return o.ID
}

func (o *Binding) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.AcknowledgedAt) {
		attrs = append(attrs, entity.Time(BindingAcknowledgedAtId, o.AcknowledgedAt))
	}
	if !entity.Empty(o.DeletedAt) {
		attrs = append(attrs, entity.Time(BindingDeletedAtId, o.DeletedAt))
	}
	if !entity.Empty(o.DetachedAt) {
		attrs = append(attrs, entity.Time(BindingDetachedAtId, o.DetachedAt))
	}
	if !entity.Empty(o.Sandbox) {
		attrs = append(attrs, entity.String(BindingSandboxId, o.Sandbox))
	}
	if !entity.Empty(o.Session) {
		attrs = append(attrs, entity.String(BindingSessionId, o.Session))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindBinding))
	return
}

func (o *Binding) Empty() bool {
	if !entity.Empty(o.AcknowledgedAt) {
		return false
	}
	if !entity.Empty(o.DeletedAt) {
		return false
	}
	if !entity.Empty(o.DetachedAt) {
		return false
	}
	if !entity.Empty(o.Sandbox) {
		return false
	}
	if !entity.Empty(o.Session) {
		return false
	}
	return true
}

func (o *Binding) InitSchema(sb *schema.SchemaBuilder) {
	sb.Time("acknowledged_at", "dev.miren.session/binding.acknowledged_at", schema.Doc("When the sandbox acknowledged deletion or detachment cleanup"))
	sb.Time("deleted_at", "dev.miren.session/binding.deleted_at", schema.Doc("When the Session was deleted"))
	sb.Time("detached_at", "dev.miren.session/binding.detached_at", schema.Doc("When this assignment was withdrawn; retained until workload cleanup is acknowledged"))
	sb.String("sandbox", "dev.miren.session/binding.sandbox", schema.Doc("Shared sandbox ID; retained even if the sandbox is removed"), schema.Indexed)
	sb.String("session", "dev.miren.session/binding.session", schema.Doc("Session ID retained after the Session is deleted"))
}

const (
	GroupCounterNextNumberId = entity.Id("dev.miren.session/group_counter.next_number")
)

type GroupCounter struct {
	ID         entity.Id `json:"id"`
	NextNumber int64     `cbor:"next_number,omitempty" json:"next_number,omitempty"`
}

func (o *GroupCounter) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(GroupCounterNextNumberId); ok && a.Value.Kind() == entity.KindInt64 {
		o.NextNumber = a.Value.Int64()
	}
}

func (o *GroupCounter) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindGroupCounter)
}

func (o *GroupCounter) ShortKind() string {
	return "group_counter"
}

func (o *GroupCounter) Kind() entity.Id {
	return KindGroupCounter
}

func (o *GroupCounter) EntityId() entity.Id {
	return o.ID
}

func (o *GroupCounter) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.NextNumber) {
		attrs = append(attrs, entity.Int64(GroupCounterNextNumberId, o.NextNumber))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindGroupCounter))
	return
}

func (o *GroupCounter) Empty() bool {
	return entity.Empty(o.NextNumber)
}

func (o *GroupCounter) InitSchema(sb *schema.SchemaBuilder) {
	sb.Int64("next_number", "dev.miren.session/group_counter.next_number", schema.Doc("Next unused shared sandbox number for this app, service, and group"))
}

const (
	SessionActivityId              = entity.Id("dev.miren.session/session.activity")
	SessionActivityUnknownId       = entity.Id("dev.miren.session/activity.unknown")
	SessionActivityActiveId        = entity.Id("dev.miren.session/activity.active")
	SessionActivityIdleId          = entity.Id("dev.miren.session/activity.idle")
	SessionActivityAtId            = entity.Id("dev.miren.session/session.activity_at")
	SessionAppId                   = entity.Id("dev.miren.session/session.app")
	SessionDesiredStateId          = entity.Id("dev.miren.session/session.desired_state")
	SessionDesiredStateRunningId   = entity.Id("dev.miren.session/desired_state.running")
	SessionDesiredStateSuspendedId = entity.Id("dev.miren.session/desired_state.suspended")
	SessionDiskId                  = entity.Id("dev.miren.session/session.disk")
	SessionFailureId               = entity.Id("dev.miren.session/session.failure")
	SessionGenerationId            = entity.Id("dev.miren.session/session.generation")
	SessionGroupId                 = entity.Id("dev.miren.session/session.group")
	SessionIdleSinceId             = entity.Id("dev.miren.session/session.idle_since")
	SessionIdleTimeoutSecondsId    = entity.Id("dev.miren.session/session.idle_timeout_seconds")
	SessionIncarnationId           = entity.Id("dev.miren.session/session.incarnation")
	SessionLastTransitionId        = entity.Id("dev.miren.session/session.last_transition")
	SessionMaxSessionsPerSandboxId = entity.Id("dev.miren.session/session.max_sessions_per_sandbox")
	SessionPhaseId                 = entity.Id("dev.miren.session/session.phase")
	SessionPhasePendingId          = entity.Id("dev.miren.session/phase.pending")
	SessionPhaseActivatingId       = entity.Id("dev.miren.session/phase.activating")
	SessionPhaseReadyId            = entity.Id("dev.miren.session/phase.ready")
	SessionPhaseSuspendingId       = entity.Id("dev.miren.session/phase.suspending")
	SessionPhaseInactiveId         = entity.Id("dev.miren.session/phase.inactive")
	SessionPhaseFailedId           = entity.Id("dev.miren.session/phase.failed")
	SessionSandboxId               = entity.Id("dev.miren.session/session.sandbox")
	SessionServiceId               = entity.Id("dev.miren.session/session.service")
	SessionSpecId                  = entity.Id("dev.miren.session/session.spec")
	SessionVersionId               = entity.Id("dev.miren.session/session.version")
)

type Session struct {
	ID                    entity.Id           `json:"id"`
	Activity              SessionActivity     `cbor:"activity,omitempty" json:"activity,omitempty"`
	ActivityAt            time.Time           `cbor:"activity_at,omitempty" json:"activity_at"`
	App                   entity.Id           `cbor:"app,omitempty" json:"app,omitempty"`
	DesiredState          SessionDesiredState `cbor:"desired_state,omitempty" json:"desired_state,omitempty"`
	Disk                  entity.Id           `cbor:"disk,omitempty" json:"disk,omitempty"`
	Failure               string              `cbor:"failure,omitempty" json:"failure,omitempty"`
	Generation            int64               `cbor:"generation,omitempty" json:"generation,omitempty"`
	Group                 string              `cbor:"group,omitempty" json:"group,omitempty"`
	IdleSince             time.Time           `cbor:"idle_since,omitempty" json:"idle_since"`
	IdleTimeoutSeconds    int64               `cbor:"idle_timeout_seconds,omitempty" json:"idle_timeout_seconds,omitempty"`
	Incarnation           []Incarnation       `cbor:"incarnation,omitempty" json:"incarnation,omitempty"`
	LastTransition        time.Time           `cbor:"last_transition,omitempty" json:"last_transition"`
	MaxSessionsPerSandbox int64               `cbor:"max_sessions_per_sandbox,omitempty" json:"max_sessions_per_sandbox,omitempty"`
	Phase                 SessionPhase        `cbor:"phase,omitempty" json:"phase,omitempty"`
	Sandbox               entity.Id           `cbor:"sandbox,omitempty" json:"sandbox,omitempty"`
	Service               string              `cbor:"service,omitempty" json:"service,omitempty"`
	Spec                  SandboxSpec         `cbor:"spec,omitempty" json:"spec"`
	Version               entity.Id           `cbor:"version,omitempty" json:"version,omitempty"`
}

type SessionActivity string

const (
	UNKNOWN SessionActivity = "activity.unknown"
	ACTIVE  SessionActivity = "activity.active"
	IDLE    SessionActivity = "activity.idle"
)

var sessionactivityFromId = map[entity.Id]SessionActivity{SessionActivityUnknownId: UNKNOWN, SessionActivityActiveId: ACTIVE, SessionActivityIdleId: IDLE}
var sessionactivityToId = map[SessionActivity]entity.Id{UNKNOWN: SessionActivityUnknownId, ACTIVE: SessionActivityActiveId, IDLE: SessionActivityIdleId}

type SessionDesiredState string

const (
	RUNNING   SessionDesiredState = "desired_state.running"
	SUSPENDED SessionDesiredState = "desired_state.suspended"
)

var sessiondesired_stateFromId = map[entity.Id]SessionDesiredState{SessionDesiredStateRunningId: RUNNING, SessionDesiredStateSuspendedId: SUSPENDED}
var sessiondesired_stateToId = map[SessionDesiredState]entity.Id{RUNNING: SessionDesiredStateRunningId, SUSPENDED: SessionDesiredStateSuspendedId}

type SessionPhase string

const (
	PENDING    SessionPhase = "phase.pending"
	ACTIVATING SessionPhase = "phase.activating"
	READY      SessionPhase = "phase.ready"
	SUSPENDING SessionPhase = "phase.suspending"
	INACTIVE   SessionPhase = "phase.inactive"
	FAILED     SessionPhase = "phase.failed"
)

var sessionphaseFromId = map[entity.Id]SessionPhase{SessionPhasePendingId: PENDING, SessionPhaseActivatingId: ACTIVATING, SessionPhaseReadyId: READY, SessionPhaseSuspendingId: SUSPENDING, SessionPhaseInactiveId: INACTIVE, SessionPhaseFailedId: FAILED}
var sessionphaseToId = map[SessionPhase]entity.Id{PENDING: SessionPhasePendingId, ACTIVATING: SessionPhaseActivatingId, READY: SessionPhaseReadyId, SUSPENDING: SessionPhaseSuspendingId, INACTIVE: SessionPhaseInactiveId, FAILED: SessionPhaseFailedId}

func (o *Session) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(SessionActivityId); ok && a.Value.Kind() == entity.KindId {
		o.Activity = sessionactivityFromId[a.Value.Id()]
	}
	if a, ok := e.Get(SessionActivityAtId); ok && a.Value.Kind() == entity.KindTime {
		o.ActivityAt = a.Value.Time()
	}
	if a, ok := e.Get(SessionAppId); ok && a.Value.Kind() == entity.KindId {
		o.App = a.Value.Id()
	}
	if a, ok := e.Get(SessionDesiredStateId); ok && a.Value.Kind() == entity.KindId {
		o.DesiredState = sessiondesired_stateFromId[a.Value.Id()]
	}
	if a, ok := e.Get(SessionDiskId); ok && a.Value.Kind() == entity.KindId {
		o.Disk = a.Value.Id()
	}
	if a, ok := e.Get(SessionFailureId); ok && a.Value.Kind() == entity.KindString {
		o.Failure = a.Value.String()
	}
	if a, ok := e.Get(SessionGenerationId); ok && a.Value.Kind() == entity.KindInt64 {
		o.Generation = a.Value.Int64()
	}
	if a, ok := e.Get(SessionGroupId); ok && a.Value.Kind() == entity.KindString {
		o.Group = a.Value.String()
	}
	if a, ok := e.Get(SessionIdleSinceId); ok && a.Value.Kind() == entity.KindTime {
		o.IdleSince = a.Value.Time()
	}
	if a, ok := e.Get(SessionIdleTimeoutSecondsId); ok && a.Value.Kind() == entity.KindInt64 {
		o.IdleTimeoutSeconds = a.Value.Int64()
	}
	for _, a := range e.GetAll(SessionIncarnationId) {
		if a.Value.Kind() == entity.KindComponent {
			var v Incarnation
			v.Decode(a.Value.Component())
			o.Incarnation = append(o.Incarnation, v)
		}
	}
	if a, ok := e.Get(SessionLastTransitionId); ok && a.Value.Kind() == entity.KindTime {
		o.LastTransition = a.Value.Time()
	}
	if a, ok := e.Get(SessionMaxSessionsPerSandboxId); ok && a.Value.Kind() == entity.KindInt64 {
		o.MaxSessionsPerSandbox = a.Value.Int64()
	}
	if a, ok := e.Get(SessionPhaseId); ok && a.Value.Kind() == entity.KindId {
		o.Phase = sessionphaseFromId[a.Value.Id()]
	}
	if a, ok := e.Get(SessionSandboxId); ok && a.Value.Kind() == entity.KindId {
		o.Sandbox = a.Value.Id()
	}
	if a, ok := e.Get(SessionServiceId); ok && a.Value.Kind() == entity.KindString {
		o.Service = a.Value.String()
	}
	if a, ok := e.Get(SessionSpecId); ok && a.Value.Kind() == entity.KindComponent {
		o.Spec.Decode(a.Value.Component())
	}
	if a, ok := e.Get(SessionVersionId); ok && a.Value.Kind() == entity.KindId {
		o.Version = a.Value.Id()
	}
}

func (o *Session) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindSession)
}

func (o *Session) ShortKind() string {
	return "session"
}

func (o *Session) Kind() entity.Id {
	return KindSession
}

func (o *Session) EntityId() entity.Id {
	return o.ID
}

func (o *Session) Encode() (attrs []entity.Attr) {
	if a, ok := sessionactivityToId[o.Activity]; ok {
		attrs = append(attrs, entity.Ref(SessionActivityId, a))
	}
	if !entity.Empty(o.ActivityAt) {
		attrs = append(attrs, entity.Time(SessionActivityAtId, o.ActivityAt))
	}
	if !entity.Empty(o.App) {
		attrs = append(attrs, entity.Ref(SessionAppId, o.App))
	}
	if a, ok := sessiondesired_stateToId[o.DesiredState]; ok {
		attrs = append(attrs, entity.Ref(SessionDesiredStateId, a))
	}
	if !entity.Empty(o.Disk) {
		attrs = append(attrs, entity.Ref(SessionDiskId, o.Disk))
	}
	if !entity.Empty(o.Failure) {
		attrs = append(attrs, entity.String(SessionFailureId, o.Failure))
	}
	if !entity.Empty(o.Generation) {
		attrs = append(attrs, entity.Int64(SessionGenerationId, o.Generation))
	}
	if !entity.Empty(o.Group) {
		attrs = append(attrs, entity.String(SessionGroupId, o.Group))
	}
	if !entity.Empty(o.IdleSince) {
		attrs = append(attrs, entity.Time(SessionIdleSinceId, o.IdleSince))
	}
	if !entity.Empty(o.IdleTimeoutSeconds) {
		attrs = append(attrs, entity.Int64(SessionIdleTimeoutSecondsId, o.IdleTimeoutSeconds))
	}
	for _, v := range o.Incarnation {
		attrs = append(attrs, entity.Component(SessionIncarnationId, v.Encode()))
	}
	if !entity.Empty(o.LastTransition) {
		attrs = append(attrs, entity.Time(SessionLastTransitionId, o.LastTransition))
	}
	if !entity.Empty(o.MaxSessionsPerSandbox) {
		attrs = append(attrs, entity.Int64(SessionMaxSessionsPerSandboxId, o.MaxSessionsPerSandbox))
	}
	if a, ok := sessionphaseToId[o.Phase]; ok {
		attrs = append(attrs, entity.Ref(SessionPhaseId, a))
	}
	if !entity.Empty(o.Sandbox) {
		attrs = append(attrs, entity.Ref(SessionSandboxId, o.Sandbox))
	}
	if !entity.Empty(o.Service) {
		attrs = append(attrs, entity.String(SessionServiceId, o.Service))
	}
	if !o.Spec.Empty() {
		attrs = append(attrs, entity.Component(SessionSpecId, o.Spec.Encode()))
	}
	if !entity.Empty(o.Version) {
		attrs = append(attrs, entity.Ref(SessionVersionId, o.Version))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindSession))
	return
}

func (o *Session) Empty() bool {
	if o.Activity != "" {
		return false
	}
	if !entity.Empty(o.ActivityAt) {
		return false
	}
	if !entity.Empty(o.App) {
		return false
	}
	if o.DesiredState != "" {
		return false
	}
	if !entity.Empty(o.Disk) {
		return false
	}
	if !entity.Empty(o.Failure) {
		return false
	}
	if !entity.Empty(o.Generation) {
		return false
	}
	if !entity.Empty(o.Group) {
		return false
	}
	if !entity.Empty(o.IdleSince) {
		return false
	}
	if !entity.Empty(o.IdleTimeoutSeconds) {
		return false
	}
	if len(o.Incarnation) != 0 {
		return false
	}
	if !entity.Empty(o.LastTransition) {
		return false
	}
	if !entity.Empty(o.MaxSessionsPerSandbox) {
		return false
	}
	if o.Phase != "" {
		return false
	}
	if !entity.Empty(o.Sandbox) {
		return false
	}
	if !entity.Empty(o.Service) {
		return false
	}
	if !o.Spec.Empty() {
		return false
	}
	if !entity.Empty(o.Version) {
		return false
	}
	return true
}

func (o *Session) InitSchema(sb *schema.SchemaBuilder) {
	sb.Singleton("dev.miren.session/activity.unknown")
	sb.Singleton("dev.miren.session/activity.active")
	sb.Singleton("dev.miren.session/activity.idle")
	sb.Ref("activity", "dev.miren.session/session.activity", schema.Doc("Most recent workload-reported activity for this Session"), schema.Choices(SessionActivityUnknownId, SessionActivityActiveId, SessionActivityIdleId))
	sb.Time("activity_at", "dev.miren.session/session.activity_at", schema.Doc("When the workload reported this Session's activity state"))
	sb.Ref("app", "dev.miren.session/session.app", schema.Doc("Application this Session belongs to"), schema.Indexed)
	sb.Singleton("dev.miren.session/desired_state.running")
	sb.Singleton("dev.miren.session/desired_state.suspended")
	sb.Ref("desired_state", "dev.miren.session/session.desired_state", schema.Doc("Whether the Session should run or remain suspended"), schema.Choices(SessionDesiredStateRunningId, SessionDesiredStateSuspendedId))
	sb.Ref("disk", "dev.miren.session/session.disk", schema.Doc("Optional stable disk reference retained across sandbox incarnations"))
	sb.String("failure", "dev.miren.session/session.failure", schema.Doc("Reason the Session failed"))
	sb.Int64("generation", "dev.miren.session/session.generation", schema.Doc("Monotonic incarnation number; deterministic sandbox IDs make creation idempotent"))
	sb.String("group", "dev.miren.session/session.group", schema.Doc("Optional opaque key for sharing a sandbox with Sessions of the same app and service"))
	sb.Time("idle_since", "dev.miren.session/session.idle_since", schema.Doc("Start of the continuously idle period; cleared by an active report or reassignment"))
	sb.Int64("idle_timeout_seconds", "dev.miren.session/session.idle_timeout_seconds", schema.Doc("Automatically suspend after this many continuously idle seconds; zero disables idle parking"))
	sb.Component("incarnation", "dev.miren.session/session.incarnation", schema.Doc("Historical sandbox incarnations"), schema.Many)
	(&Incarnation{}).InitSchema(sb.Builder("session.incarnation"))
	sb.Time("last_transition", "dev.miren.session/session.last_transition", schema.Doc("When the observed phase last changed"))
	sb.Int64("max_sessions_per_sandbox", "dev.miren.session/session.max_sessions_per_sandbox", schema.Doc("Capacity of a controller-managed shared sandbox; values greater than one opt into sharing"))
	sb.Singleton("dev.miren.session/phase.pending")
	sb.Singleton("dev.miren.session/phase.activating")
	sb.Singleton("dev.miren.session/phase.ready")
	sb.Singleton("dev.miren.session/phase.suspending")
	sb.Singleton("dev.miren.session/phase.inactive")
	sb.Singleton("dev.miren.session/phase.failed")
	sb.Ref("phase", "dev.miren.session/session.phase", schema.Doc("Observed lifecycle phase"), schema.Indexed, schema.Choices(SessionPhasePendingId, SessionPhaseActivatingId, SessionPhaseReadyId, SessionPhaseSuspendingId, SessionPhaseInactiveId, SessionPhaseFailedId))
	sb.Ref("sandbox", "dev.miren.session/session.sandbox", schema.Doc("Current incarnation; absent once suspension finishes"), schema.Indexed)
	sb.String("service", "dev.miren.session/session.service", schema.Doc("Service this Session belongs to"))
	sb.Component("spec", "dev.miren.session/session.spec", schema.Doc("Resolved app configuration updated on deploy and copied to new sandbox incarnations"))
	sb.Ref("version", "dev.miren.session/session.version", schema.Doc("Desired active application version for this Session"))
}

const (
	IncarnationEndedAtId    = entity.Id("dev.miren.session/incarnation.ended_at")
	IncarnationGenerationId = entity.Id("dev.miren.session/incarnation.generation")
	IncarnationReasonId     = entity.Id("dev.miren.session/incarnation.reason")
	IncarnationSandboxId    = entity.Id("dev.miren.session/incarnation.sandbox")
	IncarnationStartedAtId  = entity.Id("dev.miren.session/incarnation.started_at")
)

type Incarnation struct {
	EndedAt    time.Time `cbor:"ended_at,omitempty" json:"ended_at"`
	Generation int64     `cbor:"generation" json:"generation"`
	Reason     string    `cbor:"reason,omitempty" json:"reason,omitempty"`
	Sandbox    string    `cbor:"sandbox,omitempty" json:"sandbox,omitempty"`
	StartedAt  time.Time `cbor:"started_at,omitempty" json:"started_at"`
}

func (o *Incarnation) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(IncarnationEndedAtId); ok && a.Value.Kind() == entity.KindTime {
		o.EndedAt = a.Value.Time()
	}
	if a, ok := e.Get(IncarnationGenerationId); ok && a.Value.Kind() == entity.KindInt64 {
		o.Generation = a.Value.Int64()
	}
	if a, ok := e.Get(IncarnationReasonId); ok && a.Value.Kind() == entity.KindString {
		o.Reason = a.Value.String()
	}
	if a, ok := e.Get(IncarnationSandboxId); ok && a.Value.Kind() == entity.KindString {
		o.Sandbox = a.Value.String()
	}
	if a, ok := e.Get(IncarnationStartedAtId); ok && a.Value.Kind() == entity.KindTime {
		o.StartedAt = a.Value.Time()
	}
}

func (o *Incarnation) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.EndedAt) {
		attrs = append(attrs, entity.Time(IncarnationEndedAtId, o.EndedAt))
	}
	attrs = append(attrs, entity.Int64(IncarnationGenerationId, o.Generation))
	if !entity.Empty(o.Reason) {
		attrs = append(attrs, entity.String(IncarnationReasonId, o.Reason))
	}
	if !entity.Empty(o.Sandbox) {
		attrs = append(attrs, entity.String(IncarnationSandboxId, o.Sandbox))
	}
	if !entity.Empty(o.StartedAt) {
		attrs = append(attrs, entity.Time(IncarnationStartedAtId, o.StartedAt))
	}
	return
}

func (o *Incarnation) Empty() bool {
	if !entity.Empty(o.EndedAt) {
		return false
	}
	if !entity.Empty(o.Generation) {
		return false
	}
	if !entity.Empty(o.Reason) {
		return false
	}
	if !entity.Empty(o.Sandbox) {
		return false
	}
	if !entity.Empty(o.StartedAt) {
		return false
	}
	return true
}

func (o *Incarnation) InitSchema(sb *schema.SchemaBuilder) {
	sb.Time("ended_at", "dev.miren.session/incarnation.ended_at", schema.Doc("When the incarnation ended"))
	sb.Int64("generation", "dev.miren.session/incarnation.generation", schema.Doc("Generation of the sandbox incarnation"), schema.Required)
	sb.String("reason", "dev.miren.session/incarnation.reason", schema.Doc("Why the incarnation ended"))
	sb.String("sandbox", "dev.miren.session/incarnation.sandbox", schema.Doc("Historical sandbox ID; remains queryable after entity GC"))
	sb.Time("started_at", "dev.miren.session/incarnation.started_at", schema.Doc("When the incarnation began"))
}

const (
	SlotSandboxId = entity.Id("dev.miren.session/slot.sandbox")
	SlotSessionId = entity.Id("dev.miren.session/slot.session")
)

type Slot struct {
	ID      entity.Id `json:"id"`
	Sandbox string    `cbor:"sandbox,omitempty" json:"sandbox,omitempty"`
	Session string    `cbor:"session,omitempty" json:"session,omitempty"`
}

func (o *Slot) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(SlotSandboxId); ok && a.Value.Kind() == entity.KindString {
		o.Sandbox = a.Value.String()
	}
	if a, ok := e.Get(SlotSessionId); ok && a.Value.Kind() == entity.KindString {
		o.Session = a.Value.String()
	}
}

func (o *Slot) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindSlot)
}

func (o *Slot) ShortKind() string {
	return "slot"
}

func (o *Slot) Kind() entity.Id {
	return KindSlot
}

func (o *Slot) EntityId() entity.Id {
	return o.ID
}

func (o *Slot) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Sandbox) {
		attrs = append(attrs, entity.String(SlotSandboxId, o.Sandbox))
	}
	if !entity.Empty(o.Session) {
		attrs = append(attrs, entity.String(SlotSessionId, o.Session))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindSlot))
	return
}

func (o *Slot) Empty() bool {
	if !entity.Empty(o.Sandbox) {
		return false
	}
	if !entity.Empty(o.Session) {
		return false
	}
	return true
}

func (o *Slot) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("sandbox", "dev.miren.session/slot.sandbox", schema.Doc("Sandbox whose capacity this slot reserves"), schema.Indexed)
	sb.String("session", "dev.miren.session/slot.session", schema.Doc("Session holding this sandbox capacity slot"), schema.Indexed)
}

var (
	KindBinding      = entity.Id("dev.miren.session/kind.binding")
	KindGroupCounter = entity.Id("dev.miren.session/kind.group_counter")
	KindSession      = entity.Id("dev.miren.session/kind.session")
	KindSlot         = entity.Id("dev.miren.session/kind.slot")
	Schema           = entity.Id("dev.miren.session/schema.v1alpha")
)

func init() {
	schema.Register("dev.miren.session", "v1alpha", func(sb *schema.SchemaBuilder) {
		(&SandboxSpec{}).InitSchema(sb)
		(&Binding{}).InitSchema(sb)
		(&GroupCounter{}).InitSchema(sb)
		(&Session{}).InitSchema(sb)
		(&Slot{}).InitSchema(sb)
	})
	schema.RegisterEncodedSchema("dev.miren.session", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\xacZɎ\xe54\x17~\x8dn\xfd\xfa\x9bA\x8cBJ\x03b\x16S\v\xd8\xf2\n\x91o|\x92뺉\x9d\xb2\x9d;\xb0\x03\xc4\x06!\x16<\x02]\xd5o\bk\xe4c'q\x12\xe7\xc6I\xf5\xa6\xdb\xc79\xfe|\xe6c\xbb\xee\x1d夂[\nǤb\x12x\xa2@)&8\x1c\x18\xa7\xea\xee\xfc\xbfɗ\xa7\xe6K\xa2J\xa1_\xe0\xda\xe3\x94\xc3|\xb4\x00\xff\xe6TT\x84\xf1\xe9\x06yΠ\xa4\xea\x97\xe7;F\xcfO\xc2\x18\x89\"\x9c\xee\xc4\x19\xf7)ZB_jȕ\x96\x8c\x17\xd7\x17[\xc2-v\x84\xb7\xb88\x824s\xc5\xf1\x03R\xd6{R֒UD^R#95\x10!h\xd4~\xc78e\xbc\xb0\x068?\x9er9\x86H#\xfc\x86z\xbc;\v\x93\x90\xec\xc0ũ\x04Z\x00M\x89\xc6m\xc5xҨF5\xab\x00\xd1ޘG\xa3P\x82\xee\x81n<z\x88\xf1\xe65\fM\xb2}\x0fr\xf0'\x86(\xafͣD\xf9\xf7\xda\xfa\x87\xb8\xb8p(\xb3^vļ\x97\a\xb9\xb2\xe8\xe5\xbf\xee\x8c6\xaf\xcf\xc2$$\xd3\xec\xc8\xf4\x05\xf7\xdbw\x14Z\x13xS\x1d\xcc?鑔\r\xa8\xe7\x94\xd1\x12ίL\xd1\xdau\x89aȑ\x82\x90\r;>\xcbR4܄\x13\x0f\tر:\x9e\xb9\xe0\x18+\xd2\x05\x87?1\f\x8e\xff_A\xa9k\\\x9d\x99\x81Y\xb5c\x14M\xf8\xf6\xfc\x1a\n\x8aI\xa0\xa9\xd2D\x03\xae\xae\x86SAc\xfe]Ȇs\x13\n\x01\xe8\xc1\xfa\xc412ը\x1a8\x85`\xda\x0e\x97t\xac\xb3ժ\x15\x9e\xa9\x03\xcaLq\xe4T\x9eˁvUNX\xd9H\xabl\xd1\x12\xe3\x1c\nT\x83v}\x01\x1c$\xd1m\x1a\xddx\xb4A\xc9\x18\xd7\b\x11\b\xb5\x0eB\x8a\xc6\xfa\n\xecp\xc5\xf6&JS\xc5xf5\xb8\xf1\xe8a\xa4$\v\x18\x86Q4:U\x90\tN\x15\xa2\xe9\xe0\x97V\xad\xfb\x850f<#\x92[ST\x84_\xfe\xb1\xc1\xecO\x1b(\x96\x89\xaa\x16\x1c\xb8\xeeG\xaefL\xc1\x93\x00\xf8\xaa.\xf1\xd6T^\x0f*\xc1@k\xf3n\xdfQCS\xbes\x1d#6\"\x02^\xf5a$\x10\xe5 r7\x1e\xc7E\xc0\xf8>BT{XPFi\"\xfdf\xe7ѝQ\xae\xb6\t\xdf\xdfs}\xbaujI\x94N\xb5$\\\xb1\xce|b<9\xf4Ň\xf3h\x159\xa7n\xac\xd2\x1ad\xea\xdb\xe3\xfch\xf6s뤻\x85\xb4\xad\xf7D٬\x03;\f\x16\xc7\x17 \x81\xd0K\xa8R\xe3\xaa\x04?\xe7\xa6\xf2@\xb0\xbcY.\xfb\xbd0\x95\xd0\x14ڀX\x96\xcf1\xec\x19w\xad\xeb\xd59Ζ\xe3\x06\xff#\xda\xc0\x06\x9a\x97e\xeeyn\\=\xbe\xca\xde\xf3,\x95\xdf\xd9\x18\x8d(\xdd\n䑹\xc2W\xb4\x84\x17\xdfw\v\rCՐن\x81\xa3\x85b\x14H\x94\x8e\xa5U#5@\x91\xf5\xe8\xf7\xfb\x99\xf8\r\xa3&\x99\xe0\x9a0\x0e\xd2+\xa6\xac\x9f\\\x90~\xbaO\xb2\xb4O\xa4\x1e\x7f\xa2\x99?]\xafGBd\xa1<e(\xd2\xe3\xfa\xf4\xc5\x06\xe0LT\x15\xe1\xd4\x06FKx\xc0h\xf9\xaf7\x01\xf3\x9c\x15inr\xd1ki\xfe\xf4\x82\x1f\xa6\xbb.\xfa\xc1\xdf5\xd2'\xbf\xa2\xe9\xbe{\x98\x86\t%\x9a\xb8\x03\x95\x19\x8d=\xf3P\xf8JP\x9b\xbb\x14G/\x1b\xbe&zo\xe1q\x14{\xad9?q\xd8\x06\xbaC\xb6\xc0\x06\x17e\xfbr\x83l\x94Iȴ\x90\xf6~\xc2zr\\\xb1>\xd9\x00\x0e\xfc\xe8\x05dfȱ=?\xdb\x00\xcb*R\xb8\x16g\x87\xe3$\xda\x02Z\x89\x86kOZ\xb0\x13\v\x893\xddi9q\x10x\xd5K\xca\xf7[\xf51W'\xcdx\x7f\xec;\xf8\x13c_|\xb3y\x1b%\x1a\xe9Z^\xee\xc6ё\xfd8\x14\xd9\b\xfb|k\t\xc7\xfc\xea\xff\x19\xeb\xb9%O\x84\xa8R\x95\tw%c=9\xb8wl\x11\xb6\x16ҏ;\x8a\xf4B\xd8M\xf7Y\x0e;\x83\xbb\xeaR\xb2\xc5Lf\x93%\xf3?ی+(\xa4h\x1e\xf4AO\x0e.0\x9b\xc5\xee\x90\xe9\x00\x14\xab߷\x9bA\xa5\xd0\"\x13\xa5\xbd\xbauT\xf8\xc5\"\xd3Y\xbd)\xd9\a[%:\xab\xb3\x86\xbe\f\xa4\x86\xd6\x0f\xb3)\xea\xd9EA|Qx\x14*\n\x06\x11\xc5\xf9j\x8b8\x92\x1dY\t\x05\xd8\x03؍G\xa3h;!\xca\xcd\x1d^\xed\x1bMŉ\xb7\xaf\x12\xb8E=\x99}\x19\xcdOi\xcal-\a;\x1cJ\xbf\xa5Mk\xf7:\x99\xe9\xf6a\xd2\xc0]u\x90\x0e\xf9\a\x05\xf8(Z\x80\xbdP\xfaG\xd0'!\xed\x03\xd9\xc1\x9f\xe8\xc4\xc0\xf4\xfb8\x1a\xb4\x14\xc53\xad%\xdb5\xda?\f\x97\x83y\x03\x0e%\xd9A9w[\x9fG\xff\x81\xeb\xf65\x97\xf5\xe4\xf6\xfb\x81\t\xea\xf4D\x98\x1e\xc4\xce\xedtz| \x8b\xef5\x12\xf0}$\xadE\xc92+:\x1fͅ\xeb\x11p8\x82\\\x91nC\xd4\x04\x97\xe7\xa4<\x91\x8bZq\xb3\x19\xa1\xd8\xf5\xf73υs\x18b\x18\x00`'\x16\xfa\xea\x14\x7f\xae\xaf\"ܪC\\|H \xf6\xba\xa3[|\xda[\xf0\x82h8\x11\x1b\vEK\xc4V\xe7\xdaK~\x84\xbb_\x97\xf8J\x13ͲԤ\xbb\x7f_\xf5\xa7\x17\xfc4\xddk\xceO\x1e\xe8*o}\xbeE\x1b,i\xb6\xdduZ\xf8^\x8aOY\x1f\x94\xd9W\xf8\x1d\xab\xa3=\xd4x\x1e\xb2P\x06\tex?Z\x06\xb7\x81\x8d\x91\x96p\x8f`\xe8\xf1\xa7\xf1P\xa2l*?\x1ds7\xb3\xe0\xe7\xe9\x0es~\xb6x\x91.\xfec\xa57,xBw\xf61\x05\r\xd2\x12c\x1f\xc7\aN\x8b\xca\xd4!\xedNˬ'\xb7\xf7\x14\x87l\xe4S\x17\xa5\xa1\xb2G\x1e\x8f\xde~\xbbw\xd8\xd8=\xfdw\xb2\xdc͌zk|\xefh\x81\x81(\x18\xf4\xc2j8\xf5`\xb3\xe0\xdd2\xed\x1ebn<z\x8c\x1d_\xd3\x1c\xf6\xc2\xd5'\xfe\x1c\xe3\xf0ĉ\x83\xb4\r\xcc\x0e\xb7\x1f!\x1db-őQ\a\xba\xef\xa8\a\x87\xb1\x04BS\xc1Kw4\xea\xc9\xe1\xf9tu\xd2)\xf6\x13\xa4\xc5\xce=\xa7;bp\xe1[-\xaa\xba-\x99\x86\x94ٛ\x00\xeb\xc9\xd8\xf2z\xeb\x95W\x8by\x95\xbd\xf4\x85X\xfa\xf3\xc1\xb5\xaa{\xfd\x97\x0f\x0e!\xf4\x17<\xfc\xe5\x03\xfe\x157\xcd\xf0\xf5I\xba\x02\x1b\xf8\x8b̀-\xb2\xa0\xfe\x8cZ\xbd\xb7\x00\x96p8\xeb\x947\xd5΅\xdf\xc1\x9fh}zU\xc9j\x008f=\xa8\xbd96\xdb_9\xd9\x1f\xfb\\\xfb\xa9\xd3\xe2oE\xdc\xf7β\v\xbf)\x19\n\x17\xe7\x87\xff\x00\x00\x00\xff\xff\x01\x00\x00\xff\xff<B+\x1b\xab%\x00\x00"))
}
