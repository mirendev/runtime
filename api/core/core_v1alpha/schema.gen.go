package core_v1alpha

import (
	"time"

	entity "miren.dev/runtime/pkg/entity"
	export "miren.dev/runtime/pkg/entity/export"
	schema "miren.dev/runtime/pkg/entity/schema"
	types "miren.dev/runtime/pkg/entity/types"
)

type DiskProvider string

const (
	DiskProviderMiren  DiskProvider = "miren"
	DiskProviderLocal  DiskProvider = "local"
	DiskProviderSqlite DiskProvider = "sqlite"
)
const (
	DiskProviderMirenMemberId  = entity.Id("dev.miren.core/provider.miren")
	DiskProviderLocalMemberId  = entity.Id("dev.miren.core/provider.local")
	DiskProviderSqliteMemberId = entity.Id("dev.miren.core/provider.sqlite")
)

type PortProtocol string

const (
	PortProtocolTcp PortProtocol = "tcp"
	PortProtocolUdp PortProtocol = "udp"
)
const (
	PortProtocolTcpMemberId = entity.Id("dev.miren.core/protocol.tcp")
	PortProtocolUdpMemberId = entity.Id("dev.miren.core/protocol.udp")
)

func initEnumMembers(sb *schema.SchemaBuilder) {
	sb.Singleton("dev.miren.core/provider.miren")
	sb.Singleton("dev.miren.core/provider.local")
	sb.Singleton("dev.miren.core/provider.sqlite")
	sb.Singleton("dev.miren.core/protocol.tcp")
	sb.Singleton("dev.miren.core/protocol.udp")
}

const (
	ConfigSpecCloneAddonVariantsId = entity.Id("dev.miren.core/component.config_spec.clone_addon_variants")
	ConfigSpecCloneAddonsId        = entity.Id("dev.miren.core/component.config_spec.clone_addons")
	ConfigSpecEntrypointId         = entity.Id("dev.miren.core/component.config_spec.entrypoint")
	ConfigSpecServicesId           = entity.Id("dev.miren.core/component.config_spec.services")
	ConfigSpecStartDirectoryId     = entity.Id("dev.miren.core/component.config_spec.start_directory")
	ConfigSpecStaticDirId          = entity.Id("dev.miren.core/component.config_spec.static_dir")
	ConfigSpecStaticErrorPageId    = entity.Id("dev.miren.core/component.config_spec.static_error_page")
	ConfigSpecTasksId              = entity.Id("dev.miren.core/component.config_spec.tasks")
	ConfigSpecVariablesId          = entity.Id("dev.miren.core/component.config_spec.variables")
)

type ConfigSpec struct {
	CloneAddonVariants []ConfigSpecCloneAddonVariants `cbor:"clone_addon_variants,omitempty" json:"clone_addon_variants,omitempty"`
	CloneAddons        []string                       `cbor:"clone_addons,omitempty" json:"clone_addons,omitempty"`
	Entrypoint         string                         `cbor:"entrypoint,omitempty" json:"entrypoint,omitempty"`
	Services           []ConfigSpecServices           `cbor:"services,omitempty" json:"services,omitempty"`
	StartDirectory     string                         `cbor:"start_directory,omitempty" json:"start_directory,omitempty"`
	StaticDir          string                         `cbor:"static_dir,omitempty" json:"static_dir,omitempty"`
	StaticErrorPage    string                         `cbor:"static_error_page,omitempty" json:"static_error_page,omitempty"`
	Tasks              []ConfigSpecTasks              `cbor:"tasks,omitempty" json:"tasks,omitempty"`
	Variables          []ConfigSpecVariables          `cbor:"variables,omitempty" json:"variables,omitempty"`
}

func (o *ConfigSpec) Decode(e entity.AttrGetter) {
	for _, a := range e.GetAll(ConfigSpecCloneAddonVariantsId) {
		if a.Value.Kind() == entity.KindComponent {
			var v ConfigSpecCloneAddonVariants
			v.Decode(a.Value.Component())
			o.CloneAddonVariants = append(o.CloneAddonVariants, v)
		}
	}
	for _, a := range e.GetAll(ConfigSpecCloneAddonsId) {
		if a.Value.Kind() == entity.KindString {
			o.CloneAddons = append(o.CloneAddons, a.Value.String())
		}
	}
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
	if a, ok := e.Get(ConfigSpecStaticDirId); ok && a.Value.Kind() == entity.KindString {
		o.StaticDir = a.Value.String()
	}
	if a, ok := e.Get(ConfigSpecStaticErrorPageId); ok && a.Value.Kind() == entity.KindString {
		o.StaticErrorPage = a.Value.String()
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
	for _, v := range o.CloneAddonVariants {
		attrs = append(attrs, entity.Component(ConfigSpecCloneAddonVariantsId, v.Encode()))
	}
	for _, v := range o.CloneAddons {
		attrs = append(attrs, entity.String(ConfigSpecCloneAddonsId, v))
	}
	if !entity.Empty(o.Entrypoint) {
		attrs = append(attrs, entity.String(ConfigSpecEntrypointId, o.Entrypoint))
	}
	for _, v := range o.Services {
		attrs = append(attrs, entity.Component(ConfigSpecServicesId, v.Encode()))
	}
	if !entity.Empty(o.StartDirectory) {
		attrs = append(attrs, entity.String(ConfigSpecStartDirectoryId, o.StartDirectory))
	}
	if !entity.Empty(o.StaticDir) {
		attrs = append(attrs, entity.String(ConfigSpecStaticDirId, o.StaticDir))
	}
	if !entity.Empty(o.StaticErrorPage) {
		attrs = append(attrs, entity.String(ConfigSpecStaticErrorPageId, o.StaticErrorPage))
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
	if len(o.CloneAddonVariants) != 0 {
		return false
	}
	if len(o.CloneAddons) != 0 {
		return false
	}
	if !entity.Empty(o.Entrypoint) {
		return false
	}
	if len(o.Services) != 0 {
		return false
	}
	if !entity.Empty(o.StartDirectory) {
		return false
	}
	if !entity.Empty(o.StaticDir) {
		return false
	}
	if !entity.Empty(o.StaticErrorPage) {
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
	sb.Component("clone_addon_variants", "dev.miren.core/component.config_spec.clone_addon_variants", schema.Doc("Variant overrides for opted-in ephemeral addon clones."), schema.Many)
	(&ConfigSpecCloneAddonVariants{}).InitSchema(sb.Builder("component.config_spec.clone_addon_variants"))
	sb.String("clone_addons", "dev.miren.core/component.config_spec.clone_addons", schema.Doc("Names of addons ephemeral versions clone instead of sharing primary associations."), schema.Many)
	sb.String("entrypoint", "dev.miren.core/component.config_spec.entrypoint", schema.Doc("The container entrypoint command"))
	sb.Component("services", "dev.miren.core/component.config_spec.services", schema.Doc("Per-service configuration"), schema.Many)
	(&ConfigSpecServices{}).InitSchema(sb.Builder("component.config_spec.services"))
	sb.String("start_directory", "dev.miren.core/component.config_spec.start_directory", schema.Doc("Directory to start the process in; defaults to /app."))
	sb.String("static_dir", "dev.miren.core/component.config_spec.static_dir", schema.Doc("Absolute path exported from the application build output and served directly by HTTP ingress"))
	sb.String("static_error_page", "dev.miren.core/component.config_spec.static_error_page", schema.Doc("Relative path to the HTML error template in the static artifact"))
	sb.Component("tasks", "dev.miren.core/component.config_spec.tasks", schema.Doc("Per-task configuration. A task is a command the platform knows how to run, as opposed to a service, which is a process it keeps up. Tasks carry no ports, concurrency, image, or disks: a task always runs in the app's image, and both per-task image and per-task disks are deliberate v1 cuts tracked in RFD-97."), schema.Many)
	(&ConfigSpecTasks{}).InitSchema(sb.Builder("component.config_spec.tasks"))
	sb.Component("variables", "dev.miren.core/component.config_spec.variables", schema.Doc("Environment variables and configuration values"), schema.Many)
	(&ConfigSpecVariables{}).InitSchema(sb.Builder("component.config_spec.variables"))
}

const (
	ConfigSpecCloneAddonVariantsNameId    = entity.Id("dev.miren.core/component.config_spec.clone_addon_variants.name")
	ConfigSpecCloneAddonVariantsVariantId = entity.Id("dev.miren.core/component.config_spec.clone_addon_variants.variant")
)

type ConfigSpecCloneAddonVariants struct {
	Name    string `cbor:"name,omitempty" json:"name,omitempty"`
	Variant string `cbor:"variant,omitempty" json:"variant,omitempty"`
}

func (o *ConfigSpecCloneAddonVariants) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(ConfigSpecCloneAddonVariantsNameId); ok && a.Value.Kind() == entity.KindString {
		o.Name = a.Value.String()
	}
	if a, ok := e.Get(ConfigSpecCloneAddonVariantsVariantId); ok && a.Value.Kind() == entity.KindString {
		o.Variant = a.Value.String()
	}
}

func (o *ConfigSpecCloneAddonVariants) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Name) {
		attrs = append(attrs, entity.String(ConfigSpecCloneAddonVariantsNameId, o.Name))
	}
	if !entity.Empty(o.Variant) {
		attrs = append(attrs, entity.String(ConfigSpecCloneAddonVariantsVariantId, o.Variant))
	}
	return
}

func (o *ConfigSpecCloneAddonVariants) Empty() bool {
	if !entity.Empty(o.Name) {
		return false
	}
	if !entity.Empty(o.Variant) {
		return false
	}
	return true
}

func (o *ConfigSpecCloneAddonVariants) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("name", "dev.miren.core/component.config_spec.clone_addon_variants.name", schema.Doc("Addon name"))
	sb.String("variant", "dev.miren.core/component.config_spec.clone_addon_variants.variant", schema.Doc("Variant used by the clone instead of the primary association"))
}

const (
	ConfigSpecServicesArgsId        = entity.Id("dev.miren.core/component.config_spec.services.args")
	ConfigSpecServicesCommandId     = entity.Id("dev.miren.core/component.config_spec.services.command")
	ConfigSpecServicesConcurrencyId = entity.Id("dev.miren.core/component.config_spec.services.concurrency")
	ConfigSpecServicesDisksId       = entity.Id("dev.miren.core/component.config_spec.services.disks")
	ConfigSpecServicesEnvId         = entity.Id("dev.miren.core/component.config_spec.services.env")
	ConfigSpecServicesImageId       = entity.Id("dev.miren.core/component.config_spec.services.image")
	ConfigSpecServicesMetricsId     = entity.Id("dev.miren.core/component.config_spec.services.metrics")
	ConfigSpecServicesNameId        = entity.Id("dev.miren.core/component.config_spec.services.name")
	ConfigSpecServicesPortId        = entity.Id("dev.miren.core/component.config_spec.services.port")
	ConfigSpecServicesPortNameId    = entity.Id("dev.miren.core/component.config_spec.services.port_name")
	ConfigSpecServicesPortTimeoutId = entity.Id("dev.miren.core/component.config_spec.services.port_timeout")
	ConfigSpecServicesPortTypeId    = entity.Id("dev.miren.core/component.config_spec.services.port_type")
	ConfigSpecServicesPortsId       = entity.Id("dev.miren.core/component.config_spec.services.ports")
)

type ConfigSpecServices struct {
	Args        []string                      `cbor:"args,omitempty" json:"args,omitempty"`
	Command     string                        `cbor:"command,omitempty" json:"command,omitempty"`
	Concurrency ConfigSpecServicesConcurrency `cbor:"concurrency,omitempty" json:"concurrency"`
	Disks       []ConfigSpecServicesDisks     `cbor:"disks,omitempty" json:"disks,omitempty"`
	Env         []ConfigSpecServicesEnv       `cbor:"env,omitempty" json:"env,omitempty"`
	Image       string                        `cbor:"image,omitempty" json:"image,omitempty"`
	Metrics     ConfigSpecServicesMetrics     `cbor:"metrics,omitempty" json:"metrics"`
	Name        string                        `cbor:"name,omitempty" json:"name,omitempty"`
	Port        int64                         `cbor:"port,omitempty" json:"port,omitempty"`
	PortName    string                        `cbor:"port_name,omitempty" json:"port_name,omitempty"`
	PortTimeout string                        `cbor:"port_timeout,omitempty" json:"port_timeout,omitempty"`
	PortType    string                        `cbor:"port_type,omitempty" json:"port_type,omitempty"`
	Ports       []ConfigSpecServicesPorts     `cbor:"ports,omitempty" json:"ports,omitempty"`
}

func (o *ConfigSpecServices) Decode(e entity.AttrGetter) {
	for _, a := range e.GetAll(ConfigSpecServicesArgsId) {
		if a.Value.Kind() == entity.KindString {
			o.Args = append(o.Args, a.Value.String())
		}
	}
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
	if a, ok := e.Get(ConfigSpecServicesMetricsId); ok && a.Value.Kind() == entity.KindComponent {
		o.Metrics.Decode(a.Value.Component())
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
	for _, v := range o.Args {
		attrs = append(attrs, entity.String(ConfigSpecServicesArgsId, v))
	}
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
	if !o.Metrics.Empty() {
		attrs = append(attrs, entity.Component(ConfigSpecServicesMetricsId, o.Metrics.Encode()))
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
	if len(o.Args) != 0 {
		return false
	}
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
	if !o.Metrics.Empty() {
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
	sb.String("args", "dev.miren.core/component.config_spec.services.args", schema.Doc("Arguments that replace the image CMD while preserving its ENTRYPOINT"), schema.Many)
	sb.String("command", "dev.miren.core/component.config_spec.services.command", schema.Doc("The command to run for the service"))
	sb.Component("concurrency", "dev.miren.core/component.config_spec.services.concurrency", schema.Doc("Concurrency configuration for this service"))
	(&ConfigSpecServicesConcurrency{}).InitSchema(sb.Builder("component.config_spec.services.concurrency"))
	sb.Component("disks", "dev.miren.core/component.config_spec.services.disks", schema.Doc("Disk attachments for this service"), schema.Many)
	(&ConfigSpecServicesDisks{}).InitSchema(sb.Builder("component.config_spec.services.disks"))
	sb.Component("env", "dev.miren.core/component.config_spec.services.env", schema.Doc("Environment variables for this service only"), schema.Many)
	(&ConfigSpecServicesEnv{}).InitSchema(sb.Builder("component.config_spec.services.env"))
	sb.String("image", "dev.miren.core/component.config_spec.services.image", schema.Doc("Optional container image for this service"))
	sb.Component("metrics", "dev.miren.core/component.config_spec.services.metrics", schema.Doc("Prometheus scrape endpoint for this service"))
	(&ConfigSpecServicesMetrics{}).InitSchema(sb.Builder("component.config_spec.services.metrics"))
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
	ConfigSpecServicesDisksDbFileId         = entity.Id("dev.miren.core/component.config_spec.services.disks.db_file")
	ConfigSpecServicesDisksFilesystemId     = entity.Id("dev.miren.core/component.config_spec.services.disks.filesystem")
	ConfigSpecServicesDisksLeaseTimeoutId   = entity.Id("dev.miren.core/component.config_spec.services.disks.lease_timeout")
	ConfigSpecServicesDisksMountPathId      = entity.Id("dev.miren.core/component.config_spec.services.disks.mount_path")
	ConfigSpecServicesDisksNameId           = entity.Id("dev.miren.core/component.config_spec.services.disks.name")
	ConfigSpecServicesDisksOwnerId          = entity.Id("dev.miren.core/component.config_spec.services.disks.owner")
	ConfigSpecServicesDisksProviderId       = entity.Id("dev.miren.core/component.config_spec.services.disks.provider")
	ConfigSpecServicesDisksProviderMirenId  = entity.Id("dev.miren.core/component.config_spec.services.disks.provider.miren")
	ConfigSpecServicesDisksProviderLocalId  = entity.Id("dev.miren.core/component.config_spec.services.disks.provider.local")
	ConfigSpecServicesDisksProviderSqliteId = entity.Id("dev.miren.core/component.config_spec.services.disks.provider.sqlite")
	ConfigSpecServicesDisksReadOnlyId       = entity.Id("dev.miren.core/component.config_spec.services.disks.read_only")
	ConfigSpecServicesDisksSizeGbId         = entity.Id("dev.miren.core/component.config_spec.services.disks.size_gb")
	ConfigSpecServicesDisksSourceId         = entity.Id("dev.miren.core/component.config_spec.services.disks.source")
	ConfigSpecServicesDisksSqliteIdId       = entity.Id("dev.miren.core/component.config_spec.services.disks.sqlite_id")
)

type ConfigSpecServicesDisks struct {
	DbFile       string       `cbor:"db_file,omitempty" json:"db_file,omitempty"`
	Filesystem   string       `cbor:"filesystem,omitempty" json:"filesystem,omitempty"`
	LeaseTimeout string       `cbor:"lease_timeout,omitempty" json:"lease_timeout,omitempty"`
	MountPath    string       `cbor:"mount_path,omitempty" json:"mount_path,omitempty"`
	Name         string       `cbor:"name,omitempty" json:"name,omitempty"`
	Owner        string       `cbor:"owner,omitempty" json:"owner,omitempty"`
	Provider     DiskProvider `cbor:"provider,omitempty" json:"provider,omitempty"`
	ReadOnly     bool         `cbor:"read_only,omitempty" json:"read_only,omitempty"`
	SizeGb       int64        `cbor:"size_gb,omitempty" json:"size_gb,omitempty"`
	Source       string       `cbor:"source,omitempty" json:"source,omitempty"`
	SqliteId     string       `cbor:"sqlite_id,omitempty" json:"sqlite_id,omitempty"`
}

type ConfigSpecServicesDisksProvider = DiskProvider

const (
	ConfigSpecServicesDisksMIREN  DiskProvider = DiskProviderMiren
	ConfigSpecServicesDisksLOCAL  DiskProvider = DiskProviderLocal
	ConfigSpecServicesDisksSQLITE DiskProvider = DiskProviderSqlite
)

var ConfigSpecServicesDisksProviderFromId = map[entity.Id]DiskProvider{DiskProviderMirenMemberId: DiskProviderMiren, ConfigSpecServicesDisksProviderMirenId: DiskProviderMiren, DiskProviderLocalMemberId: DiskProviderLocal, ConfigSpecServicesDisksProviderLocalId: DiskProviderLocal, DiskProviderSqliteMemberId: DiskProviderSqlite, ConfigSpecServicesDisksProviderSqliteId: DiskProviderSqlite}
var ConfigSpecServicesDisksProviderToId = map[DiskProvider]entity.Id{DiskProviderMiren: DiskProviderMirenMemberId, DiskProvider("component.config_spec.services.disks.provider.miren"): DiskProviderMirenMemberId, DiskProviderLocal: DiskProviderLocalMemberId, DiskProvider("component.config_spec.services.disks.provider.local"): DiskProviderLocalMemberId, DiskProviderSqlite: DiskProviderSqliteMemberId, DiskProvider("component.config_spec.services.disks.provider.sqlite"): DiskProviderSqliteMemberId}

func (o *ConfigSpecServicesDisks) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(ConfigSpecServicesDisksDbFileId); ok && a.Value.Kind() == entity.KindString {
		o.DbFile = a.Value.String()
	}
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
		o.Provider = ConfigSpecServicesDisksProviderFromId[a.Value.Id()]
	}
	if a, ok := e.Get(ConfigSpecServicesDisksReadOnlyId); ok && a.Value.Kind() == entity.KindBool {
		o.ReadOnly = a.Value.Bool()
	}
	if a, ok := e.Get(ConfigSpecServicesDisksSizeGbId); ok && a.Value.Kind() == entity.KindInt64 {
		o.SizeGb = a.Value.Int64()
	}
	if a, ok := e.Get(ConfigSpecServicesDisksSourceId); ok && a.Value.Kind() == entity.KindString {
		o.Source = a.Value.String()
	}
	if a, ok := e.Get(ConfigSpecServicesDisksSqliteIdId); ok && a.Value.Kind() == entity.KindString {
		o.SqliteId = a.Value.String()
	}
}

func (o *ConfigSpecServicesDisks) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.DbFile) {
		attrs = append(attrs, entity.String(ConfigSpecServicesDisksDbFileId, o.DbFile))
	}
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
	if a, ok := ConfigSpecServicesDisksProviderToId[o.Provider]; ok {
		attrs = append(attrs, entity.Ref(ConfigSpecServicesDisksProviderId, a))
	}
	attrs = append(attrs, entity.Bool(ConfigSpecServicesDisksReadOnlyId, o.ReadOnly))
	if !entity.Empty(o.SizeGb) {
		attrs = append(attrs, entity.Int64(ConfigSpecServicesDisksSizeGbId, o.SizeGb))
	}
	if !entity.Empty(o.Source) {
		attrs = append(attrs, entity.String(ConfigSpecServicesDisksSourceId, o.Source))
	}
	if !entity.Empty(o.SqliteId) {
		attrs = append(attrs, entity.String(ConfigSpecServicesDisksSqliteIdId, o.SqliteId))
	}
	return
}

func (o *ConfigSpecServicesDisks) Empty() bool {
	if !entity.Empty(o.DbFile) {
		return false
	}
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
	if !entity.Empty(o.Source) {
		return false
	}
	if !entity.Empty(o.SqliteId) {
		return false
	}
	return true
}

func (o *ConfigSpecServicesDisks) InitSchema(sb *schema.SchemaBuilder) {
	initEnumMembers(sb)
	sb.String("db_file", "dev.miren.core/component.config_spec.services.disks.db_file", schema.Doc("Database filename inside the disk directory, for sqlite disks only; a bare filename, not a path (defaults to data.db)"))
	sb.String("filesystem", "dev.miren.core/component.config_spec.services.disks.filesystem", schema.Doc("Filesystem type (ext4, xfs, btrfs) for auto-creating the disk"))
	sb.String("lease_timeout", "dev.miren.core/component.config_spec.services.disks.lease_timeout", schema.Doc("Timeout for acquiring the disk lease"))
	sb.String("mount_path", "dev.miren.core/component.config_spec.services.disks.mount_path", schema.Doc("The path inside the container where the disk will be mounted"))
	sb.String("name", "dev.miren.core/component.config_spec.services.disks.name", schema.Doc("The name of the disk"))
	sb.String("owner", "dev.miren.core/component.config_spec.services.disks.owner", schema.Doc("Ownership policy for the mounted disk. Empty (default) makes the disk writable by the container's run user; \"keep\" leaves the raw mount ownership untouched; \"uid\" or \"uid:gid\" pins a specific numeric owner."))
	sb.Enum("provider", "dev.miren.core/component.config_spec.services.disks.provider", []entity.Id{DiskProviderMirenMemberId, DiskProviderLocalMemberId, DiskProviderSqliteMemberId}, schema.Doc("Disk provider: 'miren' (default) for network disks, 'local' for node-local persistent storage, 'sqlite' for a node-local SQLite database replicated to the coordinator"))
	sb.Bool("read_only", "dev.miren.core/component.config_spec.services.disks.read_only", schema.Doc("Whether to mount the disk as read-only"))
	sb.Int64("size_gb", "dev.miren.core/component.config_spec.services.disks.size_gb", schema.Doc("Size in GB for auto-creating the disk if it doesn't exist"))
	sb.String("source", "dev.miren.core/component.config_spec.services.disks.source", schema.Doc("Where this disk came from. Empty or \"config\" means the user declared it; \"addon\" means an addon contributed it and owns its removal."))
	sb.String("sqlite_id", "dev.miren.core/component.config_spec.services.disks.sqlite_id", schema.Doc("Identity of the database a sqlite disk attaches to, scoped to the app (defaults to \"default\")"))
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
	ConfigSpecServicesMetricsEnabledId  = entity.Id("dev.miren.core/component.config_spec.services.metrics.enabled")
	ConfigSpecServicesMetricsIntervalId = entity.Id("dev.miren.core/component.config_spec.services.metrics.interval")
	ConfigSpecServicesMetricsPathId     = entity.Id("dev.miren.core/component.config_spec.services.metrics.path")
	ConfigSpecServicesMetricsPortId     = entity.Id("dev.miren.core/component.config_spec.services.metrics.port")
	ConfigSpecServicesMetricsPublicId   = entity.Id("dev.miren.core/component.config_spec.services.metrics.public")
)

type ConfigSpecServicesMetrics struct {
	Enabled  bool   `cbor:"enabled,omitempty" json:"enabled,omitempty"`
	Interval string `cbor:"interval,omitempty" json:"interval,omitempty"`
	Path     string `cbor:"path,omitempty" json:"path,omitempty"`
	Port     int64  `cbor:"port,omitempty" json:"port,omitempty"`
	Public   bool   `cbor:"public,omitempty" json:"public,omitempty"`
}

func (o *ConfigSpecServicesMetrics) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(ConfigSpecServicesMetricsEnabledId); ok && a.Value.Kind() == entity.KindBool {
		o.Enabled = a.Value.Bool()
	}
	if a, ok := e.Get(ConfigSpecServicesMetricsIntervalId); ok && a.Value.Kind() == entity.KindString {
		o.Interval = a.Value.String()
	}
	if a, ok := e.Get(ConfigSpecServicesMetricsPathId); ok && a.Value.Kind() == entity.KindString {
		o.Path = a.Value.String()
	}
	if a, ok := e.Get(ConfigSpecServicesMetricsPortId); ok && a.Value.Kind() == entity.KindInt64 {
		o.Port = a.Value.Int64()
	}
	if a, ok := e.Get(ConfigSpecServicesMetricsPublicId); ok && a.Value.Kind() == entity.KindBool {
		o.Public = a.Value.Bool()
	}
}

func (o *ConfigSpecServicesMetrics) Encode() (attrs []entity.Attr) {
	attrs = append(attrs, entity.Bool(ConfigSpecServicesMetricsEnabledId, o.Enabled))
	if !entity.Empty(o.Interval) {
		attrs = append(attrs, entity.String(ConfigSpecServicesMetricsIntervalId, o.Interval))
	}
	if !entity.Empty(o.Path) {
		attrs = append(attrs, entity.String(ConfigSpecServicesMetricsPathId, o.Path))
	}
	if !entity.Empty(o.Port) {
		attrs = append(attrs, entity.Int64(ConfigSpecServicesMetricsPortId, o.Port))
	}
	attrs = append(attrs, entity.Bool(ConfigSpecServicesMetricsPublicId, o.Public))
	return
}

func (o *ConfigSpecServicesMetrics) Empty() bool {
	if !entity.Empty(o.Enabled) {
		return false
	}
	if !entity.Empty(o.Interval) {
		return false
	}
	if !entity.Empty(o.Path) {
		return false
	}
	if !entity.Empty(o.Port) {
		return false
	}
	if !entity.Empty(o.Public) {
		return false
	}
	return true
}

func (o *ConfigSpecServicesMetrics) InitSchema(sb *schema.SchemaBuilder) {
	sb.Bool("enabled", "dev.miren.core/component.config_spec.services.metrics.enabled", schema.Doc("Whether the runtime should scrape this service"))
	sb.String("interval", "dev.miren.core/component.config_spec.services.metrics.interval", schema.Doc("Interval between scrapes"))
	sb.String("path", "dev.miren.core/component.config_spec.services.metrics.path", schema.Doc("HTTP path to scrape"))
	sb.Int64("port", "dev.miren.core/component.config_spec.services.metrics.port", schema.Doc("Declared TCP service port to scrape"))
	sb.Bool("public", "dev.miren.core/component.config_spec.services.metrics.public", schema.Doc("Whether public ingress may serve the metrics path"))
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
	Name     string       `cbor:"name" json:"name"`
	NodePort int64        `cbor:"node_port,omitempty" json:"node_port,omitempty"`
	Port     int64        `cbor:"port" json:"port"`
	Protocol PortProtocol `cbor:"protocol,omitempty" json:"protocol,omitempty"`
	Type     string       `cbor:"type,omitempty" json:"type,omitempty"`
}

type ConfigSpecServicesPortsProtocol = PortProtocol

const (
	ConfigSpecServicesPortsTCP PortProtocol = PortProtocolTcp
	ConfigSpecServicesPortsUDP PortProtocol = PortProtocolUdp
)

var ConfigSpecServicesPortsProtocolFromId = map[entity.Id]PortProtocol{PortProtocolTcpMemberId: PortProtocolTcp, ConfigSpecServicesPortsProtocolTcpId: PortProtocolTcp, PortProtocolUdpMemberId: PortProtocolUdp, ConfigSpecServicesPortsProtocolUdpId: PortProtocolUdp}
var ConfigSpecServicesPortsProtocolToId = map[PortProtocol]entity.Id{PortProtocolTcp: PortProtocolTcpMemberId, PortProtocol("component.config_spec.services.ports.protocol.tcp"): PortProtocolTcpMemberId, PortProtocolUdp: PortProtocolUdpMemberId, PortProtocol("component.config_spec.services.ports.protocol.udp"): PortProtocolUdpMemberId}

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
		o.Protocol = ConfigSpecServicesPortsProtocolFromId[a.Value.Id()]
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
	if a, ok := ConfigSpecServicesPortsProtocolToId[o.Protocol]; ok {
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
	initEnumMembers(sb)
	sb.String("name", "dev.miren.core/component.config_spec.services.ports.name", schema.Required)
	sb.Int64("node_port", "dev.miren.core/component.config_spec.services.ports.node_port")
	sb.Int64("port", "dev.miren.core/component.config_spec.services.ports.port", schema.Required)
	sb.Enum("protocol", "dev.miren.core/component.config_spec.services.ports.protocol", []entity.Id{PortProtocolTcpMemberId, PortProtocolUdpMemberId})
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
	AppActiveDeploymentId = entity.Id("dev.miren.core/app.active_deployment")
	AppActiveVersionId    = entity.Id("dev.miren.core/app.active_version")
	AppDeploymentLockId   = entity.Id("dev.miren.core/app.deployment_lock")
	AppInitialConfigId    = entity.Id("dev.miren.core/app.initial_config")
	AppProjectId          = entity.Id("dev.miren.core/app.project")
	AppWorkloadRoleId     = entity.Id("dev.miren.core/app.workload_role")
)

type App struct {
	ID               entity.Id      `json:"id"`
	ActiveDeployment entity.Id      `cbor:"active_deployment,omitempty" json:"active_deployment,omitempty"`
	ActiveVersion    entity.Id      `cbor:"active_version,omitempty" json:"active_version,omitempty"`
	DeploymentLock   DeploymentLock `cbor:"deployment_lock,omitempty" json:"deployment_lock"`
	InitialConfig    entity.Id      `cbor:"initial_config,omitempty" json:"initial_config,omitempty"`
	Project          entity.Id      `cbor:"project,omitempty" json:"project,omitempty"`
	WorkloadRole     string         `cbor:"workload_role,omitempty" json:"workload_role,omitempty"`
}

func (o *App) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(AppActiveDeploymentId); ok && a.Value.Kind() == entity.KindId {
		o.ActiveDeployment = a.Value.Id()
	}
	if a, ok := e.Get(AppActiveVersionId); ok && a.Value.Kind() == entity.KindId {
		o.ActiveVersion = a.Value.Id()
	}
	if a, ok := e.Get(AppDeploymentLockId); ok && a.Value.Kind() == entity.KindComponent {
		o.DeploymentLock.Decode(a.Value.Component())
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
	if !entity.Empty(o.ActiveDeployment) {
		attrs = append(attrs, entity.Ref(AppActiveDeploymentId, o.ActiveDeployment))
	}
	if !entity.Empty(o.ActiveVersion) {
		attrs = append(attrs, entity.Ref(AppActiveVersionId, o.ActiveVersion))
	}
	if !o.DeploymentLock.Empty() {
		attrs = append(attrs, entity.Component(AppDeploymentLockId, o.DeploymentLock.Encode()))
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
	attrs = append(attrs, entity.Bool(entity.Id("dev.miren.meta/cloud.export"), true))
	return
}

func (o *App) Empty() bool {
	if !entity.Empty(o.ActiveDeployment) {
		return false
	}
	if !entity.Empty(o.ActiveVersion) {
		return false
	}
	if !o.DeploymentLock.Empty() {
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
	sb.Ref("active_deployment", "dev.miren.core/app.active_deployment", schema.Doc("The deployment attempt that made active_version current"))
	sb.Ref("active_version", "dev.miren.core/app.active_version", schema.Doc("The version of the project that should be used"))
	sb.Component("deployment_lock", "dev.miren.core/app.deployment_lock", schema.Doc("The expiring lock held by the deployment currently mutating this app"))
	(&DeploymentLock{}).InitSchema(sb.Builder("app.deployment_lock"))
	sb.Ref("initial_config", "dev.miren.core/app.initial_config", schema.Doc("Reference to the initial ConfigVersion entity created before the first deploy"))
	sb.Ref("project", "dev.miren.core/app.project", schema.Doc("The project that the app belongs to"))
	sb.String("workload_role", "dev.miren.core/app.workload_role", schema.Doc("The authorization role that identity tokens minted for this app's sandboxes authenticate as (see pkg/workloadroles). Empty means the default (app-readonly). Cluster-scoped roles may only be set by an operator, never via app.toml."))
}

const (
	DeploymentLockAcquiredAtId   = entity.Id("dev.miren.core/deployment_lock.acquired_at")
	DeploymentLockDeploymentIdId = entity.Id("dev.miren.core/deployment_lock.deployment_id")
	DeploymentLockExpiresAtId    = entity.Id("dev.miren.core/deployment_lock.expires_at")
)

type DeploymentLock struct {
	AcquiredAt   time.Time `cbor:"acquired_at,omitempty" json:"acquired_at"`
	DeploymentId string    `cbor:"deployment_id,omitempty" json:"deployment_id,omitempty"`
	ExpiresAt    time.Time `cbor:"expires_at,omitempty" json:"expires_at"`
}

func (o *DeploymentLock) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(DeploymentLockAcquiredAtId); ok && a.Value.Kind() == entity.KindTime {
		o.AcquiredAt = a.Value.Time()
	}
	if a, ok := e.Get(DeploymentLockDeploymentIdId); ok && a.Value.Kind() == entity.KindString {
		o.DeploymentId = a.Value.String()
	}
	if a, ok := e.Get(DeploymentLockExpiresAtId); ok && a.Value.Kind() == entity.KindTime {
		o.ExpiresAt = a.Value.Time()
	}
}

func (o *DeploymentLock) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.AcquiredAt) {
		attrs = append(attrs, entity.Time(DeploymentLockAcquiredAtId, o.AcquiredAt))
	}
	if !entity.Empty(o.DeploymentId) {
		attrs = append(attrs, entity.String(DeploymentLockDeploymentIdId, o.DeploymentId))
	}
	if !entity.Empty(o.ExpiresAt) {
		attrs = append(attrs, entity.Time(DeploymentLockExpiresAtId, o.ExpiresAt))
	}
	return
}

func (o *DeploymentLock) Empty() bool {
	if !entity.Empty(o.AcquiredAt) {
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
	sb.String("deployment_id", "dev.miren.core/deployment_lock.deployment_id", schema.Doc("The deployment attempt that owns the lock"))
	sb.Time("expires_at", "dev.miren.core/deployment_lock.expires_at", schema.Doc("When another deployment may take over the lock"))
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
	AppVersionSourceId             = entity.Id("dev.miren.core/app_version.source")
	AppVersionStaticArtifactId     = entity.Id("dev.miren.core/app_version.static_artifact")
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
	Source             Source    `cbor:"source,omitempty" json:"source"`
	StaticArtifact     string    `cbor:"static_artifact,omitempty" json:"static_artifact,omitempty"`
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
	if a, ok := e.Get(AppVersionSourceId); ok && a.Value.Kind() == entity.KindComponent {
		o.Source.Decode(a.Value.Component())
	}
	if a, ok := e.Get(AppVersionStaticArtifactId); ok && a.Value.Kind() == entity.KindString {
		o.StaticArtifact = a.Value.String()
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
	if !o.Source.Empty() {
		attrs = append(attrs, entity.Component(AppVersionSourceId, o.Source.Encode()))
	}
	if !entity.Empty(o.StaticArtifact) {
		attrs = append(attrs, entity.String(AppVersionStaticArtifactId, o.StaticArtifact))
	}
	if !entity.Empty(o.Version) {
		attrs = append(attrs, entity.String(AppVersionVersionId, o.Version))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindAppVersion))
	attrs = append(attrs, entity.Bool(entity.Id("dev.miren.meta/cloud.export"), true))
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
	if !o.Source.Empty() {
		return false
	}
	if !entity.Empty(o.StaticArtifact) {
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
	sb.Component("source", "dev.miren.core/app_version.source", schema.Doc("Sanitized source provenance captured when this version was built"))
	(&Source{}).InitSchema(sb.Builder("app_version.source"))
	sb.String("static_artifact", "dev.miren.core/app_version.static_artifact", schema.Doc("Content digest of the tar archive containing files exported from static.dir"))
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
	DisksDbFileId         = entity.Id("dev.miren.core/disks.db_file")
	DisksFilesystemId     = entity.Id("dev.miren.core/disks.filesystem")
	DisksLeaseTimeoutId   = entity.Id("dev.miren.core/disks.lease_timeout")
	DisksMountPathId      = entity.Id("dev.miren.core/disks.mount_path")
	DisksNameId           = entity.Id("dev.miren.core/disks.name")
	DisksOwnerId          = entity.Id("dev.miren.core/disks.owner")
	DisksProviderId       = entity.Id("dev.miren.core/disks.provider")
	DisksProviderMirenId  = DiskProviderMirenMemberId
	DisksProviderLocalId  = DiskProviderLocalMemberId
	DisksProviderSqliteId = DiskProviderSqliteMemberId
	DisksReadOnlyId       = entity.Id("dev.miren.core/disks.read_only")
	DisksSizeGbId         = entity.Id("dev.miren.core/disks.size_gb")
	DisksSourceId         = entity.Id("dev.miren.core/disks.source")
	DisksSqliteIdId       = entity.Id("dev.miren.core/disks.sqlite_id")
)

type Disks struct {
	DbFile       string       `cbor:"db_file,omitempty" json:"db_file,omitempty"`
	Filesystem   string       `cbor:"filesystem,omitempty" json:"filesystem,omitempty"`
	LeaseTimeout string       `cbor:"lease_timeout,omitempty" json:"lease_timeout,omitempty"`
	MountPath    string       `cbor:"mount_path,omitempty" json:"mount_path,omitempty"`
	Name         string       `cbor:"name,omitempty" json:"name,omitempty"`
	Owner        string       `cbor:"owner,omitempty" json:"owner,omitempty"`
	Provider     DiskProvider `cbor:"provider,omitempty" json:"provider,omitempty"`
	ReadOnly     bool         `cbor:"read_only,omitempty" json:"read_only,omitempty"`
	SizeGb       int64        `cbor:"size_gb,omitempty" json:"size_gb,omitempty"`
	Source       string       `cbor:"source,omitempty" json:"source,omitempty"`
	SqliteId     string       `cbor:"sqlite_id,omitempty" json:"sqlite_id,omitempty"`
}

type DisksProvider = DiskProvider

const (
	MIREN  DiskProvider = DiskProviderMiren
	LOCAL  DiskProvider = DiskProviderLocal
	SQLITE DiskProvider = DiskProviderSqlite
)

var DisksProviderFromId = map[entity.Id]DiskProvider{DiskProviderMirenMemberId: DiskProviderMiren, DiskProviderLocalMemberId: DiskProviderLocal, DiskProviderSqliteMemberId: DiskProviderSqlite}
var DisksProviderToId = map[DiskProvider]entity.Id{DiskProviderMiren: DiskProviderMirenMemberId, DiskProvider("provider.miren"): DiskProviderMirenMemberId, DiskProviderLocal: DiskProviderLocalMemberId, DiskProvider("provider.local"): DiskProviderLocalMemberId, DiskProviderSqlite: DiskProviderSqliteMemberId, DiskProvider("provider.sqlite"): DiskProviderSqliteMemberId}

func (o *Disks) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(DisksDbFileId); ok && a.Value.Kind() == entity.KindString {
		o.DbFile = a.Value.String()
	}
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
		o.Provider = DisksProviderFromId[a.Value.Id()]
	}
	if a, ok := e.Get(DisksReadOnlyId); ok && a.Value.Kind() == entity.KindBool {
		o.ReadOnly = a.Value.Bool()
	}
	if a, ok := e.Get(DisksSizeGbId); ok && a.Value.Kind() == entity.KindInt64 {
		o.SizeGb = a.Value.Int64()
	}
	if a, ok := e.Get(DisksSourceId); ok && a.Value.Kind() == entity.KindString {
		o.Source = a.Value.String()
	}
	if a, ok := e.Get(DisksSqliteIdId); ok && a.Value.Kind() == entity.KindString {
		o.SqliteId = a.Value.String()
	}
}

func (o *Disks) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.DbFile) {
		attrs = append(attrs, entity.String(DisksDbFileId, o.DbFile))
	}
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
	if a, ok := DisksProviderToId[o.Provider]; ok {
		attrs = append(attrs, entity.Ref(DisksProviderId, a))
	}
	attrs = append(attrs, entity.Bool(DisksReadOnlyId, o.ReadOnly))
	if !entity.Empty(o.SizeGb) {
		attrs = append(attrs, entity.Int64(DisksSizeGbId, o.SizeGb))
	}
	if !entity.Empty(o.Source) {
		attrs = append(attrs, entity.String(DisksSourceId, o.Source))
	}
	if !entity.Empty(o.SqliteId) {
		attrs = append(attrs, entity.String(DisksSqliteIdId, o.SqliteId))
	}
	return
}

func (o *Disks) Empty() bool {
	if !entity.Empty(o.DbFile) {
		return false
	}
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
	if !entity.Empty(o.Source) {
		return false
	}
	if !entity.Empty(o.SqliteId) {
		return false
	}
	return true
}

func (o *Disks) InitSchema(sb *schema.SchemaBuilder) {
	initEnumMembers(sb)
	sb.String("db_file", "dev.miren.core/disks.db_file", schema.Doc("Database filename inside the disk directory, for sqlite disks only; a bare filename, not a path (defaults to data.db)"))
	sb.String("filesystem", "dev.miren.core/disks.filesystem", schema.Doc("Filesystem type (ext4, xfs, btrfs) for auto-creating the disk"))
	sb.String("lease_timeout", "dev.miren.core/disks.lease_timeout", schema.Doc("Timeout for acquiring the disk lease (e.g. 5m, 10m)"))
	sb.String("mount_path", "dev.miren.core/disks.mount_path", schema.Doc("The path inside the container where the disk will be mounted"))
	sb.String("name", "dev.miren.core/disks.name", schema.Doc("The name of the disk"))
	sb.String("owner", "dev.miren.core/disks.owner", schema.Doc("Ownership policy for the mounted disk. Empty (default) makes the disk writable by the container's run user; \"keep\" leaves the raw mount ownership untouched; \"uid\" or \"uid:gid\" pins a specific numeric owner."))
	sb.Enum("provider", "dev.miren.core/disks.provider", []entity.Id{DiskProviderMirenMemberId, DiskProviderLocalMemberId, DiskProviderSqliteMemberId}, schema.Doc("Disk provider: 'miren' (default) for network disks, 'local' for node-local persistent storage, 'sqlite' for a node-local SQLite database replicated to the coordinator"))
	sb.Bool("read_only", "dev.miren.core/disks.read_only", schema.Doc("Whether to mount the disk as read-only"))
	sb.Int64("size_gb", "dev.miren.core/disks.size_gb", schema.Doc("Size in GB for auto-creating the disk if it doesn't exist"))
	sb.String("source", "dev.miren.core/disks.source", schema.Doc("Where this disk came from. Empty or \"config\" means the user declared it; \"addon\" means an addon contributed it and owns its removal."))
	sb.String("sqlite_id", "dev.miren.core/disks.sqlite_id", schema.Doc("Identity of the database a sqlite disk attaches to, scoped to the app (defaults to \"default\")"))
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
	PortsProtocolTcpId = PortProtocolTcpMemberId
	PortsProtocolUdpId = PortProtocolUdpMemberId
	PortsTypeId        = entity.Id("dev.miren.core/ports.type")
)

type Ports struct {
	Name     string       `cbor:"name" json:"name"`
	NodePort int64        `cbor:"node_port,omitempty" json:"node_port,omitempty"`
	Port     int64        `cbor:"port" json:"port"`
	Protocol PortProtocol `cbor:"protocol,omitempty" json:"protocol,omitempty"`
	Type     string       `cbor:"type,omitempty" json:"type,omitempty"`
}

type PortsProtocol = PortProtocol

const (
	TCP PortProtocol = PortProtocolTcp
	UDP PortProtocol = PortProtocolUdp
)

var PortsProtocolFromId = map[entity.Id]PortProtocol{PortProtocolTcpMemberId: PortProtocolTcp, PortProtocolUdpMemberId: PortProtocolUdp}
var PortsProtocolToId = map[PortProtocol]entity.Id{PortProtocolTcp: PortProtocolTcpMemberId, PortProtocol("protocol.tcp"): PortProtocolTcpMemberId, PortProtocolUdp: PortProtocolUdpMemberId, PortProtocol("protocol.udp"): PortProtocolUdpMemberId}

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
		o.Protocol = PortsProtocolFromId[a.Value.Id()]
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
	if a, ok := PortsProtocolToId[o.Protocol]; ok {
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
	initEnumMembers(sb)
	sb.String("name", "dev.miren.core/ports.name", schema.Required)
	sb.Int64("node_port", "dev.miren.core/ports.node_port")
	sb.Int64("port", "dev.miren.core/ports.port", schema.Required)
	sb.Enum("protocol", "dev.miren.core/ports.protocol", []entity.Id{PortProtocolTcpMemberId, PortProtocolUdpMemberId})
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
	SourceGitBranchId  = entity.Id("dev.miren.core/source.git_branch")
	SourceGitShaId     = entity.Id("dev.miren.core/source.git_sha")
	SourceKindId       = entity.Id("dev.miren.core/source.kind")
	SourceRepositoryId = entity.Id("dev.miren.core/source.repository")
	SourceValueId      = entity.Id("dev.miren.core/source.value")
)

type Source struct {
	GitBranch  string `cbor:"git_branch,omitempty" json:"git_branch,omitempty"`
	GitSha     string `cbor:"git_sha,omitempty" json:"git_sha,omitempty"`
	Kind       string `cbor:"kind,omitempty" json:"kind,omitempty"`
	Repository string `cbor:"repository,omitempty" json:"repository,omitempty"`
	Value      string `cbor:"value,omitempty" json:"value,omitempty"`
}

func (o *Source) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(SourceGitBranchId); ok && a.Value.Kind() == entity.KindString {
		o.GitBranch = a.Value.String()
	}
	if a, ok := e.Get(SourceGitShaId); ok && a.Value.Kind() == entity.KindString {
		o.GitSha = a.Value.String()
	}
	if a, ok := e.Get(SourceKindId); ok && a.Value.Kind() == entity.KindString {
		o.Kind = a.Value.String()
	}
	if a, ok := e.Get(SourceRepositoryId); ok && a.Value.Kind() == entity.KindString {
		o.Repository = a.Value.String()
	}
	if a, ok := e.Get(SourceValueId); ok && a.Value.Kind() == entity.KindString {
		o.Value = a.Value.String()
	}
}

func (o *Source) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.GitBranch) {
		attrs = append(attrs, entity.String(SourceGitBranchId, o.GitBranch))
	}
	if !entity.Empty(o.GitSha) {
		attrs = append(attrs, entity.String(SourceGitShaId, o.GitSha))
	}
	if !entity.Empty(o.Kind) {
		attrs = append(attrs, entity.String(SourceKindId, o.Kind))
	}
	if !entity.Empty(o.Repository) {
		attrs = append(attrs, entity.String(SourceRepositoryId, o.Repository))
	}
	if !entity.Empty(o.Value) {
		attrs = append(attrs, entity.String(SourceValueId, o.Value))
	}
	return
}

func (o *Source) Empty() bool {
	if !entity.Empty(o.GitBranch) {
		return false
	}
	if !entity.Empty(o.GitSha) {
		return false
	}
	if !entity.Empty(o.Kind) {
		return false
	}
	if !entity.Empty(o.Repository) {
		return false
	}
	if !entity.Empty(o.Value) {
		return false
	}
	return true
}

func (o *Source) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("git_branch", "dev.miren.core/source.git_branch", schema.Doc("Git branch used to build the version"))
	sb.String("git_sha", "dev.miren.core/source.git_sha", schema.Doc("Git commit SHA used to build the version"))
	sb.String("kind", "dev.miren.core/source.kind", schema.Doc("Build source kind (image, dockerfile, stack, or static)"))
	sb.String("repository", "dev.miren.core/source.repository", schema.Doc("Repository URL without credentials, query parameters, or fragments"))
	sb.String("value", "dev.miren.core/source.value", schema.Doc("Normalized image reference or auto-detected stack name, depending on kind"))
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
	CertificateRevocationReasonId    = entity.Id("dev.miren.core/certificate_revocation.reason")
	CertificateRevocationRevokedAtId = entity.Id("dev.miren.core/certificate_revocation.revoked_at")
	CertificateRevocationRevokedById = entity.Id("dev.miren.core/certificate_revocation.revoked_by")
	CertificateRevocationSerialId    = entity.Id("dev.miren.core/certificate_revocation.serial")
)

type CertificateRevocation struct {
	ID        entity.Id `json:"id"`
	Reason    string    `cbor:"reason,omitempty" json:"reason,omitempty"`
	RevokedAt time.Time `cbor:"revoked_at,omitempty" json:"revoked_at"`
	RevokedBy string    `cbor:"revoked_by,omitempty" json:"revoked_by,omitempty"`
	Serial    string    `cbor:"serial,omitempty" json:"serial,omitempty"`
}

func (o *CertificateRevocation) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(CertificateRevocationReasonId); ok && a.Value.Kind() == entity.KindString {
		o.Reason = a.Value.String()
	}
	if a, ok := e.Get(CertificateRevocationRevokedAtId); ok && a.Value.Kind() == entity.KindTime {
		o.RevokedAt = a.Value.Time()
	}
	if a, ok := e.Get(CertificateRevocationRevokedById); ok && a.Value.Kind() == entity.KindString {
		o.RevokedBy = a.Value.String()
	}
	if a, ok := e.Get(CertificateRevocationSerialId); ok && a.Value.Kind() == entity.KindString {
		o.Serial = a.Value.String()
	}
}

func (o *CertificateRevocation) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindCertificateRevocation)
}

func (o *CertificateRevocation) ShortKind() string {
	return "certificate_revocation"
}

func (o *CertificateRevocation) Kind() entity.Id {
	return KindCertificateRevocation
}

func (o *CertificateRevocation) EntityId() entity.Id {
	return o.ID
}

func (o *CertificateRevocation) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Reason) {
		attrs = append(attrs, entity.String(CertificateRevocationReasonId, o.Reason))
	}
	if !entity.Empty(o.RevokedAt) {
		attrs = append(attrs, entity.Time(CertificateRevocationRevokedAtId, o.RevokedAt))
	}
	if !entity.Empty(o.RevokedBy) {
		attrs = append(attrs, entity.String(CertificateRevocationRevokedById, o.RevokedBy))
	}
	if !entity.Empty(o.Serial) {
		attrs = append(attrs, entity.String(CertificateRevocationSerialId, o.Serial))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindCertificateRevocation))
	return
}

func (o *CertificateRevocation) Empty() bool {
	if !entity.Empty(o.Reason) {
		return false
	}
	if !entity.Empty(o.RevokedAt) {
		return false
	}
	if !entity.Empty(o.RevokedBy) {
		return false
	}
	if !entity.Empty(o.Serial) {
		return false
	}
	return true
}

func (o *CertificateRevocation) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("reason", "dev.miren.core/certificate_revocation.reason", schema.Doc("Why the certificate was revoked"))
	sb.Time("revoked_at", "dev.miren.core/certificate_revocation.revoked_at", schema.Doc("When the certificate was revoked"))
	sb.String("revoked_by", "dev.miren.core/certificate_revocation.revoked_by", schema.Doc("The subject of the operator who revoked the certificate"))
	sb.String("serial", "dev.miren.core/certificate_revocation.serial", schema.Doc("The certificate serial number in canonical form (lowercase hex, no separators or leading zeros)"))
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
	DeploymentAppId                = entity.Id("dev.miren.core/deployment.app")
	DeploymentAppNameId            = entity.Id("dev.miren.core/deployment.app_name")
	DeploymentAppVersionId         = entity.Id("dev.miren.core/deployment.app_version")
	DeploymentClusterIdId          = entity.Id("dev.miren.core/deployment.cluster_id")
	DeploymentCompletedAtId        = entity.Id("dev.miren.core/deployment.completed_at")
	DeploymentDeployedById         = entity.Id("dev.miren.core/deployment.deployed_by")
	DeploymentErrorMessageId       = entity.Id("dev.miren.core/deployment.error_message")
	DeploymentGitInfoId            = entity.Id("dev.miren.core/deployment.git_info")
	DeploymentMessageId            = entity.Id("dev.miren.core/deployment.message")
	DeploymentOperationId          = entity.Id("dev.miren.core/deployment.operation")
	DeploymentOutcomeId            = entity.Id("dev.miren.core/deployment.outcome")
	DeploymentParentDeploymentId   = entity.Id("dev.miren.core/deployment.parent_deployment")
	DeploymentPhaseId              = entity.Id("dev.miren.core/deployment.phase")
	DeploymentSourceDeploymentIdId = entity.Id("dev.miren.core/deployment.source_deployment_id")
	DeploymentStartedAtId          = entity.Id("dev.miren.core/deployment.started_at")
	DeploymentStatusId             = entity.Id("dev.miren.core/deployment.status")
	DeploymentVersionId            = entity.Id("dev.miren.core/deployment.version")
)

type Deployment struct {
	ID                 entity.Id  `json:"id"`
	App                entity.Id  `cbor:"app,omitempty" json:"app,omitempty"`
	AppName            string     `cbor:"app_name,omitempty" json:"app_name,omitempty"`
	AppVersion         string     `cbor:"app_version,omitempty" json:"app_version,omitempty"`
	ClusterId          string     `cbor:"cluster_id,omitempty" json:"cluster_id,omitempty"`
	CompletedAt        string     `cbor:"completed_at,omitempty" json:"completed_at,omitempty"`
	DeployedBy         DeployedBy `cbor:"deployed_by,omitempty" json:"deployed_by"`
	ErrorMessage       string     `cbor:"error_message,omitempty" json:"error_message,omitempty"`
	GitInfo            GitInfo    `cbor:"git_info,omitempty" json:"git_info"`
	Message            string     `cbor:"message,omitempty" json:"message,omitempty"`
	Operation          string     `cbor:"operation,omitempty" json:"operation,omitempty"`
	Outcome            string     `cbor:"outcome,omitempty" json:"outcome,omitempty"`
	ParentDeployment   entity.Id  `cbor:"parent_deployment,omitempty" json:"parent_deployment,omitempty"`
	Phase              string     `cbor:"phase,omitempty" json:"phase,omitempty"`
	SourceDeploymentId string     `cbor:"source_deployment_id,omitempty" json:"source_deployment_id,omitempty"`
	StartedAt          time.Time  `cbor:"started_at,omitempty" json:"started_at"`
	Status             string     `cbor:"status,omitempty" json:"status,omitempty"`
	Version            entity.Id  `cbor:"version,omitempty" json:"version,omitempty"`
}

func (o *Deployment) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(DeploymentAppId); ok && a.Value.Kind() == entity.KindId {
		o.App = a.Value.Id()
	}
	if a, ok := e.Get(DeploymentAppNameId); ok && a.Value.Kind() == entity.KindString {
		o.AppName = a.Value.String()
	}
	if a, ok := e.Get(DeploymentAppVersionId); ok && a.Value.Kind() == entity.KindString {
		o.AppVersion = a.Value.String()
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
	if a, ok := e.Get(DeploymentMessageId); ok && a.Value.Kind() == entity.KindString {
		o.Message = a.Value.String()
	}
	if a, ok := e.Get(DeploymentOperationId); ok && a.Value.Kind() == entity.KindString {
		o.Operation = a.Value.String()
	}
	if a, ok := e.Get(DeploymentOutcomeId); ok && a.Value.Kind() == entity.KindString {
		o.Outcome = a.Value.String()
	}
	if a, ok := e.Get(DeploymentParentDeploymentId); ok && a.Value.Kind() == entity.KindId {
		o.ParentDeployment = a.Value.Id()
	}
	if a, ok := e.Get(DeploymentPhaseId); ok && a.Value.Kind() == entity.KindString {
		o.Phase = a.Value.String()
	}
	if a, ok := e.Get(DeploymentSourceDeploymentIdId); ok && a.Value.Kind() == entity.KindString {
		o.SourceDeploymentId = a.Value.String()
	}
	if a, ok := e.Get(DeploymentStartedAtId); ok && a.Value.Kind() == entity.KindTime {
		o.StartedAt = a.Value.Time()
	}
	if a, ok := e.Get(DeploymentStatusId); ok && a.Value.Kind() == entity.KindString {
		o.Status = a.Value.String()
	}
	if a, ok := e.Get(DeploymentVersionId); ok && a.Value.Kind() == entity.KindId {
		o.Version = a.Value.Id()
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
	if !entity.Empty(o.App) {
		attrs = append(attrs, entity.Ref(DeploymentAppId, o.App))
	}
	if !entity.Empty(o.AppName) {
		attrs = append(attrs, entity.String(DeploymentAppNameId, o.AppName))
	}
	if !entity.Empty(o.AppVersion) {
		attrs = append(attrs, entity.String(DeploymentAppVersionId, o.AppVersion))
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
	if !entity.Empty(o.Message) {
		attrs = append(attrs, entity.String(DeploymentMessageId, o.Message))
	}
	if !entity.Empty(o.Operation) {
		attrs = append(attrs, entity.String(DeploymentOperationId, o.Operation))
	}
	if !entity.Empty(o.Outcome) {
		attrs = append(attrs, entity.String(DeploymentOutcomeId, o.Outcome))
	}
	if !entity.Empty(o.ParentDeployment) {
		attrs = append(attrs, entity.Ref(DeploymentParentDeploymentId, o.ParentDeployment))
	}
	if !entity.Empty(o.Phase) {
		attrs = append(attrs, entity.String(DeploymentPhaseId, o.Phase))
	}
	if !entity.Empty(o.SourceDeploymentId) {
		attrs = append(attrs, entity.String(DeploymentSourceDeploymentIdId, o.SourceDeploymentId))
	}
	if !entity.Empty(o.StartedAt) {
		attrs = append(attrs, entity.Time(DeploymentStartedAtId, o.StartedAt))
	}
	if !entity.Empty(o.Status) {
		attrs = append(attrs, entity.String(DeploymentStatusId, o.Status))
	}
	if !entity.Empty(o.Version) {
		attrs = append(attrs, entity.Ref(DeploymentVersionId, o.Version))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindDeployment))
	attrs = append(attrs, entity.Bool(entity.Id("dev.miren.meta/cloud.export"), true))
	return
}

func (o *Deployment) Empty() bool {
	if !entity.Empty(o.App) {
		return false
	}
	if !entity.Empty(o.AppName) {
		return false
	}
	if !entity.Empty(o.AppVersion) {
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
	if !entity.Empty(o.Message) {
		return false
	}
	if !entity.Empty(o.Operation) {
		return false
	}
	if !entity.Empty(o.Outcome) {
		return false
	}
	if !entity.Empty(o.ParentDeployment) {
		return false
	}
	if !entity.Empty(o.Phase) {
		return false
	}
	if !entity.Empty(o.SourceDeploymentId) {
		return false
	}
	if !entity.Empty(o.StartedAt) {
		return false
	}
	if !entity.Empty(o.Status) {
		return false
	}
	if !entity.Empty(o.Version) {
		return false
	}
	return true
}

func (o *Deployment) InitSchema(sb *schema.SchemaBuilder) {
	sb.Ref("app", "dev.miren.core/deployment.app", schema.Doc("The app this attempt targeted"), schema.Indexed, schema.Tags("dev.miren.app_ref"))
	sb.String("app_name", "dev.miren.core/deployment.app_name", schema.Doc("[DEPRECATED] Denormalized app name retained for downgrade compatibility; canonical identity is app"), schema.Indexed)
	sb.String("app_version", "dev.miren.core/deployment.app_version", schema.Doc("[DEPRECATED] Version value retained for downgrade compatibility; use version"))
	sb.String("cluster_id", "dev.miren.core/deployment.cluster_id", schema.Doc("[DEPRECATED] Client-supplied cluster display value; coordinator storage is cluster-scoped"), schema.Indexed)
	sb.String("completed_at", "dev.miren.core/deployment.completed_at", schema.Doc("RFC3339 time when the attempt reached a terminal outcome"))
	sb.Component("deployed_by", "dev.miren.core/deployment.deployed_by", schema.Doc("Information about who initiated the deployment"))
	(&DeployedBy{}).InitSchema(sb.Builder("deployment.deployed_by"))
	sb.String("error_message", "dev.miren.core/deployment.error_message", schema.Doc("[DEPRECATED] Bounded failure summary retained until lifecycle errors are queryable from the log stream"))
	sb.Component("git_info", "dev.miren.core/deployment.git_info", schema.Doc("Source metadata submitted with this attempt, including builds that never produce an AppVersion"))
	(&GitInfo{}).InitSchema(sb.Builder("deployment.git_info"))
	sb.String("message", "dev.miren.core/deployment.message", schema.Doc("User-supplied description of this deployment"))
	sb.String("operation", "dev.miren.core/deployment.operation", schema.Doc("The intent of this attempt"))
	sb.String("outcome", "dev.miren.core/deployment.outcome", schema.Doc("Terminal result of this attempt; absent while the attempt is in progress"), schema.Indexed)
	sb.Ref("parent_deployment", "dev.miren.core/deployment.parent_deployment", schema.Doc("Optional deployment this attempt was based on"))
	sb.String("phase", "dev.miren.core/deployment.phase", schema.Doc("Last progress phase observed while the attempt was in progress"))
	sb.String("source_deployment_id", "dev.miren.core/deployment.source_deployment_id", schema.Doc("[DEPRECATED] Lineage ID retained for downgrade compatibility; use parent_deployment"))
	sb.Time("started_at", "dev.miren.core/deployment.started_at", schema.Doc("When the attempt acquired the deployment lock"))
	sb.String("status", "dev.miren.core/deployment.status", schema.Doc("[DEPRECATED] Mutable lifecycle status retained for downgrade compatibility; use outcome and app.active_deployment"), schema.Indexed)
	sb.Ref("version", "dev.miren.core/deployment.version", schema.Doc("The app version produced or selected by this attempt"), schema.Indexed)
}

const (
	DeployedByAuthMethodId     = entity.Id("dev.miren.core/deployed_by.auth_method")
	DeployedByEmailId          = entity.Id("dev.miren.core/deployed_by.email")
	DeployedByNameId           = entity.Id("dev.miren.core/deployed_by.name")
	DeployedByOrganizationIdId = entity.Id("dev.miren.core/deployed_by.organization_id")
	DeployedBySubjectId        = entity.Id("dev.miren.core/deployed_by.subject")
	DeployedByTimestampId      = entity.Id("dev.miren.core/deployed_by.timestamp")
	DeployedByUserEmailId      = entity.Id("dev.miren.core/deployed_by.user_email")
	DeployedByUserIdId         = entity.Id("dev.miren.core/deployed_by.user_id")
	DeployedByUserNameId       = entity.Id("dev.miren.core/deployed_by.user_name")
)

type DeployedBy struct {
	AuthMethod     string `cbor:"auth_method,omitempty" json:"auth_method,omitempty"`
	Email          string `cbor:"email,omitempty" json:"email,omitempty"`
	Name           string `cbor:"name,omitempty" json:"name,omitempty"`
	OrganizationId string `cbor:"organization_id,omitempty" json:"organization_id,omitempty"`
	Subject        string `cbor:"subject,omitempty" json:"subject,omitempty"`
	Timestamp      string `cbor:"timestamp,omitempty" json:"timestamp,omitempty"`
	UserEmail      string `cbor:"user_email,omitempty" json:"user_email,omitempty"`
	UserId         string `cbor:"user_id,omitempty" json:"user_id,omitempty"`
	UserName       string `cbor:"user_name,omitempty" json:"user_name,omitempty"`
}

func (o *DeployedBy) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(DeployedByAuthMethodId); ok && a.Value.Kind() == entity.KindString {
		o.AuthMethod = a.Value.String()
	}
	if a, ok := e.Get(DeployedByEmailId); ok && a.Value.Kind() == entity.KindString {
		o.Email = a.Value.String()
	}
	if a, ok := e.Get(DeployedByNameId); ok && a.Value.Kind() == entity.KindString {
		o.Name = a.Value.String()
	}
	if a, ok := e.Get(DeployedByOrganizationIdId); ok && a.Value.Kind() == entity.KindString {
		o.OrganizationId = a.Value.String()
	}
	if a, ok := e.Get(DeployedBySubjectId); ok && a.Value.Kind() == entity.KindString {
		o.Subject = a.Value.String()
	}
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
	if !entity.Empty(o.AuthMethod) {
		attrs = append(attrs, entity.String(DeployedByAuthMethodId, o.AuthMethod))
	}
	if !entity.Empty(o.Email) {
		attrs = append(attrs, entity.String(DeployedByEmailId, o.Email))
	}
	if !entity.Empty(o.Name) {
		attrs = append(attrs, entity.String(DeployedByNameId, o.Name))
	}
	if !entity.Empty(o.OrganizationId) {
		attrs = append(attrs, entity.String(DeployedByOrganizationIdId, o.OrganizationId))
	}
	if !entity.Empty(o.Subject) {
		attrs = append(attrs, entity.String(DeployedBySubjectId, o.Subject))
	}
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
	if !entity.Empty(o.AuthMethod) {
		return false
	}
	if !entity.Empty(o.Email) {
		return false
	}
	if !entity.Empty(o.Name) {
		return false
	}
	if !entity.Empty(o.OrganizationId) {
		return false
	}
	if !entity.Empty(o.Subject) {
		return false
	}
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
	sb.String("auth_method", "dev.miren.core/deployed_by.auth_method", schema.Doc("Authentication method used by the initiating subject"))
	sb.String("email", "dev.miren.core/deployed_by.email", schema.Doc("Email from the initiating identity's verified cloud token, as of the deploy"))
	sb.String("name", "dev.miren.core/deployed_by.name", schema.Doc("Display name from the initiating identity's verified cloud token, as of the deploy"))
	sb.String("organization_id", "dev.miren.core/deployed_by.organization_id", schema.Doc("Organization from the initiating authenticated identity; not the cluster's ownership"))
	sb.String("subject", "dev.miren.core/deployed_by.subject", schema.Doc("Stable subject from the server-authenticated RPC identity"))
	sb.String("timestamp", "dev.miren.core/deployed_by.timestamp", schema.Doc("[DEPRECATED] RFC3339 start time retained for compatibility; use started_at"))
	sb.String("user_email", "dev.miren.core/deployed_by.user_email", schema.Doc("[DEPRECATED] Caller-supplied email retained for compatibility"))
	sb.String("user_id", "dev.miren.core/deployed_by.user_id", schema.Doc("[DEPRECATED] Caller-supplied user ID retained for compatibility"))
	sb.String("user_name", "dev.miren.core/deployed_by.user_name", schema.Doc("[DEPRECATED] Caller-supplied username retained for compatibility"))
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
	sb.String("repository", "dev.miren.core/git_info.repository", schema.Doc("Repository URL without credentials, query parameters, or fragments"))
	sb.String("sha", "dev.miren.core/git_info.sha", schema.Doc("Git commit SHA"))
	sb.String("working_tree_hash", "dev.miren.core/git_info.working_tree_hash", schema.Doc("Hash of working tree if dirty"))
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
	KindApp                   = entity.Id("dev.miren.core/kind.app")
	KindAppVersion            = entity.Id("dev.miren.core/kind.app_version")
	KindArtifact              = entity.Id("dev.miren.core/kind.artifact")
	KindCertificateRevocation = entity.Id("dev.miren.core/kind.certificate_revocation")
	KindConfigVersion         = entity.Id("dev.miren.core/kind.config_version")
	KindDeployment            = entity.Id("dev.miren.core/kind.deployment")
	KindKeyRotation           = entity.Id("dev.miren.core/kind.key_rotation")
	KindMetadata              = entity.Id("dev.miren.core/kind.metadata")
	KindOidcBinding           = entity.Id("dev.miren.core/kind.oidc_binding")
	KindProject               = entity.Id("dev.miren.core/kind.project")
	KindSecret                = entity.Id("dev.miren.core/kind.secret")
	KindSecretVersion         = entity.Id("dev.miren.core/kind.secret_version")
	Schema                    = entity.Id("dev.miren.core/schema.v1alpha")
)
var CloudExportContract = export.MustParse("{\"version\":1,\"target\":\"cloud\",\"marker\":\"dev.miren.meta/cloud.export\",\"kinds\":[{\"id\":\"dev.miren.compute/kind.node\",\"lifecycle\":\"mirror\",\"attributes\":[{\"id\":\"db/short-id\",\"type\":\"string\"},{\"id\":\"dev.miren.compute/node.api_address\",\"type\":\"string\"},{\"id\":\"dev.miren.compute/node.constraints\",\"type\":\"label\",\"many\":true},{\"id\":\"dev.miren.compute/node.name\",\"type\":\"string\"},{\"id\":\"dev.miren.compute/node.registered_at\",\"type\":\"time\"},{\"id\":\"dev.miren.compute/node.runner_id\",\"type\":\"string\"},{\"id\":\"dev.miren.compute/node.scheduling\",\"type\":\"enum\",\"enum_values\":[\"dev.miren.compute/scheduling.cordoned\",\"dev.miren.compute/scheduling.schedulable\"]},{\"id\":\"dev.miren.compute/node.status\",\"type\":\"enum\",\"enum_values\":[\"dev.miren.compute/status.disabled\",\"dev.miren.compute/status.ready\",\"dev.miren.compute/status.unhealthy\",\"dev.miren.compute/status.unknown\"]},{\"id\":\"dev.miren.compute/node.version\",\"type\":\"string\"}]},{\"id\":\"dev.miren.core/kind.app\",\"lifecycle\":\"mirror\",\"attributes\":[{\"id\":\"dev.miren.core/app.active_deployment\",\"type\":\"ref\"},{\"id\":\"dev.miren.core/app.active_version\",\"type\":\"ref\"},{\"id\":\"dev.miren.core/metadata.name\",\"type\":\"string\"}]},{\"id\":\"dev.miren.core/kind.app_version\",\"lifecycle\":\"archive\",\"attributes\":[{\"id\":\"db/short-id\",\"type\":\"string\"},{\"id\":\"dev.miren.core/app_version.app\",\"type\":\"ref\"},{\"id\":\"dev.miren.core/app_version.manifest_digest\",\"type\":\"string\"},{\"id\":\"dev.miren.core/app_version.source\",\"type\":\"component\"},{\"id\":\"dev.miren.core/app_version.version\",\"type\":\"string\"},{\"id\":\"dev.miren.core/source.git_branch\",\"type\":\"string\",\"parent\":\"dev.miren.core/app_version.source\"},{\"id\":\"dev.miren.core/source.git_sha\",\"type\":\"string\",\"parent\":\"dev.miren.core/app_version.source\"},{\"id\":\"dev.miren.core/source.kind\",\"type\":\"string\",\"parent\":\"dev.miren.core/app_version.source\"},{\"id\":\"dev.miren.core/source.repository\",\"type\":\"string\",\"parent\":\"dev.miren.core/app_version.source\"},{\"id\":\"dev.miren.core/source.value\",\"type\":\"string\",\"parent\":\"dev.miren.core/app_version.source\"}]},{\"id\":\"dev.miren.core/kind.deployment\",\"lifecycle\":\"archive\",\"attributes\":[{\"id\":\"db/short-id\",\"type\":\"string\"},{\"id\":\"dev.miren.core/deployed_by.auth_method\",\"type\":\"string\",\"parent\":\"dev.miren.core/deployment.deployed_by\"},{\"id\":\"dev.miren.core/deployed_by.organization_id\",\"type\":\"string\",\"parent\":\"dev.miren.core/deployment.deployed_by\"},{\"id\":\"dev.miren.core/deployed_by.subject\",\"type\":\"string\",\"parent\":\"dev.miren.core/deployment.deployed_by\"},{\"id\":\"dev.miren.core/deployment.app\",\"type\":\"ref\"},{\"id\":\"dev.miren.core/deployment.app_name\",\"type\":\"string\"},{\"id\":\"dev.miren.core/deployment.completed_at\",\"type\":\"string\"},{\"id\":\"dev.miren.core/deployment.deployed_by\",\"type\":\"component\"},{\"id\":\"dev.miren.core/deployment.git_info\",\"type\":\"component\"},{\"id\":\"dev.miren.core/deployment.message\",\"type\":\"string\"},{\"id\":\"dev.miren.core/deployment.operation\",\"type\":\"string\"},{\"id\":\"dev.miren.core/deployment.outcome\",\"type\":\"string\"},{\"id\":\"dev.miren.core/deployment.parent_deployment\",\"type\":\"ref\"},{\"id\":\"dev.miren.core/deployment.phase\",\"type\":\"string\"},{\"id\":\"dev.miren.core/deployment.started_at\",\"type\":\"time\"},{\"id\":\"dev.miren.core/deployment.version\",\"type\":\"ref\"},{\"id\":\"dev.miren.core/git_info.author\",\"type\":\"string\",\"parent\":\"dev.miren.core/deployment.git_info\"},{\"id\":\"dev.miren.core/git_info.branch\",\"type\":\"string\",\"parent\":\"dev.miren.core/deployment.git_info\"},{\"id\":\"dev.miren.core/git_info.commit_author_email\",\"type\":\"string\",\"parent\":\"dev.miren.core/deployment.git_info\"},{\"id\":\"dev.miren.core/git_info.commit_timestamp\",\"type\":\"string\",\"parent\":\"dev.miren.core/deployment.git_info\"},{\"id\":\"dev.miren.core/git_info.is_dirty\",\"type\":\"bool\",\"parent\":\"dev.miren.core/deployment.git_info\"},{\"id\":\"dev.miren.core/git_info.message\",\"type\":\"string\",\"parent\":\"dev.miren.core/deployment.git_info\"},{\"id\":\"dev.miren.core/git_info.repository\",\"type\":\"string\",\"parent\":\"dev.miren.core/deployment.git_info\"},{\"id\":\"dev.miren.core/git_info.sha\",\"type\":\"string\",\"parent\":\"dev.miren.core/deployment.git_info\"},{\"id\":\"dev.miren.core/git_info.working_tree_hash\",\"type\":\"string\",\"parent\":\"dev.miren.core/deployment.git_info\"}]},{\"id\":\"dev.miren.ingress/kind.http_route\",\"lifecycle\":\"mirror\",\"attributes\":[{\"id\":\"dev.miren.ingress/http_route.app\",\"type\":\"ref\"},{\"id\":\"dev.miren.ingress/http_route.default\",\"type\":\"bool\"},{\"id\":\"dev.miren.ingress/http_route.host\",\"type\":\"string\"}]}]}")

func init() {
	schema.Register("dev.miren.core", "v1alpha", func(sb *schema.SchemaBuilder) {
		(&ConfigSpec{}).InitSchema(sb)
		(&App{}).InitSchema(sb)
		(&AppVersion{}).InitSchema(sb)
		(&Artifact{}).InitSchema(sb)
		(&CertificateRevocation{}).InitSchema(sb)
		(&ConfigVersion{}).InitSchema(sb)
		(&Deployment{}).InitSchema(sb)
		(&KeyRotation{}).InitSchema(sb)
		(&Metadata{}).InitSchema(sb)
		(&OidcBinding{}).InitSchema(sb)
		(&Project{}).InitSchema(sb)
		(&Secret{}).InitSchema(sb)
		(&SecretVersion{}).InitSchema(sb)
	})
	schema.RegisterEncodedSchema("dev.miren.core", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\xdc\\\xcb\xd2\xec6\x11~\r\x02\t\x10\xee\x04\xe2$\x84p'\xc5eA\x15\x1b\x1e\xc1\xa5\xb1e[\xffؖ\x8f\xa4\x99\xf3Ov\x87;EA\x15\xaf@\xce\xc9*\xaf\akʺYj˖\xe4\xf9\xa9\xa2\xd8L\xa9e\xf7\xa7[w\xab\xd5\xea\xf1\xcbzD\x03\x1ek|-\x06\xc2\xf0XT\x94a|&c\xcd?}\xee\u05fe3\xd7\x16h\x9a>\x91<\f<EӤ\xf8\xfe\xdd\xd4t@d\x04\xa0MCp_\xf3?~|\"\xf5\xe3W\xd6\xcc\x05\xaa\x04\xb9\xe2\xb2\xc6SOo\x03\x1e\x85l\xe6ٺZ\xdc&|\"\xb5\x04zs\x1b\xe8\x8a\x19'tT\x03\x04u\x1a\xe2\xe5\f\xf1\xa5\x00\xc4\xd2Z\xd9\xd3\xea,1(\xac\x9cAHE\x87\x89\x8ex\x14KI\xcd\x0f\xc4-\x02\xb8)\x13\xf6;9η@'\x01P\x81\xaag\x17\xc2p]\"5mg\xb7b\xeeh-Ȁ%Է#P\x0eMj\t6\xf8U3\\\xc3\x05#c+\x01\xbf\x19\x01ď\x13a\x98\x9b\xae=8\xb4\xedY\xabW\xa6\xbd\xbe\x87\xfa\xa9C\xfd\xc4Ȁح\x9cgh\\\x10g\xc0ͅ'#\x11\x04\xf5eEǆ\xb4j\xe1A\x9d+;\x9f\r@L\x8c>\xe0Ju\xb45\x84\xcb\xf4\xc5\x00\xd3s\xca\xce=Eu\xc9h\x8fՌ\xf9UΌ\xed\x0e\xb4BӴ\xea\x96T;\x8e+\x86\xb5d]\xc0\v\xeaY\x8a,\xfdV\x0e\xe1\xabA\xfe\xa2\xba06\xaf\x99\xab7\x14V\xee͟ƙ\x90\xe8$s-K\xa9co\x14\xfb\xe3\xe7B\xc3\xd7+\xa1\xc6\x7f\x05o\xe8\x87)\x13\xf0Bv\xfc\xf50@A\x9f\x8f\x98\xc9&\xb0*\xa6\xf6\xddH\xca\nY\x99L&H\x83L\xef\xa1U5OS\xba\xff{\xd9}8C\x06a\xb60\xb2\x89Y\x8c\xf6\x85\xd6p\fh$\r\xe6J\xde;KA\r\xffz\x8c\xbf\xacIk`(\xactФ\xc1\xfd\xfc\x16\x1a\x17H\\\xb8\x04itY\x1a\b<^\x86\xf3\xfcS^Q\x7f\xc1\xfc\x9f\x8d2\xe7\xab\xe9VLz\x03\xe8\x10\xab:r\xc5\xeb\x06\xcdk\xfa\xf9\xee\xd2v\xa6w\xe1\xb5\x1d\xb0@5\x12(\xbc\xb6\xe6i\x92\x9d\x0f\u038dA(zt\xc2=\xaf\a4\xde\xfe\xa5fH\xd7\xcc3\x84e9(\xdb\x16@j\xe4\xf2\x03\x97\xf8\v[|{\x16q\x7f\xe6\f\xc4jPr\xe6\x16\x9b\xae\xf7\xcb\xd76\xb7\x91\x94\xe9\xfb\x87\x1c\xc5\x1b\x9b\x18\xdb\xca\x01\xf7\x7f\x9f\xa7\xb4S\xd6Y\n\xce\x1d4\xa8\x00\xc15\xa8g\xb7\x02\xe2@\xaf\xc8\xc1\xa9\xfa\v\x17\x98\x99\x1d\xf9\xc1\xa1!\xca\xd7vP\xe80\xf5X,nB\xef\xd5@E\xdd\x19\x97*\xe2\xba<\xddԸ܊\x88c\x04`\x8b0lʢ\xffeg\xc0\x12\xa4@\x17ѕ\x03\x16\x1d\xad\xf5\xfc;\x15p栙t\x81\xf0\x80H\xaf\xf6\x06U\x8c)\x90\xcb\x1cѽ\xb0s\xa7X)k\xd1H>B\x82\xd0Ѭ>\x85\x95\x100,\xd2\n\x90_N\x8b>\x1b\"M\x12\x15\xc0\xec\xaeq\x81\x06\xa5Nd!\xd3\xd4B\x81\\8f\xe52\xa7\x0f\x0e\x9d3\x18ɦg\xa55D\xce`$\x8f]\x19\xb2\x90\xa9[\xff\x83A;݂[\xa5#ܘ1\xca\xca\x01s\x8eZ\xed&\xfaUP\xffv,SKDIƆ*\xcbd\xa9\xcc#I\x000]\xed\xa0U7\bR\xe7\xa8\xf2\xa3\x1a]\x86K\xb2\xc9{bh\xac\x94\xfb\xd8\xe82\xe4\xfd\xd6\x16oE\x87\x81\x88R5\xe9\b\x17\x0f=\x80\xa8߈\xa0\xfaR?\xadjc\xb6\xc4\xe2\x11^ք\te6;KIG\xe7Di\x1f4&\x96ە\x9e6 7A\x8d\xb1\xdc\fO\x94\x13A\x99j\xfd\xc1\xa1!\x06t2-\x06\xef\x90\xdaG\xe7B\xec(h\xb9\xe6\xf3\x10\x19\xdbR0\x8c\xcb\x0eq\xb5\xc4\xcf\xd6\xd5\xc9.wKČ\x1c<\f:r\x9d4a_\xde\xe6\xa7\x13f\xd2\xce*\x03\xb1\x90\x10c\xa7\x0f\xf4\"*\xaaMLk\x88\x98X;\xfc\x13\x92g0\x18\x19YW\xbbnMx?Rx\x1d\xe2\xaa7X\x15a_\x8am^N/\xacr\xc31\xc6\xfc\x8a\xe0\x93\f\x17\x87\v\xc4\x1c\xd7\xe4\xc1\xa1\xfd\x00Fx\x9b6\x18\xa1\x13D\xe2:\xb9\x8eZ\vN\xbc)\x9b\xc0\f\xb2\x9aw\x1353^\x9f6Ł\xe0\x83y#\xc5\x04\xff=\xe8\xf98 \x05\xaa\a2\x96\x82\x9e\xb1\xf1<\x9d\x8a\x98=\xf6\x80\xb6\x1cg\xa85\x1e\x93>5i\xcf\xd9Pn\xdc-\x10\xc1\xb1\xecN\x04\xa7q\"7;[ۛ\xebh\x1b@K\x8aN\xbe\n͆\xe2\x97;\x01\x1ak\xf7\x10\xd6ٺH\xf7ފv\xcf§\ar\xa0\xa8\x19\x04\x03\xa5\x04\xd9\x101\x87\xd5rs̮\xa4\xd2\xe6\xca\x10\xa9v\xd9\xceHPU\xf5P\xf1(\xd8m\xa2d4\x11\xc1\x85\x86\xbd\x84z\xa2\x11&ʄ\x0e2ͥ\x99\xab\"\xa3\xd8[>=\x12o\xf9l\xdd\xfd\xcbg\xa0\x92\x1c\xa8W\xa1êA(j\xc2\xcfn7\xb1\xaa\x88\xf4\xf1\xdd\xf4>\xaa\x16Rz\xfa\xd7`PA\xb2\x17\xf5\xa9l\x88\x0ex\xb6\x86\x88\x1e\x8b$\xeb\xfc*\xbfq\x81\a%\x00\x0e\x1du\xff%@\x8f\x11\xc7\xd2\xfd\xa2\x17%\b\x83_\x95֏\x81^FQڈ\xe5\x83CC\x80UxB\x02DNvPz\x15\xd3^\x9c1(\x19\x8amb\xf4Jj\xccd<le>\xe7J\xb9\xae\xa5}O\x8a\xb8\xa5\x82\xa1\xb4\x8fqO+ԯZ4\\\x85|\x8c\xe5\x93\xed\x97d]ß\xf5D\xe0\x95\xfaٷ\xd4\xf3\xe0~\xa3\x06\xc80\xaaK:\xf6\xca;%\v\xe9;\xc7ai\xe4\xe4#\\\xb6'm\xb54a\xecBХ\xd5|\xd2m\xd1.\x83*\xc7vG\xcd(\x87c\xdc\x1f\xb2\x90\xa9\xa6R\xa9\xf5\xabP\u05ec\xa2\xe2\xf1\xea\x18\x82j&#f\xa0\xc80\x03x\xbc\xa6\x18\x81?\x05e\x19\x8f\xd7KٝP\x93n\x88\xd8\xf4͌5\xe6\x15#\x93u\xac\xcfn\x05\x00\x80\xf1\xfe\x99\xff\x8c\x95\x90Ts!vv\x99\x19\x18V\xf7aJ),\xb5/Y3#\xc7#'\x82\\u\x84`!}Vh\x1e$k\\\xb4>\x13`\x93\x9a\xa9̃*&_\x1f\xe1\xf1\x1a\f\x83\xda\x05'\x839\vaU\x84\xfdYE\xd4\rg\xc4\xd0m\xf2ml\xd4AW\xdccr\xa22\v\x19s\xe6}\x04\xb9B\v\x82:EDL\xad\x87\xe0mª\xe2\t7a\t\x98\xa2\x7f\x7f\bJ\x98d\x8f\xad\xcb\xca\x18+&Z\xe3Ү\fYHoy\xc2\r\xeey^\xab\rBq0*hE\xfb\x9d}K\xae\x8f}\xcf\xec[\x8a\n_\x01U\xa2\x9aV\x8anx\nQMե\xdey\xe1RO;c\xb4\x82SC\x99\xd97\xe8\x92\xfbe\xe84o\x17]\x17ʊ\x8e\xean\xb5R\x86\x8c\x87\x1eD\x84\xed\xc3\fa\v\xc0\xa7\x8b\x1e\fj\x06\xc0\x8a\x81\xd6z\xced\t\n\xe2\xbb\t\x10\xf3\xf2\x92\x91\v4\xce.\xb9t\xea\xfc*O<\x7f\x90\x808\xdby\xcc\x05/'\xcc,\x8e\xbaD\x0f?\xf2Zx?\xa1\x05>\xbbGeM\x9f\x8fe\x8d{\xa4\x16sZ\xd5\xc2\xe9H\x82\xee.BB\xb8n\ued2aM\x95N\xa6\xdbp\x9a\xd8?\xc9\x19\xd9\t\x06\xf3\x8d|\t\xc4DY\x13\x86+\x1bV\xa4\xb0\x12\xda܍\x03\xda\x151\x82N=v\x0fh\xb6\xee\xfe\x03\x9a\x81J\xf7x\xe0\x99\xc1 \xa4\xb9=0<b\xb9s|\x9f\x95\x05\xb3(\xbb\x1e\x10<w[\xae$7\b\x9ah˝\xe8\v\xc1\xf5]\xf8\xe3\x0e\x11\xdcB,\xefa\xaf\xc8\xca\xd0~\xfa\x89\x92\x92`\x14{-L~r\x19\xa8scd\xef\xec@\xe1\xa9\xc3\x03f\xa8/A\x86\x94\b>\xf1\x83\xa0\xf0\xce0\f,\xf3\x02\x94R\xc2\xca\xd8\x05H\x18P\x88^\xdf\\yU\xb10\xaf\v&\x1d\xcf\xf2\xc2\x14\x10YȘ\x02\xb9 \x89\x89+{\xb3t(we/h\xb9!\xdd\a\x83\x96\n!}\x8f\x86*\xaf\xf8彞s\xa9\xf6\xe0\xd01\xd5s\x10̥Ok\x88X\xa0C\xf3νWN\x81,\xc5̔\xe6\xba\xe3\x9aJ#\x1c6\x16z\xe1\xa2\xc2\xc3\x05\x12\xa4*\xbd07\x85\x95\xb1P\x96\v\xb8y\xf7\x90\xd2k7\xabd5\xa9\xf2\x02\xe2\x8co%\xa3B^]i\xe1[%\xda9\xaf\xa4\x8b\x1d\xb4\x95.J֍w\xd0nxh\r\xa3Ci\xb6\xbd\xceR\xb1\xb4\x03\x0f\x83\xe1\xe7\fM\x93\xde\x00\xc9B\x1a\xc7/x\xf1\xeeA\xa4f\xa9\xbd\xack:\xae/zt\xf2\xd9\xfc\xaci\x10\xe9q\xe0\xf0\xac^QO;\x86\x05\x99\a\xb7\x95\xc6f\x9e?\xe8\xc1̯\xae\x14˼j\xde\bʣ7LA\xed\\7\xba\x9c*\x8e\xbd\v\x14\x96GJ\xea\xaa<\x91\xb1&c\xbb!\x8f\xee+\xe9\x99\xe5\xd0gsQ\x82WY\xafB9\xd1\x1eW\xd5#2̞yM\xe6\x01\xb9A\x81i\xf5,b\xf1AC\xc5nC\xe97Ap\xab\x84H\xfb\xde\"<ޭ\xb8'$\x04f\xda8\x19\"U\x1a\xa8\x84[ЂMz\xf3\x90\xe5 C!\xf6\x90\b\xe7\x17\x1d\bot9fr<\xfe\x9dP\xfa\xdeݽ\x87\xa1s\xacJw\x12)\xacLV-\x17z\xad\xc0\xb3j\xf9ި\x96;\xb8\xb7\xfb/\xa5\v\x1a\xf4\x7f|\x9c\xa0\x82\x05-*\xe0\xe3\x13\xae\x94\x9f K\x11%\x82Kf\x9f\x9b\xb1\xcf \xe9\xf7p?L\x81+\xaa\x9e\x8e\xb8D\xb3Q/\xe5\x89b\xf4\u0083\"\xf8<2\x10\xd0r\x91\xder\xfa\x8a}xxt\xb1\xd8\xe2Ϗ#\xeb\x82\xf6v4\x91\xaa\x03\x8f\xaf+\xfc\x19^\xa2Kp\x83-\xc5\xed\xbdܮ\xb9K\xd9{\xf5p\xd4\xf0D\x17\x86N\xbb\xe1\x96\xe2\xf7v\x12\xe0=\xb7\xd7o'\x89Y\xce\x05\xf6\xdf\xe4,\x7f'\xab\xe7\x05b\xad\xdb\xfdZ\xd2pz?\xc8Ìe:\xbcLWp\a\xd3\x0f͞3B\xb2i\x1a\x1dj*\xdd\xe5N\xd3\xe8P\x1b\xb1 \xed\xaf\x8e#\xe7\xc5n\x7fs\xbc\xa1\xfbB\xba\xbf>\xde\xf0\xc1H\xef=->m\x00\xf8\xf1\x8d\xc5n\x9a\xf6\x9c\xe6^\x85\xe2ґ\xde\x1e\xcbTy?OI2\x93U~|`\bi\xb9,\x99\x8a\x97\x9d꒶\xa1\x02\xfc\xecL\x98C\xa3\xc8H\x94\x81\xf72I\xf8\x11W#ӂ\xc7\xd3l>\x99Q\x7fr\x04\xf5\xff#\v\x87\xc9\xd6{ܢ\xea\xe6w\xe2\xc5\xe3/\xee\x99\x16\xb7\xab\xf7B\xb9\x03z\xf1\xf8˻\xb0\x9c䣟\x1e\x01J\xccM:d|\xe2\xa9K?:\x04\x1b\xbfm94\x15w'>=_\xefCK*T\x9a\xeb~G\x82\xd4{y\xbbOV\x8eT\xa6\x9dJN\xa1\xca\\\xa7\xdc\f\xab\\\xaf;\x9a\x81\x95)\xaf\xc9\tZ\x99ꕑ\xbf\x95\xb9k%\xa6w}?\x1f\xf5\xf0\xd5\xc5e\xadU&\x1f,ӫ\xdb\xcb\x12{y@\\\x06,\x18\xa9ԩ\xa05DDK?\xc8\xd3R\x8d\x9a~\x88\xca\xd4(\x8d_\xe0Qf#\xa8\xa1\x18\u0097\xa4L\xff\xca \x93Q`vE:\xfb\xc9Rwj\x96A\xdf\xfe(\xc3]\xa8;\xc9}\x99Ε\x85\xbc\x9cz\xa2b\x81\x8d.\xdb\xe9\xdd?ݼ\xb6\x96\x7f\r\xfa\xf1\x81`E\xc4!\xcdDۛ\xa8L\x1b\x91\x9c\x1b\x99\xb9\xa6*E\xd29F\xf4^͝\x96-9\x1f3\xd3T\x1d\xcb\xd2\xcc<\x80f&jfn%Iy\x9c\x99\xe6*'\xcd\xf3Pw7\x04\xfaȱ\xea\x7f:I4pN\x92\xa8/\x1e\x7fv\xcf(M\xdb\xf7\u00984\xd6Ckx0\xcb5\xe0\xbdK\xbc\xfd\xe4\xc35\x93\xec\xf8w\xd3:\x9e\x99e\x98\x11\xa8\xd79\x1b5Q\a\xf5\a\x87\x86\x80\xdf\xcb\x01Ty\x0e\x93q\xa4\x9e\xad\xab\xa1\xe5\x83\xe9&ax\x81@\xc4MUd\xa6G\xee`\xa7ع?g\xec\x81\x124\x1a\xa7\x973\x00\xffv\xbc\ax\xff\x1fc\"\xe8釾\xb4\x8d\xd6\x02\xe7\x9d\xccr&9z,K\xf3\xdb\x170\xcaHKT'\x1b]>\xe6\a,\x90IǼ4s\xb6\x80&\x9e\xf1r\x87\x9fp\xc0K\xb3_\v\xe4\xe1ӝc@\x95\x9a\xea\xa3]Z\xc4Au`@\x8fK\x82\xb9ھGP\xe7y\x06i\x16Ta?\x89\xbf\xac\xa0\xd8\xec\xb5\xebK\xa4\xd6\x10\xbb\x89\xf9{h\xbc\xeap}ѡ\xfd\xceR\xf7t\xd0\xf5\x92\xdb-\a9\v\x90\x91\xb6ա\xe2\xd6\x10\xe9\xf95\xbedd\x98R\x93\x19\xedn(d\xa9|\x12\x83j\xe1\xd2\rj\xdaFk\x81\xd3BhiVz\x01\xfd/X\xe9\x05|\xd7J\xa7\x99\xd4\x05\xec\tM\xea\x02\xfa\xa4&Ձ}*\x93\xba@\x1e6\xa9|Q\x1c\x8b\xb6\x9ff\xebt`\xff\x83\xa5~zS8OK}\xef2\x92\xa7忔\x9e\x06\t\xb3\xf9}\x9c\xa2\"S\x87\x99\xc0\x8f:=š\xe7\xd9ç\x9b\xd0\x0e:̵\x02@g|6\x17\x01\x8d.\xc7\x12\xec\x00\x82\"\xb5D\xa8\xb2\x9b8\x063\x1b!\xb7@B/\xbe*\x86\xef\xd7L\xa8.\x98j\x8bMT\xaf\xab\tW\xaf\xad\x96A\xbef\x1e\x93\x1as\xc1\xe8m\xe3Ӓ\xf2\x1f>\xea\xf9ƿ\xe7\xbc!\xc8.\x96\x03\xaa\x8c\xf95$\x9cH\x98j\rpt&sYc\xf5\xbd\xe2\xb3[\xb1\xac\xea\xbe\xe0\xfa\x98\xab3\x89J0\xc4L\x90\x86TH\xe0\x92\xe1+\xadܬ\xf2\xd5\xffĂ/\xa7\x7f\xec\x14f\xe6\x86\xf1\n\x86\x11׆\xba\xd1e8}\xf0\x1f\x88\x9bHWzv>C\xe4\xd0\xfe?p2\xf1N7\x1f\xef\xb46\xfc\x89c\xe5\x98\x11\x1d'nt9\xd5\xe4]Ð\x90\xe7\xcc;ʄ2\x92\x9fVh\x9a\xb6>\x05n>ڻ\xf3\xcdb\xfbmܽ\x0f\xfbF\xbe\xb2j\x9e.\x9f\x14\xdd\xfd\x18\xab\xfb9\xa6ȷG\xbd\x7fN\xc4>ݔ\x90\xd7\xee\xbe\xe1\xe7\xeaF\xd3\xe0\x936\r\xff\x1d\xa8\xae\t\xfb̆\x04\xe4h\xfa\x7f\x00\x00\x00\xff\xff\x01\x00\x00\xff\xff\x14\x84vY4^\x00\x00"))
}
