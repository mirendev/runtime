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
	schema.RegisterEncodedSchema("dev.miren.ingress", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\x94\x96ے\xdb \f\x86_\xa4\x9d\x1e\xa7纳O\xc4\x10#\xdb\xdap\n`or\xd9\xde\xf4A\xba\xed\x1b\xb6\xd7\x1d\x83\xb36\xe08\xe4&#\v\xf1\t\xc9?\x8a\x1f\x99\xa4\x02\x0e\f\x86J\xa0\x01Y\xa1l\rX\v{\x94\xcc>\x1e_e+\xdfƕ\xaasN\x13\xa3z\a\x7f<\xe1\xf8,\x0f\x9cc\x02\xed_Ô\xa0(\xf3lM\x83\xc0\x99\xfd\xf9k\x87\xec\xf8r\x8bTQ\xad}\xc2z4\xdcI\xc3\x0e\x99\xdf\xf6i{[\xef:\xa2\x8d\x1a\x90\x81\xf1\x00\x11\xbb&\xd4\xef\x11\xf5y\x13Us\x8a\x82\b\xaa5\xca\xd62A\xe5\xe9\xaf'\xcadeDb\xad\x84V\x12\xa4\x9b\xad\xa9cy\x96\xeab\x96\xc2\x06\xfe\xf0\x9dx\x97\x1f?\xa6\x05\xb8?\x05\x04s<jc\x9dA\xd9z\xc4\xfb\xab\x88\x0e蹓\xcdd/ \xed\x00Ƣ\x92\xedpG\xb9\xee(\xd7\x06\x055'2\xd6!<\xeaL\xf2\xf9\xdenv\x9cAC{\xee|\xb2\xf6\xfc0fc;\xa5\xb8\a\xac\xe8t\x01\xe8\x94\r\xbb\x99\xb7\xd2j\xbfln6p\xe8\xc1:\xe2P\x80\xea\x03G\xa5\xce\x14\xf9a\x13\xf9@\x9bQy\rr\xf0\xb8\xfd\xd21)q\xb3\x85\xf73\xec\xf8\xfa\xc2\x15]0'\xc5=\xcf#\x17A\x85\x1a\xfb~\xa9e\vT\xa5\xa9\xa1R!%\x1c\x06\xe0\xe1v$\xbe\xb1\xcc\x1a\xa5۬s٘5\x91\xf8B\x15\xb2\xfa\xe9\"O\xa5\xbe\xc8c\xa3\xb0\x9b&\xd2\xc7+\xb0\xaa\xe6\b\xd2\x11d>9Ώ\xa9,\xbe\x16\x92,\xd4\x06\x82\xd4D\xecJ\x89+\xb3*!*\xd9`K\ueb52AkKGJ\xab\nh\x12j\xa7\f\xf1\xf7/\x8c\xbdؗ2W^[\xcc\xf4\x17s\xfe)\xb8\x9d\xf1\xfe\xb3Az\x13\xa4\xc6#O\xca[\x19\x8f1\xcf\xd6J\x83\r\xa3m\xb2\x8bG[DZ\x9b\x02^\xb1\x9aZ\xfb\xa0\fKU\xfb&\x8f\xcfBo\xfa+X9@\x06\xbc\xd6\xff\xbb\x12Ɠ\xa7\xa3\xb6\v\xba\x8d]\xa5\x1d<d\xec4|o;e\x1c\t\x1f(\xcbAx\xfd[%\x1a'\x05s3y\x9dE\x03(/\xa0\\\x06\xff\x01\x00\x00\xff\xff\x01\x00\x00\xff\xff\x1c\x06\xbc\xb1\x8e\t\x00\x00"))
}
