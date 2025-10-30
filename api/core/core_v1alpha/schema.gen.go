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
	sb.Ref("app", "dev.miren.core/app_version.app", schema.Doc("The application the version is for"))
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
	sb.Int64("port", "dev.miren.core/config.port", schema.Doc("The TCP port to access, default to 3000"))
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
	ServicesImageId              = entity.Id("dev.miren.core/services.image")
	ServicesNameId               = entity.Id("dev.miren.core/services.name")
	ServicesServiceConcurrencyId = entity.Id("dev.miren.core/services.service_concurrency")
)

type Services struct {
	Image              string             `cbor:"image,omitempty" json:"image,omitempty"`
	Name               string             `cbor:"name,omitempty" json:"name,omitempty"`
	ServiceConcurrency ServiceConcurrency `cbor:"service_concurrency,omitempty" json:"service_concurrency,omitempty"`
}

func (o *Services) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(ServicesImageId); ok && a.Value.Kind() == entity.KindString {
		o.Image = a.Value.String()
	}
	if a, ok := e.Get(ServicesNameId); ok && a.Value.Kind() == entity.KindString {
		o.Name = a.Value.String()
	}
	if a, ok := e.Get(ServicesServiceConcurrencyId); ok && a.Value.Kind() == entity.KindComponent {
		o.ServiceConcurrency.Decode(a.Value.Component())
	}
}

func (o *Services) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Image) {
		attrs = append(attrs, entity.String(ServicesImageId, o.Image))
	}
	if !entity.Empty(o.Name) {
		attrs = append(attrs, entity.String(ServicesNameId, o.Name))
	}
	if !o.ServiceConcurrency.Empty() {
		attrs = append(attrs, entity.Component(ServicesServiceConcurrencyId, o.ServiceConcurrency.Encode()))
	}
	return
}

func (o *Services) Empty() bool {
	if !entity.Empty(o.Image) {
		return false
	}
	if !entity.Empty(o.Name) {
		return false
	}
	if !o.ServiceConcurrency.Empty() {
		return false
	}
	return true
}

func (o *Services) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("image", "dev.miren.core/services.image", schema.Doc("Optional container image for this service (e.g. postgres:16). If not specified, uses the app-level built image."))
	sb.String("name", "dev.miren.core/services.name", schema.Doc("The service name (e.g. web, worker)"))
	sb.Component("service_concurrency", "dev.miren.core/services.service_concurrency", schema.Doc("Concurrency configuration for this service"))
	(&ServiceConcurrency{}).InitSchema(sb.Builder("services.service_concurrency"))
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
	VariableValueId     = entity.Id("dev.miren.core/variable.value")
)

type Variable struct {
	Key       string `cbor:"key,omitempty" json:"key,omitempty"`
	Sensitive bool   `cbor:"sensitive,omitempty" json:"sensitive,omitempty"`
	Value     string `cbor:"value,omitempty" json:"value,omitempty"`
}

func (o *Variable) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(VariableKeyId); ok && a.Value.Kind() == entity.KindString {
		o.Key = a.Value.String()
	}
	if a, ok := e.Get(VariableSensitiveId); ok && a.Value.Kind() == entity.KindBool {
		o.Sensitive = a.Value.Bool()
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
	if !entity.Empty(o.Value) {
		return false
	}
	return true
}

func (o *Variable) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("key", "dev.miren.core/variable.key", schema.Doc("The name of the variable"))
	sb.Bool("sensitive", "dev.miren.core/variable.sensitive", schema.Doc("Whether or not the value is sensitive"))
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
	sb.Ref("app", "dev.miren.core/artifact.app", schema.Doc("The application the artifact is for"))
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
)

type DeployedBy struct {
	Timestamp string `cbor:"timestamp,omitempty" json:"timestamp,omitempty"`
	UserEmail string `cbor:"user_email,omitempty" json:"user_email,omitempty"`
	UserId    string `cbor:"user_id,omitempty" json:"user_id,omitempty"`
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
	return true
}

func (o *DeployedBy) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("timestamp", "dev.miren.core/deployed_by.timestamp", schema.Doc("When the deployment was initiated (RFC3339 format)"))
	sb.String("user_email", "dev.miren.core/deployed_by.user_email", schema.Doc("The email of the user who deployed"))
	sb.String("user_id", "dev.miren.core/deployed_by.user_id", schema.Doc("The ID of the user who deployed"))
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
	schema.RegisterEncodedSchema("dev.miren.core", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\xa4\x98\xdb\xd2\xdb&\x10\x80\x9f\xa3\xe76=\xa73j;\xbd\xe9U_\x85\xc1b%\xf1[\x02\x05\x90\xffߗ=M\xa7}\x8d\xb8y\xc3\xf6:\x03\v\x18c$\x91\xe4\xc6Â\xf6\x03v\x97eͅ\t:\x81`pj&\xae@4\xadT\x00G.\x98~\xf5x\xdb\xfb\xbd\xedm\xe8<\xbfr:*\x1b\xa5\xf3\x8cz\xffwLN\x94\x8b\f\xdau\x1cF\xa6\x7f{y\xe0\xec\xe9\xb3{冶\x86\x9f\x80\x9c@i.\x05\xae+\xeb3\xe7\x19\x0e\x9c9\xc4\xfb\x05Ĭ\xe4\x03\xb4\xc6\xe9\xf6A\xf0J\xbd\x87\xf4\xa7\x1f\xe98\x0ft\x9c\x15\x9f\xa8:\x13\xbb\xe8\x96\xce\xf3\xd3\a\xa5\xfdz\n\xee\xf9\x94}\xe1\ak\xf6\xfd\xab[\xf4\x87e@#\x1f\x05(7\x05`\xd3.\xba\xd3Fq\xd1o.<\xec\xf2\x8e\x8c\xceR\x86w4\xac>\xf7g\x18\xadY\xfe\xefn\xf9\xb9\x85\x02\xc1F\x85\x9b\xc2\xda\xf1\xc6K\x9f\xaeiLT\xf0\x0e4\xfaj\x88R\xb2o\xa7\xff\xf5\x9e>a\xbc\x0f\x18\x99w\xd6Zq\bز\x19'0\x94QC\xcbf\f\xa3Uf\xbc\xd8M}\xbcBhFz\x80Q\xb3\x89\x8a\xf3\x7fn\xae\xce\xf7؍\x80k\x17\xc3(\x02\xac\x0e\xbb\xfe\xe4\xd6\xfcdM\xef\xad\x0f\xce\x10\x10w\x9br\x96c0\x8f\xf2<\x81\xf0!\xf8\xf4^\xf6\xd5\xf5\x83\x1a\xf3\xfd\xe3v\xf1l\x95a\xe3\x90\xc4\xed\x0fQ\xca\xed\xf0\xe56!MAǴ#\xe7|\xb1\xce9,|dd\x94\xbdv\x98\x87D~\x03J;.ڀ\"\x9c!%\x91s\xcaW\x1b\x149\xcd#\x18`\x84\xa2\x8bǛ\x9e\x84tٱ\x0e6\x81\x91\xc3\x19\xad\x93vX\x0e\xb7d)@\x98k˻>\xc36el}2*\x9b\xcdA\x1a\xc3'ІN\x98\x95\xf8U\xac\x8b\x04\x84,\x1a\x14\x81\x89\xf2\x11\x8d\x9f\xc89\xa6\x1c\x92\t\xc6;\xb0\x0fBmfz\b\xa4ù\x98\x0f\x13#\x82RR\x91\t\xb4\xa6=\x1e\x80\xe9\xb6+\xf7\xf3\xc69\xea\xb9!\\t\x12\xcfQ\x94v<\xfcl\xdd\xc3\x01Q\xe3\u07bf_\x96\x92d 4t1\x83\xc4˲\xf3\xed\xdc\x1d\xab\xba\aEE;\xa0\xaeo\xe7\xba߭\xe9\xb6r\x9a\xb8!8e\x12\x17\xba4\x90S\xbf١\xde\x06\xec|כ\xf3\xf2{5\xf2\xb8&\x8c+\x83\xc7s\x88\x92\xd5g\a)\xc7\xe2=\x10\xb5\xd3\xe8\xe9\vqS\f\xf6\xa8\xad`\x96\x9a\x1b\xa9p\xf6\x87D\xce\x19y%\x11\x19z\xa0XI\xd8F\xae\xf5\xed\x9a֣TG.zb\x14\x00\x19\xa8F\x17\xbf\xb8ﮮ\xabzn,\xb9h\xae$\xae\xe7\x81j4\x17`s\xcfQ\x89\xae6\xd4,x=t\xbe\xfd\x86i\xc1b\xeeV\x17\xca\xf4pk\xf9\xc3Y\xa8\x97\xc3\x175\x87\xf2\xaf\xe2\xc1J \xeb5\xe0\xe7[J\xbe\xf0\xf2\x17v\x90\xbc\xfae\xe5\xbfBTo\xa5\xe8x\x8f&\xf4\xed\x9d\x1c\x95њ{Z\x8d5\xfe\xfc\xb7d\r\xd4wG\x9a\n\x96\xd6qC\xec\xdbY\xde\xf3\xdd\xe5E|\xfd\xbf\xad<B\x02!\xa0\xf0\xb8\aa\xafh\x8c\xda\x1aԉ\xb7>Y\x04\xa1\xba\xe4\x0e\x98\xe2)\xf1[\x05a\xd4y\x96\\`|<$r\xbe\xca<\xbc=a\x96\nu\x99kY\xad\x96\v\xb3\xe5>\xbf\x93\x1b\xf7žww_@\xd5\x17:\x1fe\xeb\f\x84\x86O!W\x036s\xa3\xe4\xff\x13\xa2\xe6\xfa\xff\x84K\xe9\x0e\x8cz\xbeAZ)\xdaE)\x10-\xa6z]\x1aر\xd5/\xf5\xb6*\xcd[c\xbf?\x8a5S\x01\xd6L\x92y[\xb8Vn\xc8\x1f*\x10b\x99\b\x17\xdaPa\x03\xc5\xd5_\xb7]!\xf8\x1c\xf1\xe7\n\xa2\x82\x17\vh\xa3\xc9l\x8bF\xcfq\xe4\xa5<t3\xc3O\x153薎@\x98|\x14\x84\xc1Hљ\xf3]o\xed\xa1V~\x8ed\x8a\xed$\x10\x1c\xbcu\x1eOTqz\x18!=\x8f\xb1\xef\xdd\xcfc@\xbd\xfd+H 4G@\x03\xb6\xb6\x91\x87P~\x8dE-\rBs\xc3O\xe8Y~\x15ok\xb6<\vD\xfd\x13\x1d\x17\x9f\x05\xb0Y\x9d\x82\x03b\xf3+\x7f\xaf\x16\xffr\xa5\xe6t\x19\x88,\n\x8bb~\x15sCl\x95\x03\x95\xafB\xcf+\x10\xb5\x0fCŒ6\x05\xa6\xaf\x01}\xe1%`\xd3z\xe9\xf3A\xfe\xe1Q\x0fR\x19\x82\x8f\xad\xb6jZ{p\x8d\x8f|[/\x94;oXa\xf4\xfa`\xb3\xf9ԕ\x16\x98;/;\xe9\x16w\x8b\xd1\xd7\x00\x00\x00\xff\xff\x01\x00\x00\xff\xff\xfd\xdc\x11\xccp\x16\x00\x00"))
}
