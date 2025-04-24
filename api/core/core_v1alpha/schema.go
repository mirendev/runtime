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

func (o *App) InitSchema(sb *schema.SchemaBuilder) {
	sb.Ref("active_version", "dev.miren.core/app.active_version", schema.Doc("The version of the project that should be used"))
	sb.Ref("project", "dev.miren.core/app.project", schema.Doc("The project that the app belongs to"))
}

const (
	AppVersionAppId           = entity.Id("dev.miren.core/app_version.app")
	AppVersionConcurrencyId   = entity.Id("dev.miren.core/app_version.concurrency")
	AppVersionConfigurationId = entity.Id("dev.miren.core/app_version.configuration")
	AppVersionImageUrlId      = entity.Id("dev.miren.core/app_version.image_url")
	AppVersionVersionId       = entity.Id("dev.miren.core/app_version.version")
)

type AppVersion struct {
	ID            entity.Id `json:"id"`
	App           entity.Id `cbor:"app,omitempty" json:"app,omitempty"`
	Concurrency   int64     `cbor:"concurrency,omitempty" json:"concurrency,omitempty"`
	Configuration []byte    `cbor:"configuration,omitempty" json:"configuration,omitempty"`
	ImageUrl      string    `cbor:"image_url,omitempty" json:"image_url,omitempty"`
	Version       string    `cbor:"version,omitempty" json:"version,omitempty"`
}

func (o *AppVersion) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(AppVersionAppId); ok && a.Value.Kind() == entity.KindId {
		o.App = a.Value.Id()
	}
	if a, ok := e.Get(AppVersionConcurrencyId); ok && a.Value.Kind() == entity.KindInt64 {
		o.Concurrency = a.Value.Int64()
	}
	if a, ok := e.Get(AppVersionConfigurationId); ok && a.Value.Kind() == entity.KindBytes {
		o.Configuration = a.Value.Bytes()
	}
	if a, ok := e.Get(AppVersionImageUrlId); ok && a.Value.Kind() == entity.KindString {
		o.ImageUrl = a.Value.String()
	}
	if a, ok := e.Get(AppVersionVersionId); ok && a.Value.Kind() == entity.KindString {
		o.Version = a.Value.String()
	}
}

func (o *AppVersion) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindAppVersion)
}

func (o *AppVersion) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.App) {
		attrs = append(attrs, entity.Ref(AppVersionAppId, o.App))
	}
	if !entity.Empty(o.Concurrency) {
		attrs = append(attrs, entity.Int64(AppVersionConcurrencyId, o.Concurrency))
	}
	if len(o.Configuration) == 0 {
		attrs = append(attrs, entity.Bytes(AppVersionConfigurationId, o.Configuration))
	}
	if !entity.Empty(o.ImageUrl) {
		attrs = append(attrs, entity.String(AppVersionImageUrlId, o.ImageUrl))
	}
	if !entity.Empty(o.Version) {
		attrs = append(attrs, entity.String(AppVersionVersionId, o.Version))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindAppVersion))
	return
}

func (o *AppVersion) InitSchema(sb *schema.SchemaBuilder) {
	sb.Ref("app", "dev.miren.core/app_version.app", schema.Doc("The application the version is for"))
	sb.Int64("concurrency", "dev.miren.core/app_version.concurrency", schema.Doc("How concurrent requests this app can handle"))
	sb.Bytes("configuration", "dev.miren.core/app_version.configuration", schema.Doc("The configuration of the application at this version"))
	sb.String("image_url", "dev.miren.core/app_version.image_url", schema.Doc("The OCI url for the versions code"))
	sb.String("version", "dev.miren.core/app_version.version", schema.Doc("The version of this app"))
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

func (o *Project) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Owner) {
		attrs = append(attrs, entity.String(ProjectOwnerId, o.Owner))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindProject))
	return
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
	schema.RegisterEncodedSchema("dev.miren.core", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\x84\x94\xddn\xa30\x10\x85\xdfcw\xb5\xab\xad\xaa\xaaWT}\"4\xd8\x03q\xc1?\x1a\x1c\x02\x97m\xd5'i\xd47l\xaf+cC\x80\x1a\xe7&\x89\u009c\x8f3g<>s\x05\x12\x15\xc7.\x93\x82PeL\x13V\x1dR+\xb4\xaa\xbaGh\xcc\x01\xb0\x16\x8a\xb7\xe7Ӻ\xea\xc1\xfd\x9b\x811\x1f%\xd7\x12\x84\xdaPF2m4`L\x9c\xfeU\x96\x02\x1b\u07be\xbc{G\xc0\xac\xe80\x0f\xb5\xdc\x0e\x06\v\xc1\v\xc1\xfb\xff?\x91ٺ\xda#*C\xfa\t\x99]j\x7fE\xb4\xa1\xac1$$А;;\f\x8c\xe9\xff\xed\xf4;\xbd&\xd5w\xecM\xf9\xdcx*\x807\xef\xde9X:\xff\xbb\xcfs\x9e\xbc\xa8fZ\xb1#\x11*6\x8cb&\x94u껄z\xa1\xf1\x14ɴ*Eu$\xb0S\xf4X\f\x16[G\xbaO\x93.:\xcf\x12BB\x85\xf9\x91\x9a\x91S\xb6\x96\x84\xaa\x1c\xe86\x01\x9aEa\x92\xcbS\xb0@\xdc$\x10\xe1{5\xd5zQ\xd0\xff\x89MW\xa2\x05\x0e\x16R\xa3ݮ\xc1\xa4I\x8f\xf5\xd5oZ\xd9@\x81M\xebC\x1d\x7fǆ;!\xb3P-A\r\x9f>\v\xff\xb1\tb\xdbˬw\xc5\xfb۰=\xe0\xb3,\xb6\x12\x87\xe9i\xff;\x96\\\x90\xa4\x82\xeb6\xba I\xe7\xf6\xec\xed\xa3>)\xa4k}\ab6\x16\xaf\xdcO\xed\xd7\xedA\x93\xcd\xfd\x85\xe6vl\xefR[\x9d\x95k7\xc1%\x9b䩚L$\x13\xfc\x06\x00\x00\xff\xff"))
}
