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
	HttpRouteServiceId        = entity.Id("dev.miren.ingress/http_route.service")
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
	Service        string          `cbor:"service,omitempty" json:"service,omitempty"`
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
	if a, ok := e.Get(HttpRouteServiceId); ok && a.Value.Kind() == entity.KindString {
		o.Service = a.Value.String()
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
	if !entity.Empty(o.Service) {
		attrs = append(attrs, entity.String(HttpRouteServiceId, o.Service))
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
	if !entity.Empty(o.Service) {
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
	sb.String("service", "dev.miren.ingress/http_route.service", schema.Doc("The HTTP-capable app service to route to. Empty defaults to web for routes stored before this field existed."))
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
	schema.RegisterEncodedSchema("dev.miren.ingress", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\x9c\x96ێ\xd4<\f\xc7_\xe4\xfb\xc4\xf9\fE\xfbDQڸ\xadw\x9a\xc3&iw\xe6\x12\x84\x84x\x0e\x16\xde\x10\xaeQ\x93v'\x87n'p3J\\\xe7\xe7\xf8o\x8f\xdb;&(\x87\x1b\x06S\xc5Q\x83\xa8Pt\x1a\x8c\x81\x03\nf\xee\x8eO\xb2'\x1f\xe7'Uo\xad\"Z\x8e\x16~:\xc2\xf1\xbf\xdc\xf1\xec\xe3i\xbf[&9E\x91Gk[\x84\x81\x99o\xdfkd\xc7\xc7{\xa4\x8a*\xe5\x026\xf3\u009e\x14\xd4\xc8ܱ\xb7\xfb\xc7F\xdb\x13\xa5\xe5\x84\f\xb4\x03\xf0ش\xa0~̨w\xbb\xa8f\xa0\xc8\t\xa7J\xa1\xe8\f\xe3T\x9c~9\xa2H\x9e\xccHl$WR\x80\xb0\xe7բX\x1e\xa5z0J\xa1\x80\x9f\x9d\x12/\xf3\xeb\xc74\x0fw\xb7\x00\xbf\x9c\xaf\xda\x1a\xabQt\x0e\xf1\xea\"\xa2\a\xba*\xd9.\xeb\x00\xd2M\xa0\rJ\xd1MWtP=\x1d\x94FN\xf5\x89\xccyp\x87ZI.\xde\xf3]\xc5\x19\xb4t\x1c\xac\v֭\x9b9\x1a\xab\xa5\x1c\x1c`\xa3O\x03@/\x8d?\xcd\xdc*\xb8\xe8\xdd|\xf8\xf5\xee\xe1Yq\v\x82\x8a\x06\x1c\xe3\x10\x1a.\xd48'W\xdb\xe4\xc2\x02\x7fqɾ\xc8\xef\x1b\xa0\xaa\x9a6\aB\x17\xb9\xd6MZ\xe1\r\xc5C\x86\x06j\xa4\xf0\xe5]\xd6)aC\xb5\x90`,\xd5\x16\xd8z\x91\xeb`\xff\x8f\xa4\xfa\x14\x93\xeaSq˅5s!\xdf\xef\x96\\\xc3\xcd\b\xc6\x12\x8b\x1c\xe4\xe8\x13\x90\xa9\xb1@\xd1\x00i@O\xb8tP\xb7n\n\x84\b\x10\xb7\xb4\x9d\xe7U\x8b\xc3҈\xa1a\x99_\xbb*\\\x9faǧ\x0f\f\xf6\x80\xb9\xf4\xf0\xff\xb9g\xe0Tظ\x9f\x1eR=@U\x8aj*$R2\xc0\x04\x83\x9f\xa9\x89mN\xb3Aa\xf7\xab\x1d@\xb7\xca\xe2\x12\x95Ț\xfb\xf1\xbf\xa4\xfa(\xf7\x8d\xdc\n\x93\xfd\xea\x92}s\x01V5\x03\x82\xb0\x04\x99\v\x8e\xe7m\xda\x16\x1f\nI\x06\x1a\r\xbe[ylJ\x89\x1bo\xb8\x84(E\x8b\x1d\xb9^\x87\xc0!4\xa4\xb4\xaa\x80&\xa0\xb1R\x137\xb5\xfd\xcb2\xb6\x15\xfc\x9bb\xa6\x1b\xe7\xe7\x9f\xf4\xfcF\xab\xc5\xe7\xd7\x05\x19\xb5o\xb5!\xb2\xa4\xbc\x8d\x97j\xcc3\x8dT`\xfc\xc4\\\xd6\xc5/Ĉ\xb45\x05\\\xc7*j̭\xd4,\xed\xdag\xb9\x7f\xe6\xfaW\x1f\x10\x1b\x17Ȁ\x97\xf4\xbf*a\xdc[zjz߷\xb1\xa9T\xc1\x9b\x8c\x9d\xba\x1fL/\xb5%\xfe\xb36\x1c\x84\x97\xbfp\xa3qR07\x93r\x16\r\xa0<\x81\xf26\xf8\x03\x00\x00\xff\xff\x01\x00\x00\xff\xff&t^\x94\xc4\v\x00\x00"))
}
