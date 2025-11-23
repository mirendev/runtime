package core_v1alpha

import (
	entity "miren.dev/runtime/pkg/entity"
	schema "miren.dev/runtime/pkg/entity/schema"
	types "miren.dev/runtime/pkg/entity/types"
)

const (
	AppActiveVersionId = entity.Id("dev.miren.core/app.active_version")
	AppProjectId       = entity.Id("dev.miren.core/app.project")
)

type App struct {
	ID            entity.Id `json:"id"`
	ActiveVersion entity.Id `cbor:"active_version,omitempty" json:"active_version,omitempty"`
	Project       entity.Id `cbor:"project,omitempty" json:"project,omitempty"`
}

func (o *App) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(AppActiveVersionId); ok && a.Value.Kind() == entity.KindId {
		o.ActiveVersion = a.Value.Id()
	}
	if a, ok := e.Get(AppProjectId); ok && a.Value.Kind() == entity.KindId {
		o.Project = a.Value.Id()
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
	if !entity.Empty(o.Project) {
		attrs = append(attrs, entity.Ref(AppProjectId, o.Project))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindApp))
	return
}

func (o *App) Empty() bool {
	if !entity.Empty(o.ActiveVersion) {
		return false
	}
	if !entity.Empty(o.Project) {
		return false
	}
	return true
}

func (o *App) InitSchema(sb *schema.SchemaBuilder) {
	sb.Ref("active_version", "dev.miren.core/app.active_version", schema.Doc("The version of the project that should be used"))
	sb.Ref("project", "dev.miren.core/app.project", schema.Doc("The project that the app belongs to"))
}

const (
	AppVersionAppId            = entity.Id("dev.miren.core/app_version.app")
	AppVersionArtifactId       = entity.Id("dev.miren.core/app_version.artifact")
	AppVersionConfigId         = entity.Id("dev.miren.core/app_version.config")
	AppVersionImageUrlId       = entity.Id("dev.miren.core/app_version.image_url")
	AppVersionManifestId       = entity.Id("dev.miren.core/app_version.manifest")
	AppVersionManifestDigestId = entity.Id("dev.miren.core/app_version.manifest_digest")
	AppVersionVersionId        = entity.Id("dev.miren.core/app_version.version")
)

type AppVersion struct {
	ID             entity.Id `json:"id"`
	App            entity.Id `cbor:"app,omitempty" json:"app,omitempty"`
	Artifact       entity.Id `cbor:"artifact,omitempty" json:"artifact,omitempty"`
	Config         Config    `cbor:"config,omitempty" json:"config,omitempty"`
	ImageUrl       string    `cbor:"image_url,omitempty" json:"image_url,omitempty"`
	Manifest       string    `cbor:"manifest,omitempty" json:"manifest,omitempty"`
	ManifestDigest string    `cbor:"manifest_digest,omitempty" json:"manifest_digest,omitempty"`
	Version        string    `cbor:"version,omitempty" json:"version,omitempty"`
}

func (o *AppVersion) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(AppVersionAppId); ok && a.Value.Kind() == entity.KindId {
		o.App = a.Value.Id()
	}
	if a, ok := e.Get(AppVersionArtifactId); ok && a.Value.Kind() == entity.KindId {
		o.Artifact = a.Value.Id()
	}
	if a, ok := e.Get(AppVersionConfigId); ok && a.Value.Kind() == entity.KindComponent {
		o.Config.Decode(a.Value.Component())
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
	if !entity.Empty(o.App) {
		attrs = append(attrs, entity.Ref(AppVersionAppId, o.App))
	}
	if !entity.Empty(o.Artifact) {
		attrs = append(attrs, entity.Ref(AppVersionArtifactId, o.Artifact))
	}
	if !o.Config.Empty() {
		attrs = append(attrs, entity.Component(AppVersionConfigId, o.Config.Encode()))
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
	if !entity.Empty(o.App) {
		return false
	}
	if !entity.Empty(o.Artifact) {
		return false
	}
	if !o.Config.Empty() {
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
	sb.Ref("app", "dev.miren.core/app_version.app", schema.Doc("The application the version is for"), schema.Indexed, schema.Tags("dev.miren.app_ref"))
	sb.Ref("artifact", "dev.miren.core/app_version.artifact", schema.Doc("The artifact to deploy for the version"))
	sb.Component("config", "dev.miren.core/app_version.config", schema.Doc("The configuration of the version"))
	(&Config{}).InitSchema(sb.Builder("app_version.config"))
	sb.String("image_url", "dev.miren.core/app_version.image_url", schema.Doc("The OCI url for the versions code"))
	sb.String("manifest", "dev.miren.core/app_version.manifest", schema.Doc("The OCI image manifest for the version"))
	sb.String("manifest_digest", "dev.miren.core/app_version.manifest_digest", schema.Doc("The digest of the manifest"), schema.Indexed)
	sb.String("version", "dev.miren.core/app_version.version", schema.Doc("The version of this app"))
}

const (
	ConfigCommandsId   = entity.Id("dev.miren.core/config.commands")
	ConfigEntrypointId = entity.Id("dev.miren.core/config.entrypoint")
	ConfigPortId       = entity.Id("dev.miren.core/config.port")
	ConfigServicesId   = entity.Id("dev.miren.core/config.services")
	ConfigVariableId   = entity.Id("dev.miren.core/config.variable")
)

type Config struct {
	Commands   []Commands `cbor:"commands,omitempty" json:"commands,omitempty"`
	Entrypoint string     `cbor:"entrypoint,omitempty" json:"entrypoint,omitempty"`
	Port       int64      `cbor:"port,omitempty" json:"port,omitempty"`
	Services   []Services `cbor:"services,omitempty" json:"services,omitempty"`
	Variable   []Variable `cbor:"variable,omitempty" json:"variable,omitempty"`
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
	ServiceConcurrency ServiceConcurrency `cbor:"service_concurrency,omitempty" json:"service_concurrency,omitempty"`
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
	sb.Component("service_concurrency", "dev.miren.core/services.service_concurrency", schema.Doc("Concurrency configuration for this service"))
	(&ServiceConcurrency{}).InitSchema(sb.Builder("services.service_concurrency"))
}

const (
	DisksFilesystemId   = entity.Id("dev.miren.core/disks.filesystem")
	DisksLeaseTimeoutId = entity.Id("dev.miren.core/disks.lease_timeout")
	DisksMountPathId    = entity.Id("dev.miren.core/disks.mount_path")
	DisksNameId         = entity.Id("dev.miren.core/disks.name")
	DisksReadOnlyId     = entity.Id("dev.miren.core/disks.read_only")
	DisksSizeGbId       = entity.Id("dev.miren.core/disks.size_gb")
)

type Disks struct {
	Filesystem   string `cbor:"filesystem,omitempty" json:"filesystem,omitempty"`
	LeaseTimeout string `cbor:"lease_timeout,omitempty" json:"lease_timeout,omitempty"`
	MountPath    string `cbor:"mount_path,omitempty" json:"mount_path,omitempty"`
	Name         string `cbor:"name,omitempty" json:"name,omitempty"`
	ReadOnly     bool   `cbor:"read_only,omitempty" json:"read_only,omitempty"`
	SizeGb       int64  `cbor:"size_gb,omitempty" json:"size_gb,omitempty"`
}

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
	sb.String("name", "dev.miren.core/disks.name", schema.Doc("The name of the disk entity to attach"))
	sb.Bool("read_only", "dev.miren.core/disks.read_only", schema.Doc("Whether to mount the disk as read-only"))
	sb.Int64("size_gb", "dev.miren.core/disks.size_gb", schema.Doc("Size in GB for auto-creating the disk if it doesn't exist"))
}

const (
	EnvKeyId       = entity.Id("dev.miren.core/env.key")
	EnvSensitiveId = entity.Id("dev.miren.core/env.sensitive")
	EnvSourceId    = entity.Id("dev.miren.core/env.source")
	EnvValueId     = entity.Id("dev.miren.core/env.value")
)

type Env struct {
	Key       string `cbor:"key,omitempty" json:"key,omitempty"`
	Sensitive bool   `cbor:"sensitive,omitempty" json:"sensitive,omitempty"`
	Source    string `cbor:"source,omitempty" json:"source,omitempty"`
	Value     string `cbor:"value,omitempty" json:"value,omitempty"`
}

func (o *Env) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(EnvKeyId); ok && a.Value.Kind() == entity.KindString {
		o.Key = a.Value.String()
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
	if !entity.Empty(o.Key) {
		attrs = append(attrs, entity.String(EnvKeyId, o.Key))
	}
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
	if !entity.Empty(o.Key) {
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
	sb.String("key", "dev.miren.core/env.key", schema.Doc("The name of the variable"))
	sb.Bool("sensitive", "dev.miren.core/env.sensitive", schema.Doc("Whether or not the value is sensitive"))
	sb.String("source", "dev.miren.core/env.source", schema.Doc("The source of the variable (config or manual). Defaults to config for backward compatibility."))
	sb.String("value", "dev.miren.core/env.value", schema.Doc("The value of the variable"))
}

const (
	ServiceConcurrencyModeId                = entity.Id("dev.miren.core/service_concurrency.mode")
	ServiceConcurrencyNumInstancesId        = entity.Id("dev.miren.core/service_concurrency.num_instances")
	ServiceConcurrencyRequestsPerInstanceId = entity.Id("dev.miren.core/service_concurrency.requests_per_instance")
	ServiceConcurrencyScaleDownDelayId      = entity.Id("dev.miren.core/service_concurrency.scale_down_delay")
)

type ServiceConcurrency struct {
	Mode                string `cbor:"mode,omitempty" json:"mode,omitempty"`
	NumInstances        int64  `cbor:"num_instances,omitempty" json:"num_instances,omitempty"`
	RequestsPerInstance int64  `cbor:"requests_per_instance,omitempty" json:"requests_per_instance,omitempty"`
	ScaleDownDelay      string `cbor:"scale_down_delay,omitempty" json:"scale_down_delay,omitempty"`
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
	return true
}

func (o *ServiceConcurrency) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("mode", "dev.miren.core/service_concurrency.mode", schema.Doc("The concurrency mode (auto or fixed)"))
	sb.Int64("num_instances", "dev.miren.core/service_concurrency.num_instances", schema.Doc("For fixed mode, number of instances to maintain"))
	sb.Int64("requests_per_instance", "dev.miren.core/service_concurrency.requests_per_instance", schema.Doc("For auto mode, number of concurrent requests per instance"))
	sb.String("scale_down_delay", "dev.miren.core/service_concurrency.scale_down_delay", schema.Doc("For auto mode, delay before scaling down idle instances (e.g. 2m, 15m)"))
}

const (
	VariableKeyId       = entity.Id("dev.miren.core/variable.key")
	VariableSensitiveId = entity.Id("dev.miren.core/variable.sensitive")
	VariableSourceId    = entity.Id("dev.miren.core/variable.source")
	VariableValueId     = entity.Id("dev.miren.core/variable.value")
)

type Variable struct {
	Key       string `cbor:"key,omitempty" json:"key,omitempty"`
	Sensitive bool   `cbor:"sensitive,omitempty" json:"sensitive,omitempty"`
	Source    string `cbor:"source,omitempty" json:"source,omitempty"`
	Value     string `cbor:"value,omitempty" json:"value,omitempty"`
}

func (o *Variable) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(VariableKeyId); ok && a.Value.Kind() == entity.KindString {
		o.Key = a.Value.String()
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
	if !entity.Empty(o.Key) {
		attrs = append(attrs, entity.String(VariableKeyId, o.Key))
	}
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
	if !entity.Empty(o.Key) {
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
	sb.String("key", "dev.miren.core/variable.key", schema.Doc("The name of the variable"))
	sb.Bool("sensitive", "dev.miren.core/variable.sensitive", schema.Doc("Whether or not the value is sensitive"))
	sb.String("source", "dev.miren.core/variable.source", schema.Doc("The source of the variable (config or manual). Defaults to config for backward compatibility."))
	sb.String("value", "dev.miren.core/variable.value", schema.Doc("The value of the value"))
}

const (
	ArtifactAppId            = entity.Id("dev.miren.core/artifact.app")
	ArtifactManifestId       = entity.Id("dev.miren.core/artifact.manifest")
	ArtifactManifestDigestId = entity.Id("dev.miren.core/artifact.manifest_digest")
)

type Artifact struct {
	ID             entity.Id `json:"id"`
	App            entity.Id `cbor:"app,omitempty" json:"app,omitempty"`
	Manifest       string    `cbor:"manifest,omitempty" json:"manifest,omitempty"`
	ManifestDigest string    `cbor:"manifest_digest,omitempty" json:"manifest_digest,omitempty"`
}

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
	return true
}

func (o *Artifact) InitSchema(sb *schema.SchemaBuilder) {
	sb.Ref("app", "dev.miren.core/artifact.app", schema.Doc("The application the artifact is for"), schema.Indexed, schema.Tags("dev.miren.app_ref"))
	sb.String("manifest", "dev.miren.core/artifact.manifest", schema.Doc("The OCI image manifest for the version"))
	sb.String("manifest_digest", "dev.miren.core/artifact.manifest_digest", schema.Doc("The digest of the manifest"), schema.Indexed)
}

const (
	DeploymentAppNameId      = entity.Id("dev.miren.core/deployment.app_name")
	DeploymentAppVersionId   = entity.Id("dev.miren.core/deployment.app_version")
	DeploymentBuildLogsId    = entity.Id("dev.miren.core/deployment.build_logs")
	DeploymentClusterIdId    = entity.Id("dev.miren.core/deployment.cluster_id")
	DeploymentCompletedAtId  = entity.Id("dev.miren.core/deployment.completed_at")
	DeploymentDeployedById   = entity.Id("dev.miren.core/deployment.deployed_by")
	DeploymentErrorMessageId = entity.Id("dev.miren.core/deployment.error_message")
	DeploymentGitInfoId      = entity.Id("dev.miren.core/deployment.git_info")
	DeploymentPhaseId        = entity.Id("dev.miren.core/deployment.phase")
	DeploymentStatusId       = entity.Id("dev.miren.core/deployment.status")
)

type Deployment struct {
	ID           entity.Id  `json:"id"`
	AppName      string     `cbor:"app_name,omitempty" json:"app_name,omitempty"`
	AppVersion   string     `cbor:"app_version,omitempty" json:"app_version,omitempty"`
	BuildLogs    string     `cbor:"build_logs,omitempty" json:"build_logs,omitempty"`
	ClusterId    string     `cbor:"cluster_id,omitempty" json:"cluster_id,omitempty"`
	CompletedAt  string     `cbor:"completed_at,omitempty" json:"completed_at,omitempty"`
	DeployedBy   DeployedBy `cbor:"deployed_by,omitempty" json:"deployed_by,omitempty"`
	ErrorMessage string     `cbor:"error_message,omitempty" json:"error_message,omitempty"`
	GitInfo      GitInfo    `cbor:"git_info,omitempty" json:"git_info,omitempty"`
	Phase        string     `cbor:"phase,omitempty" json:"phase,omitempty"`
	Status       string     `cbor:"status,omitempty" json:"status,omitempty"`
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
	if !entity.Empty(o.Owner) {
		return false
	}
	return true
}

func (o *Project) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("owner", "dev.miren.core/project.owner", schema.Doc("The email address of the project owner"))
}

var (
	KindApp        = entity.Id("dev.miren.core/kind.app")
	KindAppVersion = entity.Id("dev.miren.core/kind.app_version")
	KindArtifact   = entity.Id("dev.miren.core/kind.artifact")
	KindDeployment = entity.Id("dev.miren.core/kind.deployment")
	KindMetadata   = entity.Id("dev.miren.core/kind.metadata")
	KindProject    = entity.Id("dev.miren.core/kind.project")
	Schema         = entity.Id("dev.miren.core/schema.v1alpha")
)

func init() {
	schema.Register("dev.miren.core", "v1alpha", func(sb *schema.SchemaBuilder) {
		(&App{}).InitSchema(sb)
		(&AppVersion{}).InitSchema(sb)
		(&Artifact{}).InitSchema(sb)
		(&Deployment{}).InitSchema(sb)
		(&Metadata{}).InitSchema(sb)
		(&Project{}).InitSchema(sb)
	})
	schema.RegisterEncodedSchema("dev.miren.core", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\xa4Yɒ\xe4&\x13~\x8d\xff\xf7\xee\xf1>\x8e\x90\xc7\xe1\x8bO~\x15\x82\x12)\x89*\t4\x80\xd4]\xbey_^\xa3{\xe6\r\xed\xb3\x83U\x88B\x12=s\xa9 A\xdfG\x92\x99$\t\xf5H\x18\x1e\x80\x11\x98\xab\x81\n`U\xcd\x05\xc0\x852\"_߭{\xbfѽ\x15\x1e\xc7\xd7\x06#\x92Q<\x8e\x16\xf7oC\xf8\x80)KH\x9b\x86BO\xe4\xcf\x0f'J\xee?\xbe\x05W\xb8Vt\x064\x83\x90\x943\xabWҧ\xae#\x9c(1\x14\xefd(F\xc1\xcfP+\x83m\xbd\xe0@\xad#i\xe7oq?v\xb8\x1f\x05\x1d\xb0\xb8\"\xadt\x8d\xc7\xf1\xfe\xdd\xdcz\x1d\x8b]\xf3\x9c|\xe1\x06K\xd6\xfd\x93Q\xfa\xbd<A\xc5\xef\x18\b3\x05ئV\xba\x91JP\xd6\xee*\xeeWy\xc3l\x9d%\x14m\xb0\xd7>\xf5\xa7\x1f-Q\xff\x17\xa3~j!Ϡ\xa3\xc2L\xa1\xed\xb8\xf2\xd2G[\x88\x013ڀ\xb4\xbe\xea\x82\x14\xad\xdb\xe0\xbf8\xc2#B[O\xc3\xd3\xceR+v\x9e6o\xc6\x01\x14&X\xe1\xbc\x19\xfdh\x91\x19\x1f\xf5\xa2>\xd8`\xa8z|\x82^\x92\x01\xb3\xeb?f\xae\xc6\xf5腀ig\xc3(\x10h\fY~Rk~\xb8\x85{\xe3\x8d\xd3y\x8a\x9bE\x19\xcb\x11\x18{~\x1d\x80\xb9\x10\xbc\xff\x7f\xf2\xd5\xf2A\x89\xf9\xfe6\xabx\xb6ɡ\xe3\x10\x85\xe5wAJ\xed\xf0\xd9>C\x9c\x82.qG\xca\xf3\xe96\xcfi\xa2=A=o\xa5\xa19G\xf2\x13X\xea~\x92\n\x04\xa2ĲDr\xca\xf2\xf9\x0e\v\x1f\xc6\x1e\x14\x10\x84\xad\x8b\xfbUO\xc4\xf4x`\x1d\xdb\x04\x82NWk\x9d\xb8C\xf3P\xcd\xcc\x190\xb5\xb4\x9c\xeb\x13\xda*O[\x12\x06\xbf\xee\x98͐T\x8a\x0e \x15\x1elV\xa2\x8bX\x16\t\x96d\x92 \x10\f\x98\xf6\xd6\xf8\x91\x9c\xd2\xe4C2\xa2q\x0el\xbdP\x16\x03\x11A\x88j\xba\x88\xa5\xe9\xed\xec\xd9N\xd7lR\x8d<\x01Bp\x81\x06\x90\x12\xb7v\xbeaݕ\x06\xcb\xcefl\xa9B\x945\xdcn\xc6 \x1d\x84ɳ\xed0\xf1\x14%1\xf2\xd7C.\xd3z\x86\nO\xaa\xe3\xf6\xc4m\\;u\xc9&\xf6$0\xab;\x8bu\xed\x14\xfb\xf5\x16\xb6\xe6\xc3@\x15\xb2SF\xc1%s\x03)\xeb\x97\a\xac\xeb\xa8\x1fozS\xbe\xf4p\x0e|T\"B\x85\xb2{\xbc\v\x92Ɠ\x13\xe7}\xf60\t\xe88z\xdaL\xdcdwL@\v\x18\xb9\xa4\x8a\v;\xfb9\x92S\x8e\xb4\x1c\t\x1c\xb2ö\x1cэ\x14\xf5\xd5\x16ꎋ\ve-R\x02\x00uXZ\x17\xbf\xbc\xed..\xceZ\xaa4s\xd6\\Q\\\x8f\x1d\x96\xd6\\`\x9bG\x8e\x8a\xb0Ra5\xd93\xa6q\xed'\xa6\x05Ms\xa3\x9d\xaf\xf5\xfd\xd1\xe76g\xa6\xe8\xf6_\x94l\xca?\xb2\x1b+\"\xd9.$?\xd9\x03\xb9\xea͝\xfa^r\xf0Ǎ\vG\x80ל5\xb4\xb5&t\xed\x83\x1c\x95\xb0U\xb7l%\xd6\xf8\xedU\xce\x1a\x16o\xb64f$.\x06\xbb\xd0w\xa0\xde\xf3C\xf5\x02}\xf9\x95-\x8d\x10\xcf\xe0\xa9\xecv\xf7\xc2Q\xe5\x19\xd0\x12\xc4Lk\x97,\xbcP\\\xb7{\x9a\xec.qK\x05\xa6\xc4u\xe4\x94\xd9\xf88Gr\xaae\x1aގa\xe4\xc2b\x89iiTM\x99\xdas\x9f[\xc9\xca}\xa1\xef\xed\xdd\xe7\xa9J\xdc\xf7\xa7\xd1\xf3\xfdDO\xcfP\x11*/\xb1\x9a`;\x0et|Q\xae\xa3\x9d\xa1D\xd3\xdf\xf3\x89Rë\x86\xf6 \xafR\xc1`\xbd\x18ɇŘ!\xe8\x01K0\x87!\x9f\xac7\x87u\xd7Q\xc8Z\x9a\x81OL\xa1\x11+{:\x9c#9%\xb8\xb9\xeb\x18\x82\x83+Z\x1aO\x16$\x00\x13\xc4Yo\xcfD\xba\x88\xeb#9\xbd\x17Z\xb0\xa4?\x02jOn\x8b9\xc1\a\xf1\xee\xfe\xb2\xb1\xf0*w\xd6\x06\xef\x02\x9b\xa3詵x\x10;\xd5\x13b\a\xd8\\|#H_g\x80\xcd\xd5\x05\xac\xc9j\xddHm\x9d\x9aK\x03$0I\x15\x9d]\xb1\xbd\x88kK\xa7\xbe5P>\t\x97\xc9\x1a\xd7Ng\xfc_\x066\xe3~r\xa7\xbfm\x96f?m\xeb\x87ݽM\a_\x87\x81m\x1eY  \x0f\xa2t\x13\xb7\x91*\xb3\x8f~+Pt\xc1Y\xc4t\xda}\x06㡅\xc1\x88\xe9u%-\xcc\x03\x83k\xa0\x9a\xb3z\x12\x02Xm\x03G\xe6\x06\x0e\x02\xfc\x87'\x04x\x86\xbe\xfc\n\x9c^\xe42d\xd5\xc0\x89s\xa2i\xa5&}Q@\xc1\xa6\x01Q&\x15f\xfa\xf42\xa9sݵr\xf3\xf7\x05\x8c\x02^N \x95D\xa3\xbe\x0e;\x1e\xc3<\xe5\x87V3|W0\x83\xacq\x0f\x88\xf0;\x86\b\xf4\xd8:s\xbc\xe9-\xddk\xc2\xcd\x11M\xb1_\x99x\a\xef\x15\t3\x16\x14\x9fz\x88\x8b\x84\xd0\xf7\xf6E\x82\xa7*\x8f\xa74\xc9{\x86\xfd4\x9a\xeeʀ*̥\xa9u\x16\xfcqBMS_\xc0\xbeqV\r\x1e\xd8\xfd\xca]\x14\xb2o7\xb1+L\xdaE\x93\xb0\xb7|\xba\x88\xe9B\xf6\xee7\x85o\xe5\xcf\v(J\x9f˳\x85TL\x18\xbf\x91\xb6\x99\xf7\xd1]\xebŏ\xaa\xe9\x87\x17\xd9\xe9\xecm\xff\x82\xd2\xd7\xc0\xad\xbf\xa1\xc2_\x1f{\xff\xdb\x1c\xbc\xec\xfb\xd1\xe5\x19{\xf7\x0f\x80\xf8\xc6|\xf0\xde\x1d/\xf1\xf0v\xfd\x1f\x00\x00\x00\xff\xff\x01\x00\x00\xff\xff\xcek\xaf\x1a\x86\x1b\x00\x00"))
}
