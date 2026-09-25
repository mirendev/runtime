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
	ConfigSpecEntrypointId      = entity.Id("dev.miren.core/component.config_spec.entrypoint")
	ConfigSpecServicesId        = entity.Id("dev.miren.core/component.config_spec.services")
	ConfigSpecStartDirectoryId  = entity.Id("dev.miren.core/component.config_spec.start_directory")
	ConfigSpecStaticDirId       = entity.Id("dev.miren.core/component.config_spec.static_dir")
	ConfigSpecStaticErrorPageId = entity.Id("dev.miren.core/component.config_spec.static_error_page")
	ConfigSpecTasksId           = entity.Id("dev.miren.core/component.config_spec.tasks")
	ConfigSpecVariablesId       = entity.Id("dev.miren.core/component.config_spec.variables")
)

type ConfigSpec struct {
	Entrypoint      string                `cbor:"entrypoint,omitempty" json:"entrypoint,omitempty"`
	Services        []ConfigSpecServices  `cbor:"services,omitempty" json:"services,omitempty"`
	StartDirectory  string                `cbor:"start_directory,omitempty" json:"start_directory,omitempty"`
	StaticDir       string                `cbor:"static_dir,omitempty" json:"static_dir,omitempty"`
	StaticErrorPage string                `cbor:"static_error_page,omitempty" json:"static_error_page,omitempty"`
	Tasks           []ConfigSpecTasks     `cbor:"tasks,omitempty" json:"tasks,omitempty"`
	Variables       []ConfigSpecVariables `cbor:"variables,omitempty" json:"variables,omitempty"`
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
	DeployedByOrganizationIdId = entity.Id("dev.miren.core/deployed_by.organization_id")
	DeployedBySubjectId        = entity.Id("dev.miren.core/deployed_by.subject")
	DeployedByTimestampId      = entity.Id("dev.miren.core/deployed_by.timestamp")
	DeployedByUserEmailId      = entity.Id("dev.miren.core/deployed_by.user_email")
	DeployedByUserIdId         = entity.Id("dev.miren.core/deployed_by.user_id")
	DeployedByUserNameId       = entity.Id("dev.miren.core/deployed_by.user_name")
)

type DeployedBy struct {
	AuthMethod     string `cbor:"auth_method,omitempty" json:"auth_method,omitempty"`
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
	KindApp           = entity.Id("dev.miren.core/kind.app")
	KindAppVersion    = entity.Id("dev.miren.core/kind.app_version")
	KindArtifact      = entity.Id("dev.miren.core/kind.artifact")
	KindConfigVersion = entity.Id("dev.miren.core/kind.config_version")
	KindDeployment    = entity.Id("dev.miren.core/kind.deployment")
	KindKeyRotation   = entity.Id("dev.miren.core/kind.key_rotation")
	KindMetadata      = entity.Id("dev.miren.core/kind.metadata")
	KindOidcBinding   = entity.Id("dev.miren.core/kind.oidc_binding")
	KindProject       = entity.Id("dev.miren.core/kind.project")
	KindSecret        = entity.Id("dev.miren.core/kind.secret")
	KindSecretVersion = entity.Id("dev.miren.core/kind.secret_version")
	Schema            = entity.Id("dev.miren.core/schema.v1alpha")
)
var CloudExportContract = export.MustParse("{\"version\":1,\"target\":\"cloud\",\"marker\":\"dev.miren.meta/cloud.export\",\"kinds\":[{\"id\":\"dev.miren.compute/kind.node\",\"lifecycle\":\"mirror\",\"attributes\":[{\"id\":\"db/short-id\",\"type\":\"string\"},{\"id\":\"dev.miren.compute/node.api_address\",\"type\":\"string\"},{\"id\":\"dev.miren.compute/node.constraints\",\"type\":\"label\",\"many\":true},{\"id\":\"dev.miren.compute/node.name\",\"type\":\"string\"},{\"id\":\"dev.miren.compute/node.registered_at\",\"type\":\"time\"},{\"id\":\"dev.miren.compute/node.runner_id\",\"type\":\"string\"},{\"id\":\"dev.miren.compute/node.scheduling\",\"type\":\"enum\",\"enum_values\":[\"dev.miren.compute/scheduling.cordoned\",\"dev.miren.compute/scheduling.schedulable\"]},{\"id\":\"dev.miren.compute/node.status\",\"type\":\"enum\",\"enum_values\":[\"dev.miren.compute/status.disabled\",\"dev.miren.compute/status.ready\",\"dev.miren.compute/status.unhealthy\",\"dev.miren.compute/status.unknown\"]},{\"id\":\"dev.miren.compute/node.version\",\"type\":\"string\"}]},{\"id\":\"dev.miren.core/kind.app\",\"lifecycle\":\"mirror\",\"attributes\":[{\"id\":\"dev.miren.core/app.active_deployment\",\"type\":\"ref\"},{\"id\":\"dev.miren.core/app.active_version\",\"type\":\"ref\"},{\"id\":\"dev.miren.core/metadata.name\",\"type\":\"string\"}]},{\"id\":\"dev.miren.core/kind.app_version\",\"lifecycle\":\"archive\",\"attributes\":[{\"id\":\"db/short-id\",\"type\":\"string\"},{\"id\":\"dev.miren.core/app_version.app\",\"type\":\"ref\"},{\"id\":\"dev.miren.core/app_version.manifest_digest\",\"type\":\"string\"},{\"id\":\"dev.miren.core/app_version.source\",\"type\":\"component\"},{\"id\":\"dev.miren.core/app_version.version\",\"type\":\"string\"},{\"id\":\"dev.miren.core/source.git_branch\",\"type\":\"string\",\"parent\":\"dev.miren.core/app_version.source\"},{\"id\":\"dev.miren.core/source.git_sha\",\"type\":\"string\",\"parent\":\"dev.miren.core/app_version.source\"},{\"id\":\"dev.miren.core/source.kind\",\"type\":\"string\",\"parent\":\"dev.miren.core/app_version.source\"},{\"id\":\"dev.miren.core/source.repository\",\"type\":\"string\",\"parent\":\"dev.miren.core/app_version.source\"},{\"id\":\"dev.miren.core/source.value\",\"type\":\"string\",\"parent\":\"dev.miren.core/app_version.source\"}]},{\"id\":\"dev.miren.core/kind.deployment\",\"lifecycle\":\"archive\",\"attributes\":[{\"id\":\"db/short-id\",\"type\":\"string\"},{\"id\":\"dev.miren.core/deployed_by.auth_method\",\"type\":\"string\",\"parent\":\"dev.miren.core/deployment.deployed_by\"},{\"id\":\"dev.miren.core/deployed_by.organization_id\",\"type\":\"string\",\"parent\":\"dev.miren.core/deployment.deployed_by\"},{\"id\":\"dev.miren.core/deployed_by.subject\",\"type\":\"string\",\"parent\":\"dev.miren.core/deployment.deployed_by\"},{\"id\":\"dev.miren.core/deployment.app\",\"type\":\"ref\"},{\"id\":\"dev.miren.core/deployment.app_name\",\"type\":\"string\"},{\"id\":\"dev.miren.core/deployment.completed_at\",\"type\":\"string\"},{\"id\":\"dev.miren.core/deployment.deployed_by\",\"type\":\"component\"},{\"id\":\"dev.miren.core/deployment.git_info\",\"type\":\"component\"},{\"id\":\"dev.miren.core/deployment.message\",\"type\":\"string\"},{\"id\":\"dev.miren.core/deployment.operation\",\"type\":\"string\"},{\"id\":\"dev.miren.core/deployment.outcome\",\"type\":\"string\"},{\"id\":\"dev.miren.core/deployment.parent_deployment\",\"type\":\"ref\"},{\"id\":\"dev.miren.core/deployment.phase\",\"type\":\"string\"},{\"id\":\"dev.miren.core/deployment.started_at\",\"type\":\"time\"},{\"id\":\"dev.miren.core/deployment.version\",\"type\":\"ref\"},{\"id\":\"dev.miren.core/git_info.author\",\"type\":\"string\",\"parent\":\"dev.miren.core/deployment.git_info\"},{\"id\":\"dev.miren.core/git_info.branch\",\"type\":\"string\",\"parent\":\"dev.miren.core/deployment.git_info\"},{\"id\":\"dev.miren.core/git_info.commit_author_email\",\"type\":\"string\",\"parent\":\"dev.miren.core/deployment.git_info\"},{\"id\":\"dev.miren.core/git_info.commit_timestamp\",\"type\":\"string\",\"parent\":\"dev.miren.core/deployment.git_info\"},{\"id\":\"dev.miren.core/git_info.is_dirty\",\"type\":\"bool\",\"parent\":\"dev.miren.core/deployment.git_info\"},{\"id\":\"dev.miren.core/git_info.message\",\"type\":\"string\",\"parent\":\"dev.miren.core/deployment.git_info\"},{\"id\":\"dev.miren.core/git_info.repository\",\"type\":\"string\",\"parent\":\"dev.miren.core/deployment.git_info\"},{\"id\":\"dev.miren.core/git_info.sha\",\"type\":\"string\",\"parent\":\"dev.miren.core/deployment.git_info\"},{\"id\":\"dev.miren.core/git_info.working_tree_hash\",\"type\":\"string\",\"parent\":\"dev.miren.core/deployment.git_info\"}]}]}")

func init() {
	schema.Register("dev.miren.core", "v1alpha", func(sb *schema.SchemaBuilder) {
		(&ConfigSpec{}).InitSchema(sb)
		(&App{}).InitSchema(sb)
		(&AppVersion{}).InitSchema(sb)
		(&Artifact{}).InitSchema(sb)
		(&ConfigVersion{}).InitSchema(sb)
		(&Deployment{}).InitSchema(sb)
		(&KeyRotation{}).InitSchema(sb)
		(&Metadata{}).InitSchema(sb)
		(&OidcBinding{}).InitSchema(sb)
		(&Project{}).InitSchema(sb)
		(&Secret{}).InitSchema(sb)
		(&SecretVersion{}).InitSchema(sb)
	})
	schema.RegisterEncodedSchema("dev.miren.core", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\xdc\\K\xb3\xec6\x11\xfe\x1b\x04\b\x10\xde\x04p\x12B\x80\xf0H\xf1XPņ\x9f\xe0\xd2\xd8\x1a\x8f\xceؖ\xaf\xac\x99{&\xbb˛\xa2\xa0\x8a\xbf\xc0\xbd7\x1b~\x1f\xac)\xab%Yj˖\xe49TQlN\xa9\xdb\xeaO\xafV\xab\xd5\xea3\xaf\xea\x9et\xb4\xaf\xe9\xb5蘠}QqA\xe9\x99\xf5\xf5\xf8\xcf\xe7>\xf7\x9d\x89[\x90a\xf8D\xc9\b\xf4\x95\f\x03\xc8\xfd\xfbX\xf3\x8e\xb0\x1e\x81\x1e\x8f\x8c\xb6\xf5\xf8\x87\x97\aV?~y)\\\x90J\xb2+-k:\xb4\xfc\xd6\xd1^\xaaf\x9e-\xd9\xf26\xd0\x03\xab\x15\xd0[\xeb@W*F\xc6{\x18 \xe2i\x88W\x13\xc4\x17\x03\x10ske˫\xb3\xc2\xe0\x989\x81\xb0\x8aw\x03\xefi/\xe7\x12\xcc\x0f\xc6-\x02\xb8)\x13\xf6[5ηQ'\x11PA\xaag\x17&h]\x12\x98\xb6\xb3˘:ZK\xd6Q\x05\xf5\xad\b\x94C\xb3Z\x81u>k\x82;\x8eR\xb0\xbeQ\x80߈\x00\xd2ǁ\t:\x9a\xae=8\xb4\xedY\xa3W\xa6\xb9\xbeG\xda\xe1D\xdaA\xb0\x8e\x88[9\xcdP?#N\x80\xab\v\xcfz&\x19iˊ\xf7G\xd6\xc0\xc2#\x9e\xab;\x9f\x0e@\f\x82?\xd0\n:\xda\x18\xc2\x15\xfaB@\xe89\x17疓\xba\x14\xbc\xa50c>˙\xb1́Vd\x18\x16\xddR\xdbn\xa4\x95\xa0Z\xb3.\xa8\x02|Kѥߨ!|%(_T\x17!\xa65s\xf7\r\xc7̭\xf9\xd38\x03\x91'%\\\xabR\xea؏ \xfe\xf8\x99\xd0\xf0\xf5J\xc0\xf8\xaf\xa8\x86\xfe\x982\x01/T\xc7?\x1b\x06(\xf8\xf3\x9e\n\xd5\x04\x85bjߍ\xa6,\x90\xc1d\nɎ\xc4\xf4\x1e[U\xf35\xa5\xfb\xbfS\xdd\xc73d\x10&\v\xa3\x9a\x98\xd4h[i\x8dDGzv\xa4#\xe8\xfb\xc9Rx\x87\x7f-&_֬10\x1c3\x1d4ep?\xb7\x866J\"/\xa3\x029\xea\xb22\x10\xb4\xbft\xe7\xe9Oy%텎\xff8\x829_L7\b\xe9\x03\xe0DDubW\xbal\xd0T\xd3\xdf7\x97\xf6dz\x17^ێJR\x13I\xc2kk\xbe&\xd9\xf9\xe0\xdc\x18\x84\xa2%\aڎuG\xfaۿ`\x864g\x9a!\xaa\xcaAݶ\x00jG\xce\x7f\xf0\x12\x7f~Mn\xcb\"nϜ\x81X\fJ\xcd\xdcl\xd3\xf5y\xf9\xc6\xea1\x922}\x7fW\xa3xs\x15c}s\xe0\xf3ߗ)픝,\x85\xe7\x0e\x1bT\x84\xe0\x1aԳ\xcb\xc08\xd8+rp\xaa\xf62J*̉\xfc\xe0\xd0\x18\xe5\xab\x1b(\xbc\x1bZ*g7\xa1\xf58x\xa3n\x8c\v\x8a\xb4.\x0f7\x18\x97ˈ8F\b\xb6\bæ,\xfa\x1f7\x06\xac@\nr\x91\xa7\xb2\xa3\xf2\xc4k=\xff\x0e\x03\xcf\\\xd8\xc9\x02 .\x1aҳ\x8f\x89d\xbc7\xab\xc01\x13\x03\x86U\v\x00\xc7\xcba\xdeW\x86H\xd3\b\x00\x98ܦQ\x92\x0eԚ\xcdd\x9az\x02\xc8e\xa4\xa2\xa4\x1da-\xe8\x95C\xe7\fF\x89\xe9Yi\f\x913\x18%c\xb7\x1a\x9b\xc9\xd4#\xf8\xc1\xa0\x1dn\xc1#\xcbQ2*\x04\x17eGǑ4\xda]\xf3Yx\x1flX\x88\x86ɒ\xf5G\x0e\x16\xc2R\x99W\x83\x00`\x8a\xfa\xff\xf9e\xe8\xc80\bJ\xf79\xf83G]\xc6K\xb2*{\x10\xa4\xaf\xc0\x8d;\xea2\x96\xfd\xe6\x9alŻ\x8e\xc9\x12\x9at\x94k\f}\xc0\xa8_\x8f\xa0\xfaZ?,\xb8\x18\x0f\xbb>\x16\x8f\x8dë́\x04\xf3u\xb2\x94r8\x0e\x9c\xb7\xc1S\xd1J\xbb\xda\xd3\x04\xf4&\xb8c\xac\xb4\xa0\x03\x1f\x99\xe4\x02Z\x7fph\x8c\x81\x9d=\x8b1\x9e\b\x9cgS!v%\xb3Rӽ\x84\xf5M)\x05\xa5剌\xb0\xc4ϖ\xecd\u05f7arB\x0e^\xca\x1c\xbdN\x9a\xb0/\xad\xcb\xf3\x81\neg\xc1@\xcc$\xc6\xd8\xe8\x03\xbfȊk\x13\xd3\x18\"\xa6֎\xfc@\xd4]\bG(\x96l\u05fd\xc0*\xe4\xe2\x9d\xc8\b\xbd\xa1P\xc4})\xd6eG~\x11\x95\x1b\x161\xe6W\x06\xbfd\xb8\x1a\xa3$\xc2q\x11\x1e\x1c\xda\x0f$\xe0\xad\xe5c\x84<\xf9\xc4ur\x1d\xa6\x06\xdd<S\x0e\x81\td1\xef&ze\xbc/m\x8a\x03A\x00S#\xc5\x04\xff-\xe8\x818 \x05\xa9;֗\x92\x9f\xa9\xf1\x00\x1dF\xcc\x1e{@k\x0e,\xde5\x9e\x90\xbe\xbdh\x0f\xd6Pn\xfc+\x10I\xb1\xe2N$\xe5\xe8DP6\x8e\xb6\xb7\x96Q/\x84\x96\x14%|\x1d\x9a\r\x90W'\x01\xe9k\xf72t\xb2\xbcH\xf7ގv\xcf§\aT\xb0\xaa\x19\x04\x03\x05\x8al\x88\xd8\xcd\xcbJ\x8fT\\Y\xa5͕!R\xed\xb2\x9d\x91\xe0V\xd5C\xa5\xbd\x14\xb7\x81\xb3\xdeD\xe6f\x1a\xf7\x12\xef\x13\x8d0p!u\xb0g*MR\x15\xeb\xe5\xd6\xf2\xe9\x91x\xcbgy\xf7/\x9f\x81Jr\xa0^\x87.\x8d\x06\xa1\xa8\xd9xv\xbbI\x81\x11\xe9\xe3\xbb\xe9}\x84\x16Rz\xfa\x97\xe0\xe5^\x89\x17\xf5\xa1<2\x1dxl\f\x11\xd32\x10\x9d\xaa\x8e\xb7Q\xd2\x0e\x14\xc0\xa1\xa3\xee\xbf\x02h)\x19\xa9r\xbf\xf8\x05\x14\xa1\xf3Yi\xfd\xe8\xf8\xa5\x97\xa5\x8d\x1c>84\x06X\x84\t\x14@$\xba\x81\xb5\x17\x84\xb6\xe2}A\xcd\x00\xb1A\xf0+\xab\xa9Pq\xa9\x85\xf9\x9c\x98j]K[O\xa9\xb8\xa5\x82!\xad\x97\xb4\xe5\x15i\x17-\x1a\xa9B}\xa6\xea\xcbz%\xc5;\x8e\xcfZ&\xe9b\xfb\xd9Z\xf0=x\xde\xc0\x00\x05%u\xc9\xfb\x16\xbcS6\x93\xbes\x1c\xd6Ƒ}L\xcb栭\x96&\x8c]\b\xba\xb4ZN\xb9-\xdae\x80r\xectԂj8\xc6\xfda3\x99j*a[\xbf\x0eu\xcdnT\xda_\x1dCPMd\xc4\f\x14\x19f\x80\xf6\xd7\xf4p\a\xd6e\xda_\x8b\x03\xa9&w\x02&\xdd\x10\xb1\xe9\x9b\x04k:V\x82\rֱ>\xbb\f\x04\x80\xe3\xee\x93\xfc\x99\x82\x92TS!vw\x99\x04\x04\x85w)\xd8\x14\x96\xda֬Ip\xa4\xfd\xc8$\xbb\xea\b\xc1L\xfa\xa2\xd8<(Ѹj}* \xa6v&\x98\a(&?\xe3\xd0\xfe\x1a\fG\xda\x05g\x9d\xb9\vQ(\xe2\xfe,\"\xdbF2b\xe8V\xe5V\x0e\xea\xa0+\xee\t9Q\x99\x99\x8c9\xf3>\x82Z\xa1\x19\x01n\x11\x11S\xeb!x\x8700\x9e\xf0\x10V\x80)\xfb\xef\xf7A\rS\xe2\xb1uY\x18c\x10\xe25-\xedʰ\x99\xf4\x96'\xdc\xe0\x96\xe7\xb58 @Bp\xc9+\xden\x9c[j}l=sn\x01\x15~\x8a\xa9d5,6\xba\x91)d5T\x97z\xa3¥\x1e6\xc6h\x15\xa7\xc6:\xb3mЕ\xf4\xab\xd0m\xde.\xba.\x94\x15\xef፳\x02C6\x86>D\x94\xed\xa3\fe\v\xc0\xa7\xab\x1e\x0ej\x06\xc0\x8a\x8e\xd7z\xceT\t+\xe2\xbb\t\x10\xd3\xf2\xb2~\x94\xa4\x9f\\r\xe5\xd4\xf9,O=\x7f\x90\x808\xd9y:ʱ\x1c\xa8\xb08\xf0\x98\x1d\xfe\xe4\xb5\xf0~B\v\xe3\xe4\x1e\x955\x7fޗ5m\t,\xe6\xb0\xe0\xe2\xe9H\x82>]\xa4\x82p\xdd\xdca\xc1M\xd5N\xa1\xdbp\x9aؾ\xc9\x19\xdd\t\x06\xf3\x8d~I\"dY3A+\x1bV䘉m\xee\xca\x05\xedJ\x04#\x87\x96\xba\x174˻\xff\x82f\xa0\xd2=\x1e|g0\bin\x0f\x0e\x8fX\xe9\x1c\xdfga\xc1,ʦ\a\x84\xef\xddV*\xc9\r\xc2&\xdaJ'\xfaBx}g\xf9\xb8C\x84\x8f\x10+\xbb\xdb+\xb2:\xb4\x9d\x06\x02Z\x12\x8cb/\x95\xc9O\xf2B<7F\xf6\xce\x06\x14\x1dN\xb4\xa3\x82\xb4%\xcaT\x92\xc1/~\x10\x14\xbf\x19\x86\x81\xd5\xfb<lJ̌=\x80\x84\x01\xa5l\xf5˕Ǌ\x85y]0\xe5x\x96\x17\x01@l&c\x1b\xc8\x05IL ٚ\xa5]9$[A\xcb\x15\xed\xde\x19\xb4\x04\x84\xf43\x1aoy\x90W\xefzΣڃCǶ\x9e\x83`\x1e}\x1aC\xc4\x02\x1dZv\xea=8\x05\xaa\x143SZ\xea\x8eg*\x8d\xb0\xdbX腋*\xcf(\x89dU酹9f\xc6BY.\xe0\xea\xdbCJ\xaf\xdd\xec\x8eŤ\xaa\a\x883\xbd\x95\x82K\xf5t\xa5\x95o\x91\xf0\xe6TIW;l+]\x94\xac\x17\xef\xa0\xdd\xf0Ў\x82w\xa59\xf6N\x96\x8a\xa5\x1dx\x18\x82>\x17d\x18\xf4\x01\xc8f\xd28~\xc1\x87w\x0f\"5[\xecU]\xf3~\xf9У\x93\xc0\xa6o\xc7#a-\r\\\x9e\xa1\n|=\t*\xd94\xb8\xb5t2\xf3\xfdA\x0ff\xaa\xba\xd8X\xa6\xaa\xa9\x11\xd4Go\x98\x92۹>\xear\xaa:\xb6.PX\x1f9\xab\xab\xf2\xc0\xfa\x9a\xf5͊>\xbaU\xd23\xbc\xb1\xcf\xe6\xa2\x04\x9f\xb2^\x87r\x93=\xa9\xaa%\xac\x9b<\xf3\x9aM\x03r\x83\x02\xc3\xe2[\xc4⣆\x8a͆\xd2_\x82\xf0Q\x89\x91\xb6\xbdE|\xbd[H\x0fDJ*\xb4q2D\xaa6p\x057\xa3\x05\x9b\xf4\xe6!\xcbA\xc6J\xec!\xb1q\xbc\xe8@\xf8Q\x97c&Ǔ\xdf\b\xa5o\xbd\xdd{\x18:Ǫt'\x91cf\xf2\xd6r\xa1\x97\x1bx\xdaZ\xbe7\xaa\xf5\x0e\x9f\xed~\xa5tE\xc3\xfe\x8f\x8f\x13\xdc`A\x8b\x8a\xe4ƁV\xe0'\xa8Rd\x13\xe1%\xb3\xdf\xcd\xd8'\x90\xf4k\x1e\xf6уp\x89o\x96ʚ|;\t\xf0\x9e\xf7H\xd4B\xb1\xddB\xcaT\xfcU-\xd3w\xb2z^\x10Ѹݯ\x15\x8d7\xc7\ay\x98\xb1\xb7k\xd5\xcf\x0fs1\xfd`\xdb9#\xc8\xf6a\xd6T\x17\xbb\xe2k\x1f\xed\x1eN,\xec\xf6\xcb\xfd\xc8yѸ_\xefo\xe8\xbe ݯ\xf67\xbc3vwO\x8bO\x1b\xd2{|\x13Z\x9c\x1a4\xed9ͽ\x0eE\x1a#\xbdݗ{\xf0~\xde&\xc9L?\xf8ю!\xa4e'dn\xbc\xec䅟\xed\xc1\xcf\xcem\xd85\x8a\x8c\xd4\a\x1ciO\u008f<LeZ\xf0x\xe2\xc4'\x13\xea\x8f\xf7\xa0\xfe\x7f\xe4U\b\xd5zK\x1bR\xdd\xfcN\xbcx\xfc\xf9=\xd3\xe2v\xf5^(w@/\x1e\x7fq\x17\x96\x93N\xf2\x93=@\x89\xd9&\xbb\x8cO<\x19击`\xe3\xf1\xf3]Sqw*\xcb\xf3\xe594'\xb7\xbc\x97ץ\xfc\x94\x97\xf7\xf2N\x9f\xac\xac\x97L;\x95\x9c\x14\x93\xb9N\xb993\xb9^w4\xa7&S_\x93Sn2\xb7WFFN橕\x98\xb0\xf3\xfd|\xd4\xdd\xc1\xe8\xcbrW\x99\f\x9fL\xafn+\xef\xe7\xd5\x0eu\xe9\xa8\x14\xac\x82[Ac\x88\xc8.\xfd o\x97j\xd4\xf4KT\xe6\x8e\xd2\xf8\x05\xed\xd5\xfb2\f\xc5\x10\xbe&e\xfaW\x06\x99\xf5\x92\x8a+\xd1\xf9,\x96\xbasg\x19\xf4\xf5\x7fw\xbf\vu#]+ӹ\xb2\x90\x97C\xcb \xbas\xd4e;\xbd۷\x9b7\x96\xfa\xafA_\xee\bVD\x1c\xd2L\xb4\xad\x89ʴ\x11\xc9\xd9n\x99k\nIo\xce5\xa2\xf58wZ\xb6\xe4\f\xbbLS\xb5/\xef.\xf3\x02\x9a\x99z\x97y\x94$e\xe6e\x9a\xab\x9cĽ]\xdd]Q\xe8=ת\xff鴿\xc0=I\xa1\xbex\xfc\xe9=\xa34m\xdf\vc\x12\x13w\xad\xe1μŀ\xf7\xae\xf0\xb6\xd3ɖB\xaa\xe3\xdfM\xebxf\xdeXF\xa0^\xbf\xc2\xd7\f.\xea\x0f\x0e\x8d\x01\xbf\x97\x03\b/׃q\xa4\x9e-\xd9\xd8\xf2\xe1\x04\x820\xbc$(\xe2\x06\x8c̄\xb7\r\xec\x14;\xf7\xa7\x8c3P\x81F\xe3\xf4j\x06\xf0?\x92n\x01\xde\xff\xaf\x0e\x11\xf4\xf4K_\xdaAk\x81\xf3nf9\x93\x1c\xbd\x96\xa5\xf9\xed3\x18\x17\xaca\xd0ɣ.\xef\xf3\x03fȤk^\x9a9\x9bA\x13\xefx\xb9\xc3O\xb8\xe0\xa5ٯ\x19r\xf7\xed\xce1\xa0\xb0M\xf5\xd5.-\xe2\x00\x1d\xe8\xc8\xe3\x9c2\f\xc7w\x8fx\x9eg\x90fA\x01\xfbI\xfce\x80\x12\x93\u05ee\x1f\x91\x1aCl\xa6Zo\xa1\x8dՉ\xd6\x17\x1d\xda?Y\xea\x9e\x0e\xba^r\xb3\xe6 g\x01\n\xd64:T\xdc\x18\"=c\xc2\u05cc\fSjr]\xdd\x03\x85\xcd\xcc'1\xa8\x16.ݠ\xa6\x1d\xb4\x168-\x84\x96f\xa5g\xd0\xff\x82\x95\x9e\xc17\xadt\x9aI\x9d\xc1\x9eФΠOjR\x1dا2\xa93\xe4n\x93:\xce\x1bǢm'N:\x1d\xd8\xfe)H?a%\x9cy\x03\xbf$\x18ɼ\xf1+\xa5'\xb6\xe1\xfcl\x1f\xa7\xa8\xd8p\xa2B\xd2G\x9d\x9e\xe2\xd0\xd3\xec\xd1\xc3Mj\a\x1dg\xcf \xa03=\x9b\x87\x80\xa3.\xc7R\xa6\x10\x02\x90Z#\xa0\xec\xa6\x02\xe1\\5,-\x89ԋ\x0f\xc5\xf0\xfb\x9a\t\xd5\x05\x93'\xa9\x89\xea\x9dj6B\xb5\xc52\xa8j\xe63\xab\xe9(\x05\xbf\xad\xfch\x9f\xfa\x9f\r\xf8\xbe\xf2\xffP\xde\x10T\x17ˎT\xc6\xfc\x1a\x12O$N\x9eE8:7\xb5\xac)\xfc\x12\xec\xd9e̫\xba\xad\xb8>&\xae{\x1eO\xd3\x1d\x1c~x\xb7\"ð\xf6\xe3\xbb\xe6g27~%\xd4\xfe\x1a\xe5\xd6OiF~\xd7\xd0|\x9d\x7f\xc4o\xf3\xe7\x0f\xdd\x1f^\x89\xfcڟ\x97#\x1d\xfb\x91\x96\x84\fV\xb7\x86\x9f\x95\x17MxM2&~\x1d\xb4\x8c)\xf6\xe7?\x00\x00\x00\xff\xff\x01\x00\x00\xff\xff#\x9b\rxcY\x00\x00"))
}
