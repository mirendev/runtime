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
	SandboxBoundPortId      = entity.Id("dev.miren.compute/sandbox.bound_port")
	SandboxContainerId      = entity.Id("dev.miren.compute/sandbox.container")
	SandboxExitId           = entity.Id("dev.miren.compute/sandbox.exit")
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
	BoundPort    []BoundPort   `cbor:"bound_port,omitempty" json:"bound_port,omitempty"`
	Container    []Container   `cbor:"container" json:"container"`
	Exit         Exit          `cbor:"exit,omitempty" json:"exit"`
	HostNetwork  bool          `cbor:"hostNetwork,omitempty" json:"hostNetwork,omitempty"`
	Labels       []string      `cbor:"labels,omitempty" json:"labels,omitempty"`
	LastActivity time.Time     `cbor:"last_activity,omitempty" json:"last_activity"`
	LogAttribute types.Labels  `cbor:"logAttribute,omitempty" json:"logAttribute,omitempty"`
	LogEntity    string        `cbor:"logEntity,omitempty" json:"logEntity,omitempty"`
	Network      []Network     `cbor:"network,omitempty" json:"network,omitempty"`
	Route        []Route       `cbor:"route,omitempty" json:"route,omitempty"`
	Spec         SandboxSpec   `cbor:"spec,omitempty" json:"spec"`
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
	sb.Int64("consecutive_crash_count", "dev.miren.compute/sandbox_pool.consecutive_crash_count", schema.Doc("Number of consecutive quick crashes (sandboxes that died within 60s of creation)"))
	sb.Time("cooldown_until", "dev.miren.compute/sandbox_pool.cooldown_until", schema.Doc("Timestamp until which new sandbox creation is paused due to crash loop"))
	sb.Int64("current_instances", "dev.miren.compute/sandbox_pool.current_instances", schema.Doc("Current number of sandbox instances (non-STOPPED)"))
	sb.Int64("desired_instances", "dev.miren.compute/sandbox_pool.desired_instances", schema.Doc("Target number of sandbox instances"))
	sb.Bool("ephemeral", "dev.miren.compute/sandbox_pool.ephemeral", schema.Doc("True when this pool backs an ephemeral AppVersion. Ephemeral pools never scale beyond 1 instance."))
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
	schema.RegisterEncodedSchema("dev.miren.compute", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\xec\\َ\xec<\x11~\r\x0e\xcba\xdf\x04d\x00\xb1\x8b\xed\b\xb8\xe5\x15\"w\\\x9d\xf6tbgl\xa7\x17\xee\x00!!\x04<\x04g\x867\x84k\xe4%\x89\x93؉\xe3F\xfaor3\xb2ݮ\xcfv\xb9\\U.\xd7\xe4\x15ST\xc3\v\x86KV\x13\x0e4+Xݴ\x12\xe0L(\x16o\xb7\xcf\xcc~yR\xbfd\x94a\xf8\xb7\xa6\xbd\xcc{\xa8\x1f\r\xc0\x7f\x8f\x98Ո\xd0\xf9\x00\xc7#\x81\n\x8b\xbf~<\x10|\xfb\x92\x1f#C\r\xc9\x11\xc6\x1c\x84\xd0c\x9d\xdd\x06yo\xe0($'\xb4|]\x02)\x18\x15\x92#B\xa5\xc05\xa2\xf7\xff\x18(\xb7YAA\x85\x0eP\xe9\xe9x\x16\xad\x91\x14\x1d\x1e\xfe8\x13\xd0d_\t\x90q(\x89\x90\xc0\x01\xe7Hj\xd2zܤ\x80\xb0$5h\x98/\x84`ZJ\x81\xe7\x04k\b2T\xa7\x8c\xf8b\x00@\x14'\xc0mEh\xa9\x11\x9e\x9d\xba\x9e\x01ж>\xab?\xf9\x05U-\x88\x7f\x9d\n\xc61\xa3\x80o_\x9dC\x0e\xd4Y\xd7\xedl\xdbС\x82\xdb7\x16I\x9c\x9ezΟ\v\xcdY\"ٚ\xcd?ڲw\xae\xaf\xc0\x01\xe1\xfb\xed\xbdgTM\x96\xe9\xdf˖\x9e)\xbbR\x1f\x93m?\xdbㄉP\xb3\xf3\xb2\xd3v\xed\xba\x90\x96\x9e\x00U\xf2t\xf7\ta\x8fk\xfb\xe8M\xf6\xccS\xaf\xf7\x02\\\x10F\xf5\x82ˮ\xe2lp\xd7V^\xbe\x87\xaa愪\x86\x93\x1a\xf1{\xae\x8e\x1bV\x10\xb7\xcf\x06\x8el\x05H\xd83{\x9dwѿF\x1e\xda?\xeb%|-\x00\x92UH\xc8\xfc\x04\x88\xcb\x03X\x81\xa7\x93\xb6\xb1\xc4\x7f>\x84\xd4p\xf6\f\x85\x81(\xbb\x8a\xa2=\x10\xbcL)\x10\xc5\av3\x94]\xc5R.\xf2\x104\xbdo\x7f4\x13-\x94a\xe3\xed\x9dg\xb7M\x87HN\xfe\xf3-\xa08,Lv`-\xc5yøtt׳Ӫ\xd6D\x14\x15\xa3@\xe5P\xb23\x9cCgs\xe8\xc8\xc9\xfe)\xa4\xe5\x06\xa4\xcc\xd5֥GS\x7f\f((\aB/K+\xd8~\x81\x05\xa1rq\u05c8\xa6W\xfd5C\xbf\x1cfh\xc1\xa8D\x84\x02w\xf8I\x86\xc6\x15v\u0381\xb3\x19p$7\xff\xf110\xd3\x1eH\xb5Ԉ\x1a}_v\x15\x87\x99z\xad__F\xa0GR\xe6GR\xc1\xc4\xf6\xf5\xcd++~\x8aX\xb1;\xcc&\x15\xe2і\x0eT\x86\x91DF\x12ti*F+\xd45\xc3\xd6P\xeb\xd2F\xea\x06ɓ\x95BU\x8aU\xc1\xcf\x06CA\xe8Q<\x86s`\x1b&\x1c\n\xc9\xf8\xddH\xe1P\x9dZt\x8f\x9a\x1bP\x80^\x9c\xbd-T5\xe2\xc0\r\xf4\xa4F\xa5a\x14\x98\xe2T\xc2\x16\xa9k\xd6RW7\x81iX\x91\xaao\xc6H\x95Fڤ\x9b<\xa7I\x83d\x18\x84$\x14\xc9δ\x9e݆)\xb7<\xbaߠ\b\xd6\xf2\x02\xac3bʱra\xd8\x12\xf2\xef\x865\xaf\xb8\x98\x8b\xe2\xc4X\x9d\x8b\x82qCK\x86j\xa7B\xdfV\x87\x9f\x18\x1a\x1ccb掞g/7\x98\x99\xbf\x84|pm\x1dV\x18\xe4Y\x9d!c\x18\xf2\u07b2\x90\xa1\xda\xf1fqЀI\n\x9dMCÙd\x05\xab4ݩ\xaf\xf9=\xedB\x16\x8dO\xee:\xb2L\x16M\xd1\xe2\xe5>-n\x16W\xa1\x87\xee\xb9\x16\xefU*\xe2\x90\xc7\xe7\xec0'\x17RA\t\xc6^=;u=܁1s\xbb\xfa\xf6\x12\x8a8\xb5\x12\xb3+͕s\xc8Z\xc3\xf5fֺI\xc1\t\x89\x899\xf6`\x8a\xe3\xf9,*W)\x8dr.T\xa1\xa7[\xf6Ez\xea׀6\xe9\x8e\a܈\x15+]Z9gs\xa0\xcc\x05\xdadx=\x8e\xab\xc2Ȭ\xbb~\x98\xb9\xe8\x1e\x99\xd2\x04Eod\x8b\xce\xc8\xf6\xa7\xc9s\x16-M\xe7my\x9c\xaf(\x89T0!}\xd8\xf1\xe4Ą\xfc=\xc8+\xe3g\xa3\xf2݆~'_\x03\x13\xedPtP\xc0\x8d\x1b\x1cm\xcbT\x02=n\u0600!d\x8e\nI.\xc4JS=n\xeaY\xfd\x1a8e=\x12+?H\xc9ɡ\x95\xae?W\x8d\xda'\xc1\x8c\x05_\xb8b\xe5\xef\xa8\xec&E\x86j\x84\a\xd0aP\xcb\xd1a6%u\x98\xbc \xcfs\xd0l\x02\xba\xc9\xf6\xfb\xa2\x1e\x06&\xeeR\xe2\v\xbbXz\xd1\x1e(Hk\xf7M9VT;f\xbc\x054M\xb7b\xce\xc6[\n\xa6a\x85\x85s\xc0l\x04\xf8\xa8\xf3\xa4A\xb69O\x9e5\x1a\x94\x12I\xb8\"#jeW\x89v\x9f4ƚ>\x15\r\x14F\x19\xe9\xd2f\xbf\xe5\xa9\xefұ1W@\x91\\\xfc\x9b\xde\xe3\xefǢ>x\x13\x9d\x8f\x93\xad\x8d\x13\x1b\x93\xd0l\xfe\xf1\xf6ud\x88\x97\xae\xaaĺ>\x15\x90\x9f%\x00G]\x84\x7f\x99\x04\xfc\xe8\xfdx>\xea\xea>$_\x97\x7f\xf3\xd8\n\xd7\xeeӏ¯\\\xb8\x1f\x85O\xbc\x91\xdf\xde[l\x05\xdd#O\xae\xe9?O\x98[\xf4\xed\xfdG\t\xe0\x11\x97\xfa\x9f$\xc0\xae\xde\xf5S@\xd3B\x00\xf3\x91\xd6\x0f\xce\xf6\x88\xc0oS׳\xcd\xea\xfd*y\x98\ab\n\xb7w>\xc9\x1e\x02\r)*|\xe5z\x9drN\xe2\xc2\x12)\x93M\x89V\xcc\xc7Y\x17\xbb\xcd\xc1\x8b\x146\xc5D7>$\xe3F\x85?\x92\xa7\xbd\x14\x1f\xf9u2\xe8\xe6\x00J\xcaa\x1f\rՇY\x1eG\xea\x821\xc9<M\x8c\xd6\xdc>\xe5S\n}\b\xe7\x17)Ӊ\x8d\xec\xa4X\xf8ĀO\x8a\x9dZ\x89\x03\xa5\x98\xe9\x84\xf0\x90\xf4폞\xc0\x0f\xa2'\xb0!\xb6\xf1\xc3hД\xe0B\xfcm'&ְ\xf1~\xa0\x84:\xbf\"\"G\xb2\xf32o\x9e:d\U00076183\x90\x88˼a\x15)\xcc\xd4\xe9\xa4ͯ\x8f\x80\xc2\x05\xf8\x86\xe36F\xcd4\xf9\x11UWt\x17\x1bn6\x13\x14C\xaf\xedk\x16\x8f\x91\x14\x8a\x98\xe3\x87\xec\xea\xf6\xc8D\xbcH$\x04,\xe2\x8f\xfd\xff!\x8e\xd18\x87_ým;\xf8B\"I\x8a\\\x1dw\xf7\xbe\xea6\xaf\xec\xd3|\xac\xd0>9\xa0\x9bv\xeb\xa7)\xab\xd1*͘\xbb~\x15\xee.\xc5\x1fY\x17\x944&\x9cM\x9a\xe8\x1dj\x9d\x1d2P\nI\xcf\xe1\xbb\xd1s\bf\xf1\x1c\b\xd6;>\x7fU\x0fB\xb1\xaa\xad\xdd\xe3x\xb4-\xdb\xdf\xed\x17G\x88\xdc\xe2\xbfo\xdc\r\x03\x9e\xe1\x83\t\xa6h\x86t\x95\xe9\x1e\xc7\vN\x87J\xc49\xef\xbde2T\xd3m\x8aEV\xf3\x13w!\xa16.\x8fSO\xbf\xdd[\xec\xc5'\x05Ƕ\xc6ێ\x0e\x18\x90\x80\x91-\xac\xc7M\x0f\xb3E\xdf-\xf3>\x10\xf3\xecԧ\xd8\xf1:\xcdb\xaf\\}\xe2\xfd\x18\x8bǮ\xdd\v\x13\x98b\xba\vi\x11\x1b\xce.\x04[\xd0S_{X\x8c9 \x9c3ZY\xd7h\xa8\x8e\xfd\xd3͇N\x90?@^\x1el\x0e\x9c\xad\x8c.|\x9b\xa7*^*\"\xa1\xcfA\x1d\xaa\xb1\xea\xf5\xc5Q\xaf\x06s\xb1{\xe5N\xe2m\xe5\xc5\xefq\xe38\a\xcf<\xe0\x9b\xaca !4\xd2\xec\x05\xd2T\x1f\xb0oσQ[{\xfb\x8cͽ}\xc3\x18\x907\x85\xb7˓\x05\x84\xcb\x06(&\xb4\\ȼ\xb5=J\xdeR\xba\xdc\xd3\xf6(\x85dM\x03A6\xb5\"\xb3=\be27\x19\xc2\xe1\fݾO(O\xa6cL\xaaM\x9eCfc\xc8\xd8׀P\xe2\xc9F\v\xe3ٱ8e\xecyk\x8d֑\x8b\xd2y\x8c\xd0\t]:\xaf\x8f\x01&G\xd7\xe4\x96\xdb\\\xe7ۧ=\x1bi{D\xf2\xfb\x8f\xc1\x87G\x8b\x93\x9d\xc1\xc6\x01T!!\x8f\xc3\xc1٤]<k;\xc3=3\xc9\xe0j\xe7t\xc9͘\x0eP\xd0\xfe\xed\x86vo7k\x99\xd2j\xad\x8b\x1dNݲ|O\xcbn6u\xde0V\x05\xb9\xf3\xe4\xf6ڔ\\\xeb;\xea\x0eV\x86\x1a\xa3?\vUp\x99\xe4\xf1\xe9F\x84\x05\xa3\x02\x8aV\x92\v\xe4\x05G\xe2\x94\x17\xfa\xc9C\x81]C?\x8e,\xefwVG`\x95\x0e\x80\xb5T\x12\x13\x02\xa5\x93\xb6q\x82\x8e\xe7r2\x06l9\a*sB\x85D\xb4\x00\xa3\xd7_\xe6ͣi\xae\xa1b\x10\x84\x03\x9e\xa2ΛG\xa8\xbe\x7f\rqQ\xa19A\r\x1c\x99u\x93\xa1:v\x88<!\x8d\x11\x8cN\xb21;\xa0ؤ\xc1شq\xcc\xc55Hm\x1e&\xabe\xd3\xc6Q\xf8\xdb\xe3\xb7N\x10\x8f\xc0\x81\x16\x80\xf3\xc3=\xb7\xc7\xc9\xd5ݗ@\x0f+\xaf\xaf1\xd2\xd4Uf\x86\x81N~\x99\x18\x88X܆Ñ\xdcƈ\xb6mz]\xfaV$d\x9f\xcb1r\x01\xf7\x9c\x8e=\xa7c\xcf\xe9\xd8s:\xf6\x9c\x8e=\xa7c\xcf\xe9\xd8s:\xf6\x9c\x8e=\xa7c\xcf\xe9\xd8s:\xf6\x9c\x8e=\xa7c\xcf\xe9\xd8s:\xf6\x9c\x8e=\xa7c\xcf\xe9\xd8s:\xf6\x9c\x8e=\xa7c\xcf鈄\xfc$s:B\xdf\x15\x18\xbf\t\x00\xbf\x10\x1bP)\xbbJ\xec\xec*\x17j\xda\xf3,Nʳ4\x1f\xbb3\x9f\xcfZ\xfa\xe2\x9d\xfd8\xd4\xe2\x17\xb6\xfa\xe7\xe9\x95OH\r\xaf\xa3k\xefأ\x15D\xbd\xa5\xfe\x0f\x00\x00\xff\xff\x01\x00\x00\xff\xff\x90\x04\xd7[\xd6O\x00\x00"))
}
