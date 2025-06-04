package core_v1alpha

import (
	entity "miren.dev/runtime/pkg/entity"
	schema "miren.dev/runtime/pkg/entity/schema"
	types "miren.dev/runtime/pkg/entity/types"
)

const (
	AppActiveVersionId = entity.Id("dev.miren.core/app.active_version")
	AppDefaultId       = entity.Id("dev.miren.core/app.default")
	AppProjectId       = entity.Id("dev.miren.core/app.project")
)

type App struct {
	ID            entity.Id `json:"id"`
	ActiveVersion entity.Id `cbor:"active_version,omitempty" json:"active_version,omitempty"`
	Default       bool      `cbor:"default,omitempty" json:"default,omitempty"`
	Project       entity.Id `cbor:"project,omitempty" json:"project,omitempty"`
}

func (o *App) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(AppActiveVersionId); ok && a.Value.Kind() == entity.KindId {
		o.ActiveVersion = a.Value.Id()
	}
	if a, ok := e.Get(AppDefaultId); ok && a.Value.Kind() == entity.KindBool {
		o.Default = a.Value.Bool()
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
	attrs = append(attrs, entity.Bool(AppDefaultId, o.Default))
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
	if !entity.Empty(o.Default) {
		return false
	}
	if !entity.Empty(o.Project) {
		return false
	}
	return true
}

func (o *App) InitSchema(sb *schema.SchemaBuilder) {
	sb.Ref("active_version", "dev.miren.core/app.active_version", schema.Doc("The version of the project that should be used"))
	sb.Bool("default", "dev.miren.core/app.default", schema.Doc("Whether this is the default application for routing"), schema.Indexed)
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
	(&Config{}).InitSchema(sb.Builder("config"))
	sb.String("image_url", "dev.miren.core/app_version.image_url", schema.Doc("The OCI url for the versions code"))
	sb.String("manifest", "dev.miren.core/app_version.manifest", schema.Doc("The OCI image manifest for the version"))
	sb.String("manifest_digest", "dev.miren.core/app_version.manifest_digest", schema.Doc("The digest of the manifest"), schema.Indexed)
	sb.String("version", "dev.miren.core/app_version.version", schema.Doc("The version of this app"))
}

const (
	ConfigCommandsId    = entity.Id("dev.miren.core/config.commands")
	ConfigConcurrencyId = entity.Id("dev.miren.core/config.concurrency")
	ConfigEntrypointId  = entity.Id("dev.miren.core/config.entrypoint")
	ConfigPortId        = entity.Id("dev.miren.core/config.port")
	ConfigVariableId    = entity.Id("dev.miren.core/config.variable")
)

type Config struct {
	Commands    []Commands  `cbor:"commands,omitempty" json:"commands,omitempty"`
	Concurrency Concurrency `cbor:"concurrency,omitempty" json:"concurrency,omitempty"`
	Entrypoint  string      `cbor:"entrypoint,omitempty" json:"entrypoint,omitempty"`
	Port        int64       `cbor:"port,omitempty" json:"port,omitempty"`
	Variable    []Variable  `cbor:"variable,omitempty" json:"variable,omitempty"`
}

func (o *Config) Decode(e entity.AttrGetter) {
	for _, a := range e.GetAll(ConfigCommandsId) {
		if a.Value.Kind() == entity.KindComponent {
			var v Commands
			v.Decode(a.Value.Component())
			o.Commands = append(o.Commands, v)
		}
	}
	if a, ok := e.Get(ConfigConcurrencyId); ok && a.Value.Kind() == entity.KindComponent {
		o.Concurrency.Decode(a.Value.Component())
	}
	if a, ok := e.Get(ConfigEntrypointId); ok && a.Value.Kind() == entity.KindString {
		o.Entrypoint = a.Value.String()
	}
	if a, ok := e.Get(ConfigPortId); ok && a.Value.Kind() == entity.KindInt64 {
		o.Port = a.Value.Int64()
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
	if !o.Concurrency.Empty() {
		attrs = append(attrs, entity.Component(ConfigConcurrencyId, o.Concurrency.Encode()))
	}
	if !entity.Empty(o.Entrypoint) {
		attrs = append(attrs, entity.String(ConfigEntrypointId, o.Entrypoint))
	}
	if !entity.Empty(o.Port) {
		attrs = append(attrs, entity.Int64(ConfigPortId, o.Port))
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
	if !o.Concurrency.Empty() {
		return false
	}
	if !entity.Empty(o.Entrypoint) {
		return false
	}
	if !entity.Empty(o.Port) {
		return false
	}
	if len(o.Variable) != 0 {
		return false
	}
	return true
}

func (o *Config) InitSchema(sb *schema.SchemaBuilder) {
	sb.Component("commands", "dev.miren.core/config.commands", schema.Doc("The command to run for a specific service type"), schema.Many)
	(&Commands{}).InitSchema(sb.Builder("commands"))
	sb.Component("concurrency", "dev.miren.core/config.concurrency", schema.Doc("How to control the concurrency for the application"))
	(&Concurrency{}).InitSchema(sb.Builder("concurrency"))
	sb.String("entrypoint", "dev.miren.core/config.entrypoint", schema.Doc("The container entrypoint command"))
	sb.Int64("port", "dev.miren.core/config.port", schema.Doc("The TCP port to access, default to 3000"))
	sb.Component("variable", "dev.miren.core/config.variable", schema.Doc("A variable to be exposed to the app"), schema.Many)
	(&Variable{}).InitSchema(sb.Builder("variable"))
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
	ConcurrencyAutoId  = entity.Id("dev.miren.core/concurrency.auto")
	ConcurrencyFixedId = entity.Id("dev.miren.core/concurrency.fixed")
)

type Concurrency struct {
	Auto  int64 `cbor:"auto,omitempty" json:"auto,omitempty"`
	Fixed int64 `cbor:"fixed,omitempty" json:"fixed,omitempty"`
}

func (o *Concurrency) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(ConcurrencyAutoId); ok && a.Value.Kind() == entity.KindInt64 {
		o.Auto = a.Value.Int64()
	}
	if a, ok := e.Get(ConcurrencyFixedId); ok && a.Value.Kind() == entity.KindInt64 {
		o.Fixed = a.Value.Int64()
	}
}

func (o *Concurrency) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Auto) {
		attrs = append(attrs, entity.Int64(ConcurrencyAutoId, o.Auto))
	}
	if !entity.Empty(o.Fixed) {
		attrs = append(attrs, entity.Int64(ConcurrencyFixedId, o.Fixed))
	}
	return
}

func (o *Concurrency) Empty() bool {
	if !entity.Empty(o.Auto) {
		return false
	}
	if !entity.Empty(o.Fixed) {
		return false
	}
	return true
}

func (o *Concurrency) InitSchema(sb *schema.SchemaBuilder) {
	sb.Int64("auto", "dev.miren.core/concurrency.auto", schema.Doc("How to scale the application based on the node"))
	sb.Int64("fixed", "dev.miren.core/concurrency.fixed", schema.Doc("How concurrent requests this app can handle"))
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
	KindMetadata   = entity.Id("dev.miren.core/kind.metadata")
	KindProject    = entity.Id("dev.miren.core/kind.project")
	Schema         = entity.Id("dev.miren.core/schema.v1alpha")
)

func init() {
	schema.Register("dev.miren.core", "v1alpha", func(sb *schema.SchemaBuilder) {
		(&App{}).InitSchema(sb)
		(&AppVersion{}).InitSchema(sb)
		(&Artifact{}).InitSchema(sb)
		(&Metadata{}).InitSchema(sb)
		(&Project{}).InitSchema(sb)
	})
	schema.RegisterEncodedSchema("dev.miren.core", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\xacVݎ\xd3<\x10}\x8f\xef\xe3\x1f$\xa4\x95\b≪\xa9=I\xbdMl\xcbq\xb3\xed% ă\xc0\xf2\x86p\x8db\x8f\xdd\xd8M\x9c\xac\xc4\xcd\xfe$\xe7\x9c\x1c\x1f\x8fg\xfc\x93K\xe8Pr\x1c\xaaN\x18\x94\x15S\x06\x9b\x01M/\x94l\x86O\xd0\xea\x03\xe0QH\xde?>\xa4\xa8\x8f\xe3\xd3\n\xb4\xfeUsՁ\x90\x99\x8aS6\x19\a\xb4\x9eW\xffS\xd7\x02[\xde\x7f\xfd\xe1\x1d\x01\xb3b\xc0\x1da\xb9\xbdh\xdc\v\xbe\x17\xfc\xfc\xfaV\xb2J\xd1^\xa2\xe1Xé\xb5\x8e\xcb\xf7J\xb5#\xfb\xbf\x196\x01\x89\xa6\x8d\xbaGf\xa7\x9f\x9c#\x11\xac\xd5Ft`.\xbbq\x15\f\xb4>\xbf\\\x88)\xb8+\xc55\xf7\xa5]̫\x94\xdbw\xef~t0u\xfebYo\xf4\xe4I\a0VԐ\xae\xf9m\x89Ix_=5S\xb2\x16\x8d#\v\xa6:\xad$J\xbb\xb0UQÓ\xae\xf8b,\x99Pu+TN\xe7ۣ_(S]\a\x92\xf73^\xf3\xa4\xbcl\x15\x19\x1d\xc8\xcb\xef\x8dv\xefV\xedFݲ\xef/T\x93\x84v\xb6\xeb\xde\x1a!\x9b\xd1s^jA4\xa8\x13\xbbG3\b\x86\x9bلOJ;F\xe7\xf7\xfcȔd'cP\xb2ˆ\x8d\x8f\x8b\x8e\xa4\x8dI~ؒd\x14\xdd\x14&\x87\x93U\xce3\x13\xde\xedm\x10Q\xb1\x1a\xc1\x9e\x87\xb58#O\x88\xaf\nD\x87N\"\x9cf\xe6%\xefQZs\xd1JH\x9boΌ\xf4\xb8\xd8+\x81\x16\xa3\x95\xb1\x89\xa7\xbc\x83\x10q\xc4\xd1!\x18\xc0\bط\xb8\xfd\x10D\xc6?>\x04Aw\xd3H`G\xbc\xe4)\xfd\x9f\xf9\r\x82\xd5\x11)bѣ\xec\xc58\x1d\xd29\x90WhdF<\xed\xfa\x00\xed\xe9\xe6\xe8<_b;tzp»\xe4)\xf5L\xf2(:hpw2m\xfe\x9dw\x85\x06\x1aI\xd4\xc4;\x90\xa2\xc6\xfe\xa6\x92J\x8d<p\xbc\x84\n\xff\xee\xb8hf\x94\xee6(\x11\x95:\xcft|O\x84\xde\x14\x84\xe8wzr&\x80\xf3\xb3\xd9\xf9J\x13\xa9T\x91\xf9\xfd%p\xb6U_6X\xf3\xca\vb\x93\xa9\xba\xb4!\xf9юԧ\xed\xc6\xfb5\x19\xe2\xa5\xd5\x18P\xf31vh\x81\x83\x85\xa7\xc4\x188+1\xd2]\xa1\x85=\xb6~\xfe\xa2\xfb{\xae\xed\x04ɊЮ\xedP\xc3s?\xb2(\xf2\xb5D\xfe\b^\xbe\xd6\xe5}?\xd2\xe6\xeev\x87\xf0\xf6f\xeb]rD)\x057d<\xa2\x94s\xfbLmH=H4k\xeb&\xc5ʁ\x13\xf7a\xf9\xc7\xfe\xa0\x8c\xdd\xf9\v\xfdX\xd3K\x97\xfa\xe4ȭ]iW\xaa*\xbc\xbdFX,\xbe\xe0\xb5\x18\xf4_\x00\x00\x00\xff\xff\x01\x00\x00\xff\xff\xb8\xac\xad\x97\xb9\f\x00\x00"))
}
