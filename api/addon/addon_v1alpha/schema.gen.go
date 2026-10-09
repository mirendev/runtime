package addon_v1alpha

import (
	entity "miren.dev/runtime/pkg/entity"
	schema "miren.dev/runtime/pkg/entity/schema"
)

const (
	AddonDefaultVariantId = entity.Id("dev.miren.addon/addon.default_variant")
	AddonDefaultVersionId = entity.Id("dev.miren.addon/addon.default_version")
	AddonDescriptionId    = entity.Id("dev.miren.addon/addon.description")
	AddonDisplayNameId    = entity.Id("dev.miren.addon/addon.display_name")
	AddonLocalityModeId   = entity.Id("dev.miren.addon/addon.locality_mode")
	AddonNameId           = entity.Id("dev.miren.addon/addon.name")
	AddonVariantsId       = entity.Id("dev.miren.addon/addon.variants")
)

type Addon struct {
	ID             entity.Id  `json:"id"`
	DefaultVariant string     `cbor:"default_variant,omitempty" json:"default_variant,omitempty"`
	DefaultVersion string     `cbor:"default_version,omitempty" json:"default_version,omitempty"`
	Description    string     `cbor:"description,omitempty" json:"description,omitempty"`
	DisplayName    string     `cbor:"display_name,omitempty" json:"display_name,omitempty"`
	LocalityMode   string     `cbor:"locality_mode,omitempty" json:"locality_mode,omitempty"`
	Name           string     `cbor:"name,omitempty" json:"name,omitempty"`
	Variants       []Variants `cbor:"variants,omitempty" json:"variants,omitempty"`
}

func (o *Addon) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(AddonDefaultVariantId); ok && a.Value.Kind() == entity.KindString {
		o.DefaultVariant = a.Value.String()
	}
	if a, ok := e.Get(AddonDefaultVersionId); ok && a.Value.Kind() == entity.KindString {
		o.DefaultVersion = a.Value.String()
	}
	if a, ok := e.Get(AddonDescriptionId); ok && a.Value.Kind() == entity.KindString {
		o.Description = a.Value.String()
	}
	if a, ok := e.Get(AddonDisplayNameId); ok && a.Value.Kind() == entity.KindString {
		o.DisplayName = a.Value.String()
	}
	if a, ok := e.Get(AddonLocalityModeId); ok && a.Value.Kind() == entity.KindString {
		o.LocalityMode = a.Value.String()
	}
	if a, ok := e.Get(AddonNameId); ok && a.Value.Kind() == entity.KindString {
		o.Name = a.Value.String()
	}
	for _, a := range e.GetAll(AddonVariantsId) {
		if a.Value.Kind() == entity.KindComponent {
			var v Variants
			v.Decode(a.Value.Component())
			o.Variants = append(o.Variants, v)
		}
	}
}

func (o *Addon) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindAddon)
}

func (o *Addon) ShortKind() string {
	return "addon"
}

func (o *Addon) Kind() entity.Id {
	return KindAddon
}

func (o *Addon) EntityId() entity.Id {
	return o.ID
}

func (o *Addon) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.DefaultVariant) {
		attrs = append(attrs, entity.String(AddonDefaultVariantId, o.DefaultVariant))
	}
	if !entity.Empty(o.DefaultVersion) {
		attrs = append(attrs, entity.String(AddonDefaultVersionId, o.DefaultVersion))
	}
	if !entity.Empty(o.Description) {
		attrs = append(attrs, entity.String(AddonDescriptionId, o.Description))
	}
	if !entity.Empty(o.DisplayName) {
		attrs = append(attrs, entity.String(AddonDisplayNameId, o.DisplayName))
	}
	if !entity.Empty(o.LocalityMode) {
		attrs = append(attrs, entity.String(AddonLocalityModeId, o.LocalityMode))
	}
	if !entity.Empty(o.Name) {
		attrs = append(attrs, entity.String(AddonNameId, o.Name))
	}
	for _, v := range o.Variants {
		attrs = append(attrs, entity.Component(AddonVariantsId, v.Encode()))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindAddon))
	return
}

func (o *Addon) Empty() bool {
	if !entity.Empty(o.DefaultVariant) {
		return false
	}
	if !entity.Empty(o.DefaultVersion) {
		return false
	}
	if !entity.Empty(o.Description) {
		return false
	}
	if !entity.Empty(o.DisplayName) {
		return false
	}
	if !entity.Empty(o.LocalityMode) {
		return false
	}
	if !entity.Empty(o.Name) {
		return false
	}
	if len(o.Variants) != 0 {
		return false
	}
	return true
}

func (o *Addon) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("default_variant", "dev.miren.addon/addon.default_variant")
	sb.String("default_version", "dev.miren.addon/addon.default_version")
	sb.String("description", "dev.miren.addon/addon.description")
	sb.String("display_name", "dev.miren.addon/addon.display_name")
	sb.String("locality_mode", "dev.miren.addon/addon.locality_mode")
	sb.String("name", "dev.miren.addon/addon.name", schema.Indexed)
	sb.Component("variants", "dev.miren.addon/addon.variants", schema.Many)
	(&Variants{}).InitSchema(sb.Builder("addon.variants"))
}

const (
	VariantsDescriptionId = entity.Id("dev.miren.addon/variants.description")
	VariantsDetailsId     = entity.Id("dev.miren.addon/variants.details")
	VariantsNameId        = entity.Id("dev.miren.addon/variants.name")
)

type Variants struct {
	Description string    `cbor:"description,omitempty" json:"description,omitempty"`
	Details     []Details `cbor:"details,omitempty" json:"details,omitempty"`
	Name        string    `cbor:"name,omitempty" json:"name,omitempty"`
}

func (o *Variants) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(VariantsDescriptionId); ok && a.Value.Kind() == entity.KindString {
		o.Description = a.Value.String()
	}
	for _, a := range e.GetAll(VariantsDetailsId) {
		if a.Value.Kind() == entity.KindComponent {
			var v Details
			v.Decode(a.Value.Component())
			o.Details = append(o.Details, v)
		}
	}
	if a, ok := e.Get(VariantsNameId); ok && a.Value.Kind() == entity.KindString {
		o.Name = a.Value.String()
	}
}

func (o *Variants) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Description) {
		attrs = append(attrs, entity.String(VariantsDescriptionId, o.Description))
	}
	for _, v := range o.Details {
		attrs = append(attrs, entity.Component(VariantsDetailsId, v.Encode()))
	}
	if !entity.Empty(o.Name) {
		attrs = append(attrs, entity.String(VariantsNameId, o.Name))
	}
	return
}

func (o *Variants) Empty() bool {
	if !entity.Empty(o.Description) {
		return false
	}
	if len(o.Details) != 0 {
		return false
	}
	if !entity.Empty(o.Name) {
		return false
	}
	return true
}

func (o *Variants) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("description", "dev.miren.addon/variants.description")
	sb.Component("details", "dev.miren.addon/variants.details", schema.Many)
	(&Details{}).InitSchema(sb.Builder("variants.details"))
	sb.String("name", "dev.miren.addon/variants.name")
}

const (
	DetailsKeyId   = entity.Id("dev.miren.addon/details.key")
	DetailsValueId = entity.Id("dev.miren.addon/details.value")
)

type Details struct {
	Key   string `cbor:"key,omitempty" json:"key,omitempty"`
	Value string `cbor:"value,omitempty" json:"value,omitempty"`
}

func (o *Details) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(DetailsKeyId); ok && a.Value.Kind() == entity.KindString {
		o.Key = a.Value.String()
	}
	if a, ok := e.Get(DetailsValueId); ok && a.Value.Kind() == entity.KindString {
		o.Value = a.Value.String()
	}
}

func (o *Details) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Key) {
		attrs = append(attrs, entity.String(DetailsKeyId, o.Key))
	}
	if !entity.Empty(o.Value) {
		attrs = append(attrs, entity.String(DetailsValueId, o.Value))
	}
	return
}

func (o *Details) Empty() bool {
	if !entity.Empty(o.Key) {
		return false
	}
	if !entity.Empty(o.Value) {
		return false
	}
	return true
}

func (o *Details) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("key", "dev.miren.addon/details.key")
	sb.String("value", "dev.miren.addon/details.value")
}

const (
	AddonAssociationAddonId             = entity.Id("dev.miren.addon/addon_association.addon")
	AddonAssociationAppId               = entity.Id("dev.miren.addon/addon_association.app")
	AddonAssociationAppVersionId        = entity.Id("dev.miren.addon/addon_association.app_version")
	AddonAssociationDisksId             = entity.Id("dev.miren.addon/addon_association.disks")
	AddonAssociationErrorMessageId      = entity.Id("dev.miren.addon/addon_association.error_message")
	AddonAssociationServicesId          = entity.Id("dev.miren.addon/addon_association.services")
	AddonAssociationSourceAssociationId = entity.Id("dev.miren.addon/addon_association.source_association")
	AddonAssociationStatusId            = entity.Id("dev.miren.addon/addon_association.status")
	AddonAssociationVariablesId         = entity.Id("dev.miren.addon/addon_association.variables")
	AddonAssociationVariantId           = entity.Id("dev.miren.addon/addon_association.variant")
	AddonAssociationVersionId           = entity.Id("dev.miren.addon/addon_association.version")
)

type AddonAssociation struct {
	ID                entity.Id   `json:"id"`
	Addon             entity.Id   `cbor:"addon,omitempty" json:"addon,omitempty"`
	App               entity.Id   `cbor:"app,omitempty" json:"app,omitempty"`
	AppVersion        entity.Id   `cbor:"app_version,omitempty" json:"app_version,omitempty"`
	Disks             []Disks     `cbor:"disks,omitempty" json:"disks,omitempty"`
	ErrorMessage      string      `cbor:"error_message,omitempty" json:"error_message,omitempty"`
	Services          []string    `cbor:"services,omitempty" json:"services,omitempty"`
	SourceAssociation entity.Id   `cbor:"source_association,omitempty" json:"source_association,omitempty"`
	Status            string      `cbor:"status,omitempty" json:"status,omitempty"`
	Variables         []Variables `cbor:"variables,omitempty" json:"variables,omitempty"`
	Variant           string      `cbor:"variant,omitempty" json:"variant,omitempty"`
	Version           string      `cbor:"version,omitempty" json:"version,omitempty"`
}

func (o *AddonAssociation) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(AddonAssociationAddonId); ok && a.Value.Kind() == entity.KindId {
		o.Addon = a.Value.Id()
	}
	if a, ok := e.Get(AddonAssociationAppId); ok && a.Value.Kind() == entity.KindId {
		o.App = a.Value.Id()
	}
	if a, ok := e.Get(AddonAssociationAppVersionId); ok && a.Value.Kind() == entity.KindId {
		o.AppVersion = a.Value.Id()
	}
	for _, a := range e.GetAll(AddonAssociationDisksId) {
		if a.Value.Kind() == entity.KindComponent {
			var v Disks
			v.Decode(a.Value.Component())
			o.Disks = append(o.Disks, v)
		}
	}
	if a, ok := e.Get(AddonAssociationErrorMessageId); ok && a.Value.Kind() == entity.KindString {
		o.ErrorMessage = a.Value.String()
	}
	for _, a := range e.GetAll(AddonAssociationServicesId) {
		if a.Value.Kind() == entity.KindString {
			o.Services = append(o.Services, a.Value.String())
		}
	}
	if a, ok := e.Get(AddonAssociationSourceAssociationId); ok && a.Value.Kind() == entity.KindId {
		o.SourceAssociation = a.Value.Id()
	}
	if a, ok := e.Get(AddonAssociationStatusId); ok && a.Value.Kind() == entity.KindString {
		o.Status = a.Value.String()
	}
	for _, a := range e.GetAll(AddonAssociationVariablesId) {
		if a.Value.Kind() == entity.KindComponent {
			var v Variables
			v.Decode(a.Value.Component())
			o.Variables = append(o.Variables, v)
		}
	}
	if a, ok := e.Get(AddonAssociationVariantId); ok && a.Value.Kind() == entity.KindString {
		o.Variant = a.Value.String()
	}
	if a, ok := e.Get(AddonAssociationVersionId); ok && a.Value.Kind() == entity.KindString {
		o.Version = a.Value.String()
	}
}

func (o *AddonAssociation) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindAddonAssociation)
}

func (o *AddonAssociation) ShortKind() string {
	return "addon_association"
}

func (o *AddonAssociation) Kind() entity.Id {
	return KindAddonAssociation
}

func (o *AddonAssociation) EntityId() entity.Id {
	return o.ID
}

func (o *AddonAssociation) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Addon) {
		attrs = append(attrs, entity.Ref(AddonAssociationAddonId, o.Addon))
	}
	if !entity.Empty(o.App) {
		attrs = append(attrs, entity.Ref(AddonAssociationAppId, o.App))
	}
	if !entity.Empty(o.AppVersion) {
		attrs = append(attrs, entity.Ref(AddonAssociationAppVersionId, o.AppVersion))
	}
	for _, v := range o.Disks {
		attrs = append(attrs, entity.Component(AddonAssociationDisksId, v.Encode()))
	}
	if !entity.Empty(o.ErrorMessage) {
		attrs = append(attrs, entity.String(AddonAssociationErrorMessageId, o.ErrorMessage))
	}
	for _, v := range o.Services {
		attrs = append(attrs, entity.String(AddonAssociationServicesId, v))
	}
	if !entity.Empty(o.SourceAssociation) {
		attrs = append(attrs, entity.Ref(AddonAssociationSourceAssociationId, o.SourceAssociation))
	}
	if !entity.Empty(o.Status) {
		attrs = append(attrs, entity.String(AddonAssociationStatusId, o.Status))
	}
	for _, v := range o.Variables {
		attrs = append(attrs, entity.Component(AddonAssociationVariablesId, v.Encode()))
	}
	if !entity.Empty(o.Variant) {
		attrs = append(attrs, entity.String(AddonAssociationVariantId, o.Variant))
	}
	if !entity.Empty(o.Version) {
		attrs = append(attrs, entity.String(AddonAssociationVersionId, o.Version))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindAddonAssociation))
	return
}

func (o *AddonAssociation) Empty() bool {
	if !entity.Empty(o.Addon) {
		return false
	}
	if !entity.Empty(o.App) {
		return false
	}
	if !entity.Empty(o.AppVersion) {
		return false
	}
	if len(o.Disks) != 0 {
		return false
	}
	if !entity.Empty(o.ErrorMessage) {
		return false
	}
	if len(o.Services) != 0 {
		return false
	}
	if !entity.Empty(o.SourceAssociation) {
		return false
	}
	if !entity.Empty(o.Status) {
		return false
	}
	if len(o.Variables) != 0 {
		return false
	}
	if !entity.Empty(o.Variant) {
		return false
	}
	if !entity.Empty(o.Version) {
		return false
	}
	return true
}

func (o *AddonAssociation) InitSchema(sb *schema.SchemaBuilder) {
	sb.Ref("addon", "dev.miren.addon/addon_association.addon", schema.Indexed)
	sb.Ref("app", "dev.miren.addon/addon_association.app", schema.Indexed, schema.Tags("dev.miren.app_ref"))
	sb.Ref("app_version", "dev.miren.addon/addon_association.app_version", schema.Doc("Ephemeral app version this cloned association belongs to; empty for the app's primary association"), schema.Indexed)
	sb.Component("disks", "dev.miren.addon/addon_association.disks", schema.Many)
	(&Disks{}).InitSchema(sb.Builder("addon_association.disks"))
	sb.String("error_message", "dev.miren.addon/addon_association.error_message")
	sb.String("services", "dev.miren.addon/addon_association.services", schema.Many)
	sb.Ref("source_association", "dev.miren.addon/addon_association.source_association", schema.Doc("Primary association this association was cloned from"))
	sb.String("status", "dev.miren.addon/addon_association.status", schema.Indexed)
	sb.Component("variables", "dev.miren.addon/addon_association.variables", schema.Many)
	(&Variables{}).InitSchema(sb.Builder("addon_association.variables"))
	sb.String("variant", "dev.miren.addon/addon_association.variant")
	sb.String("version", "dev.miren.addon/addon_association.version")
}

const (
	DisksDbFileId               = entity.Id("dev.miren.addon/disks.db_file")
	DisksMountPathId            = entity.Id("dev.miren.addon/disks.mount_path")
	DisksNameId                 = entity.Id("dev.miren.addon/disks.name")
	DisksProviderId             = entity.Id("dev.miren.addon/disks.provider")
	DisksRequiresSingleWriterId = entity.Id("dev.miren.addon/disks.requires_single_writer")
)

type Disks struct {
	DbFile               string `cbor:"db_file,omitempty" json:"db_file,omitempty"`
	MountPath            string `cbor:"mount_path,omitempty" json:"mount_path,omitempty"`
	Name                 string `cbor:"name,omitempty" json:"name,omitempty"`
	Provider             string `cbor:"provider,omitempty" json:"provider,omitempty"`
	RequiresSingleWriter bool   `cbor:"requires_single_writer,omitempty" json:"requires_single_writer,omitempty"`
}

func (o *Disks) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(DisksDbFileId); ok && a.Value.Kind() == entity.KindString {
		o.DbFile = a.Value.String()
	}
	if a, ok := e.Get(DisksMountPathId); ok && a.Value.Kind() == entity.KindString {
		o.MountPath = a.Value.String()
	}
	if a, ok := e.Get(DisksNameId); ok && a.Value.Kind() == entity.KindString {
		o.Name = a.Value.String()
	}
	if a, ok := e.Get(DisksProviderId); ok && a.Value.Kind() == entity.KindString {
		o.Provider = a.Value.String()
	}
	if a, ok := e.Get(DisksRequiresSingleWriterId); ok && a.Value.Kind() == entity.KindBool {
		o.RequiresSingleWriter = a.Value.Bool()
	}
}

func (o *Disks) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.DbFile) {
		attrs = append(attrs, entity.String(DisksDbFileId, o.DbFile))
	}
	if !entity.Empty(o.MountPath) {
		attrs = append(attrs, entity.String(DisksMountPathId, o.MountPath))
	}
	if !entity.Empty(o.Name) {
		attrs = append(attrs, entity.String(DisksNameId, o.Name))
	}
	if !entity.Empty(o.Provider) {
		attrs = append(attrs, entity.String(DisksProviderId, o.Provider))
	}
	attrs = append(attrs, entity.Bool(DisksRequiresSingleWriterId, o.RequiresSingleWriter))
	return
}

func (o *Disks) Empty() bool {
	if !entity.Empty(o.DbFile) {
		return false
	}
	if !entity.Empty(o.MountPath) {
		return false
	}
	if !entity.Empty(o.Name) {
		return false
	}
	if !entity.Empty(o.Provider) {
		return false
	}
	if !entity.Empty(o.RequiresSingleWriter) {
		return false
	}
	return true
}

func (o *Disks) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("db_file", "dev.miren.addon/disks.db_file", schema.Doc("Database filename within the mounted directory, for providers that manage one"))
	sb.String("mount_path", "dev.miren.addon/disks.mount_path", schema.Doc("Where the disk appears inside the container"))
	sb.String("name", "dev.miren.addon/disks.name", schema.Doc("Disk name within a service's spec"))
	sb.String("provider", "dev.miren.addon/disks.provider", schema.Doc("Volume provider (see controllers/sandbox/volume.go)"))
	sb.Bool("requires_single_writer", "dev.miren.addon/disks.requires_single_writer", schema.Doc("Storage only one process may write, so a receiving service must run a single fixed instance"))
}

const (
	VariablesKeyId       = entity.Id("dev.miren.addon/variables.key")
	VariablesSensitiveId = entity.Id("dev.miren.addon/variables.sensitive")
	VariablesValueId     = entity.Id("dev.miren.addon/variables.value")
)

type Variables struct {
	Key       string `cbor:"key,omitempty" json:"key,omitempty"`
	Sensitive bool   `cbor:"sensitive,omitempty" json:"sensitive,omitempty"`
	Value     string `cbor:"value,omitempty" json:"value,omitempty"`
}

func (o *Variables) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(VariablesKeyId); ok && a.Value.Kind() == entity.KindString {
		o.Key = a.Value.String()
	}
	if a, ok := e.Get(VariablesSensitiveId); ok && a.Value.Kind() == entity.KindBool {
		o.Sensitive = a.Value.Bool()
	}
	if a, ok := e.Get(VariablesValueId); ok && a.Value.Kind() == entity.KindString {
		o.Value = a.Value.String()
	}
}

func (o *Variables) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Key) {
		attrs = append(attrs, entity.String(VariablesKeyId, o.Key))
	}
	attrs = append(attrs, entity.Bool(VariablesSensitiveId, o.Sensitive))
	if !entity.Empty(o.Value) {
		attrs = append(attrs, entity.String(VariablesValueId, o.Value))
	}
	return
}

func (o *Variables) Empty() bool {
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

func (o *Variables) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("key", "dev.miren.addon/variables.key")
	sb.Bool("sensitive", "dev.miren.addon/variables.sensitive")
	sb.String("value", "dev.miren.addon/variables.value")
}

const (
	MemcacheDedicatedDataMemcacheServerId = entity.Id("dev.miren.addon/memcache_dedicated_data.memcache_server")
)

type MemcacheDedicatedData struct {
	ID             entity.Id `json:"id"`
	MemcacheServer entity.Id `cbor:"memcache_server,omitempty" json:"memcache_server,omitempty"`
}

func (o *MemcacheDedicatedData) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(MemcacheDedicatedDataMemcacheServerId); ok && a.Value.Kind() == entity.KindId {
		o.MemcacheServer = a.Value.Id()
	}
}

func (o *MemcacheDedicatedData) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindMemcacheDedicatedData)
}

func (o *MemcacheDedicatedData) ShortKind() string {
	return "memcache_dedicated_data"
}

func (o *MemcacheDedicatedData) Kind() entity.Id {
	return KindMemcacheDedicatedData
}

func (o *MemcacheDedicatedData) EntityId() entity.Id {
	return o.ID
}

func (o *MemcacheDedicatedData) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.MemcacheServer) {
		attrs = append(attrs, entity.Ref(MemcacheDedicatedDataMemcacheServerId, o.MemcacheServer))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindMemcacheDedicatedData))
	return
}

func (o *MemcacheDedicatedData) Empty() bool {
	return entity.Empty(o.MemcacheServer)
}

func (o *MemcacheDedicatedData) InitSchema(sb *schema.SchemaBuilder) {
	sb.Ref("memcache_server", "dev.miren.addon/memcache_dedicated_data.memcache_server")
}

const (
	MemcacheServerAddonNameId        = entity.Id("dev.miren.addon/memcache_server.addon_name")
	MemcacheServerAssociationCountId = entity.Id("dev.miren.addon/memcache_server.association_count")
	MemcacheServerSandboxPoolId      = entity.Id("dev.miren.addon/memcache_server.sandbox_pool")
	MemcacheServerServiceId          = entity.Id("dev.miren.addon/memcache_server.service")
	MemcacheServerStatusId           = entity.Id("dev.miren.addon/memcache_server.status")
	MemcacheServerVariantId          = entity.Id("dev.miren.addon/memcache_server.variant")
)

type MemcacheServer struct {
	ID               entity.Id `json:"id"`
	AddonName        string    `cbor:"addon_name,omitempty" json:"addon_name,omitempty"`
	AssociationCount int64     `cbor:"association_count,omitempty" json:"association_count,omitempty"`
	SandboxPool      entity.Id `cbor:"sandbox_pool,omitempty" json:"sandbox_pool,omitempty"`
	Service          entity.Id `cbor:"service,omitempty" json:"service,omitempty"`
	Status           string    `cbor:"status,omitempty" json:"status,omitempty"`
	Variant          string    `cbor:"variant,omitempty" json:"variant,omitempty"`
}

func (o *MemcacheServer) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(MemcacheServerAddonNameId); ok && a.Value.Kind() == entity.KindString {
		o.AddonName = a.Value.String()
	}
	if a, ok := e.Get(MemcacheServerAssociationCountId); ok && a.Value.Kind() == entity.KindInt64 {
		o.AssociationCount = a.Value.Int64()
	}
	if a, ok := e.Get(MemcacheServerSandboxPoolId); ok && a.Value.Kind() == entity.KindId {
		o.SandboxPool = a.Value.Id()
	}
	if a, ok := e.Get(MemcacheServerServiceId); ok && a.Value.Kind() == entity.KindId {
		o.Service = a.Value.Id()
	}
	if a, ok := e.Get(MemcacheServerStatusId); ok && a.Value.Kind() == entity.KindString {
		o.Status = a.Value.String()
	}
	if a, ok := e.Get(MemcacheServerVariantId); ok && a.Value.Kind() == entity.KindString {
		o.Variant = a.Value.String()
	}
}

func (o *MemcacheServer) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindMemcacheServer)
}

func (o *MemcacheServer) ShortKind() string {
	return "memcache_server"
}

func (o *MemcacheServer) Kind() entity.Id {
	return KindMemcacheServer
}

func (o *MemcacheServer) EntityId() entity.Id {
	return o.ID
}

func (o *MemcacheServer) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.AddonName) {
		attrs = append(attrs, entity.String(MemcacheServerAddonNameId, o.AddonName))
	}
	if !entity.Empty(o.AssociationCount) {
		attrs = append(attrs, entity.Int64(MemcacheServerAssociationCountId, o.AssociationCount))
	}
	if !entity.Empty(o.SandboxPool) {
		attrs = append(attrs, entity.Ref(MemcacheServerSandboxPoolId, o.SandboxPool))
	}
	if !entity.Empty(o.Service) {
		attrs = append(attrs, entity.Ref(MemcacheServerServiceId, o.Service))
	}
	if !entity.Empty(o.Status) {
		attrs = append(attrs, entity.String(MemcacheServerStatusId, o.Status))
	}
	if !entity.Empty(o.Variant) {
		attrs = append(attrs, entity.String(MemcacheServerVariantId, o.Variant))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindMemcacheServer))
	return
}

func (o *MemcacheServer) Empty() bool {
	if !entity.Empty(o.AddonName) {
		return false
	}
	if !entity.Empty(o.AssociationCount) {
		return false
	}
	if !entity.Empty(o.SandboxPool) {
		return false
	}
	if !entity.Empty(o.Service) {
		return false
	}
	if !entity.Empty(o.Status) {
		return false
	}
	if !entity.Empty(o.Variant) {
		return false
	}
	return true
}

func (o *MemcacheServer) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("addon_name", "dev.miren.addon/memcache_server.addon_name", schema.Indexed)
	sb.Int64("association_count", "dev.miren.addon/memcache_server.association_count")
	sb.Ref("sandbox_pool", "dev.miren.addon/memcache_server.sandbox_pool")
	sb.Ref("service", "dev.miren.addon/memcache_server.service")
	sb.String("status", "dev.miren.addon/memcache_server.status")
	sb.String("variant", "dev.miren.addon/memcache_server.variant")
}

const (
	MysqlDedicatedDataDatabaseNameId = entity.Id("dev.miren.addon/mysql_dedicated_data.database_name")
	MysqlDedicatedDataMysqlServerId  = entity.Id("dev.miren.addon/mysql_dedicated_data.mysql_server")
	MysqlDedicatedDataUsernameId     = entity.Id("dev.miren.addon/mysql_dedicated_data.username")
)

type MysqlDedicatedData struct {
	ID           entity.Id `json:"id"`
	DatabaseName string    `cbor:"database_name,omitempty" json:"database_name,omitempty"`
	MysqlServer  entity.Id `cbor:"mysql_server,omitempty" json:"mysql_server,omitempty"`
	Username     string    `cbor:"username,omitempty" json:"username,omitempty"`
}

func (o *MysqlDedicatedData) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(MysqlDedicatedDataDatabaseNameId); ok && a.Value.Kind() == entity.KindString {
		o.DatabaseName = a.Value.String()
	}
	if a, ok := e.Get(MysqlDedicatedDataMysqlServerId); ok && a.Value.Kind() == entity.KindId {
		o.MysqlServer = a.Value.Id()
	}
	if a, ok := e.Get(MysqlDedicatedDataUsernameId); ok && a.Value.Kind() == entity.KindString {
		o.Username = a.Value.String()
	}
}

func (o *MysqlDedicatedData) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindMysqlDedicatedData)
}

func (o *MysqlDedicatedData) ShortKind() string {
	return "mysql_dedicated_data"
}

func (o *MysqlDedicatedData) Kind() entity.Id {
	return KindMysqlDedicatedData
}

func (o *MysqlDedicatedData) EntityId() entity.Id {
	return o.ID
}

func (o *MysqlDedicatedData) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.DatabaseName) {
		attrs = append(attrs, entity.String(MysqlDedicatedDataDatabaseNameId, o.DatabaseName))
	}
	if !entity.Empty(o.MysqlServer) {
		attrs = append(attrs, entity.Ref(MysqlDedicatedDataMysqlServerId, o.MysqlServer))
	}
	if !entity.Empty(o.Username) {
		attrs = append(attrs, entity.String(MysqlDedicatedDataUsernameId, o.Username))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindMysqlDedicatedData))
	return
}

func (o *MysqlDedicatedData) Empty() bool {
	if !entity.Empty(o.DatabaseName) {
		return false
	}
	if !entity.Empty(o.MysqlServer) {
		return false
	}
	if !entity.Empty(o.Username) {
		return false
	}
	return true
}

func (o *MysqlDedicatedData) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("database_name", "dev.miren.addon/mysql_dedicated_data.database_name")
	sb.Ref("mysql_server", "dev.miren.addon/mysql_dedicated_data.mysql_server")
	sb.String("username", "dev.miren.addon/mysql_dedicated_data.username")
}

const (
	MysqlServerAddonNameId        = entity.Id("dev.miren.addon/mysql_server.addon_name")
	MysqlServerAssociationCountId = entity.Id("dev.miren.addon/mysql_server.association_count")
	MysqlServerDiskNameId         = entity.Id("dev.miren.addon/mysql_server.disk_name")
	MysqlServerImageId            = entity.Id("dev.miren.addon/mysql_server.image")
	MysqlServerRootPasswordId     = entity.Id("dev.miren.addon/mysql_server.root_password")
	MysqlServerSandboxPoolId      = entity.Id("dev.miren.addon/mysql_server.sandbox_pool")
	MysqlServerServiceId          = entity.Id("dev.miren.addon/mysql_server.service")
	MysqlServerStatusId           = entity.Id("dev.miren.addon/mysql_server.status")
	MysqlServerVariantId          = entity.Id("dev.miren.addon/mysql_server.variant")
)

type MysqlServer struct {
	ID               entity.Id `json:"id"`
	AddonName        string    `cbor:"addon_name,omitempty" json:"addon_name,omitempty"`
	AssociationCount int64     `cbor:"association_count,omitempty" json:"association_count,omitempty"`
	DiskName         string    `cbor:"disk_name,omitempty" json:"disk_name,omitempty"`
	Image            string    `cbor:"image,omitempty" json:"image,omitempty"`
	RootPassword     string    `cbor:"root_password,omitempty" json:"root_password,omitempty"`
	SandboxPool      entity.Id `cbor:"sandbox_pool,omitempty" json:"sandbox_pool,omitempty"`
	Service          entity.Id `cbor:"service,omitempty" json:"service,omitempty"`
	Status           string    `cbor:"status,omitempty" json:"status,omitempty"`
	Variant          string    `cbor:"variant,omitempty" json:"variant,omitempty"`
}

func (o *MysqlServer) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(MysqlServerAddonNameId); ok && a.Value.Kind() == entity.KindString {
		o.AddonName = a.Value.String()
	}
	if a, ok := e.Get(MysqlServerAssociationCountId); ok && a.Value.Kind() == entity.KindInt64 {
		o.AssociationCount = a.Value.Int64()
	}
	if a, ok := e.Get(MysqlServerDiskNameId); ok && a.Value.Kind() == entity.KindString {
		o.DiskName = a.Value.String()
	}
	if a, ok := e.Get(MysqlServerImageId); ok && a.Value.Kind() == entity.KindString {
		o.Image = a.Value.String()
	}
	if a, ok := e.Get(MysqlServerRootPasswordId); ok && a.Value.Kind() == entity.KindString {
		o.RootPassword = a.Value.String()
	}
	if a, ok := e.Get(MysqlServerSandboxPoolId); ok && a.Value.Kind() == entity.KindId {
		o.SandboxPool = a.Value.Id()
	}
	if a, ok := e.Get(MysqlServerServiceId); ok && a.Value.Kind() == entity.KindId {
		o.Service = a.Value.Id()
	}
	if a, ok := e.Get(MysqlServerStatusId); ok && a.Value.Kind() == entity.KindString {
		o.Status = a.Value.String()
	}
	if a, ok := e.Get(MysqlServerVariantId); ok && a.Value.Kind() == entity.KindString {
		o.Variant = a.Value.String()
	}
}

func (o *MysqlServer) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindMysqlServer)
}

func (o *MysqlServer) ShortKind() string {
	return "mysql_server"
}

func (o *MysqlServer) Kind() entity.Id {
	return KindMysqlServer
}

func (o *MysqlServer) EntityId() entity.Id {
	return o.ID
}

func (o *MysqlServer) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.AddonName) {
		attrs = append(attrs, entity.String(MysqlServerAddonNameId, o.AddonName))
	}
	if !entity.Empty(o.AssociationCount) {
		attrs = append(attrs, entity.Int64(MysqlServerAssociationCountId, o.AssociationCount))
	}
	if !entity.Empty(o.DiskName) {
		attrs = append(attrs, entity.String(MysqlServerDiskNameId, o.DiskName))
	}
	if !entity.Empty(o.Image) {
		attrs = append(attrs, entity.String(MysqlServerImageId, o.Image))
	}
	if !entity.Empty(o.RootPassword) {
		attrs = append(attrs, entity.String(MysqlServerRootPasswordId, o.RootPassword))
	}
	if !entity.Empty(o.SandboxPool) {
		attrs = append(attrs, entity.Ref(MysqlServerSandboxPoolId, o.SandboxPool))
	}
	if !entity.Empty(o.Service) {
		attrs = append(attrs, entity.Ref(MysqlServerServiceId, o.Service))
	}
	if !entity.Empty(o.Status) {
		attrs = append(attrs, entity.String(MysqlServerStatusId, o.Status))
	}
	if !entity.Empty(o.Variant) {
		attrs = append(attrs, entity.String(MysqlServerVariantId, o.Variant))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindMysqlServer))
	return
}

func (o *MysqlServer) Empty() bool {
	if !entity.Empty(o.AddonName) {
		return false
	}
	if !entity.Empty(o.AssociationCount) {
		return false
	}
	if !entity.Empty(o.DiskName) {
		return false
	}
	if !entity.Empty(o.Image) {
		return false
	}
	if !entity.Empty(o.RootPassword) {
		return false
	}
	if !entity.Empty(o.SandboxPool) {
		return false
	}
	if !entity.Empty(o.Service) {
		return false
	}
	if !entity.Empty(o.Status) {
		return false
	}
	if !entity.Empty(o.Variant) {
		return false
	}
	return true
}

func (o *MysqlServer) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("addon_name", "dev.miren.addon/mysql_server.addon_name", schema.Indexed)
	sb.Int64("association_count", "dev.miren.addon/mysql_server.association_count")
	sb.String("disk_name", "dev.miren.addon/mysql_server.disk_name")
	sb.String("image", "dev.miren.addon/mysql_server.image")
	sb.String("root_password", "dev.miren.addon/mysql_server.root_password")
	sb.Ref("sandbox_pool", "dev.miren.addon/mysql_server.sandbox_pool")
	sb.Ref("service", "dev.miren.addon/mysql_server.service")
	sb.String("status", "dev.miren.addon/mysql_server.status")
	sb.String("variant", "dev.miren.addon/mysql_server.variant")
}

const (
	MysqlSharedDataDatabaseNameId = entity.Id("dev.miren.addon/mysql_shared_data.database_name")
	MysqlSharedDataMysqlServerId  = entity.Id("dev.miren.addon/mysql_shared_data.mysql_server")
	MysqlSharedDataUsernameId     = entity.Id("dev.miren.addon/mysql_shared_data.username")
)

type MysqlSharedData struct {
	ID           entity.Id `json:"id"`
	DatabaseName string    `cbor:"database_name,omitempty" json:"database_name,omitempty"`
	MysqlServer  entity.Id `cbor:"mysql_server,omitempty" json:"mysql_server,omitempty"`
	Username     string    `cbor:"username,omitempty" json:"username,omitempty"`
}

func (o *MysqlSharedData) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(MysqlSharedDataDatabaseNameId); ok && a.Value.Kind() == entity.KindString {
		o.DatabaseName = a.Value.String()
	}
	if a, ok := e.Get(MysqlSharedDataMysqlServerId); ok && a.Value.Kind() == entity.KindId {
		o.MysqlServer = a.Value.Id()
	}
	if a, ok := e.Get(MysqlSharedDataUsernameId); ok && a.Value.Kind() == entity.KindString {
		o.Username = a.Value.String()
	}
}

func (o *MysqlSharedData) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindMysqlSharedData)
}

func (o *MysqlSharedData) ShortKind() string {
	return "mysql_shared_data"
}

func (o *MysqlSharedData) Kind() entity.Id {
	return KindMysqlSharedData
}

func (o *MysqlSharedData) EntityId() entity.Id {
	return o.ID
}

func (o *MysqlSharedData) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.DatabaseName) {
		attrs = append(attrs, entity.String(MysqlSharedDataDatabaseNameId, o.DatabaseName))
	}
	if !entity.Empty(o.MysqlServer) {
		attrs = append(attrs, entity.Ref(MysqlSharedDataMysqlServerId, o.MysqlServer))
	}
	if !entity.Empty(o.Username) {
		attrs = append(attrs, entity.String(MysqlSharedDataUsernameId, o.Username))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindMysqlSharedData))
	return
}

func (o *MysqlSharedData) Empty() bool {
	if !entity.Empty(o.DatabaseName) {
		return false
	}
	if !entity.Empty(o.MysqlServer) {
		return false
	}
	if !entity.Empty(o.Username) {
		return false
	}
	return true
}

func (o *MysqlSharedData) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("database_name", "dev.miren.addon/mysql_shared_data.database_name")
	sb.Ref("mysql_server", "dev.miren.addon/mysql_shared_data.mysql_server")
	sb.String("username", "dev.miren.addon/mysql_shared_data.username")
}

const (
	PostgresServerAddonNameId         = entity.Id("dev.miren.addon/postgres_server.addon_name")
	PostgresServerAssociationCountId  = entity.Id("dev.miren.addon/postgres_server.association_count")
	PostgresServerDiskNameId          = entity.Id("dev.miren.addon/postgres_server.disk_name")
	PostgresServerImageId             = entity.Id("dev.miren.addon/postgres_server.image")
	PostgresServerSandboxPoolId       = entity.Id("dev.miren.addon/postgres_server.sandbox_pool")
	PostgresServerServiceId           = entity.Id("dev.miren.addon/postgres_server.service")
	PostgresServerStatusId            = entity.Id("dev.miren.addon/postgres_server.status")
	PostgresServerSuperuserPasswordId = entity.Id("dev.miren.addon/postgres_server.superuser_password")
	PostgresServerVariantId           = entity.Id("dev.miren.addon/postgres_server.variant")
)

type PostgresServer struct {
	ID                entity.Id `json:"id"`
	AddonName         string    `cbor:"addon_name,omitempty" json:"addon_name,omitempty"`
	AssociationCount  int64     `cbor:"association_count,omitempty" json:"association_count,omitempty"`
	DiskName          string    `cbor:"disk_name,omitempty" json:"disk_name,omitempty"`
	Image             string    `cbor:"image,omitempty" json:"image,omitempty"`
	SandboxPool       entity.Id `cbor:"sandbox_pool,omitempty" json:"sandbox_pool,omitempty"`
	Service           entity.Id `cbor:"service,omitempty" json:"service,omitempty"`
	Status            string    `cbor:"status,omitempty" json:"status,omitempty"`
	SuperuserPassword string    `cbor:"superuser_password,omitempty" json:"superuser_password,omitempty"`
	Variant           string    `cbor:"variant,omitempty" json:"variant,omitempty"`
}

func (o *PostgresServer) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(PostgresServerAddonNameId); ok && a.Value.Kind() == entity.KindString {
		o.AddonName = a.Value.String()
	}
	if a, ok := e.Get(PostgresServerAssociationCountId); ok && a.Value.Kind() == entity.KindInt64 {
		o.AssociationCount = a.Value.Int64()
	}
	if a, ok := e.Get(PostgresServerDiskNameId); ok && a.Value.Kind() == entity.KindString {
		o.DiskName = a.Value.String()
	}
	if a, ok := e.Get(PostgresServerImageId); ok && a.Value.Kind() == entity.KindString {
		o.Image = a.Value.String()
	}
	if a, ok := e.Get(PostgresServerSandboxPoolId); ok && a.Value.Kind() == entity.KindId {
		o.SandboxPool = a.Value.Id()
	}
	if a, ok := e.Get(PostgresServerServiceId); ok && a.Value.Kind() == entity.KindId {
		o.Service = a.Value.Id()
	}
	if a, ok := e.Get(PostgresServerStatusId); ok && a.Value.Kind() == entity.KindString {
		o.Status = a.Value.String()
	}
	if a, ok := e.Get(PostgresServerSuperuserPasswordId); ok && a.Value.Kind() == entity.KindString {
		o.SuperuserPassword = a.Value.String()
	}
	if a, ok := e.Get(PostgresServerVariantId); ok && a.Value.Kind() == entity.KindString {
		o.Variant = a.Value.String()
	}
}

func (o *PostgresServer) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindPostgresServer)
}

func (o *PostgresServer) ShortKind() string {
	return "postgres_server"
}

func (o *PostgresServer) Kind() entity.Id {
	return KindPostgresServer
}

func (o *PostgresServer) EntityId() entity.Id {
	return o.ID
}

func (o *PostgresServer) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.AddonName) {
		attrs = append(attrs, entity.String(PostgresServerAddonNameId, o.AddonName))
	}
	if !entity.Empty(o.AssociationCount) {
		attrs = append(attrs, entity.Int64(PostgresServerAssociationCountId, o.AssociationCount))
	}
	if !entity.Empty(o.DiskName) {
		attrs = append(attrs, entity.String(PostgresServerDiskNameId, o.DiskName))
	}
	if !entity.Empty(o.Image) {
		attrs = append(attrs, entity.String(PostgresServerImageId, o.Image))
	}
	if !entity.Empty(o.SandboxPool) {
		attrs = append(attrs, entity.Ref(PostgresServerSandboxPoolId, o.SandboxPool))
	}
	if !entity.Empty(o.Service) {
		attrs = append(attrs, entity.Ref(PostgresServerServiceId, o.Service))
	}
	if !entity.Empty(o.Status) {
		attrs = append(attrs, entity.String(PostgresServerStatusId, o.Status))
	}
	if !entity.Empty(o.SuperuserPassword) {
		attrs = append(attrs, entity.String(PostgresServerSuperuserPasswordId, o.SuperuserPassword))
	}
	if !entity.Empty(o.Variant) {
		attrs = append(attrs, entity.String(PostgresServerVariantId, o.Variant))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindPostgresServer))
	return
}

func (o *PostgresServer) Empty() bool {
	if !entity.Empty(o.AddonName) {
		return false
	}
	if !entity.Empty(o.AssociationCount) {
		return false
	}
	if !entity.Empty(o.DiskName) {
		return false
	}
	if !entity.Empty(o.Image) {
		return false
	}
	if !entity.Empty(o.SandboxPool) {
		return false
	}
	if !entity.Empty(o.Service) {
		return false
	}
	if !entity.Empty(o.Status) {
		return false
	}
	if !entity.Empty(o.SuperuserPassword) {
		return false
	}
	if !entity.Empty(o.Variant) {
		return false
	}
	return true
}

func (o *PostgresServer) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("addon_name", "dev.miren.addon/postgres_server.addon_name", schema.Indexed)
	sb.Int64("association_count", "dev.miren.addon/postgres_server.association_count")
	sb.String("disk_name", "dev.miren.addon/postgres_server.disk_name")
	sb.String("image", "dev.miren.addon/postgres_server.image")
	sb.Ref("sandbox_pool", "dev.miren.addon/postgres_server.sandbox_pool")
	sb.Ref("service", "dev.miren.addon/postgres_server.service")
	sb.String("status", "dev.miren.addon/postgres_server.status")
	sb.String("superuser_password", "dev.miren.addon/postgres_server.superuser_password")
	sb.String("variant", "dev.miren.addon/postgres_server.variant")
}

const (
	PostgresqlDedicatedDataDatabaseNameId   = entity.Id("dev.miren.addon/postgresql_dedicated_data.database_name")
	PostgresqlDedicatedDataPostgresServerId = entity.Id("dev.miren.addon/postgresql_dedicated_data.postgres_server")
	PostgresqlDedicatedDataUsernameId       = entity.Id("dev.miren.addon/postgresql_dedicated_data.username")
)

type PostgresqlDedicatedData struct {
	ID             entity.Id `json:"id"`
	DatabaseName   string    `cbor:"database_name,omitempty" json:"database_name,omitempty"`
	PostgresServer entity.Id `cbor:"postgres_server,omitempty" json:"postgres_server,omitempty"`
	Username       string    `cbor:"username,omitempty" json:"username,omitempty"`
}

func (o *PostgresqlDedicatedData) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(PostgresqlDedicatedDataDatabaseNameId); ok && a.Value.Kind() == entity.KindString {
		o.DatabaseName = a.Value.String()
	}
	if a, ok := e.Get(PostgresqlDedicatedDataPostgresServerId); ok && a.Value.Kind() == entity.KindId {
		o.PostgresServer = a.Value.Id()
	}
	if a, ok := e.Get(PostgresqlDedicatedDataUsernameId); ok && a.Value.Kind() == entity.KindString {
		o.Username = a.Value.String()
	}
}

func (o *PostgresqlDedicatedData) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindPostgresqlDedicatedData)
}

func (o *PostgresqlDedicatedData) ShortKind() string {
	return "postgresql_dedicated_data"
}

func (o *PostgresqlDedicatedData) Kind() entity.Id {
	return KindPostgresqlDedicatedData
}

func (o *PostgresqlDedicatedData) EntityId() entity.Id {
	return o.ID
}

func (o *PostgresqlDedicatedData) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.DatabaseName) {
		attrs = append(attrs, entity.String(PostgresqlDedicatedDataDatabaseNameId, o.DatabaseName))
	}
	if !entity.Empty(o.PostgresServer) {
		attrs = append(attrs, entity.Ref(PostgresqlDedicatedDataPostgresServerId, o.PostgresServer))
	}
	if !entity.Empty(o.Username) {
		attrs = append(attrs, entity.String(PostgresqlDedicatedDataUsernameId, o.Username))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindPostgresqlDedicatedData))
	return
}

func (o *PostgresqlDedicatedData) Empty() bool {
	if !entity.Empty(o.DatabaseName) {
		return false
	}
	if !entity.Empty(o.PostgresServer) {
		return false
	}
	if !entity.Empty(o.Username) {
		return false
	}
	return true
}

func (o *PostgresqlDedicatedData) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("database_name", "dev.miren.addon/postgresql_dedicated_data.database_name")
	sb.Ref("postgres_server", "dev.miren.addon/postgresql_dedicated_data.postgres_server")
	sb.String("username", "dev.miren.addon/postgresql_dedicated_data.username")
}

const (
	PostgresqlSharedDataDatabaseNameId   = entity.Id("dev.miren.addon/postgresql_shared_data.database_name")
	PostgresqlSharedDataPostgresServerId = entity.Id("dev.miren.addon/postgresql_shared_data.postgres_server")
	PostgresqlSharedDataUsernameId       = entity.Id("dev.miren.addon/postgresql_shared_data.username")
)

type PostgresqlSharedData struct {
	ID             entity.Id `json:"id"`
	DatabaseName   string    `cbor:"database_name,omitempty" json:"database_name,omitempty"`
	PostgresServer entity.Id `cbor:"postgres_server,omitempty" json:"postgres_server,omitempty"`
	Username       string    `cbor:"username,omitempty" json:"username,omitempty"`
}

func (o *PostgresqlSharedData) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(PostgresqlSharedDataDatabaseNameId); ok && a.Value.Kind() == entity.KindString {
		o.DatabaseName = a.Value.String()
	}
	if a, ok := e.Get(PostgresqlSharedDataPostgresServerId); ok && a.Value.Kind() == entity.KindId {
		o.PostgresServer = a.Value.Id()
	}
	if a, ok := e.Get(PostgresqlSharedDataUsernameId); ok && a.Value.Kind() == entity.KindString {
		o.Username = a.Value.String()
	}
}

func (o *PostgresqlSharedData) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindPostgresqlSharedData)
}

func (o *PostgresqlSharedData) ShortKind() string {
	return "postgresql_shared_data"
}

func (o *PostgresqlSharedData) Kind() entity.Id {
	return KindPostgresqlSharedData
}

func (o *PostgresqlSharedData) EntityId() entity.Id {
	return o.ID
}

func (o *PostgresqlSharedData) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.DatabaseName) {
		attrs = append(attrs, entity.String(PostgresqlSharedDataDatabaseNameId, o.DatabaseName))
	}
	if !entity.Empty(o.PostgresServer) {
		attrs = append(attrs, entity.Ref(PostgresqlSharedDataPostgresServerId, o.PostgresServer))
	}
	if !entity.Empty(o.Username) {
		attrs = append(attrs, entity.String(PostgresqlSharedDataUsernameId, o.Username))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindPostgresqlSharedData))
	return
}

func (o *PostgresqlSharedData) Empty() bool {
	if !entity.Empty(o.DatabaseName) {
		return false
	}
	if !entity.Empty(o.PostgresServer) {
		return false
	}
	if !entity.Empty(o.Username) {
		return false
	}
	return true
}

func (o *PostgresqlSharedData) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("database_name", "dev.miren.addon/postgresql_shared_data.database_name")
	sb.Ref("postgres_server", "dev.miren.addon/postgresql_shared_data.postgres_server")
	sb.String("username", "dev.miren.addon/postgresql_shared_data.username")
}

const (
	RabbitmqDedicatedDataRabbitmqServerId = entity.Id("dev.miren.addon/rabbitmq_dedicated_data.rabbitmq_server")
)

type RabbitmqDedicatedData struct {
	ID             entity.Id `json:"id"`
	RabbitmqServer entity.Id `cbor:"rabbitmq_server,omitempty" json:"rabbitmq_server,omitempty"`
}

func (o *RabbitmqDedicatedData) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(RabbitmqDedicatedDataRabbitmqServerId); ok && a.Value.Kind() == entity.KindId {
		o.RabbitmqServer = a.Value.Id()
	}
}

func (o *RabbitmqDedicatedData) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindRabbitmqDedicatedData)
}

func (o *RabbitmqDedicatedData) ShortKind() string {
	return "rabbitmq_dedicated_data"
}

func (o *RabbitmqDedicatedData) Kind() entity.Id {
	return KindRabbitmqDedicatedData
}

func (o *RabbitmqDedicatedData) EntityId() entity.Id {
	return o.ID
}

func (o *RabbitmqDedicatedData) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.RabbitmqServer) {
		attrs = append(attrs, entity.Ref(RabbitmqDedicatedDataRabbitmqServerId, o.RabbitmqServer))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindRabbitmqDedicatedData))
	return
}

func (o *RabbitmqDedicatedData) Empty() bool {
	return entity.Empty(o.RabbitmqServer)
}

func (o *RabbitmqDedicatedData) InitSchema(sb *schema.SchemaBuilder) {
	sb.Ref("rabbitmq_server", "dev.miren.addon/rabbitmq_dedicated_data.rabbitmq_server")
}

const (
	RabbitmqServerAddonNameId        = entity.Id("dev.miren.addon/rabbitmq_server.addon_name")
	RabbitmqServerAssociationCountId = entity.Id("dev.miren.addon/rabbitmq_server.association_count")
	RabbitmqServerPasswordId         = entity.Id("dev.miren.addon/rabbitmq_server.password")
	RabbitmqServerSandboxPoolId      = entity.Id("dev.miren.addon/rabbitmq_server.sandbox_pool")
	RabbitmqServerServiceId          = entity.Id("dev.miren.addon/rabbitmq_server.service")
	RabbitmqServerStatusId           = entity.Id("dev.miren.addon/rabbitmq_server.status")
	RabbitmqServerVariantId          = entity.Id("dev.miren.addon/rabbitmq_server.variant")
)

type RabbitmqServer struct {
	ID               entity.Id `json:"id"`
	AddonName        string    `cbor:"addon_name,omitempty" json:"addon_name,omitempty"`
	AssociationCount int64     `cbor:"association_count,omitempty" json:"association_count,omitempty"`
	Password         string    `cbor:"password,omitempty" json:"password,omitempty"`
	SandboxPool      entity.Id `cbor:"sandbox_pool,omitempty" json:"sandbox_pool,omitempty"`
	Service          entity.Id `cbor:"service,omitempty" json:"service,omitempty"`
	Status           string    `cbor:"status,omitempty" json:"status,omitempty"`
	Variant          string    `cbor:"variant,omitempty" json:"variant,omitempty"`
}

func (o *RabbitmqServer) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(RabbitmqServerAddonNameId); ok && a.Value.Kind() == entity.KindString {
		o.AddonName = a.Value.String()
	}
	if a, ok := e.Get(RabbitmqServerAssociationCountId); ok && a.Value.Kind() == entity.KindInt64 {
		o.AssociationCount = a.Value.Int64()
	}
	if a, ok := e.Get(RabbitmqServerPasswordId); ok && a.Value.Kind() == entity.KindString {
		o.Password = a.Value.String()
	}
	if a, ok := e.Get(RabbitmqServerSandboxPoolId); ok && a.Value.Kind() == entity.KindId {
		o.SandboxPool = a.Value.Id()
	}
	if a, ok := e.Get(RabbitmqServerServiceId); ok && a.Value.Kind() == entity.KindId {
		o.Service = a.Value.Id()
	}
	if a, ok := e.Get(RabbitmqServerStatusId); ok && a.Value.Kind() == entity.KindString {
		o.Status = a.Value.String()
	}
	if a, ok := e.Get(RabbitmqServerVariantId); ok && a.Value.Kind() == entity.KindString {
		o.Variant = a.Value.String()
	}
}

func (o *RabbitmqServer) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindRabbitmqServer)
}

func (o *RabbitmqServer) ShortKind() string {
	return "rabbitmq_server"
}

func (o *RabbitmqServer) Kind() entity.Id {
	return KindRabbitmqServer
}

func (o *RabbitmqServer) EntityId() entity.Id {
	return o.ID
}

func (o *RabbitmqServer) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.AddonName) {
		attrs = append(attrs, entity.String(RabbitmqServerAddonNameId, o.AddonName))
	}
	if !entity.Empty(o.AssociationCount) {
		attrs = append(attrs, entity.Int64(RabbitmqServerAssociationCountId, o.AssociationCount))
	}
	if !entity.Empty(o.Password) {
		attrs = append(attrs, entity.String(RabbitmqServerPasswordId, o.Password))
	}
	if !entity.Empty(o.SandboxPool) {
		attrs = append(attrs, entity.Ref(RabbitmqServerSandboxPoolId, o.SandboxPool))
	}
	if !entity.Empty(o.Service) {
		attrs = append(attrs, entity.Ref(RabbitmqServerServiceId, o.Service))
	}
	if !entity.Empty(o.Status) {
		attrs = append(attrs, entity.String(RabbitmqServerStatusId, o.Status))
	}
	if !entity.Empty(o.Variant) {
		attrs = append(attrs, entity.String(RabbitmqServerVariantId, o.Variant))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindRabbitmqServer))
	return
}

func (o *RabbitmqServer) Empty() bool {
	if !entity.Empty(o.AddonName) {
		return false
	}
	if !entity.Empty(o.AssociationCount) {
		return false
	}
	if !entity.Empty(o.Password) {
		return false
	}
	if !entity.Empty(o.SandboxPool) {
		return false
	}
	if !entity.Empty(o.Service) {
		return false
	}
	if !entity.Empty(o.Status) {
		return false
	}
	if !entity.Empty(o.Variant) {
		return false
	}
	return true
}

func (o *RabbitmqServer) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("addon_name", "dev.miren.addon/rabbitmq_server.addon_name", schema.Indexed)
	sb.Int64("association_count", "dev.miren.addon/rabbitmq_server.association_count")
	sb.String("password", "dev.miren.addon/rabbitmq_server.password")
	sb.Ref("sandbox_pool", "dev.miren.addon/rabbitmq_server.sandbox_pool")
	sb.Ref("service", "dev.miren.addon/rabbitmq_server.service")
	sb.String("status", "dev.miren.addon/rabbitmq_server.status")
	sb.String("variant", "dev.miren.addon/rabbitmq_server.variant")
}

const (
	RotationRequestAddonId        = entity.Id("dev.miren.addon/rotation_request.addon")
	RotationRequestAssociationId  = entity.Id("dev.miren.addon/rotation_request.association")
	RotationRequestCredentialId   = entity.Id("dev.miren.addon/rotation_request.credential")
	RotationRequestErrorMessageId = entity.Id("dev.miren.addon/rotation_request.error_message")
	RotationRequestNewSecretId    = entity.Id("dev.miren.addon/rotation_request.new_secret")
	RotationRequestStatusId       = entity.Id("dev.miren.addon/rotation_request.status")
)

type RotationRequest struct {
	ID           entity.Id `json:"id"`
	Addon        entity.Id `cbor:"addon,omitempty" json:"addon,omitempty"`
	Association  entity.Id `cbor:"association,omitempty" json:"association,omitempty"`
	Credential   string    `cbor:"credential,omitempty" json:"credential,omitempty"`
	ErrorMessage string    `cbor:"error_message,omitempty" json:"error_message,omitempty"`
	NewSecret    string    `cbor:"new_secret,omitempty" json:"new_secret,omitempty"`
	Status       string    `cbor:"status,omitempty" json:"status,omitempty"`
}

func (o *RotationRequest) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(RotationRequestAddonId); ok && a.Value.Kind() == entity.KindId {
		o.Addon = a.Value.Id()
	}
	if a, ok := e.Get(RotationRequestAssociationId); ok && a.Value.Kind() == entity.KindId {
		o.Association = a.Value.Id()
	}
	if a, ok := e.Get(RotationRequestCredentialId); ok && a.Value.Kind() == entity.KindString {
		o.Credential = a.Value.String()
	}
	if a, ok := e.Get(RotationRequestErrorMessageId); ok && a.Value.Kind() == entity.KindString {
		o.ErrorMessage = a.Value.String()
	}
	if a, ok := e.Get(RotationRequestNewSecretId); ok && a.Value.Kind() == entity.KindString {
		o.NewSecret = a.Value.String()
	}
	if a, ok := e.Get(RotationRequestStatusId); ok && a.Value.Kind() == entity.KindString {
		o.Status = a.Value.String()
	}
}

func (o *RotationRequest) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindRotationRequest)
}

func (o *RotationRequest) ShortKind() string {
	return "rotation_request"
}

func (o *RotationRequest) Kind() entity.Id {
	return KindRotationRequest
}

func (o *RotationRequest) EntityId() entity.Id {
	return o.ID
}

func (o *RotationRequest) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Addon) {
		attrs = append(attrs, entity.Ref(RotationRequestAddonId, o.Addon))
	}
	if !entity.Empty(o.Association) {
		attrs = append(attrs, entity.Ref(RotationRequestAssociationId, o.Association))
	}
	if !entity.Empty(o.Credential) {
		attrs = append(attrs, entity.String(RotationRequestCredentialId, o.Credential))
	}
	if !entity.Empty(o.ErrorMessage) {
		attrs = append(attrs, entity.String(RotationRequestErrorMessageId, o.ErrorMessage))
	}
	if !entity.Empty(o.NewSecret) {
		attrs = append(attrs, entity.String(RotationRequestNewSecretId, o.NewSecret))
	}
	if !entity.Empty(o.Status) {
		attrs = append(attrs, entity.String(RotationRequestStatusId, o.Status))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindRotationRequest))
	return
}

func (o *RotationRequest) Empty() bool {
	if !entity.Empty(o.Addon) {
		return false
	}
	if !entity.Empty(o.Association) {
		return false
	}
	if !entity.Empty(o.Credential) {
		return false
	}
	if !entity.Empty(o.ErrorMessage) {
		return false
	}
	if !entity.Empty(o.NewSecret) {
		return false
	}
	if !entity.Empty(o.Status) {
		return false
	}
	return true
}

func (o *RotationRequest) InitSchema(sb *schema.SchemaBuilder) {
	sb.Ref("addon", "dev.miren.addon/rotation_request.addon")
	sb.Ref("association", "dev.miren.addon/rotation_request.association", schema.Indexed)
	sb.String("credential", "dev.miren.addon/rotation_request.credential")
	sb.String("error_message", "dev.miren.addon/rotation_request.error_message")
	sb.String("new_secret", "dev.miren.addon/rotation_request.new_secret")
	sb.String("status", "dev.miren.addon/rotation_request.status", schema.Indexed)
}

const (
	ValkeyDedicatedDataValkeyServerId = entity.Id("dev.miren.addon/valkey_dedicated_data.valkey_server")
)

type ValkeyDedicatedData struct {
	ID           entity.Id `json:"id"`
	ValkeyServer entity.Id `cbor:"valkey_server,omitempty" json:"valkey_server,omitempty"`
}

func (o *ValkeyDedicatedData) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(ValkeyDedicatedDataValkeyServerId); ok && a.Value.Kind() == entity.KindId {
		o.ValkeyServer = a.Value.Id()
	}
}

func (o *ValkeyDedicatedData) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindValkeyDedicatedData)
}

func (o *ValkeyDedicatedData) ShortKind() string {
	return "valkey_dedicated_data"
}

func (o *ValkeyDedicatedData) Kind() entity.Id {
	return KindValkeyDedicatedData
}

func (o *ValkeyDedicatedData) EntityId() entity.Id {
	return o.ID
}

func (o *ValkeyDedicatedData) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.ValkeyServer) {
		attrs = append(attrs, entity.Ref(ValkeyDedicatedDataValkeyServerId, o.ValkeyServer))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindValkeyDedicatedData))
	return
}

func (o *ValkeyDedicatedData) Empty() bool {
	return entity.Empty(o.ValkeyServer)
}

func (o *ValkeyDedicatedData) InitSchema(sb *schema.SchemaBuilder) {
	sb.Ref("valkey_server", "dev.miren.addon/valkey_dedicated_data.valkey_server")
}

const (
	ValkeyServerAddonNameId        = entity.Id("dev.miren.addon/valkey_server.addon_name")
	ValkeyServerAssociationCountId = entity.Id("dev.miren.addon/valkey_server.association_count")
	ValkeyServerPasswordId         = entity.Id("dev.miren.addon/valkey_server.password")
	ValkeyServerSandboxPoolId      = entity.Id("dev.miren.addon/valkey_server.sandbox_pool")
	ValkeyServerServiceId          = entity.Id("dev.miren.addon/valkey_server.service")
	ValkeyServerStatusId           = entity.Id("dev.miren.addon/valkey_server.status")
	ValkeyServerVariantId          = entity.Id("dev.miren.addon/valkey_server.variant")
)

type ValkeyServer struct {
	ID               entity.Id `json:"id"`
	AddonName        string    `cbor:"addon_name,omitempty" json:"addon_name,omitempty"`
	AssociationCount int64     `cbor:"association_count,omitempty" json:"association_count,omitempty"`
	Password         string    `cbor:"password,omitempty" json:"password,omitempty"`
	SandboxPool      entity.Id `cbor:"sandbox_pool,omitempty" json:"sandbox_pool,omitempty"`
	Service          entity.Id `cbor:"service,omitempty" json:"service,omitempty"`
	Status           string    `cbor:"status,omitempty" json:"status,omitempty"`
	Variant          string    `cbor:"variant,omitempty" json:"variant,omitempty"`
}

func (o *ValkeyServer) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(ValkeyServerAddonNameId); ok && a.Value.Kind() == entity.KindString {
		o.AddonName = a.Value.String()
	}
	if a, ok := e.Get(ValkeyServerAssociationCountId); ok && a.Value.Kind() == entity.KindInt64 {
		o.AssociationCount = a.Value.Int64()
	}
	if a, ok := e.Get(ValkeyServerPasswordId); ok && a.Value.Kind() == entity.KindString {
		o.Password = a.Value.String()
	}
	if a, ok := e.Get(ValkeyServerSandboxPoolId); ok && a.Value.Kind() == entity.KindId {
		o.SandboxPool = a.Value.Id()
	}
	if a, ok := e.Get(ValkeyServerServiceId); ok && a.Value.Kind() == entity.KindId {
		o.Service = a.Value.Id()
	}
	if a, ok := e.Get(ValkeyServerStatusId); ok && a.Value.Kind() == entity.KindString {
		o.Status = a.Value.String()
	}
	if a, ok := e.Get(ValkeyServerVariantId); ok && a.Value.Kind() == entity.KindString {
		o.Variant = a.Value.String()
	}
}

func (o *ValkeyServer) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindValkeyServer)
}

func (o *ValkeyServer) ShortKind() string {
	return "valkey_server"
}

func (o *ValkeyServer) Kind() entity.Id {
	return KindValkeyServer
}

func (o *ValkeyServer) EntityId() entity.Id {
	return o.ID
}

func (o *ValkeyServer) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.AddonName) {
		attrs = append(attrs, entity.String(ValkeyServerAddonNameId, o.AddonName))
	}
	if !entity.Empty(o.AssociationCount) {
		attrs = append(attrs, entity.Int64(ValkeyServerAssociationCountId, o.AssociationCount))
	}
	if !entity.Empty(o.Password) {
		attrs = append(attrs, entity.String(ValkeyServerPasswordId, o.Password))
	}
	if !entity.Empty(o.SandboxPool) {
		attrs = append(attrs, entity.Ref(ValkeyServerSandboxPoolId, o.SandboxPool))
	}
	if !entity.Empty(o.Service) {
		attrs = append(attrs, entity.Ref(ValkeyServerServiceId, o.Service))
	}
	if !entity.Empty(o.Status) {
		attrs = append(attrs, entity.String(ValkeyServerStatusId, o.Status))
	}
	if !entity.Empty(o.Variant) {
		attrs = append(attrs, entity.String(ValkeyServerVariantId, o.Variant))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindValkeyServer))
	return
}

func (o *ValkeyServer) Empty() bool {
	if !entity.Empty(o.AddonName) {
		return false
	}
	if !entity.Empty(o.AssociationCount) {
		return false
	}
	if !entity.Empty(o.Password) {
		return false
	}
	if !entity.Empty(o.SandboxPool) {
		return false
	}
	if !entity.Empty(o.Service) {
		return false
	}
	if !entity.Empty(o.Status) {
		return false
	}
	if !entity.Empty(o.Variant) {
		return false
	}
	return true
}

func (o *ValkeyServer) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("addon_name", "dev.miren.addon/valkey_server.addon_name", schema.Indexed)
	sb.Int64("association_count", "dev.miren.addon/valkey_server.association_count")
	sb.String("password", "dev.miren.addon/valkey_server.password")
	sb.Ref("sandbox_pool", "dev.miren.addon/valkey_server.sandbox_pool")
	sb.Ref("service", "dev.miren.addon/valkey_server.service")
	sb.String("status", "dev.miren.addon/valkey_server.status")
	sb.String("variant", "dev.miren.addon/valkey_server.variant")
}

var (
	KindAddon                   = entity.Id("dev.miren.addon/kind.addon")
	KindAddonAssociation        = entity.Id("dev.miren.addon/kind.addon_association")
	KindMemcacheDedicatedData   = entity.Id("dev.miren.addon/kind.memcache_dedicated_data")
	KindMemcacheServer          = entity.Id("dev.miren.addon/kind.memcache_server")
	KindMysqlDedicatedData      = entity.Id("dev.miren.addon/kind.mysql_dedicated_data")
	KindMysqlServer             = entity.Id("dev.miren.addon/kind.mysql_server")
	KindMysqlSharedData         = entity.Id("dev.miren.addon/kind.mysql_shared_data")
	KindPostgresServer          = entity.Id("dev.miren.addon/kind.postgres_server")
	KindPostgresqlDedicatedData = entity.Id("dev.miren.addon/kind.postgresql_dedicated_data")
	KindPostgresqlSharedData    = entity.Id("dev.miren.addon/kind.postgresql_shared_data")
	KindRabbitmqDedicatedData   = entity.Id("dev.miren.addon/kind.rabbitmq_dedicated_data")
	KindRabbitmqServer          = entity.Id("dev.miren.addon/kind.rabbitmq_server")
	KindRotationRequest         = entity.Id("dev.miren.addon/kind.rotation_request")
	KindValkeyDedicatedData     = entity.Id("dev.miren.addon/kind.valkey_dedicated_data")
	KindValkeyServer            = entity.Id("dev.miren.addon/kind.valkey_server")
	Schema                      = entity.Id("dev.miren.addon/schema.v1alpha")
)

func init() {
	schema.Register("dev.miren.addon", "v1alpha", func(sb *schema.SchemaBuilder) {
		(&Addon{}).InitSchema(sb)
		(&AddonAssociation{}).InitSchema(sb)
		(&MemcacheDedicatedData{}).InitSchema(sb)
		(&MemcacheServer{}).InitSchema(sb)
		(&MysqlDedicatedData{}).InitSchema(sb)
		(&MysqlServer{}).InitSchema(sb)
		(&MysqlSharedData{}).InitSchema(sb)
		(&PostgresServer{}).InitSchema(sb)
		(&PostgresqlDedicatedData{}).InitSchema(sb)
		(&PostgresqlSharedData{}).InitSchema(sb)
		(&RabbitmqDedicatedData{}).InitSchema(sb)
		(&RabbitmqServer{}).InitSchema(sb)
		(&RotationRequest{}).InitSchema(sb)
		(&ValkeyDedicatedData{}).InitSchema(sb)
		(&ValkeyServer{}).InitSchema(sb)
	})
	schema.RegisterEncodedSchema("dev.miren.addon", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\xac\x9a[\xb2\xe34\x10\x86\xd7\xc1\xfd~\x87L\rPP\xacƥXJ\xa2\x89m9\x92\x92s\xf2\b\x14P\x14˘9\xbc\xb2:x\xa6lٱԭk</Sv\x8f\xf4\xd9R\xff\xddjw\xce+ڑ\x96\t\xca.\x9b\x96K\xd6m\b\xa5\xa2cG\xdeQ\xf5\xcf\xe3\x9b\xc0\xfel\xb0\x9b˿ǉg8\xc0\x9a\xfeߎ\x8a\x96\xf0\x0e\xc2w;\xce\x1a\xaa\xfex\xb9\xe5\xf4\xf1\x13/`Cَ\x9c\x1b]]\x88\xe4\xa4\xd3\xf3K\xbaF}\xed\xd9Niɻ}\x16\x8bI\xc5E\aX\x93\x11\xb2>\b\xb1T-y\xafg\xce\xd16@Ƈ\x01\x06W}C\xae\xd50\x7f\x844\x8e\x05R>\xf2S\x1aQ\x93\x86\xebk\xd5\nj0\xadk\x82\x1c\xe4Kù\xbd\x05\x85O\x7f\x1af\xbd\xeb\x9f5\xb9@іt\xd7\x7fǩ\x87\x9bm`\xf0Z\xb4\xbd\xe8X\xa7\x97+#\x19\x84ܸ\xc8,\xf1\xfc:.\xe9c\xf8r3#\xdbO\xe3\x1aߏ`4\u137d\xca\xfdlJ,\xf2\xd3\xf8\"gr\xd6b\x7f\x19\x17\xfb\x16|\xcb\t\xb19\xb2\xeb\xf8\xccz\xb8\x80^\x7f'4\xebB\x9a\xb3\xf193\x97\xd6\xcc\xfd\x14\x15\xfb\xcbs\xd2\xf4\a\xd2\xf4\x92\xb7D^\xab\xe1m\xe7\x1d\xf0\xe3o\v\f\xeb*J\xbf\xa9(:\x8a\x8d\x0f\xc3A:\xa6\xa7\xf6\xaaNM\xa5\x98\xbc09y\xe3m8\xd0\x1e\x93\xe5\x83?\xc7\xe5~\x16\xe3\x18\xd3\x12\xd6/\xac{\xe8\x96M\x1c\xa4\x94\xa89\x19\xc4Z\xd5\xe2<\xe5\xbf\x136\x0fؚwzdBɹL\xca\xd5qy7\xbe\xdc&\xb3\x96\x83\xe1-\xd9O\xb21\x97p\xfa\x97\xd1\xe9R\b]\xf5D\xa9\a!\xa9\xc9Z\xae\t⾈\xe2\x14\xe9\xe8V<V\xbd\x10\x8dI\xa5\x8ee\x80m9\xf5\xe7\n\x17\xc4\xe4\x85\xd7fa\xfb\xf9ƞ\x8e\xb2\xb0;]\x13}V\xe3\xec\xddt\r\x17\x12\x7f\xbe}\xce\xed=\xe7[4\x1a\x1a\x1b\x85\xfd7\x06Ņ4Gvu\xa3\xc2\x13\xbc֠\x82C\xfc\xf3(\xa8$.\x9e%H\xaf'0\\\xa8#\xc7CP\x89H\xd8.\xa5@\x8a\xa8N\x01\xa4\x94\x16=Ǟ3?-\xc6\xc4\x1b\xacRc\xeb\xb0\xf0˚\x1c\xcdښ\xd4\a\xe6\n\xf2=\x14#\xee\xb0,I\xfe\x1eHC.\xaaD\x94ϓ\xac\xbbd\xf9u\n[ )|0AVJT\xf8\U00100134\xac\x92o\xb1JX\x02\xd0\x02\xd2\xea\x85\xd2{\xc9TBZ`XA\x11\x80\xa4\x05P\xab\xa4\x85XwI\v\x9d\x9a\x10\x9b]\r\xa0L\x01Iɂ\x00\xc9\x1c\x12\xd6\xc8\x1c\xb1\x8ae\x8e\bi\x99\x7f\x9bd\x9c{&ϊI\xb7ԑ\x1e{2\x84 {]\b\x01Z \x84$\xd9n\xb9nO\x89\x10\x02\xc3\n\n\x06\x14B\x00\xb5*\x84\x10\xeb\xae\x10B5\r\xc4f\x96\rH\xfe\x90\xb3F\xfe\x88U,\x7fD\xb8#\xcbC\xc6:\x89\x02\x1a\xce@F\xa2B\x1b\xd7Iv:35\x7fv\xa3Oy8\xae\xa0\x84\xc0{\x05X\xc6lr\x9f\xb9\xb4\xb7\x1a;\x1eM_\x14h\x9a\x14\xb6\xc1F}\x95DՒQ\xd6iN\x8c\x84^X\xf7\xc9OO\x04cR\nY\xb5L\xa99\xb5\xb7\xae\t\"\xd3\xefױ\x87J\xb1Z2\xa3\x8a\x17\xd6}Z^\x10\x16\xd7hT]=\x84a//-\xce\xca\xf2Ǥ/\x7f[\xd0\x1e\x98%\xb0\xbf\xfc+E\xb0\xb8\xc2\xfc\xfdNw~ߛ\xde\xd0pa\xcf\xfd&k\xae\xd33=چ\x89\xf5\x94\xb9\x8e\xa1\xc0\xb0\xdbh\xcc\x18\x12M4\b\xc6n1\xe0\xac=\xff-\xd0\x0f\x1b\x00\x1b\xba\xadv\xbc\x99\x12\xe7|\x03\x95\x89\x92\x8b\x99\xdb\x0e\x87G\xd5\x13}0ڶ\xee\x93=XC\b\xf7\xca^z{\xb0fV/ŅS&\xa7\x03h\xbeK\x1e@f\xf6\xa0\x7f>V\x02\xbc\xdb7\xacz\x90\\O\xacK\xe0\xff\x062\xdd\n\xd1\xc4{s#\xdf\xff5\x8f\xfdW\x92k^yK\a̜\x0e>\xa79}\xb3\xc1\xed\xf9>\x83'βf\x15L\xd7\xd2c\xb7C\fU\x10\x1er4\x93=ys+\xa6\x8cG\xec\xb6q\x16\xcc\x17c\"\xc8\xe0\x03<Avc\x15\xf4\xe6\xfd\x9d\xe1\x01\x12oX\xa3F\xdb2O\xb1Nq\xcd/\xd3G\xcbr{\x13\xe6H@u\xeaB\xb8\xbb\xe9\xbdl\xa7\xff\xfb*\xb0g\x91\"(\x9bce\xe0\xbd\xe7ת\xe8k\x9f\x10/p\xdeM\x1d\xc4\x03\x91\x8cV\x94h\x12:\xef\xd0\xc0\x02I\xa0t\x80`\x9b\xe1\x9f-Ql)\xfd[ה\xdbH\xb7\x98vw\xd4\x14؎\xc5\x0e\xd9P\xf3ڢ\r\x9fo\xb7\x97;\xdc\xee\xb2=\x82\x88X\x04\x96G\x86lZ\x13\xed:%\xd0Jv\xc7\x16\xf8\x05}\xcf\xfax\xe5\xae\xc1m3\x1f\xb6\xc0;\xa8f\xf1\x02W:H\xfb\xa0X\x18vS\xdd\xeb\xa4P\x8b\xf5\x0e/\xfd<\xae\xfe\xbb,\xa0\xdb\xe87nrMӎF7\xe1\xece\xe3\xb3\xc8i\xb8y\x13H\xb0\xdfrw\x16A\a\xb6\x9fX\xae\xd7\x1f2\xc1\xa0\x87b\xfe\x9e\x00\x1amݢ\xbc\x17\x00\xafT\xeeŏ\xc5\x05\xa0ہ\xf7\xaa7\xdcɽ[\xbf?f\"\xe1o\x03f\x7f\xa11G\xc5\x0f\x81'\x04v\xe4\xd6x\xc8ۑ\xc0\xf05;\x12@\xc2~\x9c\xd9\x11h\xccڑ\xc0\x13\xf01\n#ۻ'\xc16\xf3\x9a\xd3\b\xedJ\x10Z\x1e\xe2?峋\xa3<\xd8\x15~\xedG\xd4\xe3\x1bA4\x9cwT\a!ue\xfe\x9ck\xfa\xab\x89\xc8\x1fu\xb9\xbf$\xa7\xff\xbc\x02\xfc֗\xf1\xd3s\xe6\x8f8`Tf\xdf\x1a\x8cB\xadĬn7n\x11\xe5u \xb3km4\xceS\x13fV\xe9\xfeb%\xbf\xa0\f\x9c\xf3\x05\xd5N\xe8\xd0))\x15\n\xf3t`t0\xb7\x15e\xfbHl\x95&\xc9\xff\x01\x00\x00\xff\xff\x01\x00\x00\xff\xff\xeb\x9a:R[)\x00\x00"))
}
