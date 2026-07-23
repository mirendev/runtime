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
	AddonAssociationErrorMessageId = entity.Id("dev.miren.addon/addon_association.error_message")
	AddonAssociationStatusId       = entity.Id("dev.miren.addon/addon_association.status")
	AddonAssociationVariablesId    = entity.Id("dev.miren.addon/addon_association.variables")
	AddonAssociationVariantId      = entity.Id("dev.miren.addon/addon_association.variant")
	AddonAssociationVersionId      = entity.Id("dev.miren.addon/addon_association.version")
)

type AddonAssociation struct {
	ID           entity.Id   `json:"id"`
	Addon        entity.Id   `cbor:"addon,omitempty" json:"addon,omitempty"`
	App          entity.Id   `cbor:"app,omitempty" json:"app,omitempty"`
	ErrorMessage string      `cbor:"error_message,omitempty" json:"error_message,omitempty"`
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
	if a, ok := e.Get(AddonAssociationErrorMessageId); ok && a.Value.Kind() == entity.KindString {
		o.ErrorMessage = a.Value.String()
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
	if !entity.Empty(o.ErrorMessage) {
		attrs = append(attrs, entity.String(AddonAssociationErrorMessageId, o.ErrorMessage))
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
	if !entity.Empty(o.ErrorMessage) {
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
	sb.String("error_message", "dev.miren.addon/addon_association.error_message")
	sb.String("status", "dev.miren.addon/addon_association.status", schema.Indexed)
	sb.Component("variables", "dev.miren.addon/addon_association.variables", schema.Many)
	(&Variables{}).InitSchema(sb.Builder("addon_association.variables"))
	sb.String("variant", "dev.miren.addon/addon_association.variant")
	sb.String("version", "dev.miren.addon/addon_association.version")
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
	schema.RegisterEncodedSchema("dev.miren.addon", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\xac\x9aێ\xf34\x10ǟ\x83\xf3\xf9\f\xfd\xf4\x01\x02\xf14\x91\x1b\xbb\xad\xb7I\x9c\xb5\xdd\xec\xf6\x12\x90@<\a|\xdc\xf2tp\x8d\x12'\x8d=\xe3c\xf3ݬ\x92\x91\xfdK\xec\xf9\xcfx2ݿhGZ&(\x1bv-\x97\xac\xdb\x11JE\xc7μ\xa3\xea\x9f\xe77\x81\xfd\xc5h7\x97\x7fO\x13/p\x805\xfd\xbf\x03\x15-\xe1\x1d\x84\x1f\x0e\x9c5T\xfd\xfe\xe7\x9e\xd3\xe7O\xbc\x80\x1de\arit5\x10\xc9I\xa7\x97\x97t\x8d\xfaڳ\x83Ғw\xc7,\x16\x93\x8a\x8b\x0e\xb0f#d}\x10b\xa9Z\xf2^/\x9c\xb3m\x80\x8c\x0f\x03\f\xae\xfa\x86\\\xabq\xfe\x04i\x1c\v\xa4|\xe4\xa74\xa2&\r\xd7ת\x15\xd4`Z\xd7\x049ȗ\x86s{\v\n\x9f\xfej\x9c\xf5\xae\x7f\xd6\xec\x02E[\xd2]\xff\x9d\xa6\x9en\xb6\x91\xc1k\xd1\xf6\xa2c\x9d^\xaf\x8cd\x10r\xe7\"\xb3\xc4\xf3봤\x8f\xe1\xcb-\x8cl?Mk|?\x82ф7\xf6*\x8f\x8b)\xb1\xc8O\xe3\x8b\\\xc8Y\x8b\xfdeZ\xec[\xf0-g\xc4\xee̮\xd33\xeb\xf1\x02z\xfd\x9dЬ\x814\x17\xe3sf.\xad\x99\xc79*\x8e\xc3K\xd2\xf4'\xd2\xf4\x92\xb7D^\xab\xf1m\x97\x1d\xf0\xe3o\v\f\xeb*J\xbf\xa9(:\x8aM\x0f\xc3A:\xa5\xa7\xf6\xaa\x1e\x9bJ1909{\xe3m8\xd0\x1e\x93\xe5\x83?\xa6\xe5~\x16\xe3\x18\xd3\x1a\xd6\x0f\xd6=t\xcb.\x0eRJԜ\x8cb\xadjq\x99\xf3\xdf#6\x8fؚwzbBɹL\xca\xd5y}7\xbe\xde&\xb3\x96\x83\xe1-9β1\x97p\xfa\x97\xd1\xe9R\b]\xf5D\xa9'!\xa9\xc9Z\xae\t⾈\xe2\x14\xe9\xe8^<W\xbd\x10\x8dI\xa5\x8ee\x84\xed9\xf5\xe7\n\x17\xc4\xe4\xc0k\xb3\xb0\xe3rcOGY؝\xae\x89\xbe\xa8i\xf6a\xbe\x86\v\x89?\xdf>玞\xf3-\x1a\r\x8d\x8d\xc2\xfe\x9b\x82b ͙]ݨ\xf0\x04\xaf5\xa8\xe0\x10\xff<\n*\x89\x8b\x17\t\xd2\xeb\t\f\x17\xea\xc8\xf1\x14T\"\x12\xb6K)\x90\"\xaaS\x00)\xa5Eϱ\xe7\xccO\x8b1\xf1\x06\x9b\xd4\xd8:,\xfc\xb2&G\xb3\xb6&\xf5\x89\xb9\x82|\x0fň;,K\x92\xbf\x05Ґ\x8b*\x11\xe5\xcb$\xeb.Y~\x9d\xc2\x16H\n\x1fL\x90\x95\x12\x15>= !-\xab\xe4[l\x12\x96\x00\xb4\x80\xb4z\xa1\xf4Q2\x95\x90\x16\x18VP\x04 i\x01\xd4&i!\xd6]\xd2B\xa7&\xc4fW\x03(S@R\xb2 @2\x87\x84-2G\xacb\x99#BZ\xe6\xdf&\x19\x97\x9eɋb\xd2-u\xa4Ǟ\f!\xc8\xde\x16B\x80\x16\b!I\xf6{\xae\xdb\xc7D\b\x81a\x05\x05\x03\n!\x80\xda\x14B\x88uW\b\xa1\x9a\x06b3\xcb\x06$\x7f\xc8\xd9\"\x7f\xc4*\x96?\"ܑ\xe5!c\x9bD\x01\rg #Q\xa1\x8d\xeb${\xbc0\xb5|v\xa3Oy8\xae\xa0\x84\xc0{\x05X\xc6lr\x9f\xb9\xb4\xb7\x1a;\x1eM_\x15h\x9a\x14\xb6\xc1F}\x95DՒQ\xd6iN\x8c\x84\x1e\xac\xfb\xe4\xa7'\x821)\x85\xacZ\xa6Ԓ\xda[\xd7\x04\x91\xe9\xf7\xeb\xd8S\xa5X-\x99QŃu\x9f\x96\x17\x84\xc55\x1aUW\x0fa\xd8\xcbk\x8b\xb3\xb2\xfc1\xeb\xcb\xdf\x16\xb4\a\x16dA\xb4R\x04\x8b+\xcc\xdf\xeft\xe7\xf7\xbd\xe9\r\x8d\x17\xf6\\\xf4\xa1\x85\xe7\x16\xcb\x00\xa5Ľ\xbb\xee\x95WL\x982\xe5\x94}\xc3\xec\xc6\x1c_\x8d\x89\xd6\x1c|\x00v\xf6\xfa\x80\x82f\xa4\xbf\x156B\xe2\x1d:\xd4YX\xe7)\xd6)\xae\xf90Wi\xeb\xedȠ{!\x9a\x89\x80\x0e\xe6\x95pw\x97o\xddN\x7fA\x19سH\xd6\xcf\xe6Xm\xfa\xa3\xa7=\x1f}\xedG\xc4\v\x04\xf8\xdc29\x11\xc9hE\x89&\xa1\x00G\x03\v$\x81\x82\f\xc1v\xe3\x9f=Ql\xaduZה\xdb9\xb4\x98v;\xc8T\x14\x8e\xc5N\x03\xa1n\x9dE\x1b\xeb\xd5\xdb˝nw\xd9\x1eAD,\x02\xcb#\x94Q^\x13\xed:%\xd0;s\xc7\x16\xf8\x05\x15\xf0>^\xb9kp\x9f\xc0\x87-\xf0\xce7Y\xc0\x8d\x0e\xd2>(\x16\x86\xddE\xf4:)\xd4S\xba\xc3K?O\xab\xff.\v\xe8v6\x8d\x9b\\Ӽ\xa3\xd1M\xb8x\xd9\xf8,r:\f\xde\x04\x12\xfc\xc0\xbc;\x8b|\x9fG,\xd7\xeb\x0f\x99`\xf0\xd1h~@\x85\xc6hq\x11\x00oT\xee\xe0\xc7\xe2\xca\xdbm9z\xd5\x1bn]ݭ\xdf\x1f3\x91\xb0\x19j\xf6\x17\x1asT\xfc\x14xB`Gn_Zy;\x12\x18\xbeeG\x02H\u06000;\x02\x8dY;\x12x\x02>Fad{\xf7$\xd8W\xdbr\x1a\xa1]\tB\xcbC\xfc\xa7|vq\x94\a\xdb`\xaf\xfd\x88z~#\x88\x86\xf3\xce\xea$\xa4\xae\xcc\xff\xaf\xcc?\x13G\xfe\x8b\xc5\xfd\xe9,\xfd{2\xf8q#㷶̮5\x18\x95٨\x03\xa3P\xef$\xab\xbd\x87\xbf\x89\xf3Z.ٵ6\x1a\xe7\xa9\t3\xabt\x7f\xb1\x92_P\x06\xce\xf9\x82j't蔔\n\x85y:0:\x98ۊ\xb2}$\xb6J\x93\xe4\xff\x00\x00\x00\xff\xff\x01\x00\x00\xff\xff\xb0\xbbL\xeeL&\x00\x00"))
}
