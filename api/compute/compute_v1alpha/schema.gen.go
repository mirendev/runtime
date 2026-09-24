package compute_v1alpha

import (
	"time"

	entity "miren.dev/runtime/pkg/entity"
	schema "miren.dev/runtime/pkg/entity/schema"
	types "miren.dev/runtime/pkg/entity/types"
)

const (
	SandboxSpecContainerId           = entity.Id("dev.miren.compute/component.sandbox_spec.container")
	SandboxSpecHostNetworkId         = entity.Id("dev.miren.compute/component.sandbox_spec.hostNetwork")
	SandboxSpecLogAttributeId        = entity.Id("dev.miren.compute/component.sandbox_spec.logAttribute")
	SandboxSpecLogEntityId           = entity.Id("dev.miren.compute/component.sandbox_spec.logEntity")
	SandboxSpecPortWaitTimeoutId     = entity.Id("dev.miren.compute/component.sandbox_spec.port_wait_timeout")
	SandboxSpecRestartPolicyId       = entity.Id("dev.miren.compute/component.sandbox_spec.restart_policy")
	SandboxSpecRestartPolicyAlwaysId = entity.Id("dev.miren.compute/component.sandbox_spec.restart_policy.always")
	SandboxSpecRestartPolicyNeverId  = entity.Id("dev.miren.compute/component.sandbox_spec.restart_policy.never")
	SandboxSpecRouteId               = entity.Id("dev.miren.compute/component.sandbox_spec.route")
	SandboxSpecStaticHostId          = entity.Id("dev.miren.compute/component.sandbox_spec.static_host")
	SandboxSpecVersionId             = entity.Id("dev.miren.compute/component.sandbox_spec.version")
	SandboxSpecVolumeId              = entity.Id("dev.miren.compute/component.sandbox_spec.volume")
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
	sb.Component("container", "dev.miren.compute/component.sandbox_spec.container", schema.Doc("Container specification"), schema.Many, schema.Required)
	(&SandboxSpecContainer{}).InitSchema(sb.Builder("component.sandbox_spec.container"))
	sb.Bool("hostNetwork", "dev.miren.compute/component.sandbox_spec.hostNetwork", schema.Doc("Whether to use host networking"))
	sb.Label("logAttribute", "dev.miren.compute/component.sandbox_spec.logAttribute", schema.Doc("Labels for log entries"), schema.Many)
	sb.String("logEntity", "dev.miren.compute/component.sandbox_spec.logEntity", schema.Doc("Entity to associate log output with"))
	sb.String("port_wait_timeout", "dev.miren.compute/component.sandbox_spec.port_wait_timeout", schema.Doc("Max time to wait for declared container ports to bind before marking\nthe sandbox DEAD. Parsed via time.ParseDuration (e.g. \"60s\"). Empty,\ninvalid, or non-positive values (including \"0s\") fall back to 15s.\nAddons with slow cold-init (e.g. MySQL first-boot ~20s) should set\nthis to a larger value.\n"))
	sb.Singleton("dev.miren.compute/component.sandbox_spec.restart_policy.always")
	sb.Singleton("dev.miren.compute/component.sandbox_spec.restart_policy.never")
	sb.Ref("restart_policy", "dev.miren.compute/component.sandbox_spec.restart_policy", schema.Doc("Whether the sandbox controller may reboot this sandbox's containers if\nthey have disappeared while the entity still reads RUNNING. Empty means\n\"always\", which is the historical behavior every service depends on.\n\n\"never\" is required for sandboxes backing a task run, whose command\nmust execute at most once. Without it a run that loses its containers\nwithout the exit being observed -- a runner restart mid-run is the\nrealistic case -- gets its command re-executed on the next reconcile,\nwhich for a migration is not a recoverable mistake.\n"), schema.Choices(SandboxSpecRestartPolicyAlwaysId, SandboxSpecRestartPolicyNeverId))
	sb.Component("route", "dev.miren.compute/component.sandbox_spec.route", schema.Doc("Network route configuration"), schema.Many)
	(&SandboxSpecRoute{}).InitSchema(sb.Builder("component.sandbox_spec.route"))
	sb.Component("static_host", "dev.miren.compute/component.sandbox_spec.static_host", schema.Doc("Static host-to-IP mapping"), schema.Many)
	(&SandboxSpecStaticHost{}).InitSchema(sb.Builder("component.sandbox_spec.static_host"))
	sb.Ref("version", "dev.miren.compute/component.sandbox_spec.version", schema.Doc("Application version reference"), schema.Indexed)
	sb.Component("volume", "dev.miren.compute/component.sandbox_spec.volume", schema.Doc("Volume configuration"), schema.Many)
	(&SandboxSpecVolume{}).InitSchema(sb.Builder("component.sandbox_spec.volume"))
}

const (
	SandboxSpecContainerArgsId            = entity.Id("dev.miren.compute/component.sandbox_spec.container.args")
	SandboxSpecContainerCommandId         = entity.Id("dev.miren.compute/component.sandbox_spec.container.command")
	SandboxSpecContainerConfigFileId      = entity.Id("dev.miren.compute/component.sandbox_spec.container.config_file")
	SandboxSpecContainerDirectoryId       = entity.Id("dev.miren.compute/component.sandbox_spec.container.directory")
	SandboxSpecContainerEnvId             = entity.Id("dev.miren.compute/component.sandbox_spec.container.env")
	SandboxSpecContainerImageId           = entity.Id("dev.miren.compute/component.sandbox_spec.container.image")
	SandboxSpecContainerMountId           = entity.Id("dev.miren.compute/component.sandbox_spec.container.mount")
	SandboxSpecContainerNameId            = entity.Id("dev.miren.compute/component.sandbox_spec.container.name")
	SandboxSpecContainerOomScoreId        = entity.Id("dev.miren.compute/component.sandbox_spec.container.oom_score")
	SandboxSpecContainerPortId            = entity.Id("dev.miren.compute/component.sandbox_spec.container.port")
	SandboxSpecContainerPrivilegedId      = entity.Id("dev.miren.compute/component.sandbox_spec.container.privileged")
	SandboxSpecContainerShutdownTimeoutId = entity.Id("dev.miren.compute/component.sandbox_spec.container.shutdown_timeout")
	SandboxSpecContainerStdinId           = entity.Id("dev.miren.compute/component.sandbox_spec.container.stdin")
	SandboxSpecContainerTtyId             = entity.Id("dev.miren.compute/component.sandbox_spec.container.tty")
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
	sb.String("args", "dev.miren.compute/component.sandbox_spec.container.args", schema.Doc("Arguments that replace the image CMD while preserving its ENTRYPOINT"), schema.Many)
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
	sb.String("shutdown_timeout", "dev.miren.compute/component.sandbox_spec.container.shutdown_timeout", schema.Doc("Time to wait for graceful shutdown before force-killing (e.g. 10s, 30s)"))
	sb.Bool("stdin", "dev.miren.compute/component.sandbox_spec.container.stdin", schema.Doc("Keep stdin open for the container"))
	sb.Bool("tty", "dev.miren.compute/component.sandbox_spec.container.tty", schema.Doc("Allocate a TTY for the container"))
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
	SandboxSpecVolumeDbFileId       = entity.Id("dev.miren.compute/component.sandbox_spec.volume.db_file")
	SandboxSpecVolumeDiskNameId     = entity.Id("dev.miren.compute/component.sandbox_spec.volume.disk_name")
	SandboxSpecVolumeFilesystemId   = entity.Id("dev.miren.compute/component.sandbox_spec.volume.filesystem")
	SandboxSpecVolumeLabelsId       = entity.Id("dev.miren.compute/component.sandbox_spec.volume.labels")
	SandboxSpecVolumeLeaseTimeoutId = entity.Id("dev.miren.compute/component.sandbox_spec.volume.lease_timeout")
	SandboxSpecVolumeMountPathId    = entity.Id("dev.miren.compute/component.sandbox_spec.volume.mount_path")
	SandboxSpecVolumeNameId         = entity.Id("dev.miren.compute/component.sandbox_spec.volume.name")
	SandboxSpecVolumeOwnerId        = entity.Id("dev.miren.compute/component.sandbox_spec.volume.owner")
	SandboxSpecVolumeProviderId     = entity.Id("dev.miren.compute/component.sandbox_spec.volume.provider")
	SandboxSpecVolumeReadOnlyId     = entity.Id("dev.miren.compute/component.sandbox_spec.volume.read_only")
	SandboxSpecVolumeSizeGbId       = entity.Id("dev.miren.compute/component.sandbox_spec.volume.size_gb")
	SandboxSpecVolumeSqliteIdId     = entity.Id("dev.miren.compute/component.sandbox_spec.volume.sqlite_id")
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
	sb.String("db_file", "dev.miren.compute/component.sandbox_spec.volume.db_file", schema.Doc("Database filename inside the disk directory (for sqlite provider; a bare filename, not a path, defaults to data.db)"))
	sb.String("disk_name", "dev.miren.compute/component.sandbox_spec.volume.disk_name", schema.Doc("Name of the disk to attach (for disk provider)"))
	sb.String("filesystem", "dev.miren.compute/component.sandbox_spec.volume.filesystem", schema.Doc("Filesystem type for auto-creation (for disk provider)"))
	sb.Label("labels", "dev.miren.compute/component.sandbox_spec.volume.labels", schema.Doc("Labels identifying the volume"), schema.Many)
	sb.String("lease_timeout", "dev.miren.compute/component.sandbox_spec.volume.lease_timeout", schema.Doc("Timeout for acquiring disk lease (for disk provider)"))
	sb.String("mount_path", "dev.miren.compute/component.sandbox_spec.volume.mount_path", schema.Doc("Path where disk should be mounted (for disk provider)"))
	sb.String("name", "dev.miren.compute/component.sandbox_spec.volume.name", schema.Doc("Volume name"))
	sb.String("owner", "dev.miren.compute/component.sandbox_spec.volume.owner", schema.Doc("Ownership policy for the mounted disk (for disk provider). Empty derives the owner from the container's resolved run user; \"keep\" leaves the raw mount ownership untouched; \"uid\" or \"uid:gid\" pins a specific numeric owner."))
	sb.String("provider", "dev.miren.compute/component.sandbox_spec.volume.provider", schema.Doc("Volume provider"))
	sb.Bool("read_only", "dev.miren.compute/component.sandbox_spec.volume.read_only", schema.Doc("Whether to mount disk as read-only (for disk provider)"))
	sb.Int64("size_gb", "dev.miren.compute/component.sandbox_spec.volume.size_gb", schema.Doc("Disk size in GB for auto-creation (for disk provider)"))
	sb.String("sqlite_id", "dev.miren.compute/component.sandbox_spec.volume.sqlite_id", schema.Doc("Identity of the database this volume attaches to (for sqlite provider, scoped to the app)"))
}

const (
	LeaseLastHeartbeatId = entity.Id("dev.miren.compute/lease.last_heartbeat")
	LeaseProjectId       = entity.Id("dev.miren.compute/lease.project")
	LeaseSandboxId       = entity.Id("dev.miren.compute/lease.sandbox")
)

type Lease struct {
	ID            entity.Id `json:"id"`
	LastHeartbeat time.Time `cbor:"last_heartbeat,omitempty" json:"last_heartbeat"`
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
	NodeApiAddressId            = entity.Id("dev.miren.compute/node.api_address")
	NodeConstraintsId           = entity.Id("dev.miren.compute/node.constraints")
	NodeNameId                  = entity.Id("dev.miren.compute/node.name")
	NodeRegisteredAtId          = entity.Id("dev.miren.compute/node.registered_at")
	NodeRunnerIdId              = entity.Id("dev.miren.compute/node.runner_id")
	NodeSchedulingId            = entity.Id("dev.miren.compute/node.scheduling")
	NodeSchedulingSchedulableId = entity.Id("dev.miren.compute/scheduling.schedulable")
	NodeSchedulingCordonedId    = entity.Id("dev.miren.compute/scheduling.cordoned")
	NodeStatusId                = entity.Id("dev.miren.compute/node.status")
	NodeStatusUnknownId         = entity.Id("dev.miren.compute/status.unknown")
	NodeStatusReadyId           = entity.Id("dev.miren.compute/status.ready")
	NodeStatusDisabledId        = entity.Id("dev.miren.compute/status.disabled")
	NodeStatusUnhealthyId       = entity.Id("dev.miren.compute/status.unhealthy")
	NodeVersionId               = entity.Id("dev.miren.compute/node.version")
)

type Node struct {
	ID           entity.Id      `json:"id"`
	ApiAddress   string         `cbor:"api_address,omitempty" json:"api_address,omitempty"`
	Constraints  types.Labels   `cbor:"constraints,omitempty" json:"constraints,omitempty"`
	Name         string         `cbor:"name,omitempty" json:"name,omitempty"`
	RegisteredAt time.Time      `cbor:"registered_at,omitempty" json:"registered_at"`
	RunnerId     string         `cbor:"runner_id,omitempty" json:"runner_id,omitempty"`
	Scheduling   NodeScheduling `cbor:"scheduling,omitempty" json:"scheduling,omitempty"`
	Status       NodeStatus     `cbor:"status,omitempty" json:"status,omitempty"`
	Version      string         `cbor:"version,omitempty" json:"version,omitempty"`
}

type NodeScheduling string

const (
	SCHEDULABLE NodeScheduling = "scheduling.schedulable"
	CORDONED    NodeScheduling = "scheduling.cordoned"
)

var nodeschedulingFromId = map[entity.Id]NodeScheduling{NodeSchedulingSchedulableId: SCHEDULABLE, NodeSchedulingCordonedId: CORDONED}
var nodeschedulingToId = map[NodeScheduling]entity.Id{SCHEDULABLE: NodeSchedulingSchedulableId, CORDONED: NodeSchedulingCordonedId}

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
	if a, ok := e.Get(NodeNameId); ok && a.Value.Kind() == entity.KindString {
		o.Name = a.Value.String()
	}
	if a, ok := e.Get(NodeRegisteredAtId); ok && a.Value.Kind() == entity.KindTime {
		o.RegisteredAt = a.Value.Time()
	}
	if a, ok := e.Get(NodeRunnerIdId); ok && a.Value.Kind() == entity.KindString {
		o.RunnerId = a.Value.String()
	}
	if a, ok := e.Get(NodeSchedulingId); ok && a.Value.Kind() == entity.KindId {
		o.Scheduling = nodeschedulingFromId[a.Value.Id()]
	}
	if a, ok := e.Get(NodeStatusId); ok && a.Value.Kind() == entity.KindId {
		o.Status = nodestatusFromId[a.Value.Id()]
	}
	if a, ok := e.Get(NodeVersionId); ok && a.Value.Kind() == entity.KindString {
		o.Version = a.Value.String()
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
	if !entity.Empty(o.Name) {
		attrs = append(attrs, entity.String(NodeNameId, o.Name))
	}
	if !entity.Empty(o.RegisteredAt) {
		attrs = append(attrs, entity.Time(NodeRegisteredAtId, o.RegisteredAt))
	}
	if !entity.Empty(o.RunnerId) {
		attrs = append(attrs, entity.String(NodeRunnerIdId, o.RunnerId))
	}
	if a, ok := nodeschedulingToId[o.Scheduling]; ok {
		attrs = append(attrs, entity.Ref(NodeSchedulingId, a))
	}
	if a, ok := nodestatusToId[o.Status]; ok {
		attrs = append(attrs, entity.Ref(NodeStatusId, a))
	}
	if !entity.Empty(o.Version) {
		attrs = append(attrs, entity.String(NodeVersionId, o.Version))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindNode))
	attrs = append(attrs, entity.Bool(entity.Id("dev.miren.meta/cloud.export"), true))
	return
}

func (o *Node) Empty() bool {
	if !entity.Empty(o.ApiAddress) {
		return false
	}
	if len(o.Constraints) != 0 {
		return false
	}
	if !entity.Empty(o.Name) {
		return false
	}
	if !entity.Empty(o.RegisteredAt) {
		return false
	}
	if !entity.Empty(o.RunnerId) {
		return false
	}
	if o.Scheduling != "" {
		return false
	}
	if o.Status != "" {
		return false
	}
	if !entity.Empty(o.Version) {
		return false
	}
	return true
}

func (o *Node) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("api_address", "dev.miren.compute/node.api_address", schema.Doc("The address to connect the node at"))
	sb.Label("constraints", "dev.miren.compute/node.constraints", schema.Doc("The label constraints the node has, used for scheduling"), schema.Many)
	sb.String("name", "dev.miren.compute/node.name", schema.Doc("Human-readable name for the runner (defaults to hostname)"))
	sb.Time("registered_at", "dev.miren.compute/node.registered_at", schema.Doc("When the runner first registered with the coordinator"))
	sb.String("runner_id", "dev.miren.compute/node.runner_id", schema.Doc("Unique identifier for the runner (for distributed runners)"), schema.Indexed)
	sb.Singleton("dev.miren.compute/scheduling.schedulable")
	sb.Singleton("dev.miren.compute/scheduling.cordoned")
	sb.Ref("scheduling", "dev.miren.compute/node.scheduling", schema.Doc("Operator-controlled scheduling eligibility for the node. Unlike status\n(which is session-scoped and reset to ready on every runner rejoin),\nscheduling is persistent, so a cordoned node stays unschedulable across\nrunner restarts until an operator uncordons it. The zero value (unset)\nis treated as schedulable. A future \"draining\" value can be added here\nwhen drain becomes asynchronous.\n"), schema.Choices(NodeSchedulingSchedulableId, NodeSchedulingCordonedId))
	sb.Singleton("dev.miren.compute/status.unknown")
	sb.Singleton("dev.miren.compute/status.ready")
	sb.Singleton("dev.miren.compute/status.disabled")
	sb.Singleton("dev.miren.compute/status.unhealthy")
	sb.Ref("status", "dev.miren.compute/node.status", schema.Doc("The status of the node"), schema.Session, schema.Choices(NodeStatusUnknownId, NodeStatusReadyId, NodeStatusDisabledId, NodeStatusUnhealthyId))
	sb.String("version", "dev.miren.compute/node.version", schema.Doc("Runner software version"))
}

const (
	SandboxBoundPortId                    = entity.Id("dev.miren.compute/sandbox.bound_port")
	SandboxContainerId                    = entity.Id("dev.miren.compute/sandbox.container")
	SandboxExitId                         = entity.Id("dev.miren.compute/sandbox.exit")
	SandboxHostNetworkId                  = entity.Id("dev.miren.compute/sandbox.hostNetwork")
	SandboxLabelsId                       = entity.Id("dev.miren.compute/sandbox.labels")
	SandboxLastActivityId                 = entity.Id("dev.miren.compute/sandbox.last_activity")
	SandboxLogAttributeId                 = entity.Id("dev.miren.compute/sandbox.logAttribute")
	SandboxLogEntityId                    = entity.Id("dev.miren.compute/sandbox.logEntity")
	SandboxNetworkId                      = entity.Id("dev.miren.compute/sandbox.network")
	SandboxRouteId                        = entity.Id("dev.miren.compute/sandbox.route")
	SandboxSpecId                         = entity.Id("dev.miren.compute/sandbox.spec")
	SandboxStartupOutcomeId               = entity.Id("dev.miren.compute/sandbox.startup_outcome")
	SandboxStartupOutcomeStartupRunningId = entity.Id("dev.miren.compute/startup_outcome.startup_running")
	SandboxStartupOutcomeStartupFailedId  = entity.Id("dev.miren.compute/startup_outcome.startup_failed")
	SandboxStaticHostId                   = entity.Id("dev.miren.compute/sandbox.static_host")
	SandboxStatusId                       = entity.Id("dev.miren.compute/sandbox.status")
	SandboxStatusPendingId                = entity.Id("dev.miren.compute/status.pending")
	SandboxStatusNotReadyId               = entity.Id("dev.miren.compute/status.not_ready")
	SandboxStatusRunningId                = entity.Id("dev.miren.compute/status.running")
	SandboxStatusStoppedId                = entity.Id("dev.miren.compute/status.stopped")
	SandboxStatusDeadId                   = entity.Id("dev.miren.compute/status.dead")
	SandboxVolumeId                       = entity.Id("dev.miren.compute/sandbox.volume")
)

type Sandbox struct {
	ID             entity.Id             `json:"id"`
	BoundPort      []BoundPort           `cbor:"bound_port,omitempty" json:"bound_port,omitempty"`
	Container      []Container           `cbor:"container" json:"container"`
	Exit           Exit                  `cbor:"exit,omitempty" json:"exit"`
	HostNetwork    bool                  `cbor:"hostNetwork,omitempty" json:"hostNetwork,omitempty"`
	Labels         []string              `cbor:"labels,omitempty" json:"labels,omitempty"`
	LastActivity   time.Time             `cbor:"last_activity,omitempty" json:"last_activity"`
	LogAttribute   types.Labels          `cbor:"logAttribute,omitempty" json:"logAttribute,omitempty"`
	LogEntity      string                `cbor:"logEntity,omitempty" json:"logEntity,omitempty"`
	Network        []Network             `cbor:"network,omitempty" json:"network,omitempty"`
	Route          []Route               `cbor:"route,omitempty" json:"route,omitempty"`
	Spec           SandboxSpec           `cbor:"spec,omitempty" json:"spec"`
	StartupOutcome SandboxStartupOutcome `cbor:"startup_outcome,omitempty" json:"startup_outcome,omitempty"`
	StaticHost     []StaticHost          `cbor:"static_host,omitempty" json:"static_host,omitempty"`
	Status         SandboxStatus         `cbor:"status,omitempty" json:"status,omitempty"`
	Volume         []Volume              `cbor:"volume,omitempty" json:"volume,omitempty"`
}

type SandboxStartupOutcome string

const (
	STARTUP_RUNNING SandboxStartupOutcome = "startup_outcome.startup_running"
	STARTUP_FAILED  SandboxStartupOutcome = "startup_outcome.startup_failed"
)

var sandboxstartup_outcomeFromId = map[entity.Id]SandboxStartupOutcome{SandboxStartupOutcomeStartupRunningId: STARTUP_RUNNING, SandboxStartupOutcomeStartupFailedId: STARTUP_FAILED}
var sandboxstartup_outcomeToId = map[SandboxStartupOutcome]entity.Id{STARTUP_RUNNING: SandboxStartupOutcomeStartupRunningId, STARTUP_FAILED: SandboxStartupOutcomeStartupFailedId}

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
	for _, a := range e.GetAll(SandboxBoundPortId) {
		if a.Value.Kind() == entity.KindComponent {
			var v BoundPort
			v.Decode(a.Value.Component())
			o.BoundPort = append(o.BoundPort, v)
		}
	}
	for _, a := range e.GetAll(SandboxContainerId) {
		if a.Value.Kind() == entity.KindComponent {
			var v Container
			v.Decode(a.Value.Component())
			o.Container = append(o.Container, v)
		}
	}
	if a, ok := e.Get(SandboxExitId); ok && a.Value.Kind() == entity.KindComponent {
		o.Exit.Decode(a.Value.Component())
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
	if a, ok := e.Get(SandboxStartupOutcomeId); ok && a.Value.Kind() == entity.KindId {
		o.StartupOutcome = sandboxstartup_outcomeFromId[a.Value.Id()]
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
	for _, v := range o.BoundPort {
		attrs = append(attrs, entity.Component(SandboxBoundPortId, v.Encode()))
	}
	for _, v := range o.Container {
		attrs = append(attrs, entity.Component(SandboxContainerId, v.Encode()))
	}
	if !o.Exit.Empty() {
		attrs = append(attrs, entity.Component(SandboxExitId, o.Exit.Encode()))
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
	if a, ok := sandboxstartup_outcomeToId[o.StartupOutcome]; ok {
		attrs = append(attrs, entity.Ref(SandboxStartupOutcomeId, a))
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
	if len(o.BoundPort) != 0 {
		return false
	}
	if len(o.Container) != 0 {
		return false
	}
	if !o.Exit.Empty() {
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
	if o.StartupOutcome != "" {
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
	sb.Component("bound_port", "dev.miren.compute/sandbox.bound_port", schema.Doc("Port the container was observed listening on, set only when it differs from the configured port"), schema.Many)
	(&BoundPort{}).InitSchema(sb.Builder("sandbox.bound_port"))
	sb.Component("container", "dev.miren.compute/sandbox.container", schema.Doc("A container running in the sandbox"), schema.Many, schema.Required)
	(&Container{}).InitSchema(sb.Builder("sandbox.container"))
	sb.Component("exit", "dev.miren.compute/sandbox.exit", schema.Doc("Terminal result of the sandbox's primary container task; absent until it exits"))
	(&Exit{}).InitSchema(sb.Builder("sandbox.exit"))
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
	sb.Singleton("dev.miren.compute/startup_outcome.startup_running")
	sb.Singleton("dev.miren.compute/startup_outcome.startup_failed")
	sb.Ref("startup_outcome", "dev.miren.compute/sandbox.startup_outcome", schema.Doc("Durable distinction between a sandbox that reached RUNNING and one that failed before then"), schema.Choices(SandboxStartupOutcomeStartupRunningId, SandboxStartupOutcomeStartupFailedId))
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
	BoundPortAddressId = entity.Id("dev.miren.compute/bound_port.address")
	BoundPortPortId    = entity.Id("dev.miren.compute/bound_port.port")
)

type BoundPort struct {
	Address string `cbor:"address,omitempty" json:"address,omitempty"`
	Port    int64  `cbor:"port,omitempty" json:"port,omitempty"`
}

func (o *BoundPort) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(BoundPortAddressId); ok && a.Value.Kind() == entity.KindString {
		o.Address = a.Value.String()
	}
	if a, ok := e.Get(BoundPortPortId); ok && a.Value.Kind() == entity.KindInt64 {
		o.Port = a.Value.Int64()
	}
}

func (o *BoundPort) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Address) {
		attrs = append(attrs, entity.String(BoundPortAddressId, o.Address))
	}
	if !entity.Empty(o.Port) {
		attrs = append(attrs, entity.Int64(BoundPortPortId, o.Port))
	}
	return
}

func (o *BoundPort) Empty() bool {
	if !entity.Empty(o.Address) {
		return false
	}
	if !entity.Empty(o.Port) {
		return false
	}
	return true
}

func (o *BoundPort) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("address", "dev.miren.compute/bound_port.address", schema.Doc("The bind address the port was observed on"))
	sb.Int64("port", "dev.miren.compute/bound_port.port", schema.Doc("The observed listening port"))
}

const (
	ContainerCommandId         = entity.Id("dev.miren.compute/container.command")
	ContainerConfigFileId      = entity.Id("dev.miren.compute/container.config_file")
	ContainerDirectoryId       = entity.Id("dev.miren.compute/container.directory")
	ContainerEnvId             = entity.Id("dev.miren.compute/container.env")
	ContainerImageId           = entity.Id("dev.miren.compute/container.image")
	ContainerMountId           = entity.Id("dev.miren.compute/container.mount")
	ContainerNameId            = entity.Id("dev.miren.compute/container.name")
	ContainerOomScoreId        = entity.Id("dev.miren.compute/container.oom_score")
	ContainerPortId            = entity.Id("dev.miren.compute/container.port")
	ContainerPrivilegedId      = entity.Id("dev.miren.compute/container.privileged")
	ContainerShutdownTimeoutId = entity.Id("dev.miren.compute/container.shutdown_timeout")
	ContainerStdinId           = entity.Id("dev.miren.compute/container.stdin")
	ContainerTtyId             = entity.Id("dev.miren.compute/container.tty")
)

type Container struct {
	Command         string       `cbor:"command,omitempty" json:"command,omitempty"`
	ConfigFile      []ConfigFile `cbor:"config_file,omitempty" json:"config_file,omitempty"`
	Directory       string       `cbor:"directory,omitempty" json:"directory,omitempty"`
	Env             []string     `cbor:"env,omitempty" json:"env,omitempty"`
	Image           string       `cbor:"image" json:"image"`
	Mount           []Mount      `cbor:"mount,omitempty" json:"mount,omitempty"`
	Name            string       `cbor:"name,omitempty" json:"name,omitempty"`
	OomScore        int64        `cbor:"oom_score,omitempty" json:"oom_score,omitempty"`
	Port            []Port       `cbor:"port,omitempty" json:"port,omitempty"`
	Privileged      bool         `cbor:"privileged,omitempty" json:"privileged,omitempty"`
	ShutdownTimeout string       `cbor:"shutdown_timeout,omitempty" json:"shutdown_timeout,omitempty"`
	Stdin           bool         `cbor:"stdin,omitempty" json:"stdin,omitempty"`
	Tty             bool         `cbor:"tty,omitempty" json:"tty,omitempty"`
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
	if a, ok := e.Get(ContainerShutdownTimeoutId); ok && a.Value.Kind() == entity.KindString {
		o.ShutdownTimeout = a.Value.String()
	}
	if a, ok := e.Get(ContainerStdinId); ok && a.Value.Kind() == entity.KindBool {
		o.Stdin = a.Value.Bool()
	}
	if a, ok := e.Get(ContainerTtyId); ok && a.Value.Kind() == entity.KindBool {
		o.Tty = a.Value.Bool()
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
	if !entity.Empty(o.ShutdownTimeout) {
		attrs = append(attrs, entity.String(ContainerShutdownTimeoutId, o.ShutdownTimeout))
	}
	attrs = append(attrs, entity.Bool(ContainerStdinId, o.Stdin))
	attrs = append(attrs, entity.Bool(ContainerTtyId, o.Tty))
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
	sb.String("shutdown_timeout", "dev.miren.compute/container.shutdown_timeout", schema.Doc("Time to wait for graceful shutdown before force-killing (e.g. 10s, 30s)"))
	sb.Bool("stdin", "dev.miren.compute/container.stdin", schema.Doc("Keep stdin open for the container"))
	sb.Bool("tty", "dev.miren.compute/container.tty", schema.Doc("Allocate a TTY for the container"))
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
	ExitAtId        = entity.Id("dev.miren.compute/exit.at")
	ExitCodeId      = entity.Id("dev.miren.compute/exit.code")
	ExitContainerId = entity.Id("dev.miren.compute/exit.container")
)

type Exit struct {
	At        time.Time `cbor:"at,omitempty" json:"at"`
	Code      int64     `cbor:"code" json:"code"`
	Container string    `cbor:"container,omitempty" json:"container,omitempty"`
}

func (o *Exit) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(ExitAtId); ok && a.Value.Kind() == entity.KindTime {
		o.At = a.Value.Time()
	}
	if a, ok := e.Get(ExitCodeId); ok && a.Value.Kind() == entity.KindInt64 {
		o.Code = a.Value.Int64()
	}
	if a, ok := e.Get(ExitContainerId); ok && a.Value.Kind() == entity.KindString {
		o.Container = a.Value.String()
	}
}

func (o *Exit) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.At) {
		attrs = append(attrs, entity.Time(ExitAtId, o.At))
	}
	attrs = append(attrs, entity.Int64(ExitCodeId, o.Code))
	if !entity.Empty(o.Container) {
		attrs = append(attrs, entity.String(ExitContainerId, o.Container))
	}
	return
}

func (o *Exit) Empty() bool {
	if !entity.Empty(o.At) {
		return false
	}
	if !entity.Empty(o.Code) {
		return false
	}
	if !entity.Empty(o.Container) {
		return false
	}
	return true
}

func (o *Exit) InitSchema(sb *schema.SchemaBuilder) {
	sb.Time("at", "dev.miren.compute/exit.at", schema.Doc("When the task exited. Always set when the component is written:\nan Exit with a zero code and a zero time is indistinguishable from\nan absent one, and would be dropped.\n"))
	sb.Int64("code", "dev.miren.compute/exit.code", schema.Doc("Process exit code. Required so a legitimate 0 survives encoding."), schema.Required)
	sb.String("container", "dev.miren.compute/exit.container", schema.Doc("Name of the container whose task exited"))
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
	SandboxPoolCountedFailuresId       = entity.Id("dev.miren.compute/sandbox_pool.counted_failures")
	SandboxPoolCurrentInstancesId      = entity.Id("dev.miren.compute/sandbox_pool.current_instances")
	SandboxPoolDesiredInstancesId      = entity.Id("dev.miren.compute/sandbox_pool.desired_instances")
	SandboxPoolEphemeralId             = entity.Id("dev.miren.compute/sandbox_pool.ephemeral")
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
	CooldownUntil         time.Time    `cbor:"cooldown_until,omitempty" json:"cooldown_until"`
	CountedFailures       string       `cbor:"counted_failures,omitempty" json:"counted_failures,omitempty"`
	CurrentInstances      int64        `cbor:"current_instances,omitempty" json:"current_instances,omitempty"`
	DesiredInstances      int64        `cbor:"desired_instances,omitempty" json:"desired_instances,omitempty"`
	Ephemeral             bool         `cbor:"ephemeral,omitempty" json:"ephemeral,omitempty"`
	LastCrashTime         time.Time    `cbor:"last_crash_time,omitempty" json:"last_crash_time"`
	ReadyInstances        int64        `cbor:"ready_instances,omitempty" json:"ready_instances,omitempty"`
	ReferencedByVersions  []entity.Id  `cbor:"referenced_by_versions,omitempty" json:"referenced_by_versions,omitempty"`
	SandboxLabels         types.Labels `cbor:"sandbox_labels,omitempty" json:"sandbox_labels,omitempty"`
	SandboxPrefix         string       `cbor:"sandbox_prefix,omitempty" json:"sandbox_prefix,omitempty"`
	SandboxSpec           SandboxSpec  `cbor:"sandbox_spec,omitempty" json:"sandbox_spec"`
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
	if a, ok := e.Get(SandboxPoolCountedFailuresId); ok && a.Value.Kind() == entity.KindString {
		o.CountedFailures = a.Value.String()
	}
	if a, ok := e.Get(SandboxPoolCurrentInstancesId); ok && a.Value.Kind() == entity.KindInt64 {
		o.CurrentInstances = a.Value.Int64()
	}
	if a, ok := e.Get(SandboxPoolDesiredInstancesId); ok && a.Value.Kind() == entity.KindInt64 {
		o.DesiredInstances = a.Value.Int64()
	}
	if a, ok := e.Get(SandboxPoolEphemeralId); ok && a.Value.Kind() == entity.KindBool {
		o.Ephemeral = a.Value.Bool()
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
	if !entity.Empty(o.CountedFailures) {
		attrs = append(attrs, entity.String(SandboxPoolCountedFailuresId, o.CountedFailures))
	}
	if !entity.Empty(o.CurrentInstances) {
		attrs = append(attrs, entity.Int64(SandboxPoolCurrentInstancesId, o.CurrentInstances))
	}
	if !entity.Empty(o.DesiredInstances) {
		attrs = append(attrs, entity.Int64(SandboxPoolDesiredInstancesId, o.DesiredInstances))
	}
	attrs = append(attrs, entity.Bool(SandboxPoolEphemeralId, o.Ephemeral))
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
	if !entity.Empty(o.CountedFailures) {
		return false
	}
	if !entity.Empty(o.CurrentInstances) {
		return false
	}
	if !entity.Empty(o.DesiredInstances) {
		return false
	}
	if !entity.Empty(o.Ephemeral) {
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
	sb.Int64("consecutive_crash_count", "dev.miren.compute/sandbox_pool.consecutive_crash_count", schema.Doc("Number of consecutive sandbox failures, including quick crashes after RUNNING"))
	sb.Time("cooldown_until", "dev.miren.compute/sandbox_pool.cooldown_until", schema.Doc("Timestamp until which new sandbox creation is paused due to crash loop"))
	sb.String("counted_failures", "dev.miren.compute/sandbox_pool.counted_failures", schema.Doc("JSON array of sandbox IDs already included in the failure streak; kept across streak resets"))
	sb.Int64("current_instances", "dev.miren.compute/sandbox_pool.current_instances", schema.Doc("Current number of sandbox instances (non-STOPPED)"))
	sb.Int64("desired_instances", "dev.miren.compute/sandbox_pool.desired_instances", schema.Doc("Target number of sandbox instances"))
	sb.Bool("ephemeral", "dev.miren.compute/sandbox_pool.ephemeral", schema.Doc("True when this pool backs an ephemeral AppVersion. Ephemeral pools never scale beyond 1 instance."))
	sb.Time("last_crash_time", "dev.miren.compute/sandbox_pool.last_crash_time", schema.Doc("Timestamp of the most recently counted sandbox failure"))
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
	Key Key       `cbor:"key,omitempty" json:"key"`
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
	return o.Key.Empty()
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
	schema.RegisterEncodedSchema("dev.miren.compute", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\xec\\َ\xf4:\x11~\r\x0e\xcba\xdf\x04\xe4\xe7 v\xb1\x1d\x01\xb7\xbcB䎫ӞN\xdb\x19\xdb\xe9\xe9\xe6\x0e\x10\x12B,\xef\xc0?\xc3\x1b\xc25\xf2\x968\x89\x9d8n\xa4s\x93\x9b\x91\xedv}\xb6\xcb\xe5\xaar\xb9&\xaf\x98\xa2\v<c\xb8\x16\x17\u0081\x16\x15\xbb\xb4\x9d\x048\x13\x8a\xc5\xdb\xed3\xb3_ީ_\n\xca0\xfc[\xd3^\xe7=ԏ\x06\xe0\xbfG\xcc.\x88\xd0\xf9\x00\xc7#\x81\x06\x8b\xbf\xbc?\x10|\xfbR\x18\xa3@-)\x11\xc6\x1c\x84\xd0c\x9d\xfd\x06yo\xe1($'\xb4~]\x02\xa9\x18\x15\x92#B\xa5\xc0\x17D\xef\xff1P~\xb3\x82\x82\x06\x1d\xa0\xd1\xd3\t,Z#):<\xfc\xf1&\xa0ɾ\x12!\xe3P\x13!\x81\x03.\x91Ԥ\x97q\x93\x02\u0092\\@\xc3|!\x06\xd3Q\n\xbc$XC\x90\xa1:e\xc4\x17#\x00\xa2:\x01\xee\x1aBk\x8d\xf0\xe4\xd5\xf5\f\x80v\x97\xb3\xfaS^QӁ\xf8שb\x1c3\n\xf8\xf6\xd59\xe4@]\xb8ngۆ\x0e\rܾ\xb1H\xe2\xf5\xd4s\xfe\\l\xce\x12\xc9\xcel\xfeі\x83s}\x05\x0e\b\xdfo\x1f\x06F\xd5d\x85\xfe\xbd\xee虲\x17\x1ab\xb2\xedg{\x9c0\x11jvAvڮ\xae\v\xe9\xe8\tP#O\xf7\x90\x10\xf6\xb8\xb6\x8f\xde\xe4\xc0<\xf5z\xaf\xc0\x05aT/\xb8v\x15o\x83][}\xfd\b5\xed\t5-'\x17\xc4\xef\xa5:nXA\xdc>\x1b9\xb2\r a\xcf\xec˼\x8b\xfe5\xf1\xd0\xfeI/\xe1k\x11\x90\xa2AB\x96'@\\\x1e\xc0\n<\x9d\xb4\x8d%\xfe\xf31\xa4\x96\xb3'\xa8\fD\xed*\x8a\xf6@\xf02\xa5@\x14\x1f\xd8\xcdP\xba\x8a\xa5\\\xe4!h\xfa\xd0\xfeh&Z(\xc3\xc6\xdb\a\x81\xdd6\x1d\x129\xf9Ϸ\x88\xe2\xb00Łu\x14\x97-\xe3\xd2\xd3]O^\xabZ\x13QT\x8c\x02\x95C\xc9\xcep\x0e]̡\x13'\xfbǘ\x96\x1b\x90\n_[\xd7\x01M\xfd>\xa2\xa0<\b\xbd,\xad`\xfb\x05V\x84\xca\xc5]#\x9a^\xf5\xd7\f\xfdr\x9c\xa1\x15\xa3\x12\x11\n\xdc\xe3'\x19\x1aW\xd89\a.f\xc0\x89\xdc\xfc\xfb\xfb\xc8L{ \xd5rA\xd4\xe8\xfb\xdaU<f\xea\xb5~}\x19\x81\x1eI]\x1eI\x03\x13\xdb\xd77\xaf\xac\xf8]\u008a\xfda6\xa9\x90\x80\xb6\xf4\xa0\n\x8c$2\x92\xa0KS1Z\xa1\xbe0l\r\xb5.m\xa4n\x91<Y)T\xa5T\x15\xfcd0\x14\x84\x1e%`8\a\xb6a¡\x92\x8cߍ\x14\x0eթE\x0f\xa8\xb9\x01\x05\xe8\xd5\xdb\xdbJU\x13\x0e\xdc@O.\xa86\x8c\x02S\x9cJ\xd8\"\xf5\x85u\xd4\xd7M`\x1aV\xa4\xea\x9b)R\xa5\x916\xe9\xa6\xc0i\xd2 \x05\x06!\tEҙֳ\xdf0\xe5V@\xf7\x1b\x14\xc1:^\x81uFL9U.\f[b\xfeݰ\xe6\x15\x17sQ\x9c\x18\xbb\x94\xa2b\xdcВ\xa1\xeaT\xe8\xdb\xea\xf0\x13C\x83SL\xcc\xdc\xd1\v\xec\xe5\x063\xf3\xe7\x98\x0f\xae\xad\xc3\n\x83\x02\xab3d\fC\xd9[\x162T\x1do\x16\a\x8d\x98\xa4\xd8\xd944\x9cIV\xb1Fӝ\xfaZ\xd8ӮdՆ\xe4Α\x15\xb2j\xab\x0e/\xf7\xe9p\xbb\xb8\n=tϵt\xafR\x11\xc7<>o\x879\xb9\x92\x06j0\xf6\xeaɫ\xeb\xe1\x0e\x8c\x99\xdbշ\x97Pĩ\x93\x98\xbd\xd0R9\x87\xac3\\og\xad\x9b\x14\x9c\x90\x98\x98c\x0f\xa68\x9eϢr\x95\xd2(\xe7J\x15z\xbae_\xa4\xa7~\x8dh\x13w<\xe0F\xacX\xe9\xd2\xca9\x9b\x03\x15>\xd0&\xc3\x1bp\\\x15Fa\xdd\xf5\xc3\xccE\x0fȔ&\xa8z#[9#۟\xa6\xc0Y\xb44\xce\xdb\n8_I\x12\xa9`b\xfa\xd0\xf1\xe4Ą\xfc\x1d\xc8\x17\xc6\xcfF\xe5\xfb\r\xfdN\xbeF&\xeaPtP\xc0\x8f\x1b\x1cm\xcbT\x02\x03n\u0600!d\x89*I\xae\xc4J\xd3e\xdcԳ\xfa5r\xcaz$V\x7f,%'\x87N\xfa\xfe\\3j\x9f\x043\x16|\xe1\x86տ\xa5\xd2M\x8a\f\xd5\x04\x0f\xc0aP\xcb\xd1a65\xf5\x98\xbc \xcfs\xd0b\x02\xba\xc9\xf6\x87\xa2\x1e\x06&\xedR\x12\n\xbbXz\xd1\x1d(Hk\xf7M9UT\x1d3\xde\"\x9aƭ\x98\xb3\xf1\x96\x82iXa\xe1\x1c\xb0\x18\x01>\xea<i\x90m\xceS`\x8d\x06\xa5F\x12^\x90\x11\xb5\xdaU\x92\xdd'\x8d\xb1\xa6OE\v\x95QF\xba\xb4\xd9oy\xd7wql,\x15P\"\x17\xff\xaa\xf7\xf8{\xa9\xa8\x0f\xdeD\xe7\xe3\x14k\xe3$\xae\xe3\x1f\x9a\xcd?ھ\x8e\x02\xf1\xdaW\x95Xק\x02\xf2\xd3\fऋ\xf0/\xb2\x80\x1f\xbd\x1f\xcfG]݇\xec\xeb\xf2\xaf\x1f[\xe1\xda}\xfaQ\xf8\x95\v\xf7\xa3\xf0\x997\xf2ۇ\x16[A\xf7ȓk\xfa\xcf2\xe6\x96|{\xffa\x06x¥\xfe\xc7\x19\xb0\xabw\xfd\x1cм\x10\xc0|\xa4\xf5\x83\xb3=\"\xf0\x9b\xdc\xf5l\xb3z\xbf\xcc\x1e恘\xc2탐d\x0f\x81\x86\x1c\x15\xber\xbd\xce9'ia\x89\x9c\xc9\xe6D+\xe6㬋\xdd\xe6\xe0E\x0e\x9bR\xa2\x1b\x1fg\xe3&\x85?\xb2\xa7\xbd\x14\x1f\xf9U6\xe8\xe6\x00J\xcea\x1f\rՇY\x1eGr\xc1\x98l\x9efFkn\x9f\n)\x85>\x84\xf3\xf3\x9c\xe9\xa4Fvr,|f\xc0'\xc7N\xadār\xcctFxH\x86\xf6GO\xe0\xfb\xc9\x13\xd8\x10\xdb\xf8A2hNp!\xfd\xb6\x93\x12k\xd8x?PB]\xbe \"G\xb2\xf3<o\x9e:d鶆\x83\x90\x88˲e\r\xa9\xcc\xd4\xe9\xa4-\xac\x8f\x80\xc2\x15\xf8\x86\xe36F-4\xf9\x115/\xe8.6\xdcl&(\x86^\xdb\xd7\"\x1d#+\x141Ǐ\xd9\xd5푉t\x91\xc8\bX\xa4\x1f\xfb\xffC\x1c\xa3\xf5\x0e\xbf\x86{\xdbv\xf0\x85D\x92T\xa5:\xee\xfe}\xd5o^٧\xf9X\xb1}\xf2@7\xed\xd6OrV\xa3U\x9a1w\xfd*\xfc]J?\xb2>(iM8\x9b\xb4\xc9;\xd4y;d\xa0\x14\x92\x9e\xc3w\x93\xe7\x10\xcd\xe29\x10\xacw|\xfe\xaa\x1e\x85bMw\xf1\x8f\xe3Ѷl\x7f\xb7_\x1c!q\x8b\xff\xb6q7\fx\x81\x0f&\x98\xa2\x19\xe2*\xd3=N\x17\x1c\x87JĹ\xec\xbde2T\xf3m\x8aEV\xf3\x13w!\xe1b\\\x1e\xaf\x9e\x7f\xbb\xb7؋O\n\x9emM\xb7\x1d\x0e\x18\x90\x80\x91-\xbc\x8c\x9b\x1ef\x8b\xbe[\x96} \xe6ɫO\xb1\xd3u\x9a\xc5^\xb9\xfa\xa4\xfb1\x16\x8f\xbd\xb8\x17&0\xc5|\x17\xd2\"\xb6\x9c]\t\xb6\xa0\xa7\xbe\xf6\xb0\x18s@\xb8d\xb4\xb1\xae\xd1P\x1d\xfb\xa7\x9b\x0f\x9d \xbf\x87\xb2>\xd8\x1c8[\x19]\xf86OU<7DB\x9f\x83:TS\xd5볧^\r\xe6b\xf7Ɵ\x84>n\xf3̑!\xfc\xaf\\\x9f\xae-Y'+f\xe5\x88M\x1b\xc3\xee\x1auݎHi\xa6\x90\xaa\x9f\x00\x15c\x8a~\x1c\xdeQJh}\xfb(\x1d\xc1\x92\xbc\xad<h>n\xfb\xe7\xe0E\x00|\x93\xb1\x8f\xe4\xbb&Z\xf5H\x16\xee\x03\xe6\xfbi\xb0\xd9kO\xbb\xa9\xa9\xc5o\x18\x03\nf(\xbb4`@\xb8n\x81b\xb5\xef\xf1\xc4bۣv\x12\x12\xefi{\xd4B\xb2\xb6\x85(\x9b:Q\xd8\x1e\x842Y\x9a\x04\xe8x\x02r\xdf'\x96\x06\xe4\x18\x93\xebr\xcc!\x8b1d\xeacG,\xaff\xa3\x01\r\xecX\x9a\xad\t<%'\x9b\x80E\xe9<&\xa8<\x97\xad\x1cb\x80IA6\xa9\xf36\x95\xfb\xf6\xe9\xc0F\xda\x1e\x89\xfc\xfeC\xf4]\xd5\xe2\x14g\xb0a\x0eU\xc8HS\xf1p6i\x97\xc0\xda\xcep/L\xae\xbb\xda9]\xf2\x13\xc2#\x14\xb4\x7f\x9a\xa2\xeeij-\x11\\\xadu\xb1\xc3\xc9-+\xf4r\xee'\x8b\x97-cM\x94;\xef\xfc^\xa9O\xb4Q\xdd\xeba\x15\xa85\xfa\xb3R\x05\x9fI\x01\x97uDX1*\xa0\xea$\xb9BYq$Ne\xa5_t\x14\xd8K\xecǑc\xf1\x9d\xd5\x11X\xa3\xe3{\x1d\x95\xc4Dx\xe9\xa4m\x9c\x7f\x14\xb80M\x00;*\x01kk\xdcq0j\xbd\x9d\xb5NOz\xc8Ώ`;\u0381ʒ(\a\x81V\x16\xf7y\xde<Z\xfd\x1a*\x06A8\xe0)\xea\xbcy\x84\x1a\xfa\x87\x1a\x1f\x15\xda\x13\\\x80#\xc3N2T\xc7nd \x104\x82ѩIfc\x15\xf7\x8d\x175m\x1co\xce\x1a\xa4\xb6:\x93ղi\xe3\xe8\xd1 \xe0\xedO\x10\x8f\xc0\x81V\x80\xcbý\xb4\xa7\xd47\t\xd7H\x0f{\f^S\x84\xd4Uf\xf6\x86N~\x99؝TܖÑ\xdcƈ\xb6mz\xc9\xfcV\"d\x9f\x013r\x9c\xf7L\x98=\x13fτ\xd93a\xf6L\x98=\x13fτ\xd93a\xf6L\x98=\x13fτ\xd93a\xf6L\x98=\x13fτ\xd93a\xf6L\x98=\x13fτ\xd93a\xf6L\x98=\x13fτI\x84\xfc$3ab_c\x18\xbf\t\x00\xbf\x12\x1bP\xa9]%uv\x8d\x0f5\xedy\x16'\xe5Y\x9aO\x04\x9a\x8f\x8e-}'\xd0~Rk\xf1\xbbd\xfd\xab\xf7ʇ\xb7\x86G\u05f5\xe7\xf1\xd1\n\x92\x9eh\xff\a\x00\x00\xff\xff\x01\x00\x00\xff\xff\xf4'A\x8c\fQ\x00\x00"))
}
