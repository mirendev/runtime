package core_v1alpha

import (
	"time"

	entity "miren.dev/runtime/pkg/entity"
	schema "miren.dev/runtime/pkg/entity/schema"
	types "miren.dev/runtime/pkg/entity/types"
)

const (
	ConfigSpecEntrypointId     = entity.Id("dev.miren.core/component.config_spec.entrypoint")
	ConfigSpecServicesId       = entity.Id("dev.miren.core/component.config_spec.services")
	ConfigSpecStartDirectoryId = entity.Id("dev.miren.core/component.config_spec.start_directory")
	ConfigSpecTasksId          = entity.Id("dev.miren.core/component.config_spec.tasks")
	ConfigSpecVariablesId      = entity.Id("dev.miren.core/component.config_spec.variables")
)

type ConfigSpec struct {
	Entrypoint     string                `cbor:"entrypoint,omitempty" json:"entrypoint,omitempty"`
	Services       []ConfigSpecServices  `cbor:"services,omitempty" json:"services,omitempty"`
	StartDirectory string                `cbor:"start_directory,omitempty" json:"start_directory,omitempty"`
	Tasks          []ConfigSpecTasks     `cbor:"tasks,omitempty" json:"tasks,omitempty"`
	Variables      []ConfigSpecVariables `cbor:"variables,omitempty" json:"variables,omitempty"`
}

func (o *ConfigSpec) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(ConfigSpecEntrypointId); ok && a.Value.Kind() == entity.KindString {
		o.Entrypoint = a.Value.String()
	}
	for _, a := range e.GetAll(ConfigSpecServicesId) {
		if a.Value.Kind() == entity.KindComponent {
			var v ConfigSpecServices
			v.Decode(a.Value.Component())
			o.Services = append(o.Services, v)
		}
	}
	if a, ok := e.Get(ConfigSpecStartDirectoryId); ok && a.Value.Kind() == entity.KindString {
		o.StartDirectory = a.Value.String()
	}
	for _, a := range e.GetAll(ConfigSpecTasksId) {
		if a.Value.Kind() == entity.KindComponent {
			var v ConfigSpecTasks
			v.Decode(a.Value.Component())
			o.Tasks = append(o.Tasks, v)
		}
	}
	for _, a := range e.GetAll(ConfigSpecVariablesId) {
		if a.Value.Kind() == entity.KindComponent {
			var v ConfigSpecVariables
			v.Decode(a.Value.Component())
			o.Variables = append(o.Variables, v)
		}
	}
}

func (o *ConfigSpec) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Entrypoint) {
		attrs = append(attrs, entity.String(ConfigSpecEntrypointId, o.Entrypoint))
	}
	for _, v := range o.Services {
		attrs = append(attrs, entity.Component(ConfigSpecServicesId, v.Encode()))
	}
	if !entity.Empty(o.StartDirectory) {
		attrs = append(attrs, entity.String(ConfigSpecStartDirectoryId, o.StartDirectory))
	}
	for _, v := range o.Tasks {
		attrs = append(attrs, entity.Component(ConfigSpecTasksId, v.Encode()))
	}
	for _, v := range o.Variables {
		attrs = append(attrs, entity.Component(ConfigSpecVariablesId, v.Encode()))
	}
	return
}

func (o *ConfigSpec) Empty() bool {
	if !entity.Empty(o.Entrypoint) {
		return false
	}
	if len(o.Services) != 0 {
		return false
	}
	if !entity.Empty(o.StartDirectory) {
		return false
	}
	if len(o.Tasks) != 0 {
		return false
	}
	if len(o.Variables) != 0 {
		return false
	}
	return true
}

func (o *ConfigSpec) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("entrypoint", "dev.miren.core/component.config_spec.entrypoint", schema.Doc("The container entrypoint command"))
	sb.Component("services", "dev.miren.core/component.config_spec.services", schema.Doc("Per-service configuration"), schema.Many)
	(&ConfigSpecServices{}).InitSchema(sb.Builder("component.config_spec.services"))
	sb.String("start_directory", "dev.miren.core/component.config_spec.start_directory", schema.Doc("Directory to start the process in; defaults to /app."))
	sb.Component("tasks", "dev.miren.core/component.config_spec.tasks", schema.Doc("Per-task configuration. A task is a command the platform knows how to run, as opposed to a service, which is a process it keeps up. Tasks carry no ports, concurrency, image, or disks: a task always runs in the app's image, and both per-task image and per-task disks are deliberate v1 cuts tracked in RFD-97."), schema.Many)
	(&ConfigSpecTasks{}).InitSchema(sb.Builder("component.config_spec.tasks"))
	sb.Component("variables", "dev.miren.core/component.config_spec.variables", schema.Doc("Environment variables and configuration values"), schema.Many)
	(&ConfigSpecVariables{}).InitSchema(sb.Builder("component.config_spec.variables"))
}

const (
	ConfigSpecServicesCommandId     = entity.Id("dev.miren.core/component.config_spec.services.command")
	ConfigSpecServicesConcurrencyId = entity.Id("dev.miren.core/component.config_spec.services.concurrency")
	ConfigSpecServicesDisksId       = entity.Id("dev.miren.core/component.config_spec.services.disks")
	ConfigSpecServicesEnvId         = entity.Id("dev.miren.core/component.config_spec.services.env")
	ConfigSpecServicesImageId       = entity.Id("dev.miren.core/component.config_spec.services.image")
	ConfigSpecServicesNameId        = entity.Id("dev.miren.core/component.config_spec.services.name")
	ConfigSpecServicesPortId        = entity.Id("dev.miren.core/component.config_spec.services.port")
	ConfigSpecServicesPortNameId    = entity.Id("dev.miren.core/component.config_spec.services.port_name")
	ConfigSpecServicesPortTimeoutId = entity.Id("dev.miren.core/component.config_spec.services.port_timeout")
	ConfigSpecServicesPortTypeId    = entity.Id("dev.miren.core/component.config_spec.services.port_type")
	ConfigSpecServicesPortsId       = entity.Id("dev.miren.core/component.config_spec.services.ports")
)

type ConfigSpecServices struct {
	Command     string                        `cbor:"command,omitempty" json:"command,omitempty"`
	Concurrency ConfigSpecServicesConcurrency `cbor:"concurrency,omitempty" json:"concurrency"`
	Disks       []ConfigSpecServicesDisks     `cbor:"disks,omitempty" json:"disks,omitempty"`
	Env         []ConfigSpecServicesEnv       `cbor:"env,omitempty" json:"env,omitempty"`
	Image       string                        `cbor:"image,omitempty" json:"image,omitempty"`
	Name        string                        `cbor:"name,omitempty" json:"name,omitempty"`
	Port        int64                         `cbor:"port,omitempty" json:"port,omitempty"`
	PortName    string                        `cbor:"port_name,omitempty" json:"port_name,omitempty"`
	PortTimeout string                        `cbor:"port_timeout,omitempty" json:"port_timeout,omitempty"`
	PortType    string                        `cbor:"port_type,omitempty" json:"port_type,omitempty"`
	Ports       []ConfigSpecServicesPorts     `cbor:"ports,omitempty" json:"ports,omitempty"`
}

func (o *ConfigSpecServices) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(ConfigSpecServicesCommandId); ok && a.Value.Kind() == entity.KindString {
		o.Command = a.Value.String()
	}
	if a, ok := e.Get(ConfigSpecServicesConcurrencyId); ok && a.Value.Kind() == entity.KindComponent {
		o.Concurrency.Decode(a.Value.Component())
	}
	for _, a := range e.GetAll(ConfigSpecServicesDisksId) {
		if a.Value.Kind() == entity.KindComponent {
			var v ConfigSpecServicesDisks
			v.Decode(a.Value.Component())
			o.Disks = append(o.Disks, v)
		}
	}
	for _, a := range e.GetAll(ConfigSpecServicesEnvId) {
		if a.Value.Kind() == entity.KindComponent {
			var v ConfigSpecServicesEnv
			v.Decode(a.Value.Component())
			o.Env = append(o.Env, v)
		}
	}
	if a, ok := e.Get(ConfigSpecServicesImageId); ok && a.Value.Kind() == entity.KindString {
		o.Image = a.Value.String()
	}
	if a, ok := e.Get(ConfigSpecServicesNameId); ok && a.Value.Kind() == entity.KindString {
		o.Name = a.Value.String()
	}
	if a, ok := e.Get(ConfigSpecServicesPortId); ok && a.Value.Kind() == entity.KindInt64 {
		o.Port = a.Value.Int64()
	}
	if a, ok := e.Get(ConfigSpecServicesPortNameId); ok && a.Value.Kind() == entity.KindString {
		o.PortName = a.Value.String()
	}
	if a, ok := e.Get(ConfigSpecServicesPortTimeoutId); ok && a.Value.Kind() == entity.KindString {
		o.PortTimeout = a.Value.String()
	}
	if a, ok := e.Get(ConfigSpecServicesPortTypeId); ok && a.Value.Kind() == entity.KindString {
		o.PortType = a.Value.String()
	}
	for _, a := range e.GetAll(ConfigSpecServicesPortsId) {
		if a.Value.Kind() == entity.KindComponent {
			var v ConfigSpecServicesPorts
			v.Decode(a.Value.Component())
			o.Ports = append(o.Ports, v)
		}
	}
}

func (o *ConfigSpecServices) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Command) {
		attrs = append(attrs, entity.String(ConfigSpecServicesCommandId, o.Command))
	}
	if !o.Concurrency.Empty() {
		attrs = append(attrs, entity.Component(ConfigSpecServicesConcurrencyId, o.Concurrency.Encode()))
	}
	for _, v := range o.Disks {
		attrs = append(attrs, entity.Component(ConfigSpecServicesDisksId, v.Encode()))
	}
	for _, v := range o.Env {
		attrs = append(attrs, entity.Component(ConfigSpecServicesEnvId, v.Encode()))
	}
	if !entity.Empty(o.Image) {
		attrs = append(attrs, entity.String(ConfigSpecServicesImageId, o.Image))
	}
	if !entity.Empty(o.Name) {
		attrs = append(attrs, entity.String(ConfigSpecServicesNameId, o.Name))
	}
	if !entity.Empty(o.Port) {
		attrs = append(attrs, entity.Int64(ConfigSpecServicesPortId, o.Port))
	}
	if !entity.Empty(o.PortName) {
		attrs = append(attrs, entity.String(ConfigSpecServicesPortNameId, o.PortName))
	}
	if !entity.Empty(o.PortTimeout) {
		attrs = append(attrs, entity.String(ConfigSpecServicesPortTimeoutId, o.PortTimeout))
	}
	if !entity.Empty(o.PortType) {
		attrs = append(attrs, entity.String(ConfigSpecServicesPortTypeId, o.PortType))
	}
	for _, v := range o.Ports {
		attrs = append(attrs, entity.Component(ConfigSpecServicesPortsId, v.Encode()))
	}
	return
}

func (o *ConfigSpecServices) Empty() bool {
	if !entity.Empty(o.Command) {
		return false
	}
	if !o.Concurrency.Empty() {
		return false
	}
	if len(o.Disks) != 0 {
		return false
	}
	if len(o.Env) != 0 {
		return false
	}
	if !entity.Empty(o.Image) {
		return false
	}
	if !entity.Empty(o.Name) {
		return false
	}
	if !entity.Empty(o.Port) {
		return false
	}
	if !entity.Empty(o.PortName) {
		return false
	}
	if !entity.Empty(o.PortTimeout) {
		return false
	}
	if !entity.Empty(o.PortType) {
		return false
	}
	if len(o.Ports) != 0 {
		return false
	}
	return true
}

func (o *ConfigSpecServices) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("command", "dev.miren.core/component.config_spec.services.command", schema.Doc("The command to run for the service"))
	sb.Component("concurrency", "dev.miren.core/component.config_spec.services.concurrency", schema.Doc("Concurrency configuration for this service"))
	(&ConfigSpecServicesConcurrency{}).InitSchema(sb.Builder("component.config_spec.services.concurrency"))
	sb.Component("disks", "dev.miren.core/component.config_spec.services.disks", schema.Doc("Disk attachments for this service"), schema.Many)
	(&ConfigSpecServicesDisks{}).InitSchema(sb.Builder("component.config_spec.services.disks"))
	sb.Component("env", "dev.miren.core/component.config_spec.services.env", schema.Doc("Environment variables for this service only"), schema.Many)
	(&ConfigSpecServicesEnv{}).InitSchema(sb.Builder("component.config_spec.services.env"))
	sb.String("image", "dev.miren.core/component.config_spec.services.image", schema.Doc("Optional container image for this service"))
	sb.String("name", "dev.miren.core/component.config_spec.services.name", schema.Doc("The service name (e.g. web, worker)"))
	sb.Int64("port", "dev.miren.core/component.config_spec.services.port", schema.Doc("The TCP port the service listens on"))
	sb.String("port_name", "dev.miren.core/component.config_spec.services.port_name", schema.Doc("The name of the port (e.g. http, grpc)"))
	sb.String("port_timeout", "dev.miren.core/component.config_spec.services.port_timeout", schema.Doc("Custom port-wait timeout (e.g. \"60s\", \"2m\"). Empty falls back to the 15s default; invalid duration strings are rejected at parse time."))
	sb.String("port_type", "dev.miren.core/component.config_spec.services.port_type", schema.Doc("The type of the port (e.g. http, tcp)"))
	sb.Component("ports", "dev.miren.core/component.config_spec.services.ports", schema.Doc("Network ports this service listens on. Overrides scalar port/port_name/port_type."), schema.Many)
	(&ConfigSpecServicesPorts{}).InitSchema(sb.Builder("component.config_spec.services.ports"))
}

const (
	ConfigSpecServicesConcurrencyModeId                = entity.Id("dev.miren.core/component.config_spec.services.concurrency.mode")
	ConfigSpecServicesConcurrencyNumInstancesId        = entity.Id("dev.miren.core/component.config_spec.services.concurrency.num_instances")
	ConfigSpecServicesConcurrencyRequestsPerInstanceId = entity.Id("dev.miren.core/component.config_spec.services.concurrency.requests_per_instance")
	ConfigSpecServicesConcurrencyScaleDownDelayId      = entity.Id("dev.miren.core/component.config_spec.services.concurrency.scale_down_delay")
	ConfigSpecServicesConcurrencyShutdownTimeoutId     = entity.Id("dev.miren.core/component.config_spec.services.concurrency.shutdown_timeout")
)

type ConfigSpecServicesConcurrency struct {
	Mode                string `cbor:"mode,omitempty" json:"mode,omitempty"`
	NumInstances        int64  `cbor:"num_instances,omitempty" json:"num_instances,omitempty"`
	RequestsPerInstance int64  `cbor:"requests_per_instance,omitempty" json:"requests_per_instance,omitempty"`
	ScaleDownDelay      string `cbor:"scale_down_delay,omitempty" json:"scale_down_delay,omitempty"`
	ShutdownTimeout     string `cbor:"shutdown_timeout,omitempty" json:"shutdown_timeout,omitempty"`
}

func (o *ConfigSpecServicesConcurrency) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(ConfigSpecServicesConcurrencyModeId); ok && a.Value.Kind() == entity.KindString {
		o.Mode = a.Value.String()
	}
	if a, ok := e.Get(ConfigSpecServicesConcurrencyNumInstancesId); ok && a.Value.Kind() == entity.KindInt64 {
		o.NumInstances = a.Value.Int64()
	}
	if a, ok := e.Get(ConfigSpecServicesConcurrencyRequestsPerInstanceId); ok && a.Value.Kind() == entity.KindInt64 {
		o.RequestsPerInstance = a.Value.Int64()
	}
	if a, ok := e.Get(ConfigSpecServicesConcurrencyScaleDownDelayId); ok && a.Value.Kind() == entity.KindString {
		o.ScaleDownDelay = a.Value.String()
	}
	if a, ok := e.Get(ConfigSpecServicesConcurrencyShutdownTimeoutId); ok && a.Value.Kind() == entity.KindString {
		o.ShutdownTimeout = a.Value.String()
	}
}

func (o *ConfigSpecServicesConcurrency) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Mode) {
		attrs = append(attrs, entity.String(ConfigSpecServicesConcurrencyModeId, o.Mode))
	}
	if !entity.Empty(o.NumInstances) {
		attrs = append(attrs, entity.Int64(ConfigSpecServicesConcurrencyNumInstancesId, o.NumInstances))
	}
	if !entity.Empty(o.RequestsPerInstance) {
		attrs = append(attrs, entity.Int64(ConfigSpecServicesConcurrencyRequestsPerInstanceId, o.RequestsPerInstance))
	}
	if !entity.Empty(o.ScaleDownDelay) {
		attrs = append(attrs, entity.String(ConfigSpecServicesConcurrencyScaleDownDelayId, o.ScaleDownDelay))
	}
	if !entity.Empty(o.ShutdownTimeout) {
		attrs = append(attrs, entity.String(ConfigSpecServicesConcurrencyShutdownTimeoutId, o.ShutdownTimeout))
	}
	return
}

func (o *ConfigSpecServicesConcurrency) Empty() bool {
	if !entity.Empty(o.Mode) {
		return false
	}
	if !entity.Empty(o.NumInstances) {
		return false
	}
	if !entity.Empty(o.RequestsPerInstance) {
		return false
	}
	if !entity.Empty(o.ScaleDownDelay) {
		return false
	}
	if !entity.Empty(o.ShutdownTimeout) {
		return false
	}
	return true
}

func (o *ConfigSpecServicesConcurrency) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("mode", "dev.miren.core/component.config_spec.services.concurrency.mode", schema.Doc("The concurrency mode (auto or fixed)"))
	sb.Int64("num_instances", "dev.miren.core/component.config_spec.services.concurrency.num_instances", schema.Doc("For fixed mode, number of instances to maintain"))
	sb.Int64("requests_per_instance", "dev.miren.core/component.config_spec.services.concurrency.requests_per_instance", schema.Doc("For auto mode, number of concurrent requests per instance"))
	sb.String("scale_down_delay", "dev.miren.core/component.config_spec.services.concurrency.scale_down_delay", schema.Doc("For auto mode, delay before scaling down idle instances"))
	sb.String("shutdown_timeout", "dev.miren.core/component.config_spec.services.concurrency.shutdown_timeout", schema.Doc("Time to wait for graceful shutdown before force-killing"))
}

const (
	ConfigSpecServicesDisksFilesystemId    = entity.Id("dev.miren.core/component.config_spec.services.disks.filesystem")
	ConfigSpecServicesDisksLeaseTimeoutId  = entity.Id("dev.miren.core/component.config_spec.services.disks.lease_timeout")
	ConfigSpecServicesDisksMountPathId     = entity.Id("dev.miren.core/component.config_spec.services.disks.mount_path")
	ConfigSpecServicesDisksNameId          = entity.Id("dev.miren.core/component.config_spec.services.disks.name")
	ConfigSpecServicesDisksOwnerId         = entity.Id("dev.miren.core/component.config_spec.services.disks.owner")
	ConfigSpecServicesDisksProviderId      = entity.Id("dev.miren.core/component.config_spec.services.disks.provider")
	ConfigSpecServicesDisksProviderMirenId = entity.Id("dev.miren.core/component.config_spec.services.disks.provider.miren")
	ConfigSpecServicesDisksProviderLocalId = entity.Id("dev.miren.core/component.config_spec.services.disks.provider.local")
	ConfigSpecServicesDisksReadOnlyId      = entity.Id("dev.miren.core/component.config_spec.services.disks.read_only")
	ConfigSpecServicesDisksSizeGbId        = entity.Id("dev.miren.core/component.config_spec.services.disks.size_gb")
)

type ConfigSpecServicesDisks struct {
	Filesystem   string                          `cbor:"filesystem,omitempty" json:"filesystem,omitempty"`
	LeaseTimeout string                          `cbor:"lease_timeout,omitempty" json:"lease_timeout,omitempty"`
	MountPath    string                          `cbor:"mount_path,omitempty" json:"mount_path,omitempty"`
	Name         string                          `cbor:"name,omitempty" json:"name,omitempty"`
	Owner        string                          `cbor:"owner,omitempty" json:"owner,omitempty"`
	Provider     ConfigSpecServicesDisksProvider `cbor:"provider,omitempty" json:"provider,omitempty"`
	ReadOnly     bool                            `cbor:"read_only,omitempty" json:"read_only,omitempty"`
	SizeGb       int64                           `cbor:"size_gb,omitempty" json:"size_gb,omitempty"`
}

type ConfigSpecServicesDisksProvider string

const (
	ConfigSpecServicesDisksMIREN ConfigSpecServicesDisksProvider = "component.config_spec.services.disks.provider.miren"
	ConfigSpecServicesDisksLOCAL ConfigSpecServicesDisksProvider = "component.config_spec.services.disks.provider.local"
)

var ConfigSpecServicesDisksproviderFromId = map[entity.Id]ConfigSpecServicesDisksProvider{ConfigSpecServicesDisksProviderMirenId: ConfigSpecServicesDisksMIREN, ConfigSpecServicesDisksProviderLocalId: ConfigSpecServicesDisksLOCAL}
var ConfigSpecServicesDisksproviderToId = map[ConfigSpecServicesDisksProvider]entity.Id{ConfigSpecServicesDisksMIREN: ConfigSpecServicesDisksProviderMirenId, ConfigSpecServicesDisksLOCAL: ConfigSpecServicesDisksProviderLocalId}

func (o *ConfigSpecServicesDisks) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(ConfigSpecServicesDisksFilesystemId); ok && a.Value.Kind() == entity.KindString {
		o.Filesystem = a.Value.String()
	}
	if a, ok := e.Get(ConfigSpecServicesDisksLeaseTimeoutId); ok && a.Value.Kind() == entity.KindString {
		o.LeaseTimeout = a.Value.String()
	}
	if a, ok := e.Get(ConfigSpecServicesDisksMountPathId); ok && a.Value.Kind() == entity.KindString {
		o.MountPath = a.Value.String()
	}
	if a, ok := e.Get(ConfigSpecServicesDisksNameId); ok && a.Value.Kind() == entity.KindString {
		o.Name = a.Value.String()
	}
	if a, ok := e.Get(ConfigSpecServicesDisksOwnerId); ok && a.Value.Kind() == entity.KindString {
		o.Owner = a.Value.String()
	}
	if a, ok := e.Get(ConfigSpecServicesDisksProviderId); ok && a.Value.Kind() == entity.KindId {
		o.Provider = ConfigSpecServicesDisksproviderFromId[a.Value.Id()]
	}
	if a, ok := e.Get(ConfigSpecServicesDisksReadOnlyId); ok && a.Value.Kind() == entity.KindBool {
		o.ReadOnly = a.Value.Bool()
	}
	if a, ok := e.Get(ConfigSpecServicesDisksSizeGbId); ok && a.Value.Kind() == entity.KindInt64 {
		o.SizeGb = a.Value.Int64()
	}
}

func (o *ConfigSpecServicesDisks) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Filesystem) {
		attrs = append(attrs, entity.String(ConfigSpecServicesDisksFilesystemId, o.Filesystem))
	}
	if !entity.Empty(o.LeaseTimeout) {
		attrs = append(attrs, entity.String(ConfigSpecServicesDisksLeaseTimeoutId, o.LeaseTimeout))
	}
	if !entity.Empty(o.MountPath) {
		attrs = append(attrs, entity.String(ConfigSpecServicesDisksMountPathId, o.MountPath))
	}
	if !entity.Empty(o.Name) {
		attrs = append(attrs, entity.String(ConfigSpecServicesDisksNameId, o.Name))
	}
	if !entity.Empty(o.Owner) {
		attrs = append(attrs, entity.String(ConfigSpecServicesDisksOwnerId, o.Owner))
	}
	if a, ok := ConfigSpecServicesDisksproviderToId[o.Provider]; ok {
		attrs = append(attrs, entity.Ref(ConfigSpecServicesDisksProviderId, a))
	}
	attrs = append(attrs, entity.Bool(ConfigSpecServicesDisksReadOnlyId, o.ReadOnly))
	if !entity.Empty(o.SizeGb) {
		attrs = append(attrs, entity.Int64(ConfigSpecServicesDisksSizeGbId, o.SizeGb))
	}
	return
}

func (o *ConfigSpecServicesDisks) Empty() bool {
	if !entity.Empty(o.Filesystem) {
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
	if o.Provider != "" {
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

func (o *ConfigSpecServicesDisks) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("filesystem", "dev.miren.core/component.config_spec.services.disks.filesystem", schema.Doc("Filesystem type (ext4, xfs, btrfs) for auto-creating the disk"))
	sb.String("lease_timeout", "dev.miren.core/component.config_spec.services.disks.lease_timeout", schema.Doc("Timeout for acquiring the disk lease"))
	sb.String("mount_path", "dev.miren.core/component.config_spec.services.disks.mount_path", schema.Doc("The path inside the container where the disk will be mounted"))
	sb.String("name", "dev.miren.core/component.config_spec.services.disks.name", schema.Doc("The name of the disk"))
	sb.String("owner", "dev.miren.core/component.config_spec.services.disks.owner", schema.Doc("Ownership policy for the mounted disk. Empty (default) makes the disk writable by the container's run user; \"keep\" leaves the raw mount ownership untouched; \"uid\" or \"uid:gid\" pins a specific numeric owner."))
	sb.Singleton("dev.miren.core/component.config_spec.services.disks.provider.miren")
	sb.Singleton("dev.miren.core/component.config_spec.services.disks.provider.local")
	sb.Ref("provider", "dev.miren.core/component.config_spec.services.disks.provider", schema.Doc("Disk provider: 'miren' (default) for network disks, 'local' for node-local persistent storage"), schema.Choices(ConfigSpecServicesDisksProviderMirenId, ConfigSpecServicesDisksProviderLocalId))
	sb.Bool("read_only", "dev.miren.core/component.config_spec.services.disks.read_only", schema.Doc("Whether to mount the disk as read-only"))
	sb.Int64("size_gb", "dev.miren.core/component.config_spec.services.disks.size_gb", schema.Doc("Size in GB for auto-creating the disk if it doesn't exist"))
}

const (
	ConfigSpecServicesEnvBackendId     = entity.Id("dev.miren.core/component.config_spec.services.env.backend")
	ConfigSpecServicesEnvDescriptionId = entity.Id("dev.miren.core/component.config_spec.services.env.description")
	ConfigSpecServicesEnvKeyId         = entity.Id("dev.miren.core/component.config_spec.services.env.key")
	ConfigSpecServicesEnvRequiredId    = entity.Id("dev.miren.core/component.config_spec.services.env.required")
	ConfigSpecServicesEnvSensitiveId   = entity.Id("dev.miren.core/component.config_spec.services.env.sensitive")
	ConfigSpecServicesEnvSourceId      = entity.Id("dev.miren.core/component.config_spec.services.env.source")
	ConfigSpecServicesEnvValueId       = entity.Id("dev.miren.core/component.config_spec.services.env.value")
)

type ConfigSpecServicesEnv struct {
	Backend     string `cbor:"backend,omitempty" json:"backend,omitempty"`
	Description string `cbor:"description,omitempty" json:"description,omitempty"`
	Key         string `cbor:"key,omitempty" json:"key,omitempty"`
	Required    bool   `cbor:"required,omitempty" json:"required,omitempty"`
	Sensitive   bool   `cbor:"sensitive,omitempty" json:"sensitive,omitempty"`
	Source      string `cbor:"source,omitempty" json:"source,omitempty"`
	Value       string `cbor:"value,omitempty" json:"value,omitempty"`
}

func (o *ConfigSpecServicesEnv) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(ConfigSpecServicesEnvBackendId); ok && a.Value.Kind() == entity.KindString {
		o.Backend = a.Value.String()
	}
	if a, ok := e.Get(ConfigSpecServicesEnvDescriptionId); ok && a.Value.Kind() == entity.KindString {
		o.Description = a.Value.String()
	}
	if a, ok := e.Get(ConfigSpecServicesEnvKeyId); ok && a.Value.Kind() == entity.KindString {
		o.Key = a.Value.String()
	}
	if a, ok := e.Get(ConfigSpecServicesEnvRequiredId); ok && a.Value.Kind() == entity.KindBool {
		o.Required = a.Value.Bool()
	}
	if a, ok := e.Get(ConfigSpecServicesEnvSensitiveId); ok && a.Value.Kind() == entity.KindBool {
		o.Sensitive = a.Value.Bool()
	}
	if a, ok := e.Get(ConfigSpecServicesEnvSourceId); ok && a.Value.Kind() == entity.KindString {
		o.Source = a.Value.String()
	}
	if a, ok := e.Get(ConfigSpecServicesEnvValueId); ok && a.Value.Kind() == entity.KindString {
		o.Value = a.Value.String()
	}
}

func (o *ConfigSpecServicesEnv) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Backend) {
		attrs = append(attrs, entity.String(ConfigSpecServicesEnvBackendId, o.Backend))
	}
	if !entity.Empty(o.Description) {
		attrs = append(attrs, entity.String(ConfigSpecServicesEnvDescriptionId, o.Description))
	}
	if !entity.Empty(o.Key) {
		attrs = append(attrs, entity.String(ConfigSpecServicesEnvKeyId, o.Key))
	}
	attrs = append(attrs, entity.Bool(ConfigSpecServicesEnvRequiredId, o.Required))
	attrs = append(attrs, entity.Bool(ConfigSpecServicesEnvSensitiveId, o.Sensitive))
	if !entity.Empty(o.Source) {
		attrs = append(attrs, entity.String(ConfigSpecServicesEnvSourceId, o.Source))
	}
	if !entity.Empty(o.Value) {
		attrs = append(attrs, entity.String(ConfigSpecServicesEnvValueId, o.Value))
	}
	return
}

func (o *ConfigSpecServicesEnv) Empty() bool {
	if !entity.Empty(o.Backend) {
		return false
	}
	if !entity.Empty(o.Description) {
		return false
	}
	if !entity.Empty(o.Key) {
		return false
	}
	if !entity.Empty(o.Required) {
		return false
	}
	if !entity.Empty(o.Sensitive) {
		return false
	}
	if !entity.Empty(o.Source) {
		return false
	}
	if !entity.Empty(o.Value) {
		return false
	}
	return true
}

func (o *ConfigSpecServicesEnv) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("backend", "dev.miren.core/component.config_spec.services.env.backend", schema.Doc("Where the value comes from. Empty means value holds an inline literal; any other name refers to a registered secret backend instance (e.g. cluster) and value holds a backend-relative reference, optionally pinned with @version."))
	sb.String("description", "dev.miren.core/component.config_spec.services.env.description", schema.Doc("Human-readable description of this variable's purpose"))
	sb.String("key", "dev.miren.core/component.config_spec.services.env.key", schema.Doc("The name of the variable"))
	sb.Bool("required", "dev.miren.core/component.config_spec.services.env.required", schema.Doc("Whether this variable must have a non-empty value for deploy to succeed"))
	sb.Bool("sensitive", "dev.miren.core/component.config_spec.services.env.sensitive", schema.Doc("Whether or not the value is sensitive"))
	sb.String("source", "dev.miren.core/component.config_spec.services.env.source", schema.Doc("The source of the variable (config or manual). Defaults to config for backward compatibility."))
	sb.String("value", "dev.miren.core/component.config_spec.services.env.value", schema.Doc("The value of the variable"))
}

const (
	ConfigSpecServicesPortsNameId        = entity.Id("dev.miren.core/component.config_spec.services.ports.name")
	ConfigSpecServicesPortsNodePortId    = entity.Id("dev.miren.core/component.config_spec.services.ports.node_port")
	ConfigSpecServicesPortsPortId        = entity.Id("dev.miren.core/component.config_spec.services.ports.port")
	ConfigSpecServicesPortsProtocolId    = entity.Id("dev.miren.core/component.config_spec.services.ports.protocol")
	ConfigSpecServicesPortsProtocolTcpId = entity.Id("dev.miren.core/component.config_spec.services.ports.protocol.tcp")
	ConfigSpecServicesPortsProtocolUdpId = entity.Id("dev.miren.core/component.config_spec.services.ports.protocol.udp")
	ConfigSpecServicesPortsTypeId        = entity.Id("dev.miren.core/component.config_spec.services.ports.type")
)

type ConfigSpecServicesPorts struct {
	Name     string                          `cbor:"name" json:"name"`
	NodePort int64                           `cbor:"node_port,omitempty" json:"node_port,omitempty"`
	Port     int64                           `cbor:"port" json:"port"`
	Protocol ConfigSpecServicesPortsProtocol `cbor:"protocol,omitempty" json:"protocol,omitempty"`
	Type     string                          `cbor:"type,omitempty" json:"type,omitempty"`
}

type ConfigSpecServicesPortsProtocol string

const (
	ConfigSpecServicesPortsTCP ConfigSpecServicesPortsProtocol = "component.config_spec.services.ports.protocol.tcp"
	ConfigSpecServicesPortsUDP ConfigSpecServicesPortsProtocol = "component.config_spec.services.ports.protocol.udp"
)

var ConfigSpecServicesPortsprotocolFromId = map[entity.Id]ConfigSpecServicesPortsProtocol{ConfigSpecServicesPortsProtocolTcpId: ConfigSpecServicesPortsTCP, ConfigSpecServicesPortsProtocolUdpId: ConfigSpecServicesPortsUDP}
var ConfigSpecServicesPortsprotocolToId = map[ConfigSpecServicesPortsProtocol]entity.Id{ConfigSpecServicesPortsTCP: ConfigSpecServicesPortsProtocolTcpId, ConfigSpecServicesPortsUDP: ConfigSpecServicesPortsProtocolUdpId}

func (o *ConfigSpecServicesPorts) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(ConfigSpecServicesPortsNameId); ok && a.Value.Kind() == entity.KindString {
		o.Name = a.Value.String()
	}
	if a, ok := e.Get(ConfigSpecServicesPortsNodePortId); ok && a.Value.Kind() == entity.KindInt64 {
		o.NodePort = a.Value.Int64()
	}
	if a, ok := e.Get(ConfigSpecServicesPortsPortId); ok && a.Value.Kind() == entity.KindInt64 {
		o.Port = a.Value.Int64()
	}
	if a, ok := e.Get(ConfigSpecServicesPortsProtocolId); ok && a.Value.Kind() == entity.KindId {
		o.Protocol = ConfigSpecServicesPortsprotocolFromId[a.Value.Id()]
	}
	if a, ok := e.Get(ConfigSpecServicesPortsTypeId); ok && a.Value.Kind() == entity.KindString {
		o.Type = a.Value.String()
	}
}

func (o *ConfigSpecServicesPorts) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Name) {
		attrs = append(attrs, entity.String(ConfigSpecServicesPortsNameId, o.Name))
	}
	if !entity.Empty(o.NodePort) {
		attrs = append(attrs, entity.Int64(ConfigSpecServicesPortsNodePortId, o.NodePort))
	}
	attrs = append(attrs, entity.Int64(ConfigSpecServicesPortsPortId, o.Port))
	if a, ok := ConfigSpecServicesPortsprotocolToId[o.Protocol]; ok {
		attrs = append(attrs, entity.Ref(ConfigSpecServicesPortsProtocolId, a))
	}
	if !entity.Empty(o.Type) {
		attrs = append(attrs, entity.String(ConfigSpecServicesPortsTypeId, o.Type))
	}
	return
}

func (o *ConfigSpecServicesPorts) Empty() bool {
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

func (o *ConfigSpecServicesPorts) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("name", "dev.miren.core/component.config_spec.services.ports.name", schema.Required)
	sb.Int64("node_port", "dev.miren.core/component.config_spec.services.ports.node_port")
	sb.Int64("port", "dev.miren.core/component.config_spec.services.ports.port", schema.Required)
	sb.Singleton("dev.miren.core/component.config_spec.services.ports.protocol.tcp")
	sb.Singleton("dev.miren.core/component.config_spec.services.ports.protocol.udp")
	sb.Ref("protocol", "dev.miren.core/component.config_spec.services.ports.protocol", schema.Choices(ConfigSpecServicesPortsProtocolTcpId, ConfigSpecServicesPortsProtocolUdpId))
	sb.String("type", "dev.miren.core/component.config_spec.services.ports.type")
}

const (
	ConfigSpecTasksCommandId       = entity.Id("dev.miren.core/component.config_spec.tasks.command")
	ConfigSpecTasksEnvId           = entity.Id("dev.miren.core/component.config_spec.tasks.env")
	ConfigSpecTasksMaxConcurrentId = entity.Id("dev.miren.core/component.config_spec.tasks.max_concurrent")
	ConfigSpecTasksNameId          = entity.Id("dev.miren.core/component.config_spec.tasks.name")
	ConfigSpecTasksRetriesId       = entity.Id("dev.miren.core/component.config_spec.tasks.retries")
	ConfigSpecTasksScheduleId      = entity.Id("dev.miren.core/component.config_spec.tasks.schedule")
	ConfigSpecTasksTimeoutId       = entity.Id("dev.miren.core/component.config_spec.tasks.timeout")
	ConfigSpecTasksTriggerId       = entity.Id("dev.miren.core/component.config_spec.tasks.trigger")
)

type ConfigSpecTasks struct {
	Command       string               `cbor:"command,omitempty" json:"command,omitempty"`
	Env           []ConfigSpecTasksEnv `cbor:"env,omitempty" json:"env,omitempty"`
	MaxConcurrent int64                `cbor:"max_concurrent,omitempty" json:"max_concurrent,omitempty"`
	Name          string               `cbor:"name,omitempty" json:"name,omitempty"`
	Retries       int64                `cbor:"retries,omitempty" json:"retries,omitempty"`
	Schedule      string               `cbor:"schedule,omitempty" json:"schedule,omitempty"`
	Timeout       string               `cbor:"timeout,omitempty" json:"timeout,omitempty"`
	Trigger       string               `cbor:"trigger,omitempty" json:"trigger,omitempty"`
}

func (o *ConfigSpecTasks) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(ConfigSpecTasksCommandId); ok && a.Value.Kind() == entity.KindString {
		o.Command = a.Value.String()
	}
	for _, a := range e.GetAll(ConfigSpecTasksEnvId) {
		if a.Value.Kind() == entity.KindComponent {
			var v ConfigSpecTasksEnv
			v.Decode(a.Value.Component())
			o.Env = append(o.Env, v)
		}
	}
	if a, ok := e.Get(ConfigSpecTasksMaxConcurrentId); ok && a.Value.Kind() == entity.KindInt64 {
		o.MaxConcurrent = a.Value.Int64()
	}
	if a, ok := e.Get(ConfigSpecTasksNameId); ok && a.Value.Kind() == entity.KindString {
		o.Name = a.Value.String()
	}
	if a, ok := e.Get(ConfigSpecTasksRetriesId); ok && a.Value.Kind() == entity.KindInt64 {
		o.Retries = a.Value.Int64()
	}
	if a, ok := e.Get(ConfigSpecTasksScheduleId); ok && a.Value.Kind() == entity.KindString {
		o.Schedule = a.Value.String()
	}
	if a, ok := e.Get(ConfigSpecTasksTimeoutId); ok && a.Value.Kind() == entity.KindString {
		o.Timeout = a.Value.String()
	}
	if a, ok := e.Get(ConfigSpecTasksTriggerId); ok && a.Value.Kind() == entity.KindString {
		o.Trigger = a.Value.String()
	}
}

func (o *ConfigSpecTasks) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Command) {
		attrs = append(attrs, entity.String(ConfigSpecTasksCommandId, o.Command))
	}
	for _, v := range o.Env {
		attrs = append(attrs, entity.Component(ConfigSpecTasksEnvId, v.Encode()))
	}
	if !entity.Empty(o.MaxConcurrent) {
		attrs = append(attrs, entity.Int64(ConfigSpecTasksMaxConcurrentId, o.MaxConcurrent))
	}
	if !entity.Empty(o.Name) {
		attrs = append(attrs, entity.String(ConfigSpecTasksNameId, o.Name))
	}
	if !entity.Empty(o.Retries) {
		attrs = append(attrs, entity.Int64(ConfigSpecTasksRetriesId, o.Retries))
	}
	if !entity.Empty(o.Schedule) {
		attrs = append(attrs, entity.String(ConfigSpecTasksScheduleId, o.Schedule))
	}
	if !entity.Empty(o.Timeout) {
		attrs = append(attrs, entity.String(ConfigSpecTasksTimeoutId, o.Timeout))
	}
	if !entity.Empty(o.Trigger) {
		attrs = append(attrs, entity.String(ConfigSpecTasksTriggerId, o.Trigger))
	}
	return
}

func (o *ConfigSpecTasks) Empty() bool {
	if !entity.Empty(o.Command) {
		return false
	}
	if len(o.Env) != 0 {
		return false
	}
	if !entity.Empty(o.MaxConcurrent) {
		return false
	}
	if !entity.Empty(o.Name) {
		return false
	}
	if !entity.Empty(o.Retries) {
		return false
	}
	if !entity.Empty(o.Schedule) {
		return false
	}
	if !entity.Empty(o.Timeout) {
		return false
	}
	if !entity.Empty(o.Trigger) {
		return false
	}
	return true
}

func (o *ConfigSpecTasks) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("command", "dev.miren.core/component.config_spec.tasks.command", schema.Doc("The default command to run. An invoke can override it, which is what makes a manually-triggered task useful for ad-hoc work."))
	sb.Component("env", "dev.miren.core/component.config_spec.tasks.env", schema.Doc("Environment variables for this task only"), schema.Many)
	(&ConfigSpecTasksEnv{}).InitSchema(sb.Builder("component.config_spec.tasks.env"))
	sb.Int64("max_concurrent", "dev.miren.core/component.config_spec.tasks.max_concurrent", schema.Doc("Caps simultaneous runs of this task. Defaults to 1, where the limit is enforced exactly; above 1 admission is best-effort."))
	sb.String("name", "dev.miren.core/component.config_spec.tasks.name", schema.Doc("The task name (e.g. migrate, cleanup)"))
	sb.Int64("retries", "dev.miren.core/component.config_spec.tasks.retries", schema.Doc("Retry budget for deploy- and schedule-triggered runs, where nobody is watching to retry by hand. A manually-triggered run that fails just fails."))
	sb.String("schedule", "dev.miren.core/component.config_spec.tasks.schedule", schema.Doc("The systemd OnCalendar expression this task fires on. An `every` interval in app.toml is desugared into this form at parse time, so there is exactly one scheduling mechanism and this is always the stored representation."))
	sb.String("timeout", "dev.miren.core/component.config_spec.tasks.timeout", schema.Doc("Bounds the run, after which the sandbox is killed and the run is marked TIMED_OUT. Empty falls back to the platform default; \"0\" means unbounded."))
	sb.String("trigger", "dev.miren.core/component.config_spec.tasks.trigger", schema.Doc("What starts the task: \"deploy\" (a new version is deployed, before it takes traffic), \"schedule\" (a calendar expression comes due), or \"manual\" (only when someone asks). Defaults to manual."))
}

const (
	ConfigSpecTasksEnvDescriptionId = entity.Id("dev.miren.core/component.config_spec.tasks.env.description")
	ConfigSpecTasksEnvKeyId         = entity.Id("dev.miren.core/component.config_spec.tasks.env.key")
	ConfigSpecTasksEnvOriginId      = entity.Id("dev.miren.core/component.config_spec.tasks.env.origin")
	ConfigSpecTasksEnvRequiredId    = entity.Id("dev.miren.core/component.config_spec.tasks.env.required")
	ConfigSpecTasksEnvSensitiveId   = entity.Id("dev.miren.core/component.config_spec.tasks.env.sensitive")
	ConfigSpecTasksEnvSourceId      = entity.Id("dev.miren.core/component.config_spec.tasks.env.source")
	ConfigSpecTasksEnvValueId       = entity.Id("dev.miren.core/component.config_spec.tasks.env.value")
)

type ConfigSpecTasksEnv struct {
	Description string `cbor:"description,omitempty" json:"description,omitempty"`
	Key         string `cbor:"key,omitempty" json:"key,omitempty"`
	Origin      string `cbor:"origin,omitempty" json:"origin,omitempty"`
	Required    bool   `cbor:"required,omitempty" json:"required,omitempty"`
	Sensitive   bool   `cbor:"sensitive,omitempty" json:"sensitive,omitempty"`
	Source      string `cbor:"source,omitempty" json:"source,omitempty"`
	Value       string `cbor:"value,omitempty" json:"value,omitempty"`
}

func (o *ConfigSpecTasksEnv) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(ConfigSpecTasksEnvDescriptionId); ok && a.Value.Kind() == entity.KindString {
		o.Description = a.Value.String()
	}
	if a, ok := e.Get(ConfigSpecTasksEnvKeyId); ok && a.Value.Kind() == entity.KindString {
		o.Key = a.Value.String()
	}
	if a, ok := e.Get(ConfigSpecTasksEnvOriginId); ok && a.Value.Kind() == entity.KindString {
		o.Origin = a.Value.String()
	}
	if a, ok := e.Get(ConfigSpecTasksEnvRequiredId); ok && a.Value.Kind() == entity.KindBool {
		o.Required = a.Value.Bool()
	}
	if a, ok := e.Get(ConfigSpecTasksEnvSensitiveId); ok && a.Value.Kind() == entity.KindBool {
		o.Sensitive = a.Value.Bool()
	}
	if a, ok := e.Get(ConfigSpecTasksEnvSourceId); ok && a.Value.Kind() == entity.KindString {
		o.Source = a.Value.String()
	}
	if a, ok := e.Get(ConfigSpecTasksEnvValueId); ok && a.Value.Kind() == entity.KindString {
		o.Value = a.Value.String()
	}
}

func (o *ConfigSpecTasksEnv) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Description) {
		attrs = append(attrs, entity.String(ConfigSpecTasksEnvDescriptionId, o.Description))
	}
	if !entity.Empty(o.Key) {
		attrs = append(attrs, entity.String(ConfigSpecTasksEnvKeyId, o.Key))
	}
	if !entity.Empty(o.Origin) {
		attrs = append(attrs, entity.String(ConfigSpecTasksEnvOriginId, o.Origin))
	}
	attrs = append(attrs, entity.Bool(ConfigSpecTasksEnvRequiredId, o.Required))
	attrs = append(attrs, entity.Bool(ConfigSpecTasksEnvSensitiveId, o.Sensitive))
	if !entity.Empty(o.Source) {
		attrs = append(attrs, entity.String(ConfigSpecTasksEnvSourceId, o.Source))
	}
	if !entity.Empty(o.Value) {
		attrs = append(attrs, entity.String(ConfigSpecTasksEnvValueId, o.Value))
	}
	return
}

func (o *ConfigSpecTasksEnv) Empty() bool {
	if !entity.Empty(o.Description) {
		return false
	}
	if !entity.Empty(o.Key) {
		return false
	}
	if !entity.Empty(o.Origin) {
		return false
	}
	if !entity.Empty(o.Required) {
		return false
	}
	if !entity.Empty(o.Sensitive) {
		return false
	}
	if !entity.Empty(o.Source) {
		return false
	}
	if !entity.Empty(o.Value) {
		return false
	}
	return true
}

func (o *ConfigSpecTasksEnv) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("description", "dev.miren.core/component.config_spec.tasks.env.description", schema.Doc("Human-readable description of this variable's purpose"))
	sb.String("key", "dev.miren.core/component.config_spec.tasks.env.key", schema.Doc("The name of the variable"))
	sb.String("origin", "dev.miren.core/component.config_spec.tasks.env.origin", schema.Doc("The provenance of the variable (user, file, generated, detected)"))
	sb.Bool("required", "dev.miren.core/component.config_spec.tasks.env.required", schema.Doc("Whether this variable must have a non-empty value for deploy to succeed"))
	sb.Bool("sensitive", "dev.miren.core/component.config_spec.tasks.env.sensitive", schema.Doc("Whether or not the value is sensitive"))
	sb.String("source", "dev.miren.core/component.config_spec.tasks.env.source", schema.Doc("The source of the variable (config or manual). Defaults to config for backward compatibility."))
	sb.String("value", "dev.miren.core/component.config_spec.tasks.env.value", schema.Doc("The value of the variable"))
}

const (
	ConfigSpecVariablesBackendId     = entity.Id("dev.miren.core/component.config_spec.variables.backend")
	ConfigSpecVariablesDescriptionId = entity.Id("dev.miren.core/component.config_spec.variables.description")
	ConfigSpecVariablesKeyId         = entity.Id("dev.miren.core/component.config_spec.variables.key")
	ConfigSpecVariablesRequiredId    = entity.Id("dev.miren.core/component.config_spec.variables.required")
	ConfigSpecVariablesSensitiveId   = entity.Id("dev.miren.core/component.config_spec.variables.sensitive")
	ConfigSpecVariablesSourceId      = entity.Id("dev.miren.core/component.config_spec.variables.source")
	ConfigSpecVariablesValueId       = entity.Id("dev.miren.core/component.config_spec.variables.value")
)

type ConfigSpecVariables struct {
	Backend     string `cbor:"backend,omitempty" json:"backend,omitempty"`
	Description string `cbor:"description,omitempty" json:"description,omitempty"`
	Key         string `cbor:"key,omitempty" json:"key,omitempty"`
	Required    bool   `cbor:"required,omitempty" json:"required,omitempty"`
	Sensitive   bool   `cbor:"sensitive,omitempty" json:"sensitive,omitempty"`
	Source      string `cbor:"source,omitempty" json:"source,omitempty"`
	Value       string `cbor:"value,omitempty" json:"value,omitempty"`
}

func (o *ConfigSpecVariables) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(ConfigSpecVariablesBackendId); ok && a.Value.Kind() == entity.KindString {
		o.Backend = a.Value.String()
	}
	if a, ok := e.Get(ConfigSpecVariablesDescriptionId); ok && a.Value.Kind() == entity.KindString {
		o.Description = a.Value.String()
	}
	if a, ok := e.Get(ConfigSpecVariablesKeyId); ok && a.Value.Kind() == entity.KindString {
		o.Key = a.Value.String()
	}
	if a, ok := e.Get(ConfigSpecVariablesRequiredId); ok && a.Value.Kind() == entity.KindBool {
		o.Required = a.Value.Bool()
	}
	if a, ok := e.Get(ConfigSpecVariablesSensitiveId); ok && a.Value.Kind() == entity.KindBool {
		o.Sensitive = a.Value.Bool()
	}
	if a, ok := e.Get(ConfigSpecVariablesSourceId); ok && a.Value.Kind() == entity.KindString {
		o.Source = a.Value.String()
	}
	if a, ok := e.Get(ConfigSpecVariablesValueId); ok && a.Value.Kind() == entity.KindString {
		o.Value = a.Value.String()
	}
}

func (o *ConfigSpecVariables) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Backend) {
		attrs = append(attrs, entity.String(ConfigSpecVariablesBackendId, o.Backend))
	}
	if !entity.Empty(o.Description) {
		attrs = append(attrs, entity.String(ConfigSpecVariablesDescriptionId, o.Description))
	}
	if !entity.Empty(o.Key) {
		attrs = append(attrs, entity.String(ConfigSpecVariablesKeyId, o.Key))
	}
	attrs = append(attrs, entity.Bool(ConfigSpecVariablesRequiredId, o.Required))
	attrs = append(attrs, entity.Bool(ConfigSpecVariablesSensitiveId, o.Sensitive))
	if !entity.Empty(o.Source) {
		attrs = append(attrs, entity.String(ConfigSpecVariablesSourceId, o.Source))
	}
	if !entity.Empty(o.Value) {
		attrs = append(attrs, entity.String(ConfigSpecVariablesValueId, o.Value))
	}
	return
}

func (o *ConfigSpecVariables) Empty() bool {
	if !entity.Empty(o.Backend) {
		return false
	}
	if !entity.Empty(o.Description) {
		return false
	}
	if !entity.Empty(o.Key) {
		return false
	}
	if !entity.Empty(o.Required) {
		return false
	}
	if !entity.Empty(o.Sensitive) {
		return false
	}
	if !entity.Empty(o.Source) {
		return false
	}
	if !entity.Empty(o.Value) {
		return false
	}
	return true
}

func (o *ConfigSpecVariables) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("backend", "dev.miren.core/component.config_spec.variables.backend", schema.Doc("Where the value comes from. Empty means value holds an inline literal; any other name refers to a registered secret backend instance (e.g. cluster) and value holds a backend-relative reference, optionally pinned with @version."))
	sb.String("description", "dev.miren.core/component.config_spec.variables.description", schema.Doc("Human-readable description of this variable's purpose"))
	sb.String("key", "dev.miren.core/component.config_spec.variables.key", schema.Doc("The name of the variable"))
	sb.Bool("required", "dev.miren.core/component.config_spec.variables.required", schema.Doc("Whether this variable must have a non-empty value for deploy to succeed"))
	sb.Bool("sensitive", "dev.miren.core/component.config_spec.variables.sensitive", schema.Doc("Whether or not the value is sensitive"))
	sb.String("source", "dev.miren.core/component.config_spec.variables.source", schema.Doc("The source of the variable (config or manual). Defaults to config for backward compatibility."))
	sb.String("value", "dev.miren.core/component.config_spec.variables.value", schema.Doc("The value of the variable"))
}

const (
	AppActiveVersionId = entity.Id("dev.miren.core/app.active_version")
	AppInitialConfigId = entity.Id("dev.miren.core/app.initial_config")
	AppProjectId       = entity.Id("dev.miren.core/app.project")
	AppWorkloadRoleId  = entity.Id("dev.miren.core/app.workload_role")
)

type App struct {
	ID            entity.Id `json:"id"`
	ActiveVersion entity.Id `cbor:"active_version,omitempty" json:"active_version,omitempty"`
	InitialConfig entity.Id `cbor:"initial_config,omitempty" json:"initial_config,omitempty"`
	Project       entity.Id `cbor:"project,omitempty" json:"project,omitempty"`
	WorkloadRole  string    `cbor:"workload_role,omitempty" json:"workload_role,omitempty"`
}

func (o *App) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(AppActiveVersionId); ok && a.Value.Kind() == entity.KindId {
		o.ActiveVersion = a.Value.Id()
	}
	if a, ok := e.Get(AppInitialConfigId); ok && a.Value.Kind() == entity.KindId {
		o.InitialConfig = a.Value.Id()
	}
	if a, ok := e.Get(AppProjectId); ok && a.Value.Kind() == entity.KindId {
		o.Project = a.Value.Id()
	}
	if a, ok := e.Get(AppWorkloadRoleId); ok && a.Value.Kind() == entity.KindString {
		o.WorkloadRole = a.Value.String()
	}
}

func (o *App) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindApp)
}

func (o *App) ShortKind() string {
	return "app"
}

func (o *App) Kind() entity.Id {
	return KindApp
}

func (o *App) EntityId() entity.Id {
	return o.ID
}

func (o *App) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.ActiveVersion) {
		attrs = append(attrs, entity.Ref(AppActiveVersionId, o.ActiveVersion))
	}
	if !entity.Empty(o.InitialConfig) {
		attrs = append(attrs, entity.Ref(AppInitialConfigId, o.InitialConfig))
	}
	if !entity.Empty(o.Project) {
		attrs = append(attrs, entity.Ref(AppProjectId, o.Project))
	}
	if !entity.Empty(o.WorkloadRole) {
		attrs = append(attrs, entity.String(AppWorkloadRoleId, o.WorkloadRole))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindApp))
	return
}

func (o *App) Empty() bool {
	if !entity.Empty(o.ActiveVersion) {
		return false
	}
	if !entity.Empty(o.InitialConfig) {
		return false
	}
	if !entity.Empty(o.Project) {
		return false
	}
	if !entity.Empty(o.WorkloadRole) {
		return false
	}
	return true
}

func (o *App) InitSchema(sb *schema.SchemaBuilder) {
	sb.Ref("active_version", "dev.miren.core/app.active_version", schema.Doc("The version of the project that should be used"))
	sb.Ref("initial_config", "dev.miren.core/app.initial_config", schema.Doc("Reference to the initial ConfigVersion entity created before the first deploy"))
	sb.Ref("project", "dev.miren.core/app.project", schema.Doc("The project that the app belongs to"))
	sb.String("workload_role", "dev.miren.core/app.workload_role", schema.Doc("The authorization role that identity tokens minted for this app's sandboxes authenticate as (see pkg/workloadroles). Empty means the default (app-readonly). Cluster-scoped roles may only be set by an operator, never via app.toml."))
}

const (
	AppVersionAdminTokenId         = entity.Id("dev.miren.core/app_version.admin_token")
	AppVersionAppId                = entity.Id("dev.miren.core/app_version.app")
	AppVersionArtifactId           = entity.Id("dev.miren.core/app_version.artifact")
	AppVersionConfigId             = entity.Id("dev.miren.core/app_version.config")
	AppVersionConfigVersionId      = entity.Id("dev.miren.core/app_version.config_version")
	AppVersionEphemeralExpiresAtId = entity.Id("dev.miren.core/app_version.ephemeral_expires_at")
	AppVersionEphemeralLabelId     = entity.Id("dev.miren.core/app_version.ephemeral_label")
	AppVersionEphemeralTtlId       = entity.Id("dev.miren.core/app_version.ephemeral_ttl")
	AppVersionImageUrlId           = entity.Id("dev.miren.core/app_version.image_url")
	AppVersionManifestId           = entity.Id("dev.miren.core/app_version.manifest")
	AppVersionManifestDigestId     = entity.Id("dev.miren.core/app_version.manifest_digest")
	AppVersionVersionId            = entity.Id("dev.miren.core/app_version.version")
)

type AppVersion struct {
	ID                 entity.Id `json:"id"`
	AdminToken         string    `cbor:"admin_token,omitempty" json:"admin_token,omitempty"`
	App                entity.Id `cbor:"app,omitempty" json:"app,omitempty"`
	Artifact           entity.Id `cbor:"artifact,omitempty" json:"artifact,omitempty"`
	Config             Config    `cbor:"config,omitempty" json:"config"`
	ConfigVersion      entity.Id `cbor:"config_version,omitempty" json:"config_version,omitempty"`
	EphemeralExpiresAt time.Time `cbor:"ephemeral_expires_at,omitempty" json:"ephemeral_expires_at"`
	EphemeralLabel     string    `cbor:"ephemeral_label,omitempty" json:"ephemeral_label,omitempty"`
	EphemeralTtl       string    `cbor:"ephemeral_ttl,omitempty" json:"ephemeral_ttl,omitempty"`
	ImageUrl           string    `cbor:"image_url,omitempty" json:"image_url,omitempty"`
	Manifest           string    `cbor:"manifest,omitempty" json:"manifest,omitempty"`
	ManifestDigest     string    `cbor:"manifest_digest,omitempty" json:"manifest_digest,omitempty"`
	Version            string    `cbor:"version,omitempty" json:"version,omitempty"`
}

func (o *AppVersion) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(AppVersionAdminTokenId); ok && a.Value.Kind() == entity.KindString {
		o.AdminToken = a.Value.String()
	}
	if a, ok := e.Get(AppVersionAppId); ok && a.Value.Kind() == entity.KindId {
		o.App = a.Value.Id()
	}
	if a, ok := e.Get(AppVersionArtifactId); ok && a.Value.Kind() == entity.KindId {
		o.Artifact = a.Value.Id()
	}
	if a, ok := e.Get(AppVersionConfigId); ok && a.Value.Kind() == entity.KindComponent {
		o.Config.Decode(a.Value.Component())
	}
	if a, ok := e.Get(AppVersionConfigVersionId); ok && a.Value.Kind() == entity.KindId {
		o.ConfigVersion = a.Value.Id()
	}
	if a, ok := e.Get(AppVersionEphemeralExpiresAtId); ok && a.Value.Kind() == entity.KindTime {
		o.EphemeralExpiresAt = a.Value.Time()
	}
	if a, ok := e.Get(AppVersionEphemeralLabelId); ok && a.Value.Kind() == entity.KindString {
		o.EphemeralLabel = a.Value.String()
	}
	if a, ok := e.Get(AppVersionEphemeralTtlId); ok && a.Value.Kind() == entity.KindString {
		o.EphemeralTtl = a.Value.String()
	}
	if a, ok := e.Get(AppVersionImageUrlId); ok && a.Value.Kind() == entity.KindString {
		o.ImageUrl = a.Value.String()
	}
	if a, ok := e.Get(AppVersionManifestId); ok && a.Value.Kind() == entity.KindString {
		o.Manifest = a.Value.String()
	}
	if a, ok := e.Get(AppVersionManifestDigestId); ok && a.Value.Kind() == entity.KindString {
		o.ManifestDigest = a.Value.String()
	}
	if a, ok := e.Get(AppVersionVersionId); ok && a.Value.Kind() == entity.KindString {
		o.Version = a.Value.String()
	}
}

func (o *AppVersion) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindAppVersion)
}

func (o *AppVersion) ShortKind() string {
	return "app_version"
}

func (o *AppVersion) Kind() entity.Id {
	return KindAppVersion
}

func (o *AppVersion) EntityId() entity.Id {
	return o.ID
}

func (o *AppVersion) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.AdminToken) {
		attrs = append(attrs, entity.String(AppVersionAdminTokenId, o.AdminToken))
	}
	if !entity.Empty(o.App) {
		attrs = append(attrs, entity.Ref(AppVersionAppId, o.App))
	}
	if !entity.Empty(o.Artifact) {
		attrs = append(attrs, entity.Ref(AppVersionArtifactId, o.Artifact))
	}
	if !o.Config.Empty() {
		attrs = append(attrs, entity.Component(AppVersionConfigId, o.Config.Encode()))
	}
	if !entity.Empty(o.ConfigVersion) {
		attrs = append(attrs, entity.Ref(AppVersionConfigVersionId, o.ConfigVersion))
	}
	if !entity.Empty(o.EphemeralExpiresAt) {
		attrs = append(attrs, entity.Time(AppVersionEphemeralExpiresAtId, o.EphemeralExpiresAt))
	}
	if !entity.Empty(o.EphemeralLabel) {
		attrs = append(attrs, entity.String(AppVersionEphemeralLabelId, o.EphemeralLabel))
	}
	if !entity.Empty(o.EphemeralTtl) {
		attrs = append(attrs, entity.String(AppVersionEphemeralTtlId, o.EphemeralTtl))
	}
	if !entity.Empty(o.ImageUrl) {
		attrs = append(attrs, entity.String(AppVersionImageUrlId, o.ImageUrl))
	}
	if !entity.Empty(o.Manifest) {
		attrs = append(attrs, entity.String(AppVersionManifestId, o.Manifest))
	}
	if !entity.Empty(o.ManifestDigest) {
		attrs = append(attrs, entity.String(AppVersionManifestDigestId, o.ManifestDigest))
	}
	if !entity.Empty(o.Version) {
		attrs = append(attrs, entity.String(AppVersionVersionId, o.Version))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindAppVersion))
	return
}

func (o *AppVersion) Empty() bool {
	if !entity.Empty(o.AdminToken) {
		return false
	}
	if !entity.Empty(o.App) {
		return false
	}
	if !entity.Empty(o.Artifact) {
		return false
	}
	if !o.Config.Empty() {
		return false
	}
	if !entity.Empty(o.ConfigVersion) {
		return false
	}
	if !entity.Empty(o.EphemeralExpiresAt) {
		return false
	}
	if !entity.Empty(o.EphemeralLabel) {
		return false
	}
	if !entity.Empty(o.EphemeralTtl) {
		return false
	}
	if !entity.Empty(o.ImageUrl) {
		return false
	}
	if !entity.Empty(o.Manifest) {
		return false
	}
	if !entity.Empty(o.ManifestDigest) {
		return false
	}
	if !entity.Empty(o.Version) {
		return false
	}
	return true
}

func (o *AppVersion) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("admin_token", "dev.miren.core/app_version.admin_token", schema.Doc("Cryptographically random token for authenticating admin API calls. Generated per-version and exposed to the app via ADMIN_TOKEN env var."))
	sb.Ref("app", "dev.miren.core/app_version.app", schema.Doc("The application the version is for"), schema.Indexed, schema.Tags("dev.miren.app_ref"))
	sb.Ref("artifact", "dev.miren.core/app_version.artifact", schema.Doc("The artifact to deploy for the version"))
	sb.Component("config", "dev.miren.core/app_version.config", schema.Doc("The configuration of the version"))
	(&Config{}).InitSchema(sb.Builder("app_version.config"))
	sb.Ref("config_version", "dev.miren.core/app_version.config_version", schema.Doc("Reference to the ConfigVersion entity containing the resolved configuration for this version"))
	sb.Time("ephemeral_expires_at", "dev.miren.core/app_version.ephemeral_expires_at", schema.Doc("Computed expiration timestamp (creation + TTL). Used by the cleanup controller."), schema.Indexed)
	sb.String("ephemeral_label", "dev.miren.core/app_version.ephemeral_label", schema.Doc("DNS-safe label for ephemeral subdomain routing (e.g., \"feat-x\"). Empty for non-ephemeral versions."), schema.Indexed)
	sb.String("ephemeral_ttl", "dev.miren.core/app_version.ephemeral_ttl", schema.Doc("TTL duration string (e.g., \"48h\") for display. Empty for non-ephemeral versions."))
	sb.String("image_url", "dev.miren.core/app_version.image_url", schema.Doc("The OCI url for the versions code"))
	sb.String("manifest", "dev.miren.core/app_version.manifest", schema.Doc("The OCI image manifest for the version"))
	sb.String("manifest_digest", "dev.miren.core/app_version.manifest_digest", schema.Doc("The digest of the manifest"), schema.Indexed)
	sb.String("version", "dev.miren.core/app_version.version", schema.Doc("The version of this app"))
}

const (
	ConfigCommandsId       = entity.Id("dev.miren.core/config.commands")
	ConfigEntrypointId     = entity.Id("dev.miren.core/config.entrypoint")
	ConfigPortId           = entity.Id("dev.miren.core/config.port")
	ConfigServicesId       = entity.Id("dev.miren.core/config.services")
	ConfigStartDirectoryId = entity.Id("dev.miren.core/config.start_directory")
	ConfigVariableId       = entity.Id("dev.miren.core/config.variable")
)

type Config struct {
	Commands       []Commands `cbor:"commands,omitempty" json:"commands,omitempty"`
	Entrypoint     string     `cbor:"entrypoint,omitempty" json:"entrypoint,omitempty"`
	Port           int64      `cbor:"port,omitempty" json:"port,omitempty"`
	Services       []Services `cbor:"services,omitempty" json:"services,omitempty"`
	StartDirectory string     `cbor:"start_directory,omitempty" json:"start_directory,omitempty"`
	Variable       []Variable `cbor:"variable,omitempty" json:"variable,omitempty"`
}

func (o *Config) Decode(e entity.AttrGetter) {
	for _, a := range e.GetAll(ConfigCommandsId) {
		if a.Value.Kind() == entity.KindComponent {
			var v Commands
			v.Decode(a.Value.Component())
			o.Commands = append(o.Commands, v)
		}
	}
	if a, ok := e.Get(ConfigEntrypointId); ok && a.Value.Kind() == entity.KindString {
		o.Entrypoint = a.Value.String()
	}
	if a, ok := e.Get(ConfigPortId); ok && a.Value.Kind() == entity.KindInt64 {
		o.Port = a.Value.Int64()
	}
	for _, a := range e.GetAll(ConfigServicesId) {
		if a.Value.Kind() == entity.KindComponent {
			var v Services
			v.Decode(a.Value.Component())
			o.Services = append(o.Services, v)
		}
	}
	if a, ok := e.Get(ConfigStartDirectoryId); ok && a.Value.Kind() == entity.KindString {
		o.StartDirectory = a.Value.String()
	}
	for _, a := range e.GetAll(ConfigVariableId) {
		if a.Value.Kind() == entity.KindComponent {
			var v Variable
			v.Decode(a.Value.Component())
			o.Variable = append(o.Variable, v)
		}
	}
}

func (o *Config) Encode() (attrs []entity.Attr) {
	for _, v := range o.Commands {
		attrs = append(attrs, entity.Component(ConfigCommandsId, v.Encode()))
	}
	if !entity.Empty(o.Entrypoint) {
		attrs = append(attrs, entity.String(ConfigEntrypointId, o.Entrypoint))
	}
	if !entity.Empty(o.Port) {
		attrs = append(attrs, entity.Int64(ConfigPortId, o.Port))
	}
	for _, v := range o.Services {
		attrs = append(attrs, entity.Component(ConfigServicesId, v.Encode()))
	}
	if !entity.Empty(o.StartDirectory) {
		attrs = append(attrs, entity.String(ConfigStartDirectoryId, o.StartDirectory))
	}
	for _, v := range o.Variable {
		attrs = append(attrs, entity.Component(ConfigVariableId, v.Encode()))
	}
	return
}

func (o *Config) Empty() bool {
	if len(o.Commands) != 0 {
		return false
	}
	if !entity.Empty(o.Entrypoint) {
		return false
	}
	if !entity.Empty(o.Port) {
		return false
	}
	if len(o.Services) != 0 {
		return false
	}
	if !entity.Empty(o.StartDirectory) {
		return false
	}
	if len(o.Variable) != 0 {
		return false
	}
	return true
}

func (o *Config) InitSchema(sb *schema.SchemaBuilder) {
	sb.Component("commands", "dev.miren.core/config.commands", schema.Doc("The command to run for a specific service type"), schema.Many)
	(&Commands{}).InitSchema(sb.Builder("config.commands"))
	sb.String("entrypoint", "dev.miren.core/config.entrypoint", schema.Doc("The container entrypoint command"))
	sb.Int64("port", "dev.miren.core/config.port", schema.Doc("[DEPRECATED] Port used for the web service; defaults to 3000. Prefer per-service ports."))
	sb.Component("services", "dev.miren.core/config.services", schema.Doc("Per-service configuration including concurrency controls"), schema.Many)
	(&Services{}).InitSchema(sb.Builder("config.services"))
	sb.String("start_directory", "dev.miren.core/config.start_directory", schema.Doc("Directory to start the process in; defaults to /app."))
	sb.Component("variable", "dev.miren.core/config.variable", schema.Doc("A variable to be exposed to the app"), schema.Many)
	(&Variable{}).InitSchema(sb.Builder("config.variable"))
}

const (
	CommandsCommandId = entity.Id("dev.miren.core/commands.command")
	CommandsServiceId = entity.Id("dev.miren.core/commands.service")
)

type Commands struct {
	Command string `cbor:"command,omitempty" json:"command,omitempty"`
	Service string `cbor:"service,omitempty" json:"service,omitempty"`
}

func (o *Commands) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(CommandsCommandId); ok && a.Value.Kind() == entity.KindString {
		o.Command = a.Value.String()
	}
	if a, ok := e.Get(CommandsServiceId); ok && a.Value.Kind() == entity.KindString {
		o.Service = a.Value.String()
	}
}

func (o *Commands) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Command) {
		attrs = append(attrs, entity.String(CommandsCommandId, o.Command))
	}
	if !entity.Empty(o.Service) {
		attrs = append(attrs, entity.String(CommandsServiceId, o.Service))
	}
	return
}

func (o *Commands) Empty() bool {
	if !entity.Empty(o.Command) {
		return false
	}
	if !entity.Empty(o.Service) {
		return false
	}
	return true
}

func (o *Commands) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("command", "dev.miren.core/commands.command", schema.Doc("The command to run for the service"))
	sb.String("service", "dev.miren.core/commands.service", schema.Doc("The service name"))
}

const (
	ServicesDisksId              = entity.Id("dev.miren.core/services.disks")
	ServicesEnvId                = entity.Id("dev.miren.core/services.env")
	ServicesImageId              = entity.Id("dev.miren.core/services.image")
	ServicesNameId               = entity.Id("dev.miren.core/services.name")
	ServicesPortId               = entity.Id("dev.miren.core/services.port")
	ServicesPortNameId           = entity.Id("dev.miren.core/services.port_name")
	ServicesPortTypeId           = entity.Id("dev.miren.core/services.port_type")
	ServicesPortsId              = entity.Id("dev.miren.core/services.ports")
	ServicesServiceConcurrencyId = entity.Id("dev.miren.core/services.service_concurrency")
)

type Services struct {
	Disks              []Disks            `cbor:"disks,omitempty" json:"disks,omitempty"`
	Env                []Env              `cbor:"env,omitempty" json:"env,omitempty"`
	Image              string             `cbor:"image,omitempty" json:"image,omitempty"`
	Name               string             `cbor:"name,omitempty" json:"name,omitempty"`
	Port               int64              `cbor:"port,omitempty" json:"port,omitempty"`
	PortName           string             `cbor:"port_name,omitempty" json:"port_name,omitempty"`
	PortType           string             `cbor:"port_type,omitempty" json:"port_type,omitempty"`
	Ports              []Ports            `cbor:"ports,omitempty" json:"ports,omitempty"`
	ServiceConcurrency ServiceConcurrency `cbor:"service_concurrency,omitempty" json:"service_concurrency"`
}

func (o *Services) Decode(e entity.AttrGetter) {
	for _, a := range e.GetAll(ServicesDisksId) {
		if a.Value.Kind() == entity.KindComponent {
			var v Disks
			v.Decode(a.Value.Component())
			o.Disks = append(o.Disks, v)
		}
	}
	for _, a := range e.GetAll(ServicesEnvId) {
		if a.Value.Kind() == entity.KindComponent {
			var v Env
			v.Decode(a.Value.Component())
			o.Env = append(o.Env, v)
		}
	}
	if a, ok := e.Get(ServicesImageId); ok && a.Value.Kind() == entity.KindString {
		o.Image = a.Value.String()
	}
	if a, ok := e.Get(ServicesNameId); ok && a.Value.Kind() == entity.KindString {
		o.Name = a.Value.String()
	}
	if a, ok := e.Get(ServicesPortId); ok && a.Value.Kind() == entity.KindInt64 {
		o.Port = a.Value.Int64()
	}
	if a, ok := e.Get(ServicesPortNameId); ok && a.Value.Kind() == entity.KindString {
		o.PortName = a.Value.String()
	}
	if a, ok := e.Get(ServicesPortTypeId); ok && a.Value.Kind() == entity.KindString {
		o.PortType = a.Value.String()
	}
	for _, a := range e.GetAll(ServicesPortsId) {
		if a.Value.Kind() == entity.KindComponent {
			var v Ports
			v.Decode(a.Value.Component())
			o.Ports = append(o.Ports, v)
		}
	}
	if a, ok := e.Get(ServicesServiceConcurrencyId); ok && a.Value.Kind() == entity.KindComponent {
		o.ServiceConcurrency.Decode(a.Value.Component())
	}
}

func (o *Services) Encode() (attrs []entity.Attr) {
	for _, v := range o.Disks {
		attrs = append(attrs, entity.Component(ServicesDisksId, v.Encode()))
	}
	for _, v := range o.Env {
		attrs = append(attrs, entity.Component(ServicesEnvId, v.Encode()))
	}
	if !entity.Empty(o.Image) {
		attrs = append(attrs, entity.String(ServicesImageId, o.Image))
	}
	if !entity.Empty(o.Name) {
		attrs = append(attrs, entity.String(ServicesNameId, o.Name))
	}
	if !entity.Empty(o.Port) {
		attrs = append(attrs, entity.Int64(ServicesPortId, o.Port))
	}
	if !entity.Empty(o.PortName) {
		attrs = append(attrs, entity.String(ServicesPortNameId, o.PortName))
	}
	if !entity.Empty(o.PortType) {
		attrs = append(attrs, entity.String(ServicesPortTypeId, o.PortType))
	}
	for _, v := range o.Ports {
		attrs = append(attrs, entity.Component(ServicesPortsId, v.Encode()))
	}
	if !o.ServiceConcurrency.Empty() {
		attrs = append(attrs, entity.Component(ServicesServiceConcurrencyId, o.ServiceConcurrency.Encode()))
	}
	return
}

func (o *Services) Empty() bool {
	if len(o.Disks) != 0 {
		return false
	}
	if len(o.Env) != 0 {
		return false
	}
	if !entity.Empty(o.Image) {
		return false
	}
	if !entity.Empty(o.Name) {
		return false
	}
	if !entity.Empty(o.Port) {
		return false
	}
	if !entity.Empty(o.PortName) {
		return false
	}
	if !entity.Empty(o.PortType) {
		return false
	}
	if len(o.Ports) != 0 {
		return false
	}
	if !o.ServiceConcurrency.Empty() {
		return false
	}
	return true
}

func (o *Services) InitSchema(sb *schema.SchemaBuilder) {
	sb.Component("disks", "dev.miren.core/services.disks", schema.Doc("Disk attachments for this service"), schema.Many)
	(&Disks{}).InitSchema(sb.Builder("services.disks"))
	sb.Component("env", "dev.miren.core/services.env", schema.Doc("Environment variables for this service only"), schema.Many)
	(&Env{}).InitSchema(sb.Builder("services.env"))
	sb.String("image", "dev.miren.core/services.image", schema.Doc("Optional container image for this service (e.g. postgres:16). If not specified, uses the app-level built image."))
	sb.String("name", "dev.miren.core/services.name", schema.Doc("The service name (e.g. web, worker)"))
	sb.Int64("port", "dev.miren.core/services.port", schema.Doc("The TCP port the service listens on. For the web service, if not specified it falls back to the deprecated top-level port (if set) or 3000. Other services must specify services.port explicitly and do not inherit the top-level port."))
	sb.String("port_name", "dev.miren.core/services.port_name", schema.Doc("The name of the port (e.g. http, grpc). Defaults to \"http\" if not specified."))
	sb.String("port_type", "dev.miren.core/services.port_type", schema.Doc("The type of the port (e.g. http, tcp). Defaults to \"http\" if not specified."))
	sb.Component("ports", "dev.miren.core/services.ports", schema.Doc("Network ports this service listens on. Overrides scalar port/port_name/port_type."), schema.Many)
	(&Ports{}).InitSchema(sb.Builder("services.ports"))
	sb.Component("service_concurrency", "dev.miren.core/services.service_concurrency", schema.Doc("Concurrency configuration for this service"))
	(&ServiceConcurrency{}).InitSchema(sb.Builder("services.service_concurrency"))
}

const (
	DisksFilesystemId    = entity.Id("dev.miren.core/disks.filesystem")
	DisksLeaseTimeoutId  = entity.Id("dev.miren.core/disks.lease_timeout")
	DisksMountPathId     = entity.Id("dev.miren.core/disks.mount_path")
	DisksNameId          = entity.Id("dev.miren.core/disks.name")
	DisksOwnerId         = entity.Id("dev.miren.core/disks.owner")
	DisksProviderId      = entity.Id("dev.miren.core/disks.provider")
	DisksProviderMirenId = entity.Id("dev.miren.core/provider.miren")
	DisksProviderLocalId = entity.Id("dev.miren.core/provider.local")
	DisksReadOnlyId      = entity.Id("dev.miren.core/disks.read_only")
	DisksSizeGbId        = entity.Id("dev.miren.core/disks.size_gb")
)

type Disks struct {
	Filesystem   string        `cbor:"filesystem,omitempty" json:"filesystem,omitempty"`
	LeaseTimeout string        `cbor:"lease_timeout,omitempty" json:"lease_timeout,omitempty"`
	MountPath    string        `cbor:"mount_path,omitempty" json:"mount_path,omitempty"`
	Name         string        `cbor:"name,omitempty" json:"name,omitempty"`
	Owner        string        `cbor:"owner,omitempty" json:"owner,omitempty"`
	Provider     DisksProvider `cbor:"provider,omitempty" json:"provider,omitempty"`
	ReadOnly     bool          `cbor:"read_only,omitempty" json:"read_only,omitempty"`
	SizeGb       int64         `cbor:"size_gb,omitempty" json:"size_gb,omitempty"`
}

type DisksProvider string

const (
	MIREN DisksProvider = "provider.miren"
	LOCAL DisksProvider = "provider.local"
)

var DisksproviderFromId = map[entity.Id]DisksProvider{DisksProviderMirenId: MIREN, DisksProviderLocalId: LOCAL}
var DisksproviderToId = map[DisksProvider]entity.Id{MIREN: DisksProviderMirenId, LOCAL: DisksProviderLocalId}

func (o *Disks) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(DisksFilesystemId); ok && a.Value.Kind() == entity.KindString {
		o.Filesystem = a.Value.String()
	}
	if a, ok := e.Get(DisksLeaseTimeoutId); ok && a.Value.Kind() == entity.KindString {
		o.LeaseTimeout = a.Value.String()
	}
	if a, ok := e.Get(DisksMountPathId); ok && a.Value.Kind() == entity.KindString {
		o.MountPath = a.Value.String()
	}
	if a, ok := e.Get(DisksNameId); ok && a.Value.Kind() == entity.KindString {
		o.Name = a.Value.String()
	}
	if a, ok := e.Get(DisksOwnerId); ok && a.Value.Kind() == entity.KindString {
		o.Owner = a.Value.String()
	}
	if a, ok := e.Get(DisksProviderId); ok && a.Value.Kind() == entity.KindId {
		o.Provider = DisksproviderFromId[a.Value.Id()]
	}
	if a, ok := e.Get(DisksReadOnlyId); ok && a.Value.Kind() == entity.KindBool {
		o.ReadOnly = a.Value.Bool()
	}
	if a, ok := e.Get(DisksSizeGbId); ok && a.Value.Kind() == entity.KindInt64 {
		o.SizeGb = a.Value.Int64()
	}
}

func (o *Disks) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Filesystem) {
		attrs = append(attrs, entity.String(DisksFilesystemId, o.Filesystem))
	}
	if !entity.Empty(o.LeaseTimeout) {
		attrs = append(attrs, entity.String(DisksLeaseTimeoutId, o.LeaseTimeout))
	}
	if !entity.Empty(o.MountPath) {
		attrs = append(attrs, entity.String(DisksMountPathId, o.MountPath))
	}
	if !entity.Empty(o.Name) {
		attrs = append(attrs, entity.String(DisksNameId, o.Name))
	}
	if !entity.Empty(o.Owner) {
		attrs = append(attrs, entity.String(DisksOwnerId, o.Owner))
	}
	if a, ok := DisksproviderToId[o.Provider]; ok {
		attrs = append(attrs, entity.Ref(DisksProviderId, a))
	}
	attrs = append(attrs, entity.Bool(DisksReadOnlyId, o.ReadOnly))
	if !entity.Empty(o.SizeGb) {
		attrs = append(attrs, entity.Int64(DisksSizeGbId, o.SizeGb))
	}
	return
}

func (o *Disks) Empty() bool {
	if !entity.Empty(o.Filesystem) {
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
	if o.Provider != "" {
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

func (o *Disks) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("filesystem", "dev.miren.core/disks.filesystem", schema.Doc("Filesystem type (ext4, xfs, btrfs) for auto-creating the disk"))
	sb.String("lease_timeout", "dev.miren.core/disks.lease_timeout", schema.Doc("Timeout for acquiring the disk lease (e.g. 5m, 10m)"))
	sb.String("mount_path", "dev.miren.core/disks.mount_path", schema.Doc("The path inside the container where the disk will be mounted"))
	sb.String("name", "dev.miren.core/disks.name", schema.Doc("The name of the disk"))
	sb.String("owner", "dev.miren.core/disks.owner", schema.Doc("Ownership policy for the mounted disk. Empty (default) makes the disk writable by the container's run user; \"keep\" leaves the raw mount ownership untouched; \"uid\" or \"uid:gid\" pins a specific numeric owner."))
	sb.Singleton("dev.miren.core/provider.miren")
	sb.Singleton("dev.miren.core/provider.local")
	sb.Ref("provider", "dev.miren.core/disks.provider", schema.Doc("Disk provider: 'miren' (default) for network disks, 'local' for node-local persistent storage"), schema.Choices(DisksProviderMirenId, DisksProviderLocalId))
	sb.Bool("read_only", "dev.miren.core/disks.read_only", schema.Doc("Whether to mount the disk as read-only"))
	sb.Int64("size_gb", "dev.miren.core/disks.size_gb", schema.Doc("Size in GB for auto-creating the disk if it doesn't exist"))
}

const (
	EnvBackendId     = entity.Id("dev.miren.core/env.backend")
	EnvDescriptionId = entity.Id("dev.miren.core/env.description")
	EnvKeyId         = entity.Id("dev.miren.core/env.key")
	EnvRequiredId    = entity.Id("dev.miren.core/env.required")
	EnvSensitiveId   = entity.Id("dev.miren.core/env.sensitive")
	EnvSourceId      = entity.Id("dev.miren.core/env.source")
	EnvValueId       = entity.Id("dev.miren.core/env.value")
)

type Env struct {
	Backend     string `cbor:"backend,omitempty" json:"backend,omitempty"`
	Description string `cbor:"description,omitempty" json:"description,omitempty"`
	Key         string `cbor:"key,omitempty" json:"key,omitempty"`
	Required    bool   `cbor:"required,omitempty" json:"required,omitempty"`
	Sensitive   bool   `cbor:"sensitive,omitempty" json:"sensitive,omitempty"`
	Source      string `cbor:"source,omitempty" json:"source,omitempty"`
	Value       string `cbor:"value,omitempty" json:"value,omitempty"`
}

func (o *Env) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(EnvBackendId); ok && a.Value.Kind() == entity.KindString {
		o.Backend = a.Value.String()
	}
	if a, ok := e.Get(EnvDescriptionId); ok && a.Value.Kind() == entity.KindString {
		o.Description = a.Value.String()
	}
	if a, ok := e.Get(EnvKeyId); ok && a.Value.Kind() == entity.KindString {
		o.Key = a.Value.String()
	}
	if a, ok := e.Get(EnvRequiredId); ok && a.Value.Kind() == entity.KindBool {
		o.Required = a.Value.Bool()
	}
	if a, ok := e.Get(EnvSensitiveId); ok && a.Value.Kind() == entity.KindBool {
		o.Sensitive = a.Value.Bool()
	}
	if a, ok := e.Get(EnvSourceId); ok && a.Value.Kind() == entity.KindString {
		o.Source = a.Value.String()
	}
	if a, ok := e.Get(EnvValueId); ok && a.Value.Kind() == entity.KindString {
		o.Value = a.Value.String()
	}
}

func (o *Env) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Backend) {
		attrs = append(attrs, entity.String(EnvBackendId, o.Backend))
	}
	if !entity.Empty(o.Description) {
		attrs = append(attrs, entity.String(EnvDescriptionId, o.Description))
	}
	if !entity.Empty(o.Key) {
		attrs = append(attrs, entity.String(EnvKeyId, o.Key))
	}
	attrs = append(attrs, entity.Bool(EnvRequiredId, o.Required))
	attrs = append(attrs, entity.Bool(EnvSensitiveId, o.Sensitive))
	if !entity.Empty(o.Source) {
		attrs = append(attrs, entity.String(EnvSourceId, o.Source))
	}
	if !entity.Empty(o.Value) {
		attrs = append(attrs, entity.String(EnvValueId, o.Value))
	}
	return
}

func (o *Env) Empty() bool {
	if !entity.Empty(o.Backend) {
		return false
	}
	if !entity.Empty(o.Description) {
		return false
	}
	if !entity.Empty(o.Key) {
		return false
	}
	if !entity.Empty(o.Required) {
		return false
	}
	if !entity.Empty(o.Sensitive) {
		return false
	}
	if !entity.Empty(o.Source) {
		return false
	}
	if !entity.Empty(o.Value) {
		return false
	}
	return true
}

func (o *Env) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("backend", "dev.miren.core/env.backend", schema.Doc("Where the value comes from. Empty means value holds an inline literal; any other name refers to a registered secret backend instance (e.g. cluster) and value holds a backend-relative reference, optionally pinned with @version."))
	sb.String("description", "dev.miren.core/env.description", schema.Doc("Human-readable description of this variable's purpose"))
	sb.String("key", "dev.miren.core/env.key", schema.Doc("The name of the variable"))
	sb.Bool("required", "dev.miren.core/env.required", schema.Doc("Whether this variable must have a non-empty value for deploy to succeed"))
	sb.Bool("sensitive", "dev.miren.core/env.sensitive", schema.Doc("Whether or not the value is sensitive"))
	sb.String("source", "dev.miren.core/env.source", schema.Doc("The source of the variable (config or manual). Defaults to config for backward compatibility."))
	sb.String("value", "dev.miren.core/env.value", schema.Doc("The value of the variable"))
}

const (
	PortsNameId        = entity.Id("dev.miren.core/ports.name")
	PortsNodePortId    = entity.Id("dev.miren.core/ports.node_port")
	PortsPortId        = entity.Id("dev.miren.core/ports.port")
	PortsProtocolId    = entity.Id("dev.miren.core/ports.protocol")
	PortsProtocolTcpId = entity.Id("dev.miren.core/protocol.tcp")
	PortsProtocolUdpId = entity.Id("dev.miren.core/protocol.udp")
	PortsTypeId        = entity.Id("dev.miren.core/ports.type")
)

type Ports struct {
	Name     string        `cbor:"name" json:"name"`
	NodePort int64         `cbor:"node_port,omitempty" json:"node_port,omitempty"`
	Port     int64         `cbor:"port" json:"port"`
	Protocol PortsProtocol `cbor:"protocol,omitempty" json:"protocol,omitempty"`
	Type     string        `cbor:"type,omitempty" json:"type,omitempty"`
}

type PortsProtocol string

const (
	TCP PortsProtocol = "protocol.tcp"
	UDP PortsProtocol = "protocol.udp"
)

var PortsprotocolFromId = map[entity.Id]PortsProtocol{PortsProtocolTcpId: TCP, PortsProtocolUdpId: UDP}
var PortsprotocolToId = map[PortsProtocol]entity.Id{TCP: PortsProtocolTcpId, UDP: PortsProtocolUdpId}

func (o *Ports) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(PortsNameId); ok && a.Value.Kind() == entity.KindString {
		o.Name = a.Value.String()
	}
	if a, ok := e.Get(PortsNodePortId); ok && a.Value.Kind() == entity.KindInt64 {
		o.NodePort = a.Value.Int64()
	}
	if a, ok := e.Get(PortsPortId); ok && a.Value.Kind() == entity.KindInt64 {
		o.Port = a.Value.Int64()
	}
	if a, ok := e.Get(PortsProtocolId); ok && a.Value.Kind() == entity.KindId {
		o.Protocol = PortsprotocolFromId[a.Value.Id()]
	}
	if a, ok := e.Get(PortsTypeId); ok && a.Value.Kind() == entity.KindString {
		o.Type = a.Value.String()
	}
}

func (o *Ports) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Name) {
		attrs = append(attrs, entity.String(PortsNameId, o.Name))
	}
	if !entity.Empty(o.NodePort) {
		attrs = append(attrs, entity.Int64(PortsNodePortId, o.NodePort))
	}
	attrs = append(attrs, entity.Int64(PortsPortId, o.Port))
	if a, ok := PortsprotocolToId[o.Protocol]; ok {
		attrs = append(attrs, entity.Ref(PortsProtocolId, a))
	}
	if !entity.Empty(o.Type) {
		attrs = append(attrs, entity.String(PortsTypeId, o.Type))
	}
	return
}

func (o *Ports) Empty() bool {
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

func (o *Ports) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("name", "dev.miren.core/ports.name", schema.Required)
	sb.Int64("node_port", "dev.miren.core/ports.node_port")
	sb.Int64("port", "dev.miren.core/ports.port", schema.Required)
	sb.Singleton("dev.miren.core/protocol.tcp")
	sb.Singleton("dev.miren.core/protocol.udp")
	sb.Ref("protocol", "dev.miren.core/ports.protocol", schema.Choices(PortsProtocolTcpId, PortsProtocolUdpId))
	sb.String("type", "dev.miren.core/ports.type")
}

const (
	ServiceConcurrencyModeId                = entity.Id("dev.miren.core/service_concurrency.mode")
	ServiceConcurrencyNumInstancesId        = entity.Id("dev.miren.core/service_concurrency.num_instances")
	ServiceConcurrencyRequestsPerInstanceId = entity.Id("dev.miren.core/service_concurrency.requests_per_instance")
	ServiceConcurrencyScaleDownDelayId      = entity.Id("dev.miren.core/service_concurrency.scale_down_delay")
	ServiceConcurrencyShutdownTimeoutId     = entity.Id("dev.miren.core/service_concurrency.shutdown_timeout")
)

type ServiceConcurrency struct {
	Mode                string `cbor:"mode,omitempty" json:"mode,omitempty"`
	NumInstances        int64  `cbor:"num_instances,omitempty" json:"num_instances,omitempty"`
	RequestsPerInstance int64  `cbor:"requests_per_instance,omitempty" json:"requests_per_instance,omitempty"`
	ScaleDownDelay      string `cbor:"scale_down_delay,omitempty" json:"scale_down_delay,omitempty"`
	ShutdownTimeout     string `cbor:"shutdown_timeout,omitempty" json:"shutdown_timeout,omitempty"`
}

func (o *ServiceConcurrency) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(ServiceConcurrencyModeId); ok && a.Value.Kind() == entity.KindString {
		o.Mode = a.Value.String()
	}
	if a, ok := e.Get(ServiceConcurrencyNumInstancesId); ok && a.Value.Kind() == entity.KindInt64 {
		o.NumInstances = a.Value.Int64()
	}
	if a, ok := e.Get(ServiceConcurrencyRequestsPerInstanceId); ok && a.Value.Kind() == entity.KindInt64 {
		o.RequestsPerInstance = a.Value.Int64()
	}
	if a, ok := e.Get(ServiceConcurrencyScaleDownDelayId); ok && a.Value.Kind() == entity.KindString {
		o.ScaleDownDelay = a.Value.String()
	}
	if a, ok := e.Get(ServiceConcurrencyShutdownTimeoutId); ok && a.Value.Kind() == entity.KindString {
		o.ShutdownTimeout = a.Value.String()
	}
}

func (o *ServiceConcurrency) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Mode) {
		attrs = append(attrs, entity.String(ServiceConcurrencyModeId, o.Mode))
	}
	if !entity.Empty(o.NumInstances) {
		attrs = append(attrs, entity.Int64(ServiceConcurrencyNumInstancesId, o.NumInstances))
	}
	if !entity.Empty(o.RequestsPerInstance) {
		attrs = append(attrs, entity.Int64(ServiceConcurrencyRequestsPerInstanceId, o.RequestsPerInstance))
	}
	if !entity.Empty(o.ScaleDownDelay) {
		attrs = append(attrs, entity.String(ServiceConcurrencyScaleDownDelayId, o.ScaleDownDelay))
	}
	if !entity.Empty(o.ShutdownTimeout) {
		attrs = append(attrs, entity.String(ServiceConcurrencyShutdownTimeoutId, o.ShutdownTimeout))
	}
	return
}

func (o *ServiceConcurrency) Empty() bool {
	if !entity.Empty(o.Mode) {
		return false
	}
	if !entity.Empty(o.NumInstances) {
		return false
	}
	if !entity.Empty(o.RequestsPerInstance) {
		return false
	}
	if !entity.Empty(o.ScaleDownDelay) {
		return false
	}
	if !entity.Empty(o.ShutdownTimeout) {
		return false
	}
	return true
}

func (o *ServiceConcurrency) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("mode", "dev.miren.core/service_concurrency.mode", schema.Doc("The concurrency mode (auto or fixed)"))
	sb.Int64("num_instances", "dev.miren.core/service_concurrency.num_instances", schema.Doc("For fixed mode, number of instances to maintain"))
	sb.Int64("requests_per_instance", "dev.miren.core/service_concurrency.requests_per_instance", schema.Doc("For auto mode, number of concurrent requests per instance"))
	sb.String("scale_down_delay", "dev.miren.core/service_concurrency.scale_down_delay", schema.Doc("For auto mode, delay before scaling down idle instances (e.g. 2m, 15m)"))
	sb.String("shutdown_timeout", "dev.miren.core/service_concurrency.shutdown_timeout", schema.Doc("Time to wait for graceful shutdown before force-killing (e.g. 10s, 30s). Defaults to 10s."))
}

const (
	VariableBackendId     = entity.Id("dev.miren.core/variable.backend")
	VariableDescriptionId = entity.Id("dev.miren.core/variable.description")
	VariableKeyId         = entity.Id("dev.miren.core/variable.key")
	VariableRequiredId    = entity.Id("dev.miren.core/variable.required")
	VariableSensitiveId   = entity.Id("dev.miren.core/variable.sensitive")
	VariableSourceId      = entity.Id("dev.miren.core/variable.source")
	VariableValueId       = entity.Id("dev.miren.core/variable.value")
)

type Variable struct {
	Backend     string `cbor:"backend,omitempty" json:"backend,omitempty"`
	Description string `cbor:"description,omitempty" json:"description,omitempty"`
	Key         string `cbor:"key,omitempty" json:"key,omitempty"`
	Required    bool   `cbor:"required,omitempty" json:"required,omitempty"`
	Sensitive   bool   `cbor:"sensitive,omitempty" json:"sensitive,omitempty"`
	Source      string `cbor:"source,omitempty" json:"source,omitempty"`
	Value       string `cbor:"value,omitempty" json:"value,omitempty"`
}

func (o *Variable) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(VariableBackendId); ok && a.Value.Kind() == entity.KindString {
		o.Backend = a.Value.String()
	}
	if a, ok := e.Get(VariableDescriptionId); ok && a.Value.Kind() == entity.KindString {
		o.Description = a.Value.String()
	}
	if a, ok := e.Get(VariableKeyId); ok && a.Value.Kind() == entity.KindString {
		o.Key = a.Value.String()
	}
	if a, ok := e.Get(VariableRequiredId); ok && a.Value.Kind() == entity.KindBool {
		o.Required = a.Value.Bool()
	}
	if a, ok := e.Get(VariableSensitiveId); ok && a.Value.Kind() == entity.KindBool {
		o.Sensitive = a.Value.Bool()
	}
	if a, ok := e.Get(VariableSourceId); ok && a.Value.Kind() == entity.KindString {
		o.Source = a.Value.String()
	}
	if a, ok := e.Get(VariableValueId); ok && a.Value.Kind() == entity.KindString {
		o.Value = a.Value.String()
	}
}

func (o *Variable) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Backend) {
		attrs = append(attrs, entity.String(VariableBackendId, o.Backend))
	}
	if !entity.Empty(o.Description) {
		attrs = append(attrs, entity.String(VariableDescriptionId, o.Description))
	}
	if !entity.Empty(o.Key) {
		attrs = append(attrs, entity.String(VariableKeyId, o.Key))
	}
	attrs = append(attrs, entity.Bool(VariableRequiredId, o.Required))
	attrs = append(attrs, entity.Bool(VariableSensitiveId, o.Sensitive))
	if !entity.Empty(o.Source) {
		attrs = append(attrs, entity.String(VariableSourceId, o.Source))
	}
	if !entity.Empty(o.Value) {
		attrs = append(attrs, entity.String(VariableValueId, o.Value))
	}
	return
}

func (o *Variable) Empty() bool {
	if !entity.Empty(o.Backend) {
		return false
	}
	if !entity.Empty(o.Description) {
		return false
	}
	if !entity.Empty(o.Key) {
		return false
	}
	if !entity.Empty(o.Required) {
		return false
	}
	if !entity.Empty(o.Sensitive) {
		return false
	}
	if !entity.Empty(o.Source) {
		return false
	}
	if !entity.Empty(o.Value) {
		return false
	}
	return true
}

func (o *Variable) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("backend", "dev.miren.core/variable.backend", schema.Doc("Where the value comes from. Empty means value holds an inline literal; any other name refers to a registered secret backend instance (e.g. cluster) and value holds a backend-relative reference, optionally pinned with @version."))
	sb.String("description", "dev.miren.core/variable.description", schema.Doc("Human-readable description of this variable's purpose"))
	sb.String("key", "dev.miren.core/variable.key", schema.Doc("The name of the variable"))
	sb.Bool("required", "dev.miren.core/variable.required", schema.Doc("Whether this variable must have a non-empty value for deploy to succeed"))
	sb.Bool("sensitive", "dev.miren.core/variable.sensitive", schema.Doc("Whether or not the value is sensitive"))
	sb.String("source", "dev.miren.core/variable.source", schema.Doc("The source of the variable (config or manual). Defaults to config for backward compatibility."))
	sb.String("value", "dev.miren.core/variable.value", schema.Doc("The value of the value"))
}

const (
	ArtifactAppId            = entity.Id("dev.miren.core/artifact.app")
	ArtifactManifestId       = entity.Id("dev.miren.core/artifact.manifest")
	ArtifactManifestDigestId = entity.Id("dev.miren.core/artifact.manifest_digest")
	ArtifactStatusId         = entity.Id("dev.miren.core/artifact.status")
	ArtifactStatusActiveId   = entity.Id("dev.miren.core/status.active")
	ArtifactStatusArchivedId = entity.Id("dev.miren.core/status.archived")
)

type Artifact struct {
	ID             entity.Id      `json:"id"`
	App            entity.Id      `cbor:"app,omitempty" json:"app,omitempty"`
	Manifest       string         `cbor:"manifest,omitempty" json:"manifest,omitempty"`
	ManifestDigest string         `cbor:"manifest_digest,omitempty" json:"manifest_digest,omitempty"`
	Status         ArtifactStatus `cbor:"status,omitempty" json:"status,omitempty"`
}

type ArtifactStatus string

const (
	ACTIVE   ArtifactStatus = "status.active"
	ARCHIVED ArtifactStatus = "status.archived"
)

var artifactstatusFromId = map[entity.Id]ArtifactStatus{ArtifactStatusActiveId: ACTIVE, ArtifactStatusArchivedId: ARCHIVED}
var artifactstatusToId = map[ArtifactStatus]entity.Id{ACTIVE: ArtifactStatusActiveId, ARCHIVED: ArtifactStatusArchivedId}

func (o *Artifact) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(ArtifactAppId); ok && a.Value.Kind() == entity.KindId {
		o.App = a.Value.Id()
	}
	if a, ok := e.Get(ArtifactManifestId); ok && a.Value.Kind() == entity.KindString {
		o.Manifest = a.Value.String()
	}
	if a, ok := e.Get(ArtifactManifestDigestId); ok && a.Value.Kind() == entity.KindString {
		o.ManifestDigest = a.Value.String()
	}
	if a, ok := e.Get(ArtifactStatusId); ok && a.Value.Kind() == entity.KindId {
		o.Status = artifactstatusFromId[a.Value.Id()]
	}
}

func (o *Artifact) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindArtifact)
}

func (o *Artifact) ShortKind() string {
	return "artifact"
}

func (o *Artifact) Kind() entity.Id {
	return KindArtifact
}

func (o *Artifact) EntityId() entity.Id {
	return o.ID
}

func (o *Artifact) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.App) {
		attrs = append(attrs, entity.Ref(ArtifactAppId, o.App))
	}
	if !entity.Empty(o.Manifest) {
		attrs = append(attrs, entity.String(ArtifactManifestId, o.Manifest))
	}
	if !entity.Empty(o.ManifestDigest) {
		attrs = append(attrs, entity.String(ArtifactManifestDigestId, o.ManifestDigest))
	}
	if a, ok := artifactstatusToId[o.Status]; ok {
		attrs = append(attrs, entity.Ref(ArtifactStatusId, a))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindArtifact))
	return
}

func (o *Artifact) Empty() bool {
	if !entity.Empty(o.App) {
		return false
	}
	if !entity.Empty(o.Manifest) {
		return false
	}
	if !entity.Empty(o.ManifestDigest) {
		return false
	}
	if o.Status != "" {
		return false
	}
	return true
}

func (o *Artifact) InitSchema(sb *schema.SchemaBuilder) {
	sb.Ref("app", "dev.miren.core/artifact.app", schema.Doc("The application the artifact is for"), schema.Indexed, schema.Tags("dev.miren.app_ref"))
	sb.String("manifest", "dev.miren.core/artifact.manifest", schema.Doc("The OCI image manifest for the version"))
	sb.String("manifest_digest", "dev.miren.core/artifact.manifest_digest", schema.Doc("The digest of the manifest"), schema.Indexed)
	sb.Singleton("dev.miren.core/status.active")
	sb.Singleton("dev.miren.core/status.archived")
	sb.Ref("status", "dev.miren.core/artifact.status", schema.Doc("Artifact lifecycle status"), schema.Indexed, schema.Choices(ArtifactStatusActiveId, ArtifactStatusArchivedId))
}

const (
	ConfigVersionAppId  = entity.Id("dev.miren.core/config_version.app")
	ConfigVersionSpecId = entity.Id("dev.miren.core/config_version.spec")
)

type ConfigVersion struct {
	ID   entity.Id  `json:"id"`
	App  entity.Id  `cbor:"app,omitempty" json:"app,omitempty"`
	Spec ConfigSpec `cbor:"spec,omitempty" json:"spec"`
}

func (o *ConfigVersion) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(ConfigVersionAppId); ok && a.Value.Kind() == entity.KindId {
		o.App = a.Value.Id()
	}
	if a, ok := e.Get(ConfigVersionSpecId); ok && a.Value.Kind() == entity.KindComponent {
		o.Spec.Decode(a.Value.Component())
	}
}

func (o *ConfigVersion) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindConfigVersion)
}

func (o *ConfigVersion) ShortKind() string {
	return "config_version"
}

func (o *ConfigVersion) Kind() entity.Id {
	return KindConfigVersion
}

func (o *ConfigVersion) EntityId() entity.Id {
	return o.ID
}

func (o *ConfigVersion) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.App) {
		attrs = append(attrs, entity.Ref(ConfigVersionAppId, o.App))
	}
	if !o.Spec.Empty() {
		attrs = append(attrs, entity.Component(ConfigVersionSpecId, o.Spec.Encode()))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindConfigVersion))
	return
}

func (o *ConfigVersion) Empty() bool {
	if !entity.Empty(o.App) {
		return false
	}
	if !o.Spec.Empty() {
		return false
	}
	return true
}

func (o *ConfigVersion) InitSchema(sb *schema.SchemaBuilder) {
	sb.Ref("app", "dev.miren.core/config_version.app", schema.Doc("The application this config version belongs to"), schema.Indexed, schema.Tags("dev.miren.app_ref"))
	sb.Component("spec", "dev.miren.core/config_version.spec", schema.Doc("The configuration specification"))
}

const (
	DeploymentAppNameId            = entity.Id("dev.miren.core/deployment.app_name")
	DeploymentAppVersionId         = entity.Id("dev.miren.core/deployment.app_version")
	DeploymentBuildLogsId          = entity.Id("dev.miren.core/deployment.build_logs")
	DeploymentClusterIdId          = entity.Id("dev.miren.core/deployment.cluster_id")
	DeploymentCompletedAtId        = entity.Id("dev.miren.core/deployment.completed_at")
	DeploymentDeployedById         = entity.Id("dev.miren.core/deployment.deployed_by")
	DeploymentErrorMessageId       = entity.Id("dev.miren.core/deployment.error_message")
	DeploymentGitInfoId            = entity.Id("dev.miren.core/deployment.git_info")
	DeploymentPhaseId              = entity.Id("dev.miren.core/deployment.phase")
	DeploymentSourceDeploymentIdId = entity.Id("dev.miren.core/deployment.source_deployment_id")
	DeploymentStatusId             = entity.Id("dev.miren.core/deployment.status")
)

type Deployment struct {
	ID                 entity.Id  `json:"id"`
	AppName            string     `cbor:"app_name,omitempty" json:"app_name,omitempty"`
	AppVersion         string     `cbor:"app_version,omitempty" json:"app_version,omitempty"`
	BuildLogs          string     `cbor:"build_logs,omitempty" json:"build_logs,omitempty"`
	ClusterId          string     `cbor:"cluster_id,omitempty" json:"cluster_id,omitempty"`
	CompletedAt        string     `cbor:"completed_at,omitempty" json:"completed_at,omitempty"`
	DeployedBy         DeployedBy `cbor:"deployed_by,omitempty" json:"deployed_by"`
	ErrorMessage       string     `cbor:"error_message,omitempty" json:"error_message,omitempty"`
	GitInfo            GitInfo    `cbor:"git_info,omitempty" json:"git_info"`
	Phase              string     `cbor:"phase,omitempty" json:"phase,omitempty"`
	SourceDeploymentId string     `cbor:"source_deployment_id,omitempty" json:"source_deployment_id,omitempty"`
	Status             string     `cbor:"status,omitempty" json:"status,omitempty"`
}

func (o *Deployment) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(DeploymentAppNameId); ok && a.Value.Kind() == entity.KindString {
		o.AppName = a.Value.String()
	}
	if a, ok := e.Get(DeploymentAppVersionId); ok && a.Value.Kind() == entity.KindString {
		o.AppVersion = a.Value.String()
	}
	if a, ok := e.Get(DeploymentBuildLogsId); ok && a.Value.Kind() == entity.KindString {
		o.BuildLogs = a.Value.String()
	}
	if a, ok := e.Get(DeploymentClusterIdId); ok && a.Value.Kind() == entity.KindString {
		o.ClusterId = a.Value.String()
	}
	if a, ok := e.Get(DeploymentCompletedAtId); ok && a.Value.Kind() == entity.KindString {
		o.CompletedAt = a.Value.String()
	}
	if a, ok := e.Get(DeploymentDeployedById); ok && a.Value.Kind() == entity.KindComponent {
		o.DeployedBy.Decode(a.Value.Component())
	}
	if a, ok := e.Get(DeploymentErrorMessageId); ok && a.Value.Kind() == entity.KindString {
		o.ErrorMessage = a.Value.String()
	}
	if a, ok := e.Get(DeploymentGitInfoId); ok && a.Value.Kind() == entity.KindComponent {
		o.GitInfo.Decode(a.Value.Component())
	}
	if a, ok := e.Get(DeploymentPhaseId); ok && a.Value.Kind() == entity.KindString {
		o.Phase = a.Value.String()
	}
	if a, ok := e.Get(DeploymentSourceDeploymentIdId); ok && a.Value.Kind() == entity.KindString {
		o.SourceDeploymentId = a.Value.String()
	}
	if a, ok := e.Get(DeploymentStatusId); ok && a.Value.Kind() == entity.KindString {
		o.Status = a.Value.String()
	}
}

func (o *Deployment) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindDeployment)
}

func (o *Deployment) ShortKind() string {
	return "deployment"
}

func (o *Deployment) Kind() entity.Id {
	return KindDeployment
}

func (o *Deployment) EntityId() entity.Id {
	return o.ID
}

func (o *Deployment) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.AppName) {
		attrs = append(attrs, entity.String(DeploymentAppNameId, o.AppName))
	}
	if !entity.Empty(o.AppVersion) {
		attrs = append(attrs, entity.String(DeploymentAppVersionId, o.AppVersion))
	}
	if !entity.Empty(o.BuildLogs) {
		attrs = append(attrs, entity.String(DeploymentBuildLogsId, o.BuildLogs))
	}
	if !entity.Empty(o.ClusterId) {
		attrs = append(attrs, entity.String(DeploymentClusterIdId, o.ClusterId))
	}
	if !entity.Empty(o.CompletedAt) {
		attrs = append(attrs, entity.String(DeploymentCompletedAtId, o.CompletedAt))
	}
	if !o.DeployedBy.Empty() {
		attrs = append(attrs, entity.Component(DeploymentDeployedById, o.DeployedBy.Encode()))
	}
	if !entity.Empty(o.ErrorMessage) {
		attrs = append(attrs, entity.String(DeploymentErrorMessageId, o.ErrorMessage))
	}
	if !o.GitInfo.Empty() {
		attrs = append(attrs, entity.Component(DeploymentGitInfoId, o.GitInfo.Encode()))
	}
	if !entity.Empty(o.Phase) {
		attrs = append(attrs, entity.String(DeploymentPhaseId, o.Phase))
	}
	if !entity.Empty(o.SourceDeploymentId) {
		attrs = append(attrs, entity.String(DeploymentSourceDeploymentIdId, o.SourceDeploymentId))
	}
	if !entity.Empty(o.Status) {
		attrs = append(attrs, entity.String(DeploymentStatusId, o.Status))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindDeployment))
	return
}

func (o *Deployment) Empty() bool {
	if !entity.Empty(o.AppName) {
		return false
	}
	if !entity.Empty(o.AppVersion) {
		return false
	}
	if !entity.Empty(o.BuildLogs) {
		return false
	}
	if !entity.Empty(o.ClusterId) {
		return false
	}
	if !entity.Empty(o.CompletedAt) {
		return false
	}
	if !o.DeployedBy.Empty() {
		return false
	}
	if !entity.Empty(o.ErrorMessage) {
		return false
	}
	if !o.GitInfo.Empty() {
		return false
	}
	if !entity.Empty(o.Phase) {
		return false
	}
	if !entity.Empty(o.SourceDeploymentId) {
		return false
	}
	if !entity.Empty(o.Status) {
		return false
	}
	return true
}

func (o *Deployment) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("app_name", "dev.miren.core/deployment.app_name", schema.Doc("The name of the app being deployed"), schema.Indexed)
	sb.String("app_version", "dev.miren.core/deployment.app_version", schema.Doc("The app version ID or temporary value (pending-build, failed-{id})"))
	sb.String("build_logs", "dev.miren.core/deployment.build_logs", schema.Doc("Build logs concatenated with newlines (especially useful for failed deployments)"))
	sb.String("cluster_id", "dev.miren.core/deployment.cluster_id", schema.Doc("The cluster where the deployment is happening"), schema.Indexed)
	sb.String("completed_at", "dev.miren.core/deployment.completed_at", schema.Doc("When the deployment was completed (RFC3339 format)"))
	sb.Component("deployed_by", "dev.miren.core/deployment.deployed_by", schema.Doc("Information about who initiated the deployment"))
	(&DeployedBy{}).InitSchema(sb.Builder("deployment.deployed_by"))
	sb.String("error_message", "dev.miren.core/deployment.error_message", schema.Doc("Error message if deployment failed"))
	sb.Component("git_info", "dev.miren.core/deployment.git_info", schema.Doc("Git information at time of deployment"))
	(&GitInfo{}).InitSchema(sb.Builder("deployment.git_info"))
	sb.String("phase", "dev.miren.core/deployment.phase", schema.Doc("Current phase of deployment (preparing, building, pushing, activating)"))
	sb.String("source_deployment_id", "dev.miren.core/deployment.source_deployment_id", schema.Doc("ID of the deployment this was based on (for rollback/redeploy provenance)"))
	sb.String("status", "dev.miren.core/deployment.status", schema.Doc("Deployment status (in_progress, active, failed, rolled_back)"), schema.Indexed)
}

const (
	DeployedByTimestampId = entity.Id("dev.miren.core/deployed_by.timestamp")
	DeployedByUserEmailId = entity.Id("dev.miren.core/deployed_by.user_email")
	DeployedByUserIdId    = entity.Id("dev.miren.core/deployed_by.user_id")
	DeployedByUserNameId  = entity.Id("dev.miren.core/deployed_by.user_name")
)

type DeployedBy struct {
	Timestamp string `cbor:"timestamp,omitempty" json:"timestamp,omitempty"`
	UserEmail string `cbor:"user_email,omitempty" json:"user_email,omitempty"`
	UserId    string `cbor:"user_id,omitempty" json:"user_id,omitempty"`
	UserName  string `cbor:"user_name,omitempty" json:"user_name,omitempty"`
}

func (o *DeployedBy) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(DeployedByTimestampId); ok && a.Value.Kind() == entity.KindString {
		o.Timestamp = a.Value.String()
	}
	if a, ok := e.Get(DeployedByUserEmailId); ok && a.Value.Kind() == entity.KindString {
		o.UserEmail = a.Value.String()
	}
	if a, ok := e.Get(DeployedByUserIdId); ok && a.Value.Kind() == entity.KindString {
		o.UserId = a.Value.String()
	}
	if a, ok := e.Get(DeployedByUserNameId); ok && a.Value.Kind() == entity.KindString {
		o.UserName = a.Value.String()
	}
}

func (o *DeployedBy) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Timestamp) {
		attrs = append(attrs, entity.String(DeployedByTimestampId, o.Timestamp))
	}
	if !entity.Empty(o.UserEmail) {
		attrs = append(attrs, entity.String(DeployedByUserEmailId, o.UserEmail))
	}
	if !entity.Empty(o.UserId) {
		attrs = append(attrs, entity.String(DeployedByUserIdId, o.UserId))
	}
	if !entity.Empty(o.UserName) {
		attrs = append(attrs, entity.String(DeployedByUserNameId, o.UserName))
	}
	return
}

func (o *DeployedBy) Empty() bool {
	if !entity.Empty(o.Timestamp) {
		return false
	}
	if !entity.Empty(o.UserEmail) {
		return false
	}
	if !entity.Empty(o.UserId) {
		return false
	}
	if !entity.Empty(o.UserName) {
		return false
	}
	return true
}

func (o *DeployedBy) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("timestamp", "dev.miren.core/deployed_by.timestamp", schema.Doc("When the deployment was initiated (RFC3339 format)"))
	sb.String("user_email", "dev.miren.core/deployed_by.user_email", schema.Doc("The email of the user who deployed"))
	sb.String("user_id", "dev.miren.core/deployed_by.user_id", schema.Doc("The ID of the user who deployed"))
	sb.String("user_name", "dev.miren.core/deployed_by.user_name", schema.Doc("The username of the user who deployed"))
}

const (
	GitInfoAuthorId            = entity.Id("dev.miren.core/git_info.author")
	GitInfoBranchId            = entity.Id("dev.miren.core/git_info.branch")
	GitInfoCommitAuthorEmailId = entity.Id("dev.miren.core/git_info.commit_author_email")
	GitInfoCommitTimestampId   = entity.Id("dev.miren.core/git_info.commit_timestamp")
	GitInfoIsDirtyId           = entity.Id("dev.miren.core/git_info.is_dirty")
	GitInfoMessageId           = entity.Id("dev.miren.core/git_info.message")
	GitInfoRepositoryId        = entity.Id("dev.miren.core/git_info.repository")
	GitInfoShaId               = entity.Id("dev.miren.core/git_info.sha")
	GitInfoWorkingTreeHashId   = entity.Id("dev.miren.core/git_info.working_tree_hash")
)

type GitInfo struct {
	Author            string `cbor:"author,omitempty" json:"author,omitempty"`
	Branch            string `cbor:"branch,omitempty" json:"branch,omitempty"`
	CommitAuthorEmail string `cbor:"commit_author_email,omitempty" json:"commit_author_email,omitempty"`
	CommitTimestamp   string `cbor:"commit_timestamp,omitempty" json:"commit_timestamp,omitempty"`
	IsDirty           bool   `cbor:"is_dirty,omitempty" json:"is_dirty,omitempty"`
	Message           string `cbor:"message,omitempty" json:"message,omitempty"`
	Repository        string `cbor:"repository,omitempty" json:"repository,omitempty"`
	Sha               string `cbor:"sha,omitempty" json:"sha,omitempty"`
	WorkingTreeHash   string `cbor:"working_tree_hash,omitempty" json:"working_tree_hash,omitempty"`
}

func (o *GitInfo) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(GitInfoAuthorId); ok && a.Value.Kind() == entity.KindString {
		o.Author = a.Value.String()
	}
	if a, ok := e.Get(GitInfoBranchId); ok && a.Value.Kind() == entity.KindString {
		o.Branch = a.Value.String()
	}
	if a, ok := e.Get(GitInfoCommitAuthorEmailId); ok && a.Value.Kind() == entity.KindString {
		o.CommitAuthorEmail = a.Value.String()
	}
	if a, ok := e.Get(GitInfoCommitTimestampId); ok && a.Value.Kind() == entity.KindString {
		o.CommitTimestamp = a.Value.String()
	}
	if a, ok := e.Get(GitInfoIsDirtyId); ok && a.Value.Kind() == entity.KindBool {
		o.IsDirty = a.Value.Bool()
	}
	if a, ok := e.Get(GitInfoMessageId); ok && a.Value.Kind() == entity.KindString {
		o.Message = a.Value.String()
	}
	if a, ok := e.Get(GitInfoRepositoryId); ok && a.Value.Kind() == entity.KindString {
		o.Repository = a.Value.String()
	}
	if a, ok := e.Get(GitInfoShaId); ok && a.Value.Kind() == entity.KindString {
		o.Sha = a.Value.String()
	}
	if a, ok := e.Get(GitInfoWorkingTreeHashId); ok && a.Value.Kind() == entity.KindString {
		o.WorkingTreeHash = a.Value.String()
	}
}

func (o *GitInfo) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Author) {
		attrs = append(attrs, entity.String(GitInfoAuthorId, o.Author))
	}
	if !entity.Empty(o.Branch) {
		attrs = append(attrs, entity.String(GitInfoBranchId, o.Branch))
	}
	if !entity.Empty(o.CommitAuthorEmail) {
		attrs = append(attrs, entity.String(GitInfoCommitAuthorEmailId, o.CommitAuthorEmail))
	}
	if !entity.Empty(o.CommitTimestamp) {
		attrs = append(attrs, entity.String(GitInfoCommitTimestampId, o.CommitTimestamp))
	}
	attrs = append(attrs, entity.Bool(GitInfoIsDirtyId, o.IsDirty))
	if !entity.Empty(o.Message) {
		attrs = append(attrs, entity.String(GitInfoMessageId, o.Message))
	}
	if !entity.Empty(o.Repository) {
		attrs = append(attrs, entity.String(GitInfoRepositoryId, o.Repository))
	}
	if !entity.Empty(o.Sha) {
		attrs = append(attrs, entity.String(GitInfoShaId, o.Sha))
	}
	if !entity.Empty(o.WorkingTreeHash) {
		attrs = append(attrs, entity.String(GitInfoWorkingTreeHashId, o.WorkingTreeHash))
	}
	return
}

func (o *GitInfo) Empty() bool {
	if !entity.Empty(o.Author) {
		return false
	}
	if !entity.Empty(o.Branch) {
		return false
	}
	if !entity.Empty(o.CommitAuthorEmail) {
		return false
	}
	if !entity.Empty(o.CommitTimestamp) {
		return false
	}
	if !entity.Empty(o.IsDirty) {
		return false
	}
	if !entity.Empty(o.Message) {
		return false
	}
	if !entity.Empty(o.Repository) {
		return false
	}
	if !entity.Empty(o.Sha) {
		return false
	}
	if !entity.Empty(o.WorkingTreeHash) {
		return false
	}
	return true
}

func (o *GitInfo) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("author", "dev.miren.core/git_info.author", schema.Doc("Git commit author"))
	sb.String("branch", "dev.miren.core/git_info.branch", schema.Doc("Git branch name"))
	sb.String("commit_author_email", "dev.miren.core/git_info.commit_author_email", schema.Doc("Git commit author email address"))
	sb.String("commit_timestamp", "dev.miren.core/git_info.commit_timestamp", schema.Doc("Git commit timestamp in RFC3339 format"))
	sb.Bool("is_dirty", "dev.miren.core/git_info.is_dirty", schema.Doc("Whether working tree had uncommitted changes"))
	sb.String("message", "dev.miren.core/git_info.message", schema.Doc("Git commit message"))
	sb.String("repository", "dev.miren.core/git_info.repository", schema.Doc("Git repository remote URL"))
	sb.String("sha", "dev.miren.core/git_info.sha", schema.Doc("Git commit SHA"))
	sb.String("working_tree_hash", "dev.miren.core/git_info.working_tree_hash", schema.Doc("Hash of working tree if dirty"))
}

const (
	DeploymentLockAcquiredAtId   = entity.Id("dev.miren.core/deployment_lock.acquired_at")
	DeploymentLockAppNameId      = entity.Id("dev.miren.core/deployment_lock.app_name")
	DeploymentLockDeploymentIdId = entity.Id("dev.miren.core/deployment_lock.deployment_id")
	DeploymentLockExpiresAtId    = entity.Id("dev.miren.core/deployment_lock.expires_at")
)

type DeploymentLock struct {
	ID           entity.Id `json:"id"`
	AcquiredAt   time.Time `cbor:"acquired_at,omitempty" json:"acquired_at"`
	AppName      string    `cbor:"app_name,omitempty" json:"app_name,omitempty"`
	DeploymentId string    `cbor:"deployment_id,omitempty" json:"deployment_id,omitempty"`
	ExpiresAt    time.Time `cbor:"expires_at,omitempty" json:"expires_at"`
}

func (o *DeploymentLock) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(DeploymentLockAcquiredAtId); ok && a.Value.Kind() == entity.KindTime {
		o.AcquiredAt = a.Value.Time()
	}
	if a, ok := e.Get(DeploymentLockAppNameId); ok && a.Value.Kind() == entity.KindString {
		o.AppName = a.Value.String()
	}
	if a, ok := e.Get(DeploymentLockDeploymentIdId); ok && a.Value.Kind() == entity.KindString {
		o.DeploymentId = a.Value.String()
	}
	if a, ok := e.Get(DeploymentLockExpiresAtId); ok && a.Value.Kind() == entity.KindTime {
		o.ExpiresAt = a.Value.Time()
	}
}

func (o *DeploymentLock) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindDeploymentLock)
}

func (o *DeploymentLock) ShortKind() string {
	return "deployment_lock"
}

func (o *DeploymentLock) Kind() entity.Id {
	return KindDeploymentLock
}

func (o *DeploymentLock) EntityId() entity.Id {
	return o.ID
}

func (o *DeploymentLock) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.AcquiredAt) {
		attrs = append(attrs, entity.Time(DeploymentLockAcquiredAtId, o.AcquiredAt))
	}
	if !entity.Empty(o.AppName) {
		attrs = append(attrs, entity.String(DeploymentLockAppNameId, o.AppName))
	}
	if !entity.Empty(o.DeploymentId) {
		attrs = append(attrs, entity.String(DeploymentLockDeploymentIdId, o.DeploymentId))
	}
	if !entity.Empty(o.ExpiresAt) {
		attrs = append(attrs, entity.Time(DeploymentLockExpiresAtId, o.ExpiresAt))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindDeploymentLock))
	return
}

func (o *DeploymentLock) Empty() bool {
	if !entity.Empty(o.AcquiredAt) {
		return false
	}
	if !entity.Empty(o.AppName) {
		return false
	}
	if !entity.Empty(o.DeploymentId) {
		return false
	}
	if !entity.Empty(o.ExpiresAt) {
		return false
	}
	return true
}

func (o *DeploymentLock) InitSchema(sb *schema.SchemaBuilder) {
	sb.Time("acquired_at", "dev.miren.core/deployment_lock.acquired_at", schema.Doc("When the lock was taken"))
	sb.String("app_name", "dev.miren.core/deployment_lock.app_name", schema.Doc("The app this lock covers. The lock is app-scoped, not app+cluster: a coordinator's store only holds its own cluster's deployments, and the client-supplied cluster_id is unreliable (see MIR-1465).\n"), schema.Indexed)
	sb.String("deployment_id", "dev.miren.core/deployment_lock.deployment_id", schema.Doc("The deployment currently holding the lock"))
	sb.Time("expires_at", "dev.miren.core/deployment_lock.expires_at", schema.Doc("When the lock may be stolen by another deployment. A holder whose deployment reached a terminal status is stealable before this too.\n"), schema.Indexed)
}

const (
	KeyRotationErrorMessageId     = entity.Id("dev.miren.core/key_rotation.error_message")
	KeyRotationFromKeyId          = entity.Id("dev.miren.core/key_rotation.from_key")
	KeyRotationRewrappedId        = entity.Id("dev.miren.core/key_rotation.rewrapped")
	KeyRotationStatusId           = entity.Id("dev.miren.core/key_rotation.status")
	KeyRotationStatusRewrappingId = entity.Id("dev.miren.core/status.rewrapping")
	KeyRotationStatusRetiringId   = entity.Id("dev.miren.core/status.retiring")
	KeyRotationStatusDoneId       = entity.Id("dev.miren.core/status.done")
	KeyRotationStatusFailedId     = entity.Id("dev.miren.core/status.failed")
	KeyRotationToKeyId            = entity.Id("dev.miren.core/key_rotation.to_key")
)

type KeyRotation struct {
	ID           entity.Id         `json:"id"`
	ErrorMessage string            `cbor:"error_message,omitempty" json:"error_message,omitempty"`
	FromKey      string            `cbor:"from_key,omitempty" json:"from_key,omitempty"`
	Rewrapped    int64             `cbor:"rewrapped,omitempty" json:"rewrapped,omitempty"`
	Status       KeyRotationStatus `cbor:"status,omitempty" json:"status,omitempty"`
	ToKey        string            `cbor:"to_key,omitempty" json:"to_key,omitempty"`
}

type KeyRotationStatus string

const (
	REWRAPPING KeyRotationStatus = "status.rewrapping"
	RETIRING   KeyRotationStatus = "status.retiring"
	DONE       KeyRotationStatus = "status.done"
	FAILED     KeyRotationStatus = "status.failed"
)

var key_rotationstatusFromId = map[entity.Id]KeyRotationStatus{KeyRotationStatusRewrappingId: REWRAPPING, KeyRotationStatusRetiringId: RETIRING, KeyRotationStatusDoneId: DONE, KeyRotationStatusFailedId: FAILED}
var key_rotationstatusToId = map[KeyRotationStatus]entity.Id{REWRAPPING: KeyRotationStatusRewrappingId, RETIRING: KeyRotationStatusRetiringId, DONE: KeyRotationStatusDoneId, FAILED: KeyRotationStatusFailedId}

func (o *KeyRotation) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(KeyRotationErrorMessageId); ok && a.Value.Kind() == entity.KindString {
		o.ErrorMessage = a.Value.String()
	}
	if a, ok := e.Get(KeyRotationFromKeyId); ok && a.Value.Kind() == entity.KindString {
		o.FromKey = a.Value.String()
	}
	if a, ok := e.Get(KeyRotationRewrappedId); ok && a.Value.Kind() == entity.KindInt64 {
		o.Rewrapped = a.Value.Int64()
	}
	if a, ok := e.Get(KeyRotationStatusId); ok && a.Value.Kind() == entity.KindId {
		o.Status = key_rotationstatusFromId[a.Value.Id()]
	}
	if a, ok := e.Get(KeyRotationToKeyId); ok && a.Value.Kind() == entity.KindString {
		o.ToKey = a.Value.String()
	}
}

func (o *KeyRotation) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindKeyRotation)
}

func (o *KeyRotation) ShortKind() string {
	return "key_rotation"
}

func (o *KeyRotation) Kind() entity.Id {
	return KindKeyRotation
}

func (o *KeyRotation) EntityId() entity.Id {
	return o.ID
}

func (o *KeyRotation) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.ErrorMessage) {
		attrs = append(attrs, entity.String(KeyRotationErrorMessageId, o.ErrorMessage))
	}
	if !entity.Empty(o.FromKey) {
		attrs = append(attrs, entity.String(KeyRotationFromKeyId, o.FromKey))
	}
	if !entity.Empty(o.Rewrapped) {
		attrs = append(attrs, entity.Int64(KeyRotationRewrappedId, o.Rewrapped))
	}
	if a, ok := key_rotationstatusToId[o.Status]; ok {
		attrs = append(attrs, entity.Ref(KeyRotationStatusId, a))
	}
	if !entity.Empty(o.ToKey) {
		attrs = append(attrs, entity.String(KeyRotationToKeyId, o.ToKey))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindKeyRotation))
	return
}

func (o *KeyRotation) Empty() bool {
	if !entity.Empty(o.ErrorMessage) {
		return false
	}
	if !entity.Empty(o.FromKey) {
		return false
	}
	if !entity.Empty(o.Rewrapped) {
		return false
	}
	if o.Status != "" {
		return false
	}
	if !entity.Empty(o.ToKey) {
		return false
	}
	return true
}

func (o *KeyRotation) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("error_message", "dev.miren.core/key_rotation.error_message", schema.Doc("Why the rotation stopped, when status is failed"))
	sb.String("from_key", "dev.miren.core/key_rotation.from_key", schema.Doc("The key being retired. Versions still wrapped by it are what the backfill moves."), schema.Indexed)
	sb.Int64("rewrapped", "dev.miren.core/key_rotation.rewrapped", schema.Doc("Versions moved so far, for progress reporting only — the backfill's real state is the kek_id query"))
	sb.Singleton("dev.miren.core/status.rewrapping")
	sb.Singleton("dev.miren.core/status.retiring")
	sb.Singleton("dev.miren.core/status.done")
	sb.Singleton("dev.miren.core/status.failed")
	sb.Ref("status", "dev.miren.core/key_rotation.status", schema.Doc("rewrapping | retiring | done | failed"), schema.Indexed, schema.Choices(KeyRotationStatusRewrappingId, KeyRotationStatusRetiringId, KeyRotationStatusDoneId, KeyRotationStatusFailedId))
	sb.String("to_key", "dev.miren.core/key_rotation.to_key", schema.Doc("The key being rotated to, already persisted and current before this record exists"))
}

const (
	MetadataLabelsId  = entity.Id("dev.miren.core/metadata.labels")
	MetadataNameId    = entity.Id("dev.miren.core/metadata.name")
	MetadataProjectId = entity.Id("dev.miren.core/metadata.project")
)

type Metadata struct {
	ID      entity.Id    `json:"id"`
	Labels  types.Labels `cbor:"labels,omitempty" json:"labels,omitempty"`
	Name    string       `cbor:"name,omitempty" json:"name,omitempty"`
	Project entity.Id    `cbor:"project,omitempty" json:"project,omitempty"`
}

func (o *Metadata) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	for _, a := range e.GetAll(MetadataLabelsId) {
		if a.Value.Kind() == entity.KindLabel {
			o.Labels = append(o.Labels, a.Value.Label())
		}
	}
	if a, ok := e.Get(MetadataNameId); ok && a.Value.Kind() == entity.KindString {
		o.Name = a.Value.String()
	}
	if a, ok := e.Get(MetadataProjectId); ok && a.Value.Kind() == entity.KindId {
		o.Project = a.Value.Id()
	}
}

func (o *Metadata) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindMetadata)
}

func (o *Metadata) ShortKind() string {
	return "metadata"
}

func (o *Metadata) Kind() entity.Id {
	return KindMetadata
}

func (o *Metadata) EntityId() entity.Id {
	return o.ID
}

func (o *Metadata) Encode() (attrs []entity.Attr) {
	for _, v := range o.Labels {
		attrs = append(attrs, entity.Label(MetadataLabelsId, v.Key, v.Value))
	}
	if !entity.Empty(o.Name) {
		attrs = append(attrs, entity.String(MetadataNameId, o.Name))
	}
	if !entity.Empty(o.Project) {
		attrs = append(attrs, entity.Ref(MetadataProjectId, o.Project))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindMetadata))
	return
}

func (o *Metadata) Empty() bool {
	if len(o.Labels) != 0 {
		return false
	}
	if !entity.Empty(o.Name) {
		return false
	}
	if !entity.Empty(o.Project) {
		return false
	}
	return true
}

func (o *Metadata) InitSchema(sb *schema.SchemaBuilder) {
	sb.Label("labels", "dev.miren.core/metadata.labels", schema.Doc("Identifying labels for the entity"), schema.Many)
	sb.String("name", "dev.miren.core/metadata.name", schema.Doc("The name of the entity"))
	sb.Ref("project", "dev.miren.core/metadata.project", schema.Doc("A reference to the project the entity belongs to"))
}

const (
	OidcBindingAppId             = entity.Id("dev.miren.core/oidc_binding.app")
	OidcBindingClaimConditionsId = entity.Id("dev.miren.core/oidc_binding.claim_conditions")
	OidcBindingDescriptionId     = entity.Id("dev.miren.core/oidc_binding.description")
	OidcBindingIssuerId          = entity.Id("dev.miren.core/oidc_binding.issuer")
	OidcBindingProviderId        = entity.Id("dev.miren.core/oidc_binding.provider")
	OidcBindingSubjectPatternId  = entity.Id("dev.miren.core/oidc_binding.subject_pattern")
)

type OidcBinding struct {
	ID              entity.Id         `json:"id"`
	App             entity.Id         `cbor:"app,omitempty" json:"app,omitempty"`
	ClaimConditions []ClaimConditions `cbor:"claim_conditions,omitempty" json:"claim_conditions,omitempty"`
	Description     string            `cbor:"description,omitempty" json:"description,omitempty"`
	Issuer          string            `cbor:"issuer,omitempty" json:"issuer,omitempty"`
	Provider        string            `cbor:"provider,omitempty" json:"provider,omitempty"`
	SubjectPattern  string            `cbor:"subject_pattern,omitempty" json:"subject_pattern,omitempty"`
}

func (o *OidcBinding) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(OidcBindingAppId); ok && a.Value.Kind() == entity.KindId {
		o.App = a.Value.Id()
	}
	for _, a := range e.GetAll(OidcBindingClaimConditionsId) {
		if a.Value.Kind() == entity.KindComponent {
			var v ClaimConditions
			v.Decode(a.Value.Component())
			o.ClaimConditions = append(o.ClaimConditions, v)
		}
	}
	if a, ok := e.Get(OidcBindingDescriptionId); ok && a.Value.Kind() == entity.KindString {
		o.Description = a.Value.String()
	}
	if a, ok := e.Get(OidcBindingIssuerId); ok && a.Value.Kind() == entity.KindString {
		o.Issuer = a.Value.String()
	}
	if a, ok := e.Get(OidcBindingProviderId); ok && a.Value.Kind() == entity.KindString {
		o.Provider = a.Value.String()
	}
	if a, ok := e.Get(OidcBindingSubjectPatternId); ok && a.Value.Kind() == entity.KindString {
		o.SubjectPattern = a.Value.String()
	}
}

func (o *OidcBinding) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindOidcBinding)
}

func (o *OidcBinding) ShortKind() string {
	return "oidc_binding"
}

func (o *OidcBinding) Kind() entity.Id {
	return KindOidcBinding
}

func (o *OidcBinding) EntityId() entity.Id {
	return o.ID
}

func (o *OidcBinding) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.App) {
		attrs = append(attrs, entity.Ref(OidcBindingAppId, o.App))
	}
	for _, v := range o.ClaimConditions {
		attrs = append(attrs, entity.Component(OidcBindingClaimConditionsId, v.Encode()))
	}
	if !entity.Empty(o.Description) {
		attrs = append(attrs, entity.String(OidcBindingDescriptionId, o.Description))
	}
	if !entity.Empty(o.Issuer) {
		attrs = append(attrs, entity.String(OidcBindingIssuerId, o.Issuer))
	}
	if !entity.Empty(o.Provider) {
		attrs = append(attrs, entity.String(OidcBindingProviderId, o.Provider))
	}
	if !entity.Empty(o.SubjectPattern) {
		attrs = append(attrs, entity.String(OidcBindingSubjectPatternId, o.SubjectPattern))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindOidcBinding))
	return
}

func (o *OidcBinding) Empty() bool {
	if !entity.Empty(o.App) {
		return false
	}
	if len(o.ClaimConditions) != 0 {
		return false
	}
	if !entity.Empty(o.Description) {
		return false
	}
	if !entity.Empty(o.Issuer) {
		return false
	}
	if !entity.Empty(o.Provider) {
		return false
	}
	if !entity.Empty(o.SubjectPattern) {
		return false
	}
	return true
}

func (o *OidcBinding) InitSchema(sb *schema.SchemaBuilder) {
	sb.Ref("app", "dev.miren.core/oidc_binding.app", schema.Doc("The application this OIDC binding is for"), schema.Indexed, schema.Tags("dev.miren.app_ref"))
	sb.Component("claim_conditions", "dev.miren.core/oidc_binding.claim_conditions", schema.Doc("Additional claim conditions that must all match"), schema.Many)
	(&ClaimConditions{}).InitSchema(sb.Builder("oidc_binding.claim_conditions"))
	sb.String("description", "dev.miren.core/oidc_binding.description", schema.Doc("Human-readable description of this binding"))
	sb.String("issuer", "dev.miren.core/oidc_binding.issuer", schema.Doc("The OIDC issuer URL (e.g. https://token.actions.githubusercontent.com)"), schema.Indexed)
	sb.String("provider", "dev.miren.core/oidc_binding.provider", schema.Doc("The OIDC provider type (github, gitlab, generic)"))
	sb.String("subject_pattern", "dev.miren.core/oidc_binding.subject_pattern", schema.Doc("Glob pattern to match the token subject claim (e.g. repo:acme/web-app:*)"))
}

const (
	ClaimConditionsKeyId     = entity.Id("dev.miren.core/claim_conditions.key")
	ClaimConditionsPatternId = entity.Id("dev.miren.core/claim_conditions.pattern")
)

type ClaimConditions struct {
	Key     string `cbor:"key,omitempty" json:"key,omitempty"`
	Pattern string `cbor:"pattern,omitempty" json:"pattern,omitempty"`
}

func (o *ClaimConditions) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(ClaimConditionsKeyId); ok && a.Value.Kind() == entity.KindString {
		o.Key = a.Value.String()
	}
	if a, ok := e.Get(ClaimConditionsPatternId); ok && a.Value.Kind() == entity.KindString {
		o.Pattern = a.Value.String()
	}
}

func (o *ClaimConditions) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Key) {
		attrs = append(attrs, entity.String(ClaimConditionsKeyId, o.Key))
	}
	if !entity.Empty(o.Pattern) {
		attrs = append(attrs, entity.String(ClaimConditionsPatternId, o.Pattern))
	}
	return
}

func (o *ClaimConditions) Empty() bool {
	if !entity.Empty(o.Key) {
		return false
	}
	if !entity.Empty(o.Pattern) {
		return false
	}
	return true
}

func (o *ClaimConditions) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("key", "dev.miren.core/claim_conditions.key", schema.Doc("The claim name to match (e.g. event_name)"))
	sb.String("pattern", "dev.miren.core/claim_conditions.pattern", schema.Doc("Glob pattern for the claim value (e.g. push,workflow_dispatch)"))
}

const (
	ProjectOwnerId = entity.Id("dev.miren.core/project.owner")
)

type Project struct {
	ID    entity.Id `json:"id"`
	Owner string    `cbor:"owner,omitempty" json:"owner,omitempty"`
}

func (o *Project) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(ProjectOwnerId); ok && a.Value.Kind() == entity.KindString {
		o.Owner = a.Value.String()
	}
}

func (o *Project) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindProject)
}

func (o *Project) ShortKind() string {
	return "project"
}

func (o *Project) Kind() entity.Id {
	return KindProject
}

func (o *Project) EntityId() entity.Id {
	return o.ID
}

func (o *Project) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Owner) {
		attrs = append(attrs, entity.String(ProjectOwnerId, o.Owner))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindProject))
	return
}

func (o *Project) Empty() bool {
	return entity.Empty(o.Owner)
}

func (o *Project) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("owner", "dev.miren.core/project.owner", schema.Doc("The email address of the project owner"))
}

const (
	SecretCurrentVersionId = entity.Id("dev.miren.core/secret.current_version")
	SecretPathId           = entity.Id("dev.miren.core/secret.path")
)

type Secret struct {
	ID             entity.Id `json:"id"`
	CurrentVersion entity.Id `cbor:"current_version,omitempty" json:"current_version,omitempty"`
	Path           string    `cbor:"path,omitempty" json:"path,omitempty"`
}

func (o *Secret) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(SecretCurrentVersionId); ok && a.Value.Kind() == entity.KindId {
		o.CurrentVersion = a.Value.Id()
	}
	if a, ok := e.Get(SecretPathId); ok && a.Value.Kind() == entity.KindString {
		o.Path = a.Value.String()
	}
}

func (o *Secret) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindSecret)
}

func (o *Secret) ShortKind() string {
	return "secret"
}

func (o *Secret) Kind() entity.Id {
	return KindSecret
}

func (o *Secret) EntityId() entity.Id {
	return o.ID
}

func (o *Secret) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.CurrentVersion) {
		attrs = append(attrs, entity.Ref(SecretCurrentVersionId, o.CurrentVersion))
	}
	if !entity.Empty(o.Path) {
		attrs = append(attrs, entity.String(SecretPathId, o.Path))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindSecret))
	return
}

func (o *Secret) Empty() bool {
	if !entity.Empty(o.CurrentVersion) {
		return false
	}
	if !entity.Empty(o.Path) {
		return false
	}
	return true
}

func (o *Secret) InitSchema(sb *schema.SchemaBuilder) {
	sb.Ref("current_version", "dev.miren.core/secret.current_version", schema.Doc("The version a floating (unpinned) reference resolves to. Moves on every write."))
	sb.String("path", "dev.miren.core/secret.path", schema.Doc("Backend-relative path, e.g. \"payments/stripe-key\""), schema.Indexed)
}

const (
	SecretVersionCiphertextId     = entity.Id("dev.miren.core/secret_version.ciphertext")
	SecretVersionKekIdId          = entity.Id("dev.miren.core/secret_version.kek_id")
	SecretVersionSecretId         = entity.Id("dev.miren.core/secret_version.secret")
	SecretVersionStateId          = entity.Id("dev.miren.core/secret_version.state")
	SecretVersionStateEnabledId   = entity.Id("dev.miren.core/state.enabled")
	SecretVersionStateDisabledId  = entity.Id("dev.miren.core/state.disabled")
	SecretVersionStateDestroyedId = entity.Id("dev.miren.core/state.destroyed")
	SecretVersionValueMacId       = entity.Id("dev.miren.core/secret_version.value_mac")
	SecretVersionWrappedDekId     = entity.Id("dev.miren.core/secret_version.wrapped_dek")
)

type SecretVersion struct {
	ID         entity.Id          `json:"id"`
	Ciphertext []byte             `cbor:"ciphertext,omitempty" json:"ciphertext,omitempty"`
	KekId      string             `cbor:"kek_id,omitempty" json:"kek_id,omitempty"`
	Secret     entity.Id          `cbor:"secret,omitempty" json:"secret,omitempty"`
	State      SecretVersionState `cbor:"state,omitempty" json:"state,omitempty"`
	ValueMac   string             `cbor:"value_mac,omitempty" json:"value_mac,omitempty"`
	WrappedDek []byte             `cbor:"wrapped_dek,omitempty" json:"wrapped_dek,omitempty"`
}

type SecretVersionState string

const (
	ENABLED   SecretVersionState = "state.enabled"
	DISABLED  SecretVersionState = "state.disabled"
	DESTROYED SecretVersionState = "state.destroyed"
)

var secret_versionstateFromId = map[entity.Id]SecretVersionState{SecretVersionStateEnabledId: ENABLED, SecretVersionStateDisabledId: DISABLED, SecretVersionStateDestroyedId: DESTROYED}
var secret_versionstateToId = map[SecretVersionState]entity.Id{ENABLED: SecretVersionStateEnabledId, DISABLED: SecretVersionStateDisabledId, DESTROYED: SecretVersionStateDestroyedId}

func (o *SecretVersion) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(SecretVersionCiphertextId); ok && a.Value.Kind() == entity.KindBytes {
		o.Ciphertext = a.Value.Bytes()
	}
	if a, ok := e.Get(SecretVersionKekIdId); ok && a.Value.Kind() == entity.KindString {
		o.KekId = a.Value.String()
	}
	if a, ok := e.Get(SecretVersionSecretId); ok && a.Value.Kind() == entity.KindId {
		o.Secret = a.Value.Id()
	}
	if a, ok := e.Get(SecretVersionStateId); ok && a.Value.Kind() == entity.KindId {
		o.State = secret_versionstateFromId[a.Value.Id()]
	}
	if a, ok := e.Get(SecretVersionValueMacId); ok && a.Value.Kind() == entity.KindString {
		o.ValueMac = a.Value.String()
	}
	if a, ok := e.Get(SecretVersionWrappedDekId); ok && a.Value.Kind() == entity.KindBytes {
		o.WrappedDek = a.Value.Bytes()
	}
}

func (o *SecretVersion) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindSecretVersion)
}

func (o *SecretVersion) ShortKind() string {
	return "secret_version"
}

func (o *SecretVersion) Kind() entity.Id {
	return KindSecretVersion
}

func (o *SecretVersion) EntityId() entity.Id {
	return o.ID
}

func (o *SecretVersion) Encode() (attrs []entity.Attr) {
	if len(o.Ciphertext) > 0 {
		attrs = append(attrs, entity.Bytes(SecretVersionCiphertextId, o.Ciphertext))
	}
	if !entity.Empty(o.KekId) {
		attrs = append(attrs, entity.String(SecretVersionKekIdId, o.KekId))
	}
	if !entity.Empty(o.Secret) {
		attrs = append(attrs, entity.Ref(SecretVersionSecretId, o.Secret))
	}
	if a, ok := secret_versionstateToId[o.State]; ok {
		attrs = append(attrs, entity.Ref(SecretVersionStateId, a))
	}
	if !entity.Empty(o.ValueMac) {
		attrs = append(attrs, entity.String(SecretVersionValueMacId, o.ValueMac))
	}
	if len(o.WrappedDek) > 0 {
		attrs = append(attrs, entity.Bytes(SecretVersionWrappedDekId, o.WrappedDek))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindSecretVersion))
	return
}

func (o *SecretVersion) Empty() bool {
	if len(o.Ciphertext) != 0 {
		return false
	}
	if !entity.Empty(o.KekId) {
		return false
	}
	if !entity.Empty(o.Secret) {
		return false
	}
	if o.State != "" {
		return false
	}
	if !entity.Empty(o.ValueMac) {
		return false
	}
	if len(o.WrappedDek) != 0 {
		return false
	}
	return true
}

func (o *SecretVersion) InitSchema(sb *schema.SchemaBuilder) {
	sb.Bytes("ciphertext", "dev.miren.core/secret_version.ciphertext", schema.Doc("The value, encrypted with this version's data key (DEK)"))
	sb.String("kek_id", "dev.miren.core/secret_version.kek_id", schema.Doc("Which of the cluster's KEKs wrapped this DEK. A resolve unwraps with it; a key rotation re-wraps rows still pointing at a retiring key."), schema.Indexed)
	sb.Ref("secret", "dev.miren.core/secret_version.secret", schema.Doc("The secret this version belongs to"), schema.Indexed)
	sb.Singleton("dev.miren.core/state.enabled")
	sb.Singleton("dev.miren.core/state.disabled")
	sb.Singleton("dev.miren.core/state.destroyed")
	sb.Ref("state", "dev.miren.core/secret_version.state", schema.Doc("Whether this version may still be resolved"), schema.Indexed, schema.Choices(SecretVersionStateEnabledId, SecretVersionStateDisabledId, SecretVersionStateDestroyedId))
	sb.String("value_mac", "dev.miren.core/secret_version.value_mac", schema.Doc("Keyed hash (HMAC) of the plaintext under the cluster key, so a write can recognize an identical existing version without decrypting. Keyed rather than a bare digest so storing it never hands an attacker a rainbow-table oracle for the plaintext."), schema.Indexed)
	sb.Bytes("wrapped_dek", "dev.miren.core/secret_version.wrapped_dek", schema.Doc("The DEK, encrypted (wrapped) by one of the cluster's KEKs — see kek_id"))
}

var (
	KindApp            = entity.Id("dev.miren.core/kind.app")
	KindAppVersion     = entity.Id("dev.miren.core/kind.app_version")
	KindArtifact       = entity.Id("dev.miren.core/kind.artifact")
	KindConfigVersion  = entity.Id("dev.miren.core/kind.config_version")
	KindDeployment     = entity.Id("dev.miren.core/kind.deployment")
	KindDeploymentLock = entity.Id("dev.miren.core/kind.deployment_lock")
	KindKeyRotation    = entity.Id("dev.miren.core/kind.key_rotation")
	KindMetadata       = entity.Id("dev.miren.core/kind.metadata")
	KindOidcBinding    = entity.Id("dev.miren.core/kind.oidc_binding")
	KindProject        = entity.Id("dev.miren.core/kind.project")
	KindSecret         = entity.Id("dev.miren.core/kind.secret")
	KindSecretVersion  = entity.Id("dev.miren.core/kind.secret_version")
	Schema             = entity.Id("dev.miren.core/schema.v1alpha")
)

func init() {
	schema.Register("dev.miren.core", "v1alpha", func(sb *schema.SchemaBuilder) {
		(&ConfigSpec{}).InitSchema(sb)
		(&App{}).InitSchema(sb)
		(&AppVersion{}).InitSchema(sb)
		(&Artifact{}).InitSchema(sb)
		(&ConfigVersion{}).InitSchema(sb)
		(&Deployment{}).InitSchema(sb)
		(&DeploymentLock{}).InitSchema(sb)
		(&KeyRotation{}).InitSchema(sb)
		(&Metadata{}).InitSchema(sb)
		(&OidcBinding{}).InitSchema(sb)
		(&Project{}).InitSchema(sb)
		(&Secret{}).InitSchema(sb)
		(&SecretVersion{}).InitSchema(sb)
	})
	schema.RegisterEncodedSchema("dev.miren.core", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\xb4\\ɲ\xec8\xd1~\x8d\xff\a\x9a\xa1\x99\x1bp\xf7\xa5\x99\x9a\xa1\x03\xd8\x10\xc1\x86Gp\xa8\xec,[\xa7l\xcb-\xa9\xea\x9eÎ\x19\x02\x9e\x82{.\xab~=X\x13\xd6d9-˒방Д\x9f\xa6\xccT*S\xe5\xe7z =\f5܊\x9er\x18\x8a\x8aq\x80\v\x1dj\xf1\xe9\xebe\xe9\xfbSiA\xc6\xf1_\x8a\x86\xa3Z2\x8e\x9a\xee?\xe7\x9a\xf5\x84\x0e\b\xf4|\xa6\xd0\xd5\xe2\x8foN\xb4~\xfcҚ\xb8 \x95\xa47(o\xc0\x05e\x83\x1e\x17*\x93O#\x9ch\xbd\tA\a*)\xe9ʊ\rg\xdah\bT\xe6C|&\x001r\xf6\x00\x95T\xb4\x8d\xcd\xf8D_\f\x10\xbdf\xfc\xd21R\x97\x9cu\xa0H\xfbe\xd1\x04p\x16\x92ӡi\xccd\x9a\xdb+ҍ-\xe9FN{\u009f\xcai\xf1*2\x8e\xaba\xa9u\x17Pq\x90z鯨\x81\xaeKY\xfd߫)|5H_TW\xcea\x90\x8b\x1d`\xb80\xb6~\x06g$\xb2UĵJ\xa5\xce\xfd\xac\xc9\x1f?\x1b\x9a\xbe\xd9\t=\xff\x1bja*S\x16\xe0wj\xe0\x9f\v\x03\x14\xec\xf5\x00\\u\x01:\x99:v\xcb)+d-3\\\xd23\xb1\xa3\xc7bekӥ\a\xaf\x90E\x98\x84Su1\xb1Q\x9ci-EO\x06z\x06\xa1\xf9\xbdu9oފ\xfe\xeb{\xf4eM\x1b\v\xc3p\xa1\x87\xf6<\xa1}~\vMH\"\xafB\x81\x9cMz\xa2\xada\xb8\xf6\x97駼\x91\xee\n\xe2\x9fg\xad\x18V˭\x89\x8c*i\t\xafZz\x83u\x87\xb6\x99\xa9\x8fnmkG\x17\xde\xdb\x1e$\xa9\x89$ή\xb5){\xfb\x87\xe0\xdaX\x84\xa2#'\xe8Dݓ\xe1\xe9\xdfz\x85LɴB\xa0\xd2A\xdev\x00J\"\xe7\x1f\xbc\xc5_آ\x8bi\xc4\xf8\xcaY\x88դ\xd4\xca\xd50v쩇\xc1\xc8\xc5\xe3\xff\xa3Vs\x83\x94\xe5\xfb\xbb\x9aŻ\x9b\x18\x93p\x94n\xfa\xad\xcb\xe1u\xc0\xca\x11!\xf8\xca\xf1\xe2\x17`\x9c\xafl㜮\xb4\xabˎ5\x9a\xd5\x1f\xbc|\x06J\xd5]\x85\x04^\xd2Z\xa3xy\x8c\xf2\xb5\b\n\xeb\xc7\x0e$\xd4%\xd1[\xdc-J\xb0\xe8FVG'\xa1.OOzu\xfc\x82\t\x87N\xc8l\x80A\xce)\xb3\xf5\b\xb6\bæk\xc8\xf0\xb2)\x90B\xd2\x1e\x84$\xbdV\x95tΦq\x82\x06\xb9\n\xe0%\xf4\x84vz\xf1\xbd<\x86\t\xb3\xa4\ac6\xb0\xb1\x994\x1e\xf0\x00\x1cW\xd39\x9bzr=X\xb4\xd3SP\xd3{;\x01\x9c3^\xf6 \x04i\x8c\x95\xb3,\xc2\xcc\x12\x11Ɔʒ\x0eg\xa6\x85\xd1\xe5v\xd8\xe4\xddm6\xb1\x10)<\xf2\xb77!Mk\x11\nr\x95-\xd3f\xc0٤\xf1\x96lҞ8\x19*m\xfd\x9cM\x1a\xd3~k\x8b\xb6b}Oe\xa9\xbb\xf4\x98K\x84*0\xea7vP\x97\\?\xaeJ1\x1e\xb6\x18\x1c\x1e\x15eM\xb9\xd42\u07ba\x9c:\xa7O\x8cu\xc1\xc3\xc4Q\xfb\xdc\xd3\x04\xf8&(1\x8e\x9a\xc3\xc8\x04\x95\x8c\xeb\xde\x1f\xbc<\xc6\xc06\x92\xc3\x10-\xd16Ҕ\xc0T\xdfܢ\x9a\xccy:4\xa5\xe4\x00eK\x84\xde\xe2O\xd6\xc5\xc9\x16cC\xe5\x84\x1c\\.\x8f\xafǖ\b\xbd\\\xa0\x93x\xc8\xc56\xad`W^A9\x97XU#\x835{,\xe0#\x87\r\xb5\f\x853\xc1\xac\xe6mo\x98\xf6P5b\x1f\xb8\xa7\xd9\x16)\xe2\xfe\x8f\xe0\x19\xe8\x81\x14\xa4\xee\xe9PJv\x01{\xb0{\x05{\xb2\xbf\x00\xda2\xc0\xbf\x1c#2\x06\xa61Llΐ?o\\v\x1d\xb9w\xd9={\x97܈\x1aEh\xc5\x1a-eY\xff\xfc6\xb4\x1a\x9a^i\x1d2Ծ\xbdں\xb2\x9dί;<\a\x9f~\xe7Ŭf\x11,\x94\xd6H6\xb3g\x1c;j\x01\xfcF+\xa3\xcfl&U\x14܊\x04\xc5\xcdL\x15\x06ɟFF\a\xcd\x1f\x0f^\x1e\x8f\x12ˉA\x18\x19\x97\xe6>>\xa5&\xaa\x8a\x0e2\xb6}f&\x8b\xedse\xf7o\x9f\x85J:\xac\xd58\xdfY\xb9\x1a4BQSq\xf1\x87\t\xba`g\x8c\x1f\xa4\x8fQ\xf7\x902ҿ\x86u\xf9D^\x9ci\a\xe2IH\xe8\xf5.z\xf9]{Q\x01t@\x04\xa8\xf3\x9a]\xf5n\xf6ˢ=\x96\xd50=\xbb\x0e\xb2t\x1e\x9a\a/\x8f\x01V\xd71\x05\xb0s\x8b\xc4,\xa8\x89b~\x95\xe7\xd0\xf6j\xb2\x91\xb3\x1b\xad\re\xebra\x97\x00t\xac\"\xdd\n\xc9R\x15\xaa\x1aT\xcdv#U\x16\xd4\xf2zD\x1cH]\xb2\xa1\xd3\xf6\a\x9d\xb3K\xf3\a\xdf\xc15\xb1\xa0\xbf\x85\xb29\x19]a2V\x1a\xa3\x8aB3\xf5ې]\xe3\xd8\x14\x86\x9b'\x06Ք\xdd\x11\x82\"C\b`\xb8\xa5\x88\xc0_\x82L\x00í8\x91j:L\xf5\xe4mf\xefh\x9d\bk\x10\x15\xa7\xa3t\x97n\xbf\x00\x01`\xc7\xe0D\x7f\x01\xbdYՔس\x12'\x02\x0e\x9f\\)\a=\xd4\xd6\xe5\xe2;<\x11\n\x18\x04\x95\xf4f\xeebsvI\x8a\xe5J\x91*{\xcc\xd8S:\x8d\x87\xfa\x7f\x012\xc5\xfaZ\xaet2\xd9\xcf\f\xc3\xedMT\xaf\xd2ޚ頓x<+כ\xa5\xdc\xd1\x10\x9bt\x1b\xc7T\xd0ٿ \xf2\xee\xbfs\x16w\x1bGP;4#\xa8\xac\x87\x10?\x82&\x92\xc5\x11\xa4\v^\xf0\bR\x80)\xf2\xf7\xa7 \x87)\xf2\xbd}\xc1\xb2g\x88X\r\xa5\xdb\x19:g\x17\xdb\x13\xeepcC\x83\n\xdfPp&Y\xc5:\xa7\xf0u.\xac\xf0+Y\x8d+\x01\xb64\x85\xac\xc6\xeaZG\x1a\\\xeb12v\xc7\x105慸\xa2V\xd4ϡk\xbe\xdbL\x93(+6\xe8\xe0J\xa5\x15\x94\bU\xec0\xd1\xc7\x19L\x14\x80Og)\xec\x16\n\x80\x15=\xab͚\xa9\x14f\xb0\x0f\x12 \xa6\xed\xa5\x83\x90d\x98\fMe\xe5,\x8b\x16l\xf7\xa3\x04\xc4I\x7f\x83\x90\xa2\x1c\x81;\x1c\x1dE\vW-z\xf80\xa1\a1\xd9\x15e\xcd^\x0fe\r\x1dћ9\xaeJ\xf1r$A\xb7W\xa9 |\xbbo\\\x95\xa6r'7}x]\xc4\xef'\x96w\x82\xeeP\xcb_\x92pY֔C\xe5\x1c3\f\x17b]\xbaq\xed\xb8\x11Nɩ\x03\xff\xda\xe1\xca\xee\xbfvX\xa8tK\x06\x1b\xd1\x16!͜\xc1\x97~G\x9dcӬ4\x98C\x89Z6\xf86騒\xcc\x1b|Z:\xeaD\x1b\a\xef\xefL\xbfo\xe8\xe0\xa3\xc1\xd1\x1e\xb6v\x1c\x0f\xc5\xe3ϚK\x82~\xc053-\xdf)\xa02\xdf\xf3\xf3~\x04\n\xc6\x16z\xe0\xa4+\xe1q\xa4\x1c\x84\r\xc2\xc8`\x8d>\x8ah\x0f\n\xf8\xbd$`\x15\x18\xd4B\x89\v\xf7\\\xc8a@);\xe3\xfb_\x14\xed\x05-|0eP\x96W\xae\x81\xe8\x9c\xdd\x13 \x1f$1r\x1d[\xa5\xdc\xe0u\xf0z\xee\x03\xfa<\xd1\x04\x02\x83Q\xfe\xf3\xa3\x89+\xf1U\x9e\xd1\v<\x95\x9cI\"g\xd7\xe8걄\xd7$\xfdt\xc7\xec\xee\xa3d\x85}\x82[\xbf@;s֗Vs\xb5.\xb7\x17{[`px\xcd\xc98\x1a\x1dF\xe7\xec\xc2\xc4\xc4\x1b\xb5\x80H}i\xf0\\\xd7lX{\xa0\xcd\x03\x82\xa9\xee|&\xb4\x83\xc0\xbdF7ѵ-\aI\xa7\xc9m=E\xb0\xf5\x0ff2SS\xcc\x03\xae\xa9m\x11\xe4\xc7\xc54%sk}6\xe9Tv\xec|\xa00?2ZW\xe5\x89\x0e5\x1d\x9a\r~\xf4\x9b$9\x95\x83Ǯ\x8f\x12\xf4\xb1+\xa3\xe2\xdb1\xaa\xaa#\xb4\x9f\x8c\xab\x9aN\x13\xf2\xefk\xe3\xaan\xc7\xd4@\x1d\x15ю\xd2]\xd4X\xdba\xa4\xf8\x81\x8f-\xf4\x15\xf5H\xa4\x04n\x94\x93ͤr\x03Sp3Z\xb0\xcb\xc5:d\xd98\x98\x89\x17HT\x88\xabq\x02\x9eMzO\xe5,\xe8#n\xc4X\\v\x81!\xae\xa7\a\xa8\x94\xa7\xd4-\"Åɢ\xe5C\xaf\x05x\x12\xad\xa5Aa\xf8\x0e[F\xcbF錆\xad\xbb%NP\xc0\x82\x1a\x15щ\x11*}\xffS\xa9\x1d!\xc2[\xe6\xea\xed\xdc'\x90\xf4#\f\x9bYA\xb8\xc4`\x8a\xd2&\xdfI\x02\xbc'P\x82z(\xe2=\xa4\xbf\x81\xfa~\xd6\xc8w\x03`j\xeb?\xca\xc5\\\xfa6.\x19>\x8d\x8f\xb2\x96\xa58\xe4\xce\xf8\xf8\xf0t\xf6\xbc\x1c\xbf:\x8e\x9c\xe7\xfc\xf8\xcd\xf1\x8e\xee\xf3\x89\xfc\xfax\xc7\a]%\xf7\xf4\xf8\xb2\x1e\x94\xc7wt\x8fS\x87\xb6?\xaf\xbb\xb7!\xc7\xce\xceh\x8f\x050?\xcc\x13\x92\xcc\x18f\xa6xd\x878\x7fq\x04?;\x02zh\x16\x19\x01R\xec~L\xc2\xdf\xf1\xc2g\xea\xd9\xc4\xf0\xeaO\x8f\xa0\x1e\x89\xbe\xfe\xf2\x9e\x8e\x16!\xda\xfb\x90\xe68\xeeώ\xe0$\x86y\x7fr\x04\xfbx\x14\xf8\xf5Z\xf7\xccq\xe1Wycɏ\x16\xbf\xca\xd38Y\x01\xe3L\xaeO\x8e'gn~n\xb89\xd7\xd2\xda\rG\xff8\x1f/ɝ\x9bɨ\x19\xc1\xecL\x1d\x98\x18\xeb\xfea>\xeaa\xe7\xf0u-U68\x9ey\x92\xef\x86̿\x9b\x87\xb7sTd\xa2\xc5\xc2\xeb\x99\xeb\x9d\x1ct\xcf\xe4g\x1d{\xf7\x0e\xf8nQr'\x97$\a\xfa3\xb7\xfdX\xf8?Ӏ\xcb|\x01\x90)\x96I\x0f\x042\x95i\xce\xfb\x81CÍ=/\xc84x\x8e\xbe>\xf8\xf9=ݸ'\n\xf7\xa1\xd8w\f\x87\xd6\xf0\xe03\x87\x80%\xa2\xf0\xe2\xd1\xe75\x91\x1a\xf8\xf7\xd2\x06~$̌\x83?ahI\xd0\rL\x17dƛ#\xd8\xe9\x17\xaf4\x9d\xae@w\xfd6oC\xff\x0e\x88\x01\xde\xff\x82p\a=\xdd L;8\x1cp\x9eՖ\xb3Ȼ&[\x9a\t8\x831N\x1b\xaa\ay6\xe9c\xe7\xda\f\x99d\x02\xa6\xa9\x87\x194\xd1\xfe˝~\x82\xf1\x97\xa6\x0ff\xc8Ö\x9f\xa7\x90\xb4\x98\x1a\xb3/\xed6\xa2\aГ\xc7\xf9Ŏ>\x8e\x06T\xb68\xe9\xd2\xdc\xd4\x1a\xfbE\xec?\r\xc5Arj\x9c\x8a\x8d\xcdD_:\xc5\xd0D\xd5B}5_:h]\xee\x9e\x01\xfaV_\xb3e\xf0e\x01r\xda4\xc6y\xd1\xd8Lz\xb4k\xc9\x19\x19\xaa\xd4>5\xf1\x0f\x14:\x17\xbe\x88Bup\xe9\n\xf5\ay\x83O\xbb^\xa7i\xe9\x19\xf4\x7f\xa0\xa5g\xf0\xa8\x96NS\xa93\xd8\v\xaa\xd4\x19\xf4EU\xaa\a\xfbR*u\x86<\xacR\xc5,8\x0e-\xfe\xe8\xc5\x1b@\xb4!zb\x15\x8e\x9a\xea/\x88\xecDM\x97\x8d\xd2\x1f%\xe0\xe7QK\x9c\xa2\xa2c\v\\£\t-z\xf9i\xf5\xe0\xf4$\x8d\xc1\x8b#\x9f\b\xe8\x02\x17\xfb?ͳI\uf17b\x11\x82\xce\x1a\x8e\xd0i?\x8c\x8b\xdf\x19`jI\xa4\xd9|\x9d\f^\x80\xde40\xa8g\x99\xc1\x87/P\x98ڶ\xa6B7[m\x83jf\xabi\rBr\xf6\xb4\xf1\xb1\x0e\xf5dR\xd7o<G^LA\r\xb1\xecIeկ\xcd\xe2\x85\xc4\x0f\x9f\x10\x8eyWT\xd6p\xd1\n\xcb/\x98w5θK\xcc\xd5\xea\xa3oa\x94\x1d\xab.\x86sW\xffAZ\xb6J\xff\x1c\x02\xbe\a!\xa0\x82TZ\xc1\xd9\xe7\x87\x17\xbf`\xf9\xeap\xfb\xfb\x00\x06*\xed+\x1b\xf8\xad\x0e\x86Y\xffg\xb9\x8f\xffY\x19\xef$\x06D\x0f,\x1fB\xcf*\xe36\x01Bč/\xa2e\\j-\xf8iE\xc6q\xeb\xf3Y\xf6;G\x91\xcf<\xb9\xcf\tž\x85\xb4\xf3a\x1a[;\x7f\x85%\xfa\xfd\x1a\xffo\xd9;\x9fkY<T\xdc\xfb\vw\xc232\xbf\xc5\xf2i\xcc\ueaf3\xa4Sa\xd9\x06\xcbc\xc2A\x82\xb7>I\x86\xff\v\x00\x00\xff\xff\x01\x00\x00\xff\xff\xf1@\x04\x88ZM\x00\x00"))
}
