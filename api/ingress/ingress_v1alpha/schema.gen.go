package ingress_v1alpha

import (
	entity "miren.dev/runtime/pkg/entity"
	schema "miren.dev/runtime/pkg/entity/schema"
)

const (
	HttpRouteAppId            = entity.Id("dev.miren.ingress/http_route.app")
	HttpRouteAuthProviderId   = entity.Id("dev.miren.ingress/http_route.auth_provider")
	HttpRouteClaimMappingsId  = entity.Id("dev.miren.ingress/http_route.claim_mappings")
	HttpRouteDefaultId        = entity.Id("dev.miren.ingress/http_route.default")
	HttpRouteHostId           = entity.Id("dev.miren.ingress/http_route.host")
	HttpRouteMaintenanceId    = entity.Id("dev.miren.ingress/http_route.maintenance")
	HttpRouteRequestTimeoutId = entity.Id("dev.miren.ingress/http_route.request_timeout")
	HttpRouteWafProfileId     = entity.Id("dev.miren.ingress/http_route.waf_profile")
)

type HttpRoute struct {
	ID             entity.Id       `json:"id"`
	App            entity.Id       `cbor:"app,omitempty" json:"app,omitempty"`
	AuthProvider   entity.Id       `cbor:"auth_provider,omitempty" json:"auth_provider,omitempty"`
	ClaimMappings  []ClaimMappings `cbor:"claim_mappings,omitempty" json:"claim_mappings,omitempty"`
	Default        bool            `cbor:"default,omitempty" json:"default,omitempty"`
	Host           string          `cbor:"host,omitempty" json:"host,omitempty"`
	Maintenance    Maintenance     `cbor:"maintenance,omitempty" json:"maintenance"`
	RequestTimeout string          `cbor:"request_timeout,omitempty" json:"request_timeout,omitempty"`
	WafProfile     entity.Id       `cbor:"waf_profile,omitempty" json:"waf_profile,omitempty"`
}

func (o *HttpRoute) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(HttpRouteAppId); ok && a.Value.Kind() == entity.KindId {
		o.App = a.Value.Id()
	}
	if a, ok := e.Get(HttpRouteAuthProviderId); ok && a.Value.Kind() == entity.KindId {
		o.AuthProvider = a.Value.Id()
	}
	for _, a := range e.GetAll(HttpRouteClaimMappingsId) {
		if a.Value.Kind() == entity.KindComponent {
			var v ClaimMappings
			v.Decode(a.Value.Component())
			o.ClaimMappings = append(o.ClaimMappings, v)
		}
	}
	if a, ok := e.Get(HttpRouteDefaultId); ok && a.Value.Kind() == entity.KindBool {
		o.Default = a.Value.Bool()
	}
	if a, ok := e.Get(HttpRouteHostId); ok && a.Value.Kind() == entity.KindString {
		o.Host = a.Value.String()
	}
	if a, ok := e.Get(HttpRouteMaintenanceId); ok && a.Value.Kind() == entity.KindComponent {
		o.Maintenance.Decode(a.Value.Component())
	}
	if a, ok := e.Get(HttpRouteRequestTimeoutId); ok && a.Value.Kind() == entity.KindString {
		o.RequestTimeout = a.Value.String()
	}
	if a, ok := e.Get(HttpRouteWafProfileId); ok && a.Value.Kind() == entity.KindId {
		o.WafProfile = a.Value.Id()
	}
}

func (o *HttpRoute) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindHttpRoute)
}

func (o *HttpRoute) ShortKind() string {
	return "http_route"
}

func (o *HttpRoute) Kind() entity.Id {
	return KindHttpRoute
}

func (o *HttpRoute) EntityId() entity.Id {
	return o.ID
}

func (o *HttpRoute) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.App) {
		attrs = append(attrs, entity.Ref(HttpRouteAppId, o.App))
	}
	if !entity.Empty(o.AuthProvider) {
		attrs = append(attrs, entity.Ref(HttpRouteAuthProviderId, o.AuthProvider))
	}
	for _, v := range o.ClaimMappings {
		attrs = append(attrs, entity.Component(HttpRouteClaimMappingsId, v.Encode()))
	}
	attrs = append(attrs, entity.Bool(HttpRouteDefaultId, o.Default))
	if !entity.Empty(o.Host) {
		attrs = append(attrs, entity.String(HttpRouteHostId, o.Host))
	}
	if !o.Maintenance.Empty() {
		attrs = append(attrs, entity.Component(HttpRouteMaintenanceId, o.Maintenance.Encode()))
	}
	if !entity.Empty(o.RequestTimeout) {
		attrs = append(attrs, entity.String(HttpRouteRequestTimeoutId, o.RequestTimeout))
	}
	if !entity.Empty(o.WafProfile) {
		attrs = append(attrs, entity.Ref(HttpRouteWafProfileId, o.WafProfile))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindHttpRoute))
	return
}

func (o *HttpRoute) Empty() bool {
	if !entity.Empty(o.App) {
		return false
	}
	if !entity.Empty(o.AuthProvider) {
		return false
	}
	if len(o.ClaimMappings) != 0 {
		return false
	}
	if !entity.Empty(o.Default) {
		return false
	}
	if !entity.Empty(o.Host) {
		return false
	}
	if !o.Maintenance.Empty() {
		return false
	}
	if !entity.Empty(o.RequestTimeout) {
		return false
	}
	if !entity.Empty(o.WafProfile) {
		return false
	}
	return true
}

func (o *HttpRoute) InitSchema(sb *schema.SchemaBuilder) {
	sb.Ref("app", "dev.miren.ingress/http_route.app", schema.Doc("The application to route to"), schema.Indexed, schema.Tags("dev.miren.app_ref"))
	sb.Ref("auth_provider", "dev.miren.ingress/http_route.auth_provider", schema.Doc("Reference to an auth provider (OIDC or password) for authentication"), schema.Indexed)
	sb.Component("claim_mappings", "dev.miren.ingress/http_route.claim_mappings", schema.Doc("Mappings from JWT claims to HTTP headers"), schema.Many)
	(&ClaimMappings{}).InitSchema(sb.Builder("http_route.claim_mappings"))
	sb.Bool("default", "dev.miren.ingress/http_route.default", schema.Doc("Whether this is the default route for routing"), schema.Indexed)
	sb.String("host", "dev.miren.ingress/http_route.host", schema.Doc("The hostname to match on for the application"), schema.Indexed)
	sb.Component("maintenance", "dev.miren.ingress/http_route.maintenance", schema.Doc("Operator-initiated maintenance state. When present, the router serves a holding page instead of proxying to the app. Absent means the route serves normally."))
	(&Maintenance{}).InitSchema(sb.Builder("http_route.maintenance"))
	sb.String("request_timeout", "dev.miren.ingress/http_route.request_timeout", schema.Doc("Per-route override for the ingress request timeout (e.g. \"10m\", \"300s\"). Empty falls back to the server's http_request_timeout (default 60s). Must be a positive duration; invalid or non-positive values are ignored at request time."))
	sb.Ref("waf_profile", "dev.miren.ingress/http_route.waf_profile", schema.Doc("Reference to a WAF profile for request filtering"))
}

const (
	ClaimMappingsClaimId  = entity.Id("dev.miren.ingress/claim_mappings.claim")
	ClaimMappingsHeaderId = entity.Id("dev.miren.ingress/claim_mappings.header")
)

type ClaimMappings struct {
	Claim  string `cbor:"claim,omitempty" json:"claim,omitempty"`
	Header string `cbor:"header,omitempty" json:"header,omitempty"`
}

func (o *ClaimMappings) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(ClaimMappingsClaimId); ok && a.Value.Kind() == entity.KindString {
		o.Claim = a.Value.String()
	}
	if a, ok := e.Get(ClaimMappingsHeaderId); ok && a.Value.Kind() == entity.KindString {
		o.Header = a.Value.String()
	}
}

func (o *ClaimMappings) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Claim) {
		attrs = append(attrs, entity.String(ClaimMappingsClaimId, o.Claim))
	}
	if !entity.Empty(o.Header) {
		attrs = append(attrs, entity.String(ClaimMappingsHeaderId, o.Header))
	}
	return
}

func (o *ClaimMappings) Empty() bool {
	if !entity.Empty(o.Claim) {
		return false
	}
	if !entity.Empty(o.Header) {
		return false
	}
	return true
}

func (o *ClaimMappings) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("claim", "dev.miren.ingress/claim_mappings.claim", schema.Doc("The JWT claim name (e.g. email, sub, name)"))
	sb.String("header", "dev.miren.ingress/claim_mappings.header", schema.Doc("The HTTP header name to inject (e.g. X-User-Email)"))
}

const (
	MaintenanceBackAtId    = entity.Id("dev.miren.ingress/maintenance.back_at")
	MaintenanceReasonId    = entity.Id("dev.miren.ingress/maintenance.reason")
	MaintenanceStartedAtId = entity.Id("dev.miren.ingress/maintenance.started_at")
	MaintenanceStartedById = entity.Id("dev.miren.ingress/maintenance.started_by")
)

type Maintenance struct {
	BackAt    string `cbor:"back_at,omitempty" json:"back_at,omitempty"`
	Reason    string `cbor:"reason,omitempty" json:"reason,omitempty"`
	StartedAt string `cbor:"started_at,omitempty" json:"started_at,omitempty"`
	StartedBy string `cbor:"started_by,omitempty" json:"started_by,omitempty"`
}

func (o *Maintenance) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(MaintenanceBackAtId); ok && a.Value.Kind() == entity.KindString {
		o.BackAt = a.Value.String()
	}
	if a, ok := e.Get(MaintenanceReasonId); ok && a.Value.Kind() == entity.KindString {
		o.Reason = a.Value.String()
	}
	if a, ok := e.Get(MaintenanceStartedAtId); ok && a.Value.Kind() == entity.KindString {
		o.StartedAt = a.Value.String()
	}
	if a, ok := e.Get(MaintenanceStartedById); ok && a.Value.Kind() == entity.KindString {
		o.StartedBy = a.Value.String()
	}
}

func (o *Maintenance) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.BackAt) {
		attrs = append(attrs, entity.String(MaintenanceBackAtId, o.BackAt))
	}
	if !entity.Empty(o.Reason) {
		attrs = append(attrs, entity.String(MaintenanceReasonId, o.Reason))
	}
	if !entity.Empty(o.StartedAt) {
		attrs = append(attrs, entity.String(MaintenanceStartedAtId, o.StartedAt))
	}
	if !entity.Empty(o.StartedBy) {
		attrs = append(attrs, entity.String(MaintenanceStartedById, o.StartedBy))
	}
	return
}

func (o *Maintenance) Empty() bool {
	if !entity.Empty(o.BackAt) {
		return false
	}
	if !entity.Empty(o.Reason) {
		return false
	}
	if !entity.Empty(o.StartedAt) {
		return false
	}
	if !entity.Empty(o.StartedBy) {
		return false
	}
	return true
}

func (o *Maintenance) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("back_at", "dev.miren.ingress/maintenance.back_at", schema.Doc("RFC 3339 timestamp for the expected end of the window. Drives the Retry-After header and the holding page copy. Empty means no estimate was given."))
	sb.String("reason", "dev.miren.ingress/maintenance.reason", schema.Doc("Operator-supplied explanation shown on the holding page"))
	sb.String("started_at", "dev.miren.ingress/maintenance.started_at", schema.Doc("RFC 3339 timestamp for when maintenance began"))
	sb.String("started_by", "dev.miren.ingress/maintenance.started_by", schema.Doc("Identifier of the operator who put the route into maintenance"))
}

const (
	OidcProviderClientIdId      = entity.Id("dev.miren.ingress/oidc_provider.client_id")
	OidcProviderClientSecretId  = entity.Id("dev.miren.ingress/oidc_provider.client_secret")
	OidcProviderConfigJsonId    = entity.Id("dev.miren.ingress/oidc_provider.config_json")
	OidcProviderConnectorTypeId = entity.Id("dev.miren.ingress/oidc_provider.connector_type")
	OidcProviderNameId          = entity.Id("dev.miren.ingress/oidc_provider.name")
	OidcProviderProviderUrlId   = entity.Id("dev.miren.ingress/oidc_provider.provider_url")
	OidcProviderScopesId        = entity.Id("dev.miren.ingress/oidc_provider.scopes")
)

type OidcProvider struct {
	ID            entity.Id `json:"id"`
	ClientId      string    `cbor:"client_id,omitempty" json:"client_id,omitempty"`
	ClientSecret  string    `cbor:"client_secret,omitempty" json:"client_secret,omitempty"`
	ConfigJson    string    `cbor:"config_json,omitempty" json:"config_json,omitempty"`
	ConnectorType string    `cbor:"connector_type,omitempty" json:"connector_type,omitempty"`
	Name          string    `cbor:"name,omitempty" json:"name,omitempty"`
	ProviderUrl   string    `cbor:"provider_url,omitempty" json:"provider_url,omitempty"`
	Scopes        string    `cbor:"scopes,omitempty" json:"scopes,omitempty"`
}

func (o *OidcProvider) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(OidcProviderClientIdId); ok && a.Value.Kind() == entity.KindString {
		o.ClientId = a.Value.String()
	}
	if a, ok := e.Get(OidcProviderClientSecretId); ok && a.Value.Kind() == entity.KindString {
		o.ClientSecret = a.Value.String()
	}
	if a, ok := e.Get(OidcProviderConfigJsonId); ok && a.Value.Kind() == entity.KindString {
		o.ConfigJson = a.Value.String()
	}
	if a, ok := e.Get(OidcProviderConnectorTypeId); ok && a.Value.Kind() == entity.KindString {
		o.ConnectorType = a.Value.String()
	}
	if a, ok := e.Get(OidcProviderNameId); ok && a.Value.Kind() == entity.KindString {
		o.Name = a.Value.String()
	}
	if a, ok := e.Get(OidcProviderProviderUrlId); ok && a.Value.Kind() == entity.KindString {
		o.ProviderUrl = a.Value.String()
	}
	if a, ok := e.Get(OidcProviderScopesId); ok && a.Value.Kind() == entity.KindString {
		o.Scopes = a.Value.String()
	}
}

func (o *OidcProvider) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindOidcProvider)
}

func (o *OidcProvider) ShortKind() string {
	return "oidc_provider"
}

func (o *OidcProvider) Kind() entity.Id {
	return KindOidcProvider
}

func (o *OidcProvider) EntityId() entity.Id {
	return o.ID
}

func (o *OidcProvider) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.ClientId) {
		attrs = append(attrs, entity.String(OidcProviderClientIdId, o.ClientId))
	}
	if !entity.Empty(o.ClientSecret) {
		attrs = append(attrs, entity.String(OidcProviderClientSecretId, o.ClientSecret))
	}
	if !entity.Empty(o.ConfigJson) {
		attrs = append(attrs, entity.String(OidcProviderConfigJsonId, o.ConfigJson))
	}
	if !entity.Empty(o.ConnectorType) {
		attrs = append(attrs, entity.String(OidcProviderConnectorTypeId, o.ConnectorType))
	}
	if !entity.Empty(o.Name) {
		attrs = append(attrs, entity.String(OidcProviderNameId, o.Name))
	}
	if !entity.Empty(o.ProviderUrl) {
		attrs = append(attrs, entity.String(OidcProviderProviderUrlId, o.ProviderUrl))
	}
	if !entity.Empty(o.Scopes) {
		attrs = append(attrs, entity.String(OidcProviderScopesId, o.Scopes))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindOidcProvider))
	return
}

func (o *OidcProvider) Empty() bool {
	if !entity.Empty(o.ClientId) {
		return false
	}
	if !entity.Empty(o.ClientSecret) {
		return false
	}
	if !entity.Empty(o.ConfigJson) {
		return false
	}
	if !entity.Empty(o.ConnectorType) {
		return false
	}
	if !entity.Empty(o.Name) {
		return false
	}
	if !entity.Empty(o.ProviderUrl) {
		return false
	}
	if !entity.Empty(o.Scopes) {
		return false
	}
	return true
}

func (o *OidcProvider) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("client_id", "dev.miren.ingress/oidc_provider.client_id", schema.Doc("The OAuth2 client ID"))
	sb.String("client_secret", "dev.miren.ingress/oidc_provider.client_secret", schema.Doc("The OAuth2 client secret"))
	sb.String("config_json", "dev.miren.ingress/oidc_provider.config_json", schema.Doc("Connector-specific configuration as a JSON object (e.g. {\"orgs\":[{\"name\":\"mirendev\"}]}). Only meaningful when connector_type is non-empty and not \"oidc\"."))
	sb.String("connector_type", "dev.miren.ingress/oidc_provider.connector_type", schema.Doc("The provider implementation. Empty or \"oidc\" uses the built-in OIDC discovery client (consumes provider_url + scopes). Any other value names a Dex-backed connector (e.g. \"github\") and uses config_json for connector-specific configuration."), schema.Indexed)
	sb.String("name", "dev.miren.ingress/oidc_provider.name", schema.Doc("A unique name for this auth provider. Despite the kind name, this entity backs all OAuth2-flavored providers (OIDC discovery, GitHub, and other connector-based ones). Kept under the `oidc_provider` kind for backward compatibility with existing v0.8.0 entities."), schema.Indexed)
	sb.String("provider_url", "dev.miren.ingress/oidc_provider.provider_url", schema.Doc("The OIDC provider URL (e.g. https://accounts.google.com). Only meaningful when connector_type is empty or \"oidc\"."), schema.Indexed)
	sb.String("scopes", "dev.miren.ingress/oidc_provider.scopes", schema.Doc("Space-separated list of OAuth2 scopes (e.g. \"openid email profile\"). Only meaningful when connector_type is empty or \"oidc\"; connectors choose their own scopes."))
}

const (
	PasswordProviderNameId         = entity.Id("dev.miren.ingress/password_provider.name")
	PasswordProviderPasswordHashId = entity.Id("dev.miren.ingress/password_provider.password_hash")
)

type PasswordProvider struct {
	ID           entity.Id `json:"id"`
	Name         string    `cbor:"name,omitempty" json:"name,omitempty"`
	PasswordHash string    `cbor:"password_hash,omitempty" json:"password_hash,omitempty"`
}

func (o *PasswordProvider) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(PasswordProviderNameId); ok && a.Value.Kind() == entity.KindString {
		o.Name = a.Value.String()
	}
	if a, ok := e.Get(PasswordProviderPasswordHashId); ok && a.Value.Kind() == entity.KindString {
		o.PasswordHash = a.Value.String()
	}
}

func (o *PasswordProvider) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindPasswordProvider)
}

func (o *PasswordProvider) ShortKind() string {
	return "password_provider"
}

func (o *PasswordProvider) Kind() entity.Id {
	return KindPasswordProvider
}

func (o *PasswordProvider) EntityId() entity.Id {
	return o.ID
}

func (o *PasswordProvider) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Name) {
		attrs = append(attrs, entity.String(PasswordProviderNameId, o.Name))
	}
	if !entity.Empty(o.PasswordHash) {
		attrs = append(attrs, entity.String(PasswordProviderPasswordHashId, o.PasswordHash))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindPasswordProvider))
	return
}

func (o *PasswordProvider) Empty() bool {
	if !entity.Empty(o.Name) {
		return false
	}
	if !entity.Empty(o.PasswordHash) {
		return false
	}
	return true
}

func (o *PasswordProvider) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("name", "dev.miren.ingress/password_provider.name", schema.Doc("A unique name for this password provider"), schema.Indexed)
	sb.String("password_hash", "dev.miren.ingress/password_provider.password_hash", schema.Doc("bcrypt hash of the shared password"))
}

const (
	WafProfileParanoiaLevelId = entity.Id("dev.miren.ingress/waf_profile.paranoia_level")
)

type WafProfile struct {
	ID            entity.Id `json:"id"`
	ParanoiaLevel int64     `cbor:"paranoia_level,omitempty" json:"paranoia_level,omitempty"`
}

func (o *WafProfile) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(WafProfileParanoiaLevelId); ok && a.Value.Kind() == entity.KindInt64 {
		o.ParanoiaLevel = a.Value.Int64()
	}
}

func (o *WafProfile) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindWafProfile)
}

func (o *WafProfile) ShortKind() string {
	return "waf_profile"
}

func (o *WafProfile) Kind() entity.Id {
	return KindWafProfile
}

func (o *WafProfile) EntityId() entity.Id {
	return o.ID
}

func (o *WafProfile) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.ParanoiaLevel) {
		attrs = append(attrs, entity.Int64(WafProfileParanoiaLevelId, o.ParanoiaLevel))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindWafProfile))
	return
}

func (o *WafProfile) Empty() bool {
	return entity.Empty(o.ParanoiaLevel)
}

func (o *WafProfile) InitSchema(sb *schema.SchemaBuilder) {
	sb.Int64("paranoia_level", "dev.miren.ingress/waf_profile.paranoia_level", schema.Doc("OWASP CRS paranoia level (1-4)"))
}

var (
	KindHttpRoute        = entity.Id("dev.miren.ingress/kind.http_route")
	KindOidcProvider     = entity.Id("dev.miren.ingress/kind.oidc_provider")
	KindPasswordProvider = entity.Id("dev.miren.ingress/kind.password_provider")
	KindWafProfile       = entity.Id("dev.miren.ingress/kind.waf_profile")
	Schema               = entity.Id("dev.miren.ingress/schema.v1alpha")
)

func init() {
	schema.Register("dev.miren.ingress", "v1alpha", func(sb *schema.SchemaBuilder) {
		(&HttpRoute{}).InitSchema(sb)
		(&OidcProvider{}).InitSchema(sb)
		(&PasswordProvider{}).InitSchema(sb)
		(&WafProfile{}).InitSchema(sb)
	})
	schema.RegisterEncodedSchema("dev.miren.ingress", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\x9c\x96Y\x92\xd3<\x10\x80/\xf2\xffž\x83\xa99\x91J\xb6\xdavO\xace$9\x93<BQ\x05\xf7 pCx\xa6,\xd9\x13-\x8e#x\x99j\xb5[_\xaf\xd3ʉ\t\xca\xe1\x8e\xc1\xbe\xe2\xa8AT(:\r\xc6\xc0\x0e\x053\xa7Ó\xec\xcb\xc7\xe9K\xd5[\xab\x88\x96\xa3\x85\x9f\x8ep\xf8/7<\xdbx\xda\xef\x96INQ\xe4\xde\xda\x16a`\xe6\xdb\xf7\x1a\xd9\xe1\xf1\x16\xa9\xa2J9\x87\xcd$أ\x82\x1a\x99\xbb\xf6v\xfb\xdah{\xa2\xb4\xdc#\x03\xed\x00<Vͨ\x1f\x13\xea\xdd&\xaa\x19(r©R(:\xc38\x15\xc7_\x8e(\x92/\x13\x12\x1bɕ\x14 \xecY\x9a+\x96{\xa9.z),\xe0gW\x89\x97y\xf81\xcd\xc3]\x14\xe0\xc5)\xd4\xd6X\x8d\xa2s\x88WW\x11=Х\x92\xed,\a\x90n\x0fڠ\x14\xdd\xfe\x86\x0e\xaa\xa7\x83\xd2ȩ>\x92)\x0f\xeeP\v\xc9\xf9{\xbeYq\x06-\x1d\a\xeb\x9cu\xcba\xf2\xc6j)\a\aX\x99\xd3\x00\xd0K\xe3o3'\x05\x81\x9e\xa6˯7/O\x15\xb7 \xa8h\xc01v\xa1\xe2J\x8fsr\xb5N.l\xf0\x17\x97\xec\x8b<\xde\x00Uմ\xd9\x11:\x97k9\xa4\x1d^\xa9x\xc8\xd0@\x8d\x14\xbe\xbd\xb3\x9c\x12V\xaa\x16\x12\x8c\xa5\xda\x02[\x02\xb9\r\xce\xffH\xaa\x8f1\xa9>\x16\x8f\\\xd83\xe7\xf2\xfdf\xcb5܍`,\xb1\xc8A\x8e>\x01\x99*\v\xb2\b\x90\xf7\xb4\x9d\x96M\x8b\xc3<E\xa1b^>\x9b)ܞa\x87\xa7\x17\xb6r\xc0\x9c\a\xf0\xff\xdc20*\x9c\xbaO\x97J\x16\xa0*E5\x15\x12)\x19`\x0f\x83_\x88\x89nJ\xb3Aa\xb7[\x15@צ\xd4%*\x915\x0f\xbb{N\xf5Qn\x1b\x99\x15&\xfb\xd5%\xfb\xe6\n\xacj\x06\x04a\t2\xe7\x1c\xcf\xc7t,>\x14\x92\f4\x1a\xfc\xa8\xf1X\x95\x12W\x9e\xa7\x84(E\x8b\x1d\xb9]\xfe\x83w\xa1\"\xa5U\x054\x01\x8d\x95\x9a\xb8\x95\xeb_\xbaXW\xb0\\b\xa6\xdb\xc5\xe7?\xe9\xfd\x95Q\x8b\xef/\x02\x19\xb5\x1f\xb5!Ҥ\xbc\x95\x171\xe6\x99F*0~\xdd\xcdr\xf1k\x16\x91ֶ\x80\x9bXE\x8d\xb9\x97\x9a\xa5S\xfb,\xb7\xcfL\xff\xea\xf5_\t \x03^\xab\xffM\t\xe3A\xd3S\xd3\xfb\xb9\x8dU\xa5\x15\xbc\xcbة\xf9\xce\xf4R[\xe2\x7f\x93\x86\x8b\xf0\xfa\xcf\xd3h\x9d\x14\xecͤ\x9dE\v(O\xa0|\f\xfe\x00\x00\x00\xff\xff\x01\x00\x00\xff\xffo\x96ԕ\x81\v\x00\x00"))
}
