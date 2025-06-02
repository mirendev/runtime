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
	AppVersionConfigId         = entity.Id("dev.miren.core/app_version.config")
	AppVersionImageUrlId       = entity.Id("dev.miren.core/app_version.image_url")
	AppVersionManifestId       = entity.Id("dev.miren.core/app_version.manifest")
	AppVersionManifestDigestId = entity.Id("dev.miren.core/app_version.manifest_digest")
	AppVersionVersionId        = entity.Id("dev.miren.core/app_version.version")
)

type AppVersion struct {
	ID             entity.Id `json:"id"`
	App            entity.Id `cbor:"app,omitempty" json:"app,omitempty"`
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
	KindMetadata   = entity.Id("dev.miren.core/kind.metadata")
	KindProject    = entity.Id("dev.miren.core/kind.project")
	Schema         = entity.Id("dev.miren.core/schema.v1alpha")
)

func init() {
	schema.Register("dev.miren.core", "v1alpha", func(sb *schema.SchemaBuilder) {
		(&App{}).InitSchema(sb)
		(&AppVersion{}).InitSchema(sb)
		(&Metadata{}).InitSchema(sb)
		(&Project{}).InitSchema(sb)
	})
	schema.RegisterEncodedSchema("dev.miren.core", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\xacV˒\xdb \x10\xfc\x8f\xbc_\xa7\xad\x8aR\xf9\"\x17\x86\x91<k\t(\x84\xb5\xd61I%?\x92M\xfe09\xa7\x04\x03+\x90\x84u\xc8e\x1fvw\xd3\xd3\xcc\x00\x8fB\xb2\x0e\xa4\x80\xa1\xeaЀ\xac\xb82\xd0\f`zT\xb2\x19>\xb3V\x9f\x18\x9cQ\x8a\xfe\xf1!E}\x9a>\xad\x98ֿk\xa1:\x862Sq\xca&\xe30\xad\xd7\xd5\xff\xd65B+\xfa\xaf?\xbd#\xc6-\x0ep \xac\xb0\xa3\x86#\x8a#\x8a\xebۥd\x95\xa2\xbdD\xa3\x8d\xba\an\xe7\xdcg+\\\x82\xb5\xda`\xc7\xccx\x98\xecp\xa6\xf5\xf5\xf5F\xbda\x99R\xddk+\x1db\xe1\xa5\x00~x\xf7\x93\x83\xb9\xf3W\xdbz\x93'\xbf\x8f5W\xb2\xc6\xc6\xf1\x90\xabN+\t\xd2n\x84\x16\xe9\x9e\xf4\x84/֕\tUK\xa1ry\xdf\x7f9\x9d\x13W]Ǥ\xe8W\xbc\xe6\xa5z\xd9*2:&\xc7?;\xed\xdeݴ\x1buw\xf5eChg\xbb\xee\xadA\xd9L\x9e\xf3^\t\xa2A\x9d\xd8=\x98\x019\xecf\x13>\xe9\xcd\x18\x9d\xdf\xf33W\x92_\x8c\x01\xc9\xc7\x1d\x1b\x1f\x8b\x8e\xa4\x9dI~ܓd\x14\xdd\x15\xa6`\x17\xab\x9cg\x8e\xde\xed2\x88\xa8XM`σ\x1a\xaf \x12\xe2\x9b\x02ѡ\x93\b\xe7\x99y\xc9{\x90\u058cZ\xa1\xb4\xf9\xe6\xacHO\xc5>\x11\xa8\x18\xad\x8cM<\xe5G\x00\x11'\x1c\r\xc1\xc0\f\xb2c\v\xfb\x87 2\xfe\xf3\x10\x04\xdd\xf2\xbe}\xa3\xb3\xe9\fc\x9e\xd2\xf3\xcco\x10\xac\xce@\x11c\x0f\xb2\xc7\xe9\x9cv\\qT\xaa]\xeb\xd0Ȍx\xda\xf5\x81\xb5\x97\xc5\xe8\xbc\xdcb;t:8\xe1\xbb\xe4S:3\xc9#v\xac\x81\xc3Ŵ\xf9:\x1f\n\ah$y\x91S\xc7$\xd6\xd0/:\xe9}A#p\xbc\x84\n\xff\x1e\x046+Jw;\x94\x88J'\xcf\xfc\"\x9d\t\xbd+\b\xd1\xeftrf\x80답\v\xb2\x03\xcb\x04\xb3\xacԑ\xf9K\"pnt\x1f]r-;B\xeb/\x0ep\x7f\xaf\xcdK\x90\xac\b\xed\xe6\x85&\xd5\xfdȂ\xc8k\x89\xfc\t\xbc\xfd\xa0\xc8\x0f\xacH[{U\x9c·\x8biq\xc9\x11\xa5\x14ܐ\xf1\x88R\xce\xed\v͏z\x90`n\xd5M\x8a\x95\x03'\xeeC\xf9\xe7\xfe\xa4\x8c=\xf87a\xd2\x0f\xb7\x1eLO\xf5\x17;',TLiz\x1fm=H\xff\x01\x00\x00\xff\xff"))
}
