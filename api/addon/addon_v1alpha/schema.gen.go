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
	AddonAssociationAddonId        = entity.Id("dev.miren.addon/addon_association.addon")
	AddonAssociationAppId          = entity.Id("dev.miren.addon/addon_association.app")
	AddonAssociationDisksId        = entity.Id("dev.miren.addon/addon_association.disks")
	AddonAssociationErrorMessageId = entity.Id("dev.miren.addon/addon_association.error_message")
	AddonAssociationServicesId     = entity.Id("dev.miren.addon/addon_association.services")
	AddonAssociationStatusId       = entity.Id("dev.miren.addon/addon_association.status")
	AddonAssociationVariablesId    = entity.Id("dev.miren.addon/addon_association.variables")
	AddonAssociationVariantId      = entity.Id("dev.miren.addon/addon_association.variant")
	AddonAssociationVersionId      = entity.Id("dev.miren.addon/addon_association.version")
)

type AddonAssociation struct {
	ID           entity.Id   `json:"id"`
	Addon        entity.Id   `cbor:"addon,omitempty" json:"addon,omitempty"`
	App          entity.Id   `cbor:"app,omitempty" json:"app,omitempty"`
	Disks        []Disks     `cbor:"disks,omitempty" json:"disks,omitempty"`
	ErrorMessage string      `cbor:"error_message,omitempty" json:"error_message,omitempty"`
	Services     []string    `cbor:"services,omitempty" json:"services,omitempty"`
	Status       string      `cbor:"status,omitempty" json:"status,omitempty"`
	Variables    []Variables `cbor:"variables,omitempty" json:"variables,omitempty"`
	Variant      string      `cbor:"variant,omitempty" json:"variant,omitempty"`
	Version      string      `cbor:"version,omitempty" json:"version,omitempty"`
}

func (o *AddonAssociation) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(AddonAssociationAddonId); ok && a.Value.Kind() == entity.KindId {
		o.Addon = a.Value.Id()
	}
	if a, ok := e.Get(AddonAssociationAppId); ok && a.Value.Kind() == entity.KindId {
		o.App = a.Value.Id()
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
	for _, v := range o.Disks {
		attrs = append(attrs, entity.Component(AddonAssociationDisksId, v.Encode()))
	}
	if !entity.Empty(o.ErrorMessage) {
		attrs = append(attrs, entity.String(AddonAssociationErrorMessageId, o.ErrorMessage))
	}
	for _, v := range o.Services {
		attrs = append(attrs, entity.String(AddonAssociationServicesId, v))
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
	if len(o.Disks) != 0 {
		return false
	}
	if !entity.Empty(o.ErrorMessage) {
		return false
	}
	if len(o.Services) != 0 {
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
	sb.Component("disks", "dev.miren.addon/addon_association.disks", schema.Many)
	(&Disks{}).InitSchema(sb.Builder("addon_association.disks"))
	sb.String("error_message", "dev.miren.addon/addon_association.error_message")
	sb.String("services", "dev.miren.addon/addon_association.services", schema.Many)
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
	schema.RegisterEncodedSchema("dev.miren.addon", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\xac\x9aے\xe34\x13ǟ\xe3\xfb8\x9f\xcf\xd9Z\xa0\xa0x\x1a\x97b)\x896\xb6\xe5\x91\x14\xcf\xe4\x12(\xa0x\x8e\xdd喧\x83kʖ\x1dK\xdd:\xc6{3e\xf7H?K\xea\x7f\xb7\xe4v^ю\xb4LP6\xecZ.Y\xb7#\x94\x8a\x8e\x9dyG\xd5\xdfO\xff\a\xf6g\xa3\xdd\\\xfe5u\xbc\xc0\x06V\xf7\x7f\x0fT\xb4\x84w\x10~8p\xd6P\xf5\xc7\xcb=\xa7O\x9fx\x01;\xca\x0e\xe4\xd2\xe8j \x92\x93N/\x83t\x8d\xfaڳ\x83Ғw\xc7,\x16\x93\x8a\x8b\x0e\xb0f#d}\x10b\xa9Z\xf2^/\x9c\xb3m\x80\x8c\x0f\x03\f\xae\xfa\x86\\\xab\xb1\xff\x04i\x1c\v\xa4|\xe4\xa74\xa2&\r\xd7ת\x15\xd4`Z\xd7\x049ȗ\x86s\x1b\x05\x85O\x7f=\xf6z\xd7\xdfkv\x81\xa2-\xe9\xae\xffL]O7\xdb\xc8\xe0\xb5h{ѱN\xafWF2\b\xb9s\x91Y\xe2\xf9u\x9a\xd2\xc7pp\v#\xdbO\xd3\x1cߏ`4\xe1\x8d=\xcb\xe3bJL\xf2\xd3\xf8$\x17r\xd6d\x7f\x99&\xfb\x16\x1c\xe5\x8c؝\xd9uzf=^@\xaf\xbf\x13\xea5\x90\xe6b|\xce̥\xd5\xf38G\xc5qxN\x9a\xfeD\x9a^\xf2\x96\xc8k5\x8evY\x01?\xfe6\xc1\xb0\xae\xa2\xf4\x9b\x8a\xa2\xad\xd8\xf40\x1c\xa4Szj\xafꡩ\x14\x93\x03\x93\xb37ކ\r\xed6Y>\xf8s\x9a\xeeg1\x8e1\xada\xfdº\x87n\xd9\xc5AJ\x89\x9a\x93Q\xacU-.s\xfe{\xc0\xe6\x11[\xf3NOL(9\x97I\xb9:\xafc\xe3\xebm2k9\x18ޒ\xe3,\x1bs\t\xbb\x7f\x19\xed.\x85\xd0UO\x94z\x14\x92\x9a\xac\xe5\x9a \xee\x8b(N\x91\x8e\xee\xc5S\xd5\vјT\xeaXF؞S\x7f\xaepAL\x0e\xbc6\x13;.7vw\x94\x85\xdd\xee\x9a苚z\x1f\xe6k8\x91\xf8\xf3\xed}\xee\xe8\xd9ߢ\xd1\xd0\xd8(\xec\xbf)(\x06Ҝ\xd9Ս\nO\xf0Z\x8d\n6\xf1ϣ\xa0\x92\xb8x\x96 \xbd\x99\xc0p\xa1\x8e\x1cOA%\"a\xbb\x94\x02)\xa2s\n \xa5\xb4\xe8\xd9\xf6\x9c\xfei1&F\xb0I\x8d\xad\xc3\u008359\x9a\xb55\xa9O\xcc\x15\xe4{(F\xdcfY\x92\xfc=\x90\x86\\T\x89(\x9f'Yw\xc9\xf2\xeb\x14\xb6@Rxc\x82\xac\x94\xa8\xf0\xee\x01\tiY%G\xb1IX\x02\xd0\x02\xd2\xea\x85\xd2G\xc9TBZ\xa0Y\xc1!\x00I\v\xa06I\v\xb1\xee\x92\x16\xda5!6\xfb4\x802\x05$%\x0f\x04H搰E\xe6\x88U,sDH\xcb\xfc\xdb$\xe3\xd23yQL\xbaG\x1d\xe9\xb1'C\b\xb2\xb7\x85\x10\xa0\x05BH\x92\xfd\x9e\xeb\xf6!\x11B\xa0Y\xc1\x81\x01\x85\x10@m\n!ĺ+\x84Й\x06b3\x8f\rH\xfe\x90\xb3E\xfe\x88U,\x7fD\xb8#\xcbC\xc66\x89\x02\x1a\xce@F\xa2B\x1b\xd7I\xf6pajy\xedF\xaf\xf2\xb0]\xc1\x11\x02\xaf\x15`\x19\xb3\xc9}\xe6\xd2^j\xecx\xd4}U\xa0)R\xd8\x06\x1b\xf5U\x12UKFY\xa791\x12za\xdd'_=\x11\x8cI)d\xd52\xa5\x96\xd4\u07ba&\x88L\x8f\xafc\x8f\x95b\xb5dF\x15/\xac\xfb\xb4\xbc ,\xaeѨ\xbaz\b\xc3^^K\x9c\x95\xe5\x8fY_\xfe\xb2\xa0\xddpK5\x01\xc1\xe2\n\xf3\xd7;\xdd\xfe}ojC\xe3\xc5\xdc\xf7u\xe6\xb3\xc7C\x81]\xfabƐ(|A0^J\x03\xceZ\xa7\xdf\x025\xac\x11\xb0\xa3\xfb\xea\xc0\x9b9\xd9-7PM(!\x98\xbe\xed\x98\xf0\xab\x9e\xe8\x93ѣu\x9f\xac\x9b\x1aB\xb8\xbe\xf5\xd2[75\xbdz)\x06N\x99\x9c7\x8d\xe5.\xb9i\x98ޣf\xf9\xb4{\xf3\xeeذ\xeaQr=\xb3\x86\xc0\xffF2\xdd\v\xd1\xc4\xebi\x13\xdf\xff\x06\x8e\xfdW\x92\x1f^y\xb7{̜7+\xa7\xa0|\xb3\xc1\xe5A{\xb3\x87\x17\xcd\x11\xaf\xbdY\vS\xa6\xcdk\xdf8\xc3\xe2\xab1\x11\n\xf0\x01\x9eP\xb8\xb1\n\xaa\xde\xfe\x9a\xeb\b\x89\x97\x82Q\tk\xed\xa7X\xa7\xb8\xe6\xc3\xfc:\xb0\xde\xde\xe43\x11\xd0\tp%\xdc]N^\x97\xd3\xff\xe6\x12X\xb3\xc8\xf1\"\x9bc}\x0f:z\xbe\x03E\x87\xfd\x80x\x81\x9dd\xae͝\x88d\xb4\xa2D\x93\xd0N\x82\x1a\x16H\x02\x05-\x82\xed\xc6?{\xa2\xd8z\xa8n]Sn\x89\xdab\xdauGstu,\xf6^\x15*\v[\xb4\xf1\xc5\xe86\xb8\xd3\xed.\xdb#\x88\x88E`yd\xccy5ѮS\x02EZ\xb7m\x81_Л\xa2\x8fW\xee\x1a\\\x90\xf2a\v\xbc\xf3M\x16p\xa3\x83\xb4\x0f\x8a\x85a\x97\xab\xbdN\n\x15/\xef\xf0\xd2\xcf\xd3\xec\xbf\xcb\x02\xba%t\xe3&\xd74\xafht\x11.^6ދ\x9cR\x967\x81\x04+\x19wg\x91\xef\xf3\x88\xe5z\xfd!\x13\f\xaa\x13\xe6K=4ںEy/\x00ި\xdc\xc1\x8f\xc5\xc74\xb7\xb6\xedUo\xb8Fz\xb7~\x7f\xccDª\xbbY_h\xccQ\xf1c\xe0\t\x81\x15\xb9\xbd\xd2\xe7\xadH\xa0\xf9\x96\x15\t a\xa5ˬ\b4f\xadH\xe0\tx\x1b\x85\x91\xed]\x93`\x01w\xcbn\x84V%\b-\x0f\xf1\x9f\xf2\xd9\xc5Q\x1e\xac\xb7\xbe\xf1-\xea\xe9\x7fA4\xecwV'!ue~(5\xff\x1e!\xf2s)\xf7\x1bm\xfa\x87\v\xe0+Z\xc6G\xdd\xcc\xcf#\xa0UfE\x18\xb4BE\xba\xac:2.\xbe\xe4\xd5\xf6\xb2\xcfڨ\x9d\xe7L\x98yJ\xf7\x1fV\xf2\x0f\x94\x81}\xbe\xe0\xb4\x13\xdatJ\x8e\n\x85y:\xd0:\x98ۊ\xb2}$\xb6J\x93\xe4\x7f\x00\x00\x00\xff\xff\x01\x00\x00\xff\xfff\xa6\xc9\x12\xb5(\x00\x00"))
}
