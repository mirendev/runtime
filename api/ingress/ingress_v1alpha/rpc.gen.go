package ingress_v1alpha

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/fxamacker/cbor/v2"
	rpc "miren.dev/runtime/pkg/rpc"
)

type routeClaimMappingData struct {
	Claim  *string `cbor:"0,keyasint,omitempty" json:"claim,omitempty"`
	Header *string `cbor:"1,keyasint,omitempty" json:"header,omitempty"`
}

type RouteClaimMapping struct {
	data routeClaimMappingData
}

func (v *RouteClaimMapping) HasClaim() bool {
	return v.data.Claim != nil
}

func (v *RouteClaimMapping) Claim() string {
	if v.data.Claim == nil {
		return ""
	}
	return *v.data.Claim
}

func (v *RouteClaimMapping) SetClaim(claim string) {
	v.data.Claim = &claim
}

func (v *RouteClaimMapping) HasHeader() bool {
	return v.data.Header != nil
}

func (v *RouteClaimMapping) Header() string {
	if v.data.Header == nil {
		return ""
	}
	return *v.data.Header
}

func (v *RouteClaimMapping) SetHeader(header string) {
	v.data.Header = &header
}

func (v *RouteClaimMapping) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RouteClaimMapping) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RouteClaimMapping) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RouteClaimMapping) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type routeInfoData struct {
	Host                 *string               `cbor:"0,keyasint,omitempty" json:"host,omitempty"`
	App                  *string               `cbor:"1,keyasint,omitempty" json:"app,omitempty"`
	Service              *string               `cbor:"2,keyasint,omitempty" json:"service,omitempty"`
	DefaultRoute         *bool                 `cbor:"3,keyasint,omitempty" json:"default_route,omitempty"`
	Provider             *string               `cbor:"4,keyasint,omitempty" json:"provider,omitempty"`
	ProviderType         *string               `cbor:"5,keyasint,omitempty" json:"provider_type,omitempty"`
	ConnectorType        *string               `cbor:"6,keyasint,omitempty" json:"connector_type,omitempty"`
	ClaimMappings        *[]*RouteClaimMapping `cbor:"7,keyasint,omitempty" json:"claim_mappings,omitempty"`
	WafLevel             *int64                `cbor:"8,keyasint,omitempty" json:"waf_level,omitempty"`
	RequestTimeout       *string               `cbor:"9,keyasint,omitempty" json:"request_timeout,omitempty"`
	TlsCheck             *string               `cbor:"10,keyasint,omitempty" json:"tls_check,omitempty"`
	MaintenanceReason    *string               `cbor:"11,keyasint,omitempty" json:"maintenance_reason,omitempty"`
	MaintenanceBackAt    *string               `cbor:"12,keyasint,omitempty" json:"maintenance_back_at,omitempty"`
	MaintenanceStartedAt *string               `cbor:"13,keyasint,omitempty" json:"maintenance_started_at,omitempty"`
	MaintenanceStartedBy *string               `cbor:"14,keyasint,omitempty" json:"maintenance_started_by,omitempty"`
	CreatedAt            *int64                `cbor:"15,keyasint,omitempty" json:"created_at,omitempty"`
	UpdatedAt            *int64                `cbor:"16,keyasint,omitempty" json:"updated_at,omitempty"`
	Protected            *bool                 `cbor:"17,keyasint,omitempty" json:"protected,omitempty"`
	ProviderUrl          *string               `cbor:"18,keyasint,omitempty" json:"provider_url,omitempty"`
	WafEnabled           *bool                 `cbor:"19,keyasint,omitempty" json:"waf_enabled,omitempty"`
	Maintenance          *bool                 `cbor:"20,keyasint,omitempty" json:"maintenance,omitempty"`
}

type RouteInfo struct {
	data routeInfoData
}

func (v *RouteInfo) HasHost() bool {
	return v.data.Host != nil
}

func (v *RouteInfo) Host() string {
	if v.data.Host == nil {
		return ""
	}
	return *v.data.Host
}

func (v *RouteInfo) SetHost(host string) {
	v.data.Host = &host
}

func (v *RouteInfo) HasApp() bool {
	return v.data.App != nil
}

func (v *RouteInfo) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *RouteInfo) SetApp(app string) {
	v.data.App = &app
}

func (v *RouteInfo) HasService() bool {
	return v.data.Service != nil
}

func (v *RouteInfo) Service() string {
	if v.data.Service == nil {
		return ""
	}
	return *v.data.Service
}

func (v *RouteInfo) SetService(service string) {
	v.data.Service = &service
}

func (v *RouteInfo) HasDefaultRoute() bool {
	return v.data.DefaultRoute != nil
}

func (v *RouteInfo) DefaultRoute() bool {
	if v.data.DefaultRoute == nil {
		return false
	}
	return *v.data.DefaultRoute
}

func (v *RouteInfo) SetDefaultRoute(default_route bool) {
	v.data.DefaultRoute = &default_route
}

func (v *RouteInfo) HasProvider() bool {
	return v.data.Provider != nil
}

func (v *RouteInfo) Provider() string {
	if v.data.Provider == nil {
		return ""
	}
	return *v.data.Provider
}

func (v *RouteInfo) SetProvider(provider string) {
	v.data.Provider = &provider
}

func (v *RouteInfo) HasProviderType() bool {
	return v.data.ProviderType != nil
}

func (v *RouteInfo) ProviderType() string {
	if v.data.ProviderType == nil {
		return ""
	}
	return *v.data.ProviderType
}

func (v *RouteInfo) SetProviderType(provider_type string) {
	v.data.ProviderType = &provider_type
}

func (v *RouteInfo) HasConnectorType() bool {
	return v.data.ConnectorType != nil
}

func (v *RouteInfo) ConnectorType() string {
	if v.data.ConnectorType == nil {
		return ""
	}
	return *v.data.ConnectorType
}

func (v *RouteInfo) SetConnectorType(connector_type string) {
	v.data.ConnectorType = &connector_type
}

func (v *RouteInfo) HasClaimMappings() bool {
	return v.data.ClaimMappings != nil
}

func (v *RouteInfo) ClaimMappings() []*RouteClaimMapping {
	if v.data.ClaimMappings == nil {
		return nil
	}
	return *v.data.ClaimMappings
}

func (v *RouteInfo) SetClaimMappings(claim_mappings []*RouteClaimMapping) {
	x := slices.Clone(claim_mappings)
	v.data.ClaimMappings = &x
}

func (v *RouteInfo) HasWafLevel() bool {
	return v.data.WafLevel != nil
}

func (v *RouteInfo) WafLevel() int64 {
	if v.data.WafLevel == nil {
		return 0
	}
	return *v.data.WafLevel
}

func (v *RouteInfo) SetWafLevel(waf_level int64) {
	v.data.WafLevel = &waf_level
}

func (v *RouteInfo) HasRequestTimeout() bool {
	return v.data.RequestTimeout != nil
}

func (v *RouteInfo) RequestTimeout() string {
	if v.data.RequestTimeout == nil {
		return ""
	}
	return *v.data.RequestTimeout
}

func (v *RouteInfo) SetRequestTimeout(request_timeout string) {
	v.data.RequestTimeout = &request_timeout
}

func (v *RouteInfo) HasTlsCheck() bool {
	return v.data.TlsCheck != nil
}

func (v *RouteInfo) TlsCheck() string {
	if v.data.TlsCheck == nil {
		return ""
	}
	return *v.data.TlsCheck
}

func (v *RouteInfo) SetTlsCheck(tls_check string) {
	v.data.TlsCheck = &tls_check
}

func (v *RouteInfo) HasMaintenanceReason() bool {
	return v.data.MaintenanceReason != nil
}

func (v *RouteInfo) MaintenanceReason() string {
	if v.data.MaintenanceReason == nil {
		return ""
	}
	return *v.data.MaintenanceReason
}

func (v *RouteInfo) SetMaintenanceReason(maintenance_reason string) {
	v.data.MaintenanceReason = &maintenance_reason
}

func (v *RouteInfo) HasMaintenanceBackAt() bool {
	return v.data.MaintenanceBackAt != nil
}

func (v *RouteInfo) MaintenanceBackAt() string {
	if v.data.MaintenanceBackAt == nil {
		return ""
	}
	return *v.data.MaintenanceBackAt
}

func (v *RouteInfo) SetMaintenanceBackAt(maintenance_back_at string) {
	v.data.MaintenanceBackAt = &maintenance_back_at
}

func (v *RouteInfo) HasMaintenanceStartedAt() bool {
	return v.data.MaintenanceStartedAt != nil
}

func (v *RouteInfo) MaintenanceStartedAt() string {
	if v.data.MaintenanceStartedAt == nil {
		return ""
	}
	return *v.data.MaintenanceStartedAt
}

func (v *RouteInfo) SetMaintenanceStartedAt(maintenance_started_at string) {
	v.data.MaintenanceStartedAt = &maintenance_started_at
}

func (v *RouteInfo) HasMaintenanceStartedBy() bool {
	return v.data.MaintenanceStartedBy != nil
}

func (v *RouteInfo) MaintenanceStartedBy() string {
	if v.data.MaintenanceStartedBy == nil {
		return ""
	}
	return *v.data.MaintenanceStartedBy
}

func (v *RouteInfo) SetMaintenanceStartedBy(maintenance_started_by string) {
	v.data.MaintenanceStartedBy = &maintenance_started_by
}

func (v *RouteInfo) HasCreatedAt() bool {
	return v.data.CreatedAt != nil
}

func (v *RouteInfo) CreatedAt() int64 {
	if v.data.CreatedAt == nil {
		return 0
	}
	return *v.data.CreatedAt
}

func (v *RouteInfo) SetCreatedAt(created_at int64) {
	v.data.CreatedAt = &created_at
}

func (v *RouteInfo) HasUpdatedAt() bool {
	return v.data.UpdatedAt != nil
}

func (v *RouteInfo) UpdatedAt() int64 {
	if v.data.UpdatedAt == nil {
		return 0
	}
	return *v.data.UpdatedAt
}

func (v *RouteInfo) SetUpdatedAt(updated_at int64) {
	v.data.UpdatedAt = &updated_at
}

func (v *RouteInfo) HasProtected() bool {
	return v.data.Protected != nil
}

func (v *RouteInfo) Protected() bool {
	if v.data.Protected == nil {
		return false
	}
	return *v.data.Protected
}

func (v *RouteInfo) SetProtected(protected bool) {
	v.data.Protected = &protected
}

func (v *RouteInfo) HasProviderUrl() bool {
	return v.data.ProviderUrl != nil
}

func (v *RouteInfo) ProviderUrl() string {
	if v.data.ProviderUrl == nil {
		return ""
	}
	return *v.data.ProviderUrl
}

func (v *RouteInfo) SetProviderUrl(provider_url string) {
	v.data.ProviderUrl = &provider_url
}

func (v *RouteInfo) HasWafEnabled() bool {
	return v.data.WafEnabled != nil
}

func (v *RouteInfo) WafEnabled() bool {
	if v.data.WafEnabled == nil {
		return false
	}
	return *v.data.WafEnabled
}

func (v *RouteInfo) SetWafEnabled(waf_enabled bool) {
	v.data.WafEnabled = &waf_enabled
}

func (v *RouteInfo) HasMaintenance() bool {
	return v.data.Maintenance != nil
}

func (v *RouteInfo) Maintenance() bool {
	if v.data.Maintenance == nil {
		return false
	}
	return *v.data.Maintenance
}

func (v *RouteInfo) SetMaintenance(maintenance bool) {
	v.data.Maintenance = &maintenance
}

func (v *RouteInfo) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RouteInfo) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RouteInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RouteInfo) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type routesSetMaintenanceArgsData struct {
	Host         *string `cbor:"0,keyasint,omitempty" json:"host,omitempty"`
	DefaultRoute *bool   `cbor:"1,keyasint,omitempty" json:"default_route,omitempty"`
	Reason       *string `cbor:"2,keyasint,omitempty" json:"reason,omitempty"`
	BackAt       *string `cbor:"3,keyasint,omitempty" json:"back_at,omitempty"`
}

type RoutesSetMaintenanceArgs struct {
	call rpc.Call
	data routesSetMaintenanceArgsData
}

func (v *RoutesSetMaintenanceArgs) HasHost() bool {
	return v.data.Host != nil
}

func (v *RoutesSetMaintenanceArgs) Host() string {
	if v.data.Host == nil {
		return ""
	}
	return *v.data.Host
}

func (v *RoutesSetMaintenanceArgs) HasDefaultRoute() bool {
	return v.data.DefaultRoute != nil
}

func (v *RoutesSetMaintenanceArgs) DefaultRoute() bool {
	if v.data.DefaultRoute == nil {
		return false
	}
	return *v.data.DefaultRoute
}

func (v *RoutesSetMaintenanceArgs) HasReason() bool {
	return v.data.Reason != nil
}

func (v *RoutesSetMaintenanceArgs) Reason() string {
	if v.data.Reason == nil {
		return ""
	}
	return *v.data.Reason
}

func (v *RoutesSetMaintenanceArgs) HasBackAt() bool {
	return v.data.BackAt != nil
}

func (v *RoutesSetMaintenanceArgs) BackAt() string {
	if v.data.BackAt == nil {
		return ""
	}
	return *v.data.BackAt
}

func (v *RoutesSetMaintenanceArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RoutesSetMaintenanceArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RoutesSetMaintenanceArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RoutesSetMaintenanceArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type routesSetMaintenanceResultsData struct {
	Route     *string `cbor:"0,keyasint,omitempty" json:"route,omitempty"`
	Reason    *string `cbor:"1,keyasint,omitempty" json:"reason,omitempty"`
	BackAt    *string `cbor:"2,keyasint,omitempty" json:"back_at,omitempty"`
	StartedAt *string `cbor:"3,keyasint,omitempty" json:"started_at,omitempty"`
	StartedBy *string `cbor:"4,keyasint,omitempty" json:"started_by,omitempty"`
}

type RoutesSetMaintenanceResults struct {
	call rpc.Call
	data routesSetMaintenanceResultsData
}

func (v *RoutesSetMaintenanceResults) SetRoute(route string) {
	v.data.Route = &route
}

func (v *RoutesSetMaintenanceResults) SetReason(reason string) {
	v.data.Reason = &reason
}

func (v *RoutesSetMaintenanceResults) SetBackAt(back_at string) {
	v.data.BackAt = &back_at
}

func (v *RoutesSetMaintenanceResults) SetStartedAt(started_at string) {
	v.data.StartedAt = &started_at
}

func (v *RoutesSetMaintenanceResults) SetStartedBy(started_by string) {
	v.data.StartedBy = &started_by
}

func (v *RoutesSetMaintenanceResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RoutesSetMaintenanceResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RoutesSetMaintenanceResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RoutesSetMaintenanceResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type routesClearMaintenanceArgsData struct {
	Host         *string `cbor:"0,keyasint,omitempty" json:"host,omitempty"`
	DefaultRoute *bool   `cbor:"1,keyasint,omitempty" json:"default_route,omitempty"`
}

type RoutesClearMaintenanceArgs struct {
	call rpc.Call
	data routesClearMaintenanceArgsData
}

func (v *RoutesClearMaintenanceArgs) HasHost() bool {
	return v.data.Host != nil
}

func (v *RoutesClearMaintenanceArgs) Host() string {
	if v.data.Host == nil {
		return ""
	}
	return *v.data.Host
}

func (v *RoutesClearMaintenanceArgs) HasDefaultRoute() bool {
	return v.data.DefaultRoute != nil
}

func (v *RoutesClearMaintenanceArgs) DefaultRoute() bool {
	if v.data.DefaultRoute == nil {
		return false
	}
	return *v.data.DefaultRoute
}

func (v *RoutesClearMaintenanceArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RoutesClearMaintenanceArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RoutesClearMaintenanceArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RoutesClearMaintenanceArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type routesClearMaintenanceResultsData struct {
	Route   *string `cbor:"0,keyasint,omitempty" json:"route,omitempty"`
	Changed *bool   `cbor:"1,keyasint,omitempty" json:"changed,omitempty"`
}

type RoutesClearMaintenanceResults struct {
	call rpc.Call
	data routesClearMaintenanceResultsData
}

func (v *RoutesClearMaintenanceResults) SetRoute(route string) {
	v.data.Route = &route
}

func (v *RoutesClearMaintenanceResults) SetChanged(changed bool) {
	v.data.Changed = &changed
}

func (v *RoutesClearMaintenanceResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RoutesClearMaintenanceResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RoutesClearMaintenanceResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RoutesClearMaintenanceResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type routesListArgsData struct {
	App *string `cbor:"0,keyasint,omitempty" json:"app,omitempty"`
}

type RoutesListArgs struct {
	call rpc.Call
	data routesListArgsData
}

func (v *RoutesListArgs) HasApp() bool {
	return v.data.App != nil
}

func (v *RoutesListArgs) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *RoutesListArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RoutesListArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RoutesListArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RoutesListArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type routesListResultsData struct {
	Routes *[]*RouteInfo `cbor:"0,keyasint,omitempty" json:"routes,omitempty"`
}

type RoutesListResults struct {
	call rpc.Call
	data routesListResultsData
}

func (v *RoutesListResults) SetRoutes(routes []*RouteInfo) {
	x := slices.Clone(routes)
	v.data.Routes = &x
}

func (v *RoutesListResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RoutesListResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RoutesListResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RoutesListResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type routesShowArgsData struct {
	Host         *string `cbor:"0,keyasint,omitempty" json:"host,omitempty"`
	DefaultRoute *bool   `cbor:"1,keyasint,omitempty" json:"default_route,omitempty"`
}

type RoutesShowArgs struct {
	call rpc.Call
	data routesShowArgsData
}

func (v *RoutesShowArgs) HasHost() bool {
	return v.data.Host != nil
}

func (v *RoutesShowArgs) Host() string {
	if v.data.Host == nil {
		return ""
	}
	return *v.data.Host
}

func (v *RoutesShowArgs) HasDefaultRoute() bool {
	return v.data.DefaultRoute != nil
}

func (v *RoutesShowArgs) DefaultRoute() bool {
	if v.data.DefaultRoute == nil {
		return false
	}
	return *v.data.DefaultRoute
}

func (v *RoutesShowArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RoutesShowArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RoutesShowArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RoutesShowArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type routesShowResultsData struct {
	Route *RouteInfo `cbor:"0,keyasint,omitempty" json:"route,omitempty"`
}

type RoutesShowResults struct {
	call rpc.Call
	data routesShowResultsData
}

func (v *RoutesShowResults) SetRoute(route *RouteInfo) {
	v.data.Route = route
}

func (v *RoutesShowResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RoutesShowResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RoutesShowResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RoutesShowResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type routesSetArgsData struct {
	Host    *string `cbor:"0,keyasint,omitempty" json:"host,omitempty"`
	App     *string `cbor:"1,keyasint,omitempty" json:"app,omitempty"`
	Service *string `cbor:"2,keyasint,omitempty" json:"service,omitempty"`
}

type RoutesSetArgs struct {
	call rpc.Call
	data routesSetArgsData
}

func (v *RoutesSetArgs) HasHost() bool {
	return v.data.Host != nil
}

func (v *RoutesSetArgs) Host() string {
	if v.data.Host == nil {
		return ""
	}
	return *v.data.Host
}

func (v *RoutesSetArgs) HasApp() bool {
	return v.data.App != nil
}

func (v *RoutesSetArgs) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *RoutesSetArgs) HasService() bool {
	return v.data.Service != nil
}

func (v *RoutesSetArgs) Service() string {
	if v.data.Service == nil {
		return ""
	}
	return *v.data.Service
}

func (v *RoutesSetArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RoutesSetArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RoutesSetArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RoutesSetArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type routesSetResultsData struct {
	Route *RouteInfo `cbor:"0,keyasint,omitempty" json:"route,omitempty"`
}

type RoutesSetResults struct {
	call rpc.Call
	data routesSetResultsData
}

func (v *RoutesSetResults) SetRoute(route *RouteInfo) {
	v.data.Route = route
}

func (v *RoutesSetResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RoutesSetResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RoutesSetResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RoutesSetResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type routesRemoveArgsData struct {
	Host *string `cbor:"0,keyasint,omitempty" json:"host,omitempty"`
}

type RoutesRemoveArgs struct {
	call rpc.Call
	data routesRemoveArgsData
}

func (v *RoutesRemoveArgs) HasHost() bool {
	return v.data.Host != nil
}

func (v *RoutesRemoveArgs) Host() string {
	if v.data.Host == nil {
		return ""
	}
	return *v.data.Host
}

func (v *RoutesRemoveArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RoutesRemoveArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RoutesRemoveArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RoutesRemoveArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type routesRemoveResultsData struct{}

type RoutesRemoveResults struct {
	call rpc.Call
	data routesRemoveResultsData
}

func (v *RoutesRemoveResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RoutesRemoveResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RoutesRemoveResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RoutesRemoveResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type routesSetDefaultArgsData struct {
	App *string `cbor:"0,keyasint,omitempty" json:"app,omitempty"`
}

type RoutesSetDefaultArgs struct {
	call rpc.Call
	data routesSetDefaultArgsData
}

func (v *RoutesSetDefaultArgs) HasApp() bool {
	return v.data.App != nil
}

func (v *RoutesSetDefaultArgs) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *RoutesSetDefaultArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RoutesSetDefaultArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RoutesSetDefaultArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RoutesSetDefaultArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type routesSetDefaultResultsData struct {
	Route *RouteInfo `cbor:"0,keyasint,omitempty" json:"route,omitempty"`
}

type RoutesSetDefaultResults struct {
	call rpc.Call
	data routesSetDefaultResultsData
}

func (v *RoutesSetDefaultResults) SetRoute(route *RouteInfo) {
	v.data.Route = route
}

func (v *RoutesSetDefaultResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RoutesSetDefaultResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RoutesSetDefaultResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RoutesSetDefaultResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type routesUnsetDefaultArgsData struct{}

type RoutesUnsetDefaultArgs struct {
	call rpc.Call
	data routesUnsetDefaultArgsData
}

func (v *RoutesUnsetDefaultArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RoutesUnsetDefaultArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RoutesUnsetDefaultArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RoutesUnsetDefaultArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type routesUnsetDefaultResultsData struct {
	Changed *bool   `cbor:"0,keyasint,omitempty" json:"changed,omitempty"`
	App     *string `cbor:"1,keyasint,omitempty" json:"app,omitempty"`
}

type RoutesUnsetDefaultResults struct {
	call rpc.Call
	data routesUnsetDefaultResultsData
}

func (v *RoutesUnsetDefaultResults) SetChanged(changed bool) {
	v.data.Changed = &changed
}

func (v *RoutesUnsetDefaultResults) SetApp(app string) {
	v.data.App = &app
}

func (v *RoutesUnsetDefaultResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RoutesUnsetDefaultResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RoutesUnsetDefaultResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RoutesUnsetDefaultResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type routesProtectArgsData struct {
	Host                        *string               `cbor:"0,keyasint,omitempty" json:"host,omitempty"`
	DefaultRoute                *bool                 `cbor:"1,keyasint,omitempty" json:"default_route,omitempty"`
	Provider                    *string               `cbor:"2,keyasint,omitempty" json:"provider,omitempty"`
	ClaimMappings               *[]*RouteClaimMapping `cbor:"3,keyasint,omitempty" json:"claim_mappings,omitempty"`
	IgnorePasswordClaimMappings *bool                 `cbor:"4,keyasint,omitempty" json:"ignore_password_claim_mappings,omitempty"`
}

type RoutesProtectArgs struct {
	call rpc.Call
	data routesProtectArgsData
}

func (v *RoutesProtectArgs) HasHost() bool {
	return v.data.Host != nil
}

func (v *RoutesProtectArgs) Host() string {
	if v.data.Host == nil {
		return ""
	}
	return *v.data.Host
}

func (v *RoutesProtectArgs) HasDefaultRoute() bool {
	return v.data.DefaultRoute != nil
}

func (v *RoutesProtectArgs) DefaultRoute() bool {
	if v.data.DefaultRoute == nil {
		return false
	}
	return *v.data.DefaultRoute
}

func (v *RoutesProtectArgs) HasProvider() bool {
	return v.data.Provider != nil
}

func (v *RoutesProtectArgs) Provider() string {
	if v.data.Provider == nil {
		return ""
	}
	return *v.data.Provider
}

func (v *RoutesProtectArgs) HasClaimMappings() bool {
	return v.data.ClaimMappings != nil
}

func (v *RoutesProtectArgs) ClaimMappings() []*RouteClaimMapping {
	if v.data.ClaimMappings == nil {
		return nil
	}
	return *v.data.ClaimMappings
}

func (v *RoutesProtectArgs) HasIgnorePasswordClaimMappings() bool {
	return v.data.IgnorePasswordClaimMappings != nil
}

func (v *RoutesProtectArgs) IgnorePasswordClaimMappings() bool {
	if v.data.IgnorePasswordClaimMappings == nil {
		return false
	}
	return *v.data.IgnorePasswordClaimMappings
}

func (v *RoutesProtectArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RoutesProtectArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RoutesProtectArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RoutesProtectArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type routesProtectResultsData struct {
	Route *RouteInfo `cbor:"0,keyasint,omitempty" json:"route,omitempty"`
}

type RoutesProtectResults struct {
	call rpc.Call
	data routesProtectResultsData
}

func (v *RoutesProtectResults) SetRoute(route *RouteInfo) {
	v.data.Route = route
}

func (v *RoutesProtectResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RoutesProtectResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RoutesProtectResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RoutesProtectResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type routesUnprotectArgsData struct {
	Host         *string `cbor:"0,keyasint,omitempty" json:"host,omitempty"`
	DefaultRoute *bool   `cbor:"1,keyasint,omitempty" json:"default_route,omitempty"`
}

type RoutesUnprotectArgs struct {
	call rpc.Call
	data routesUnprotectArgsData
}

func (v *RoutesUnprotectArgs) HasHost() bool {
	return v.data.Host != nil
}

func (v *RoutesUnprotectArgs) Host() string {
	if v.data.Host == nil {
		return ""
	}
	return *v.data.Host
}

func (v *RoutesUnprotectArgs) HasDefaultRoute() bool {
	return v.data.DefaultRoute != nil
}

func (v *RoutesUnprotectArgs) DefaultRoute() bool {
	if v.data.DefaultRoute == nil {
		return false
	}
	return *v.data.DefaultRoute
}

func (v *RoutesUnprotectArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RoutesUnprotectArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RoutesUnprotectArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RoutesUnprotectArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type routesUnprotectResultsData struct {
	Route *RouteInfo `cbor:"0,keyasint,omitempty" json:"route,omitempty"`
}

type RoutesUnprotectResults struct {
	call rpc.Call
	data routesUnprotectResultsData
}

func (v *RoutesUnprotectResults) SetRoute(route *RouteInfo) {
	v.data.Route = route
}

func (v *RoutesUnprotectResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RoutesUnprotectResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RoutesUnprotectResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RoutesUnprotectResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type routesWafArgsData struct {
	Host         *string `cbor:"0,keyasint,omitempty" json:"host,omitempty"`
	DefaultRoute *bool   `cbor:"1,keyasint,omitempty" json:"default_route,omitempty"`
	Level        *int64  `cbor:"2,keyasint,omitempty" json:"level,omitempty"`
}

type RoutesWafArgs struct {
	call rpc.Call
	data routesWafArgsData
}

func (v *RoutesWafArgs) HasHost() bool {
	return v.data.Host != nil
}

func (v *RoutesWafArgs) Host() string {
	if v.data.Host == nil {
		return ""
	}
	return *v.data.Host
}

func (v *RoutesWafArgs) HasDefaultRoute() bool {
	return v.data.DefaultRoute != nil
}

func (v *RoutesWafArgs) DefaultRoute() bool {
	if v.data.DefaultRoute == nil {
		return false
	}
	return *v.data.DefaultRoute
}

func (v *RoutesWafArgs) HasLevel() bool {
	return v.data.Level != nil
}

func (v *RoutesWafArgs) Level() int64 {
	if v.data.Level == nil {
		return 0
	}
	return *v.data.Level
}

func (v *RoutesWafArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RoutesWafArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RoutesWafArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RoutesWafArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type routesWafResultsData struct {
	Route *RouteInfo `cbor:"0,keyasint,omitempty" json:"route,omitempty"`
}

type RoutesWafResults struct {
	call rpc.Call
	data routesWafResultsData
}

func (v *RoutesWafResults) SetRoute(route *RouteInfo) {
	v.data.Route = route
}

func (v *RoutesWafResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RoutesWafResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RoutesWafResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RoutesWafResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type routesTimeoutArgsData struct {
	Host         *string `cbor:"0,keyasint,omitempty" json:"host,omitempty"`
	DefaultRoute *bool   `cbor:"1,keyasint,omitempty" json:"default_route,omitempty"`
	Timeout      *string `cbor:"2,keyasint,omitempty" json:"timeout,omitempty"`
}

type RoutesTimeoutArgs struct {
	call rpc.Call
	data routesTimeoutArgsData
}

func (v *RoutesTimeoutArgs) HasHost() bool {
	return v.data.Host != nil
}

func (v *RoutesTimeoutArgs) Host() string {
	if v.data.Host == nil {
		return ""
	}
	return *v.data.Host
}

func (v *RoutesTimeoutArgs) HasDefaultRoute() bool {
	return v.data.DefaultRoute != nil
}

func (v *RoutesTimeoutArgs) DefaultRoute() bool {
	if v.data.DefaultRoute == nil {
		return false
	}
	return *v.data.DefaultRoute
}

func (v *RoutesTimeoutArgs) HasTimeout() bool {
	return v.data.Timeout != nil
}

func (v *RoutesTimeoutArgs) Timeout() string {
	if v.data.Timeout == nil {
		return ""
	}
	return *v.data.Timeout
}

func (v *RoutesTimeoutArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RoutesTimeoutArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RoutesTimeoutArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RoutesTimeoutArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type routesTimeoutResultsData struct {
	Route *RouteInfo `cbor:"0,keyasint,omitempty" json:"route,omitempty"`
}

type RoutesTimeoutResults struct {
	call rpc.Call
	data routesTimeoutResultsData
}

func (v *RoutesTimeoutResults) SetRoute(route *RouteInfo) {
	v.data.Route = route
}

func (v *RoutesTimeoutResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RoutesTimeoutResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RoutesTimeoutResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RoutesTimeoutResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type routesTlsCheckArgsData struct {
	Host *string `cbor:"0,keyasint,omitempty" json:"host,omitempty"`
	Path *string `cbor:"1,keyasint,omitempty" json:"path,omitempty"`
}

type RoutesTlsCheckArgs struct {
	call rpc.Call
	data routesTlsCheckArgsData
}

func (v *RoutesTlsCheckArgs) HasHost() bool {
	return v.data.Host != nil
}

func (v *RoutesTlsCheckArgs) Host() string {
	if v.data.Host == nil {
		return ""
	}
	return *v.data.Host
}

func (v *RoutesTlsCheckArgs) HasPath() bool {
	return v.data.Path != nil
}

func (v *RoutesTlsCheckArgs) Path() string {
	if v.data.Path == nil {
		return ""
	}
	return *v.data.Path
}

func (v *RoutesTlsCheckArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RoutesTlsCheckArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RoutesTlsCheckArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RoutesTlsCheckArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type routesTlsCheckResultsData struct {
	Route *RouteInfo `cbor:"0,keyasint,omitempty" json:"route,omitempty"`
}

type RoutesTlsCheckResults struct {
	call rpc.Call
	data routesTlsCheckResultsData
}

func (v *RoutesTlsCheckResults) SetRoute(route *RouteInfo) {
	v.data.Route = route
}

func (v *RoutesTlsCheckResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RoutesTlsCheckResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RoutesTlsCheckResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RoutesTlsCheckResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type RoutesSetMaintenance struct {
	rpc.Call
	args    RoutesSetMaintenanceArgs
	results RoutesSetMaintenanceResults
}

func (t *RoutesSetMaintenance) Args() *RoutesSetMaintenanceArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *RoutesSetMaintenance) Results() *RoutesSetMaintenanceResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type RoutesClearMaintenance struct {
	rpc.Call
	args    RoutesClearMaintenanceArgs
	results RoutesClearMaintenanceResults
}

func (t *RoutesClearMaintenance) Args() *RoutesClearMaintenanceArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *RoutesClearMaintenance) Results() *RoutesClearMaintenanceResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type RoutesList struct {
	rpc.Call
	args    RoutesListArgs
	results RoutesListResults
}

func (t *RoutesList) Args() *RoutesListArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *RoutesList) Results() *RoutesListResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type RoutesShow struct {
	rpc.Call
	args    RoutesShowArgs
	results RoutesShowResults
}

func (t *RoutesShow) Args() *RoutesShowArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *RoutesShow) Results() *RoutesShowResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type RoutesSet struct {
	rpc.Call
	args    RoutesSetArgs
	results RoutesSetResults
}

func (t *RoutesSet) Args() *RoutesSetArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *RoutesSet) Results() *RoutesSetResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type RoutesRemove struct {
	rpc.Call
	args    RoutesRemoveArgs
	results RoutesRemoveResults
}

func (t *RoutesRemove) Args() *RoutesRemoveArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *RoutesRemove) Results() *RoutesRemoveResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type RoutesSetDefault struct {
	rpc.Call
	args    RoutesSetDefaultArgs
	results RoutesSetDefaultResults
}

func (t *RoutesSetDefault) Args() *RoutesSetDefaultArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *RoutesSetDefault) Results() *RoutesSetDefaultResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type RoutesUnsetDefault struct {
	rpc.Call
	args    RoutesUnsetDefaultArgs
	results RoutesUnsetDefaultResults
}

func (t *RoutesUnsetDefault) Args() *RoutesUnsetDefaultArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *RoutesUnsetDefault) Results() *RoutesUnsetDefaultResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type RoutesProtect struct {
	rpc.Call
	args    RoutesProtectArgs
	results RoutesProtectResults
}

func (t *RoutesProtect) Args() *RoutesProtectArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *RoutesProtect) Results() *RoutesProtectResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type RoutesUnprotect struct {
	rpc.Call
	args    RoutesUnprotectArgs
	results RoutesUnprotectResults
}

func (t *RoutesUnprotect) Args() *RoutesUnprotectArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *RoutesUnprotect) Results() *RoutesUnprotectResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type RoutesWaf struct {
	rpc.Call
	args    RoutesWafArgs
	results RoutesWafResults
}

func (t *RoutesWaf) Args() *RoutesWafArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *RoutesWaf) Results() *RoutesWafResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type RoutesTimeout struct {
	rpc.Call
	args    RoutesTimeoutArgs
	results RoutesTimeoutResults
}

func (t *RoutesTimeout) Args() *RoutesTimeoutArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *RoutesTimeout) Results() *RoutesTimeoutResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type RoutesTlsCheck struct {
	rpc.Call
	args    RoutesTlsCheckArgs
	results RoutesTlsCheckResults
}

func (t *RoutesTlsCheck) Args() *RoutesTlsCheckArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *RoutesTlsCheck) Results() *RoutesTlsCheckResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type Routes interface {
	SetMaintenance(ctx context.Context, state *RoutesSetMaintenance) error
	ClearMaintenance(ctx context.Context, state *RoutesClearMaintenance) error
	List(ctx context.Context, state *RoutesList) error
	Show(ctx context.Context, state *RoutesShow) error
	Set(ctx context.Context, state *RoutesSet) error
	Remove(ctx context.Context, state *RoutesRemove) error
	SetDefault(ctx context.Context, state *RoutesSetDefault) error
	UnsetDefault(ctx context.Context, state *RoutesUnsetDefault) error
	Protect(ctx context.Context, state *RoutesProtect) error
	Unprotect(ctx context.Context, state *RoutesUnprotect) error
	Waf(ctx context.Context, state *RoutesWaf) error
	Timeout(ctx context.Context, state *RoutesTimeout) error
	TlsCheck(ctx context.Context, state *RoutesTlsCheck) error
}

type reexportRoutes struct {
	client rpc.Client
}

func (reexportRoutes) SetMaintenance(ctx context.Context, state *RoutesSetMaintenance) error {
	panic("not implemented")
}

func (reexportRoutes) ClearMaintenance(ctx context.Context, state *RoutesClearMaintenance) error {
	panic("not implemented")
}

func (reexportRoutes) List(ctx context.Context, state *RoutesList) error {
	panic("not implemented")
}

func (reexportRoutes) Show(ctx context.Context, state *RoutesShow) error {
	panic("not implemented")
}

func (reexportRoutes) Set(ctx context.Context, state *RoutesSet) error {
	panic("not implemented")
}

func (reexportRoutes) Remove(ctx context.Context, state *RoutesRemove) error {
	panic("not implemented")
}

func (reexportRoutes) SetDefault(ctx context.Context, state *RoutesSetDefault) error {
	panic("not implemented")
}

func (reexportRoutes) UnsetDefault(ctx context.Context, state *RoutesUnsetDefault) error {
	panic("not implemented")
}

func (reexportRoutes) Protect(ctx context.Context, state *RoutesProtect) error {
	panic("not implemented")
}

func (reexportRoutes) Unprotect(ctx context.Context, state *RoutesUnprotect) error {
	panic("not implemented")
}

func (reexportRoutes) Waf(ctx context.Context, state *RoutesWaf) error {
	panic("not implemented")
}

func (reexportRoutes) Timeout(ctx context.Context, state *RoutesTimeout) error {
	panic("not implemented")
}

func (reexportRoutes) TlsCheck(ctx context.Context, state *RoutesTlsCheck) error {
	panic("not implemented")
}

func (t reexportRoutes) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptRoutes(t Routes) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "setMaintenance",
			InterfaceName: "Routes",
			Index:         0,
			Public:        false,
			Params:        []string{"host", "default_route", "reason", "back_at"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.SetMaintenance(ctx, &RoutesSetMaintenance{Call: call})
			},
		},
		{
			Name:          "clearMaintenance",
			InterfaceName: "Routes",
			Index:         1,
			Public:        false,
			Params:        []string{"host", "default_route"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.ClearMaintenance(ctx, &RoutesClearMaintenance{Call: call})
			},
		},
		{
			Name:          "list",
			InterfaceName: "Routes",
			Index:         2,
			Public:        false,
			Params:        []string{"app"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.List(ctx, &RoutesList{Call: call})
			},
		},
		{
			Name:          "show",
			InterfaceName: "Routes",
			Index:         3,
			Public:        false,
			Params:        []string{"host", "default_route"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Show(ctx, &RoutesShow{Call: call})
			},
		},
		{
			Name:          "set",
			InterfaceName: "Routes",
			Index:         4,
			Public:        false,
			Params:        []string{"host", "app", "service"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Set(ctx, &RoutesSet{Call: call})
			},
		},
		{
			Name:          "remove",
			InterfaceName: "Routes",
			Index:         5,
			Public:        false,
			Params:        []string{"host"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Remove(ctx, &RoutesRemove{Call: call})
			},
		},
		{
			Name:          "setDefault",
			InterfaceName: "Routes",
			Index:         6,
			Public:        false,
			Params:        []string{"app"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.SetDefault(ctx, &RoutesSetDefault{Call: call})
			},
		},
		{
			Name:          "unsetDefault",
			InterfaceName: "Routes",
			Index:         7,
			Public:        false,
			Params:        []string{},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.UnsetDefault(ctx, &RoutesUnsetDefault{Call: call})
			},
		},
		{
			Name:          "protect",
			InterfaceName: "Routes",
			Index:         8,
			Public:        false,
			Params:        []string{"host", "default_route", "provider", "claim_mappings", "ignore_password_claim_mappings"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Protect(ctx, &RoutesProtect{Call: call})
			},
		},
		{
			Name:          "unprotect",
			InterfaceName: "Routes",
			Index:         9,
			Public:        false,
			Params:        []string{"host", "default_route"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Unprotect(ctx, &RoutesUnprotect{Call: call})
			},
		},
		{
			Name:          "waf",
			InterfaceName: "Routes",
			Index:         10,
			Public:        false,
			Params:        []string{"host", "default_route", "level"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Waf(ctx, &RoutesWaf{Call: call})
			},
		},
		{
			Name:          "timeout",
			InterfaceName: "Routes",
			Index:         11,
			Public:        false,
			Params:        []string{"host", "default_route", "timeout"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Timeout(ctx, &RoutesTimeout{Call: call})
			},
		},
		{
			Name:          "tlsCheck",
			InterfaceName: "Routes",
			Index:         12,
			Public:        false,
			Params:        []string{"host", "path"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.TlsCheck(ctx, &RoutesTlsCheck{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type RoutesClient struct {
	rpc.Client
}

func NewRoutesClient(client rpc.Client) *RoutesClient {
	return &RoutesClient{Client: client}
}

func (c RoutesClient) Export() Routes {
	return reexportRoutes{client: c.Client}
}

type RoutesClientSetMaintenanceResults struct {
	client rpc.Client
	data   routesSetMaintenanceResultsData
}

func (v *RoutesClientSetMaintenanceResults) HasRoute() bool {
	return v.data.Route != nil
}

func (v *RoutesClientSetMaintenanceResults) Route() string {
	if v.data.Route == nil {
		return ""
	}
	return *v.data.Route
}

func (v *RoutesClientSetMaintenanceResults) HasReason() bool {
	return v.data.Reason != nil
}

func (v *RoutesClientSetMaintenanceResults) Reason() string {
	if v.data.Reason == nil {
		return ""
	}
	return *v.data.Reason
}

func (v *RoutesClientSetMaintenanceResults) HasBackAt() bool {
	return v.data.BackAt != nil
}

func (v *RoutesClientSetMaintenanceResults) BackAt() string {
	if v.data.BackAt == nil {
		return ""
	}
	return *v.data.BackAt
}

func (v *RoutesClientSetMaintenanceResults) HasStartedAt() bool {
	return v.data.StartedAt != nil
}

func (v *RoutesClientSetMaintenanceResults) StartedAt() string {
	if v.data.StartedAt == nil {
		return ""
	}
	return *v.data.StartedAt
}

func (v *RoutesClientSetMaintenanceResults) HasStartedBy() bool {
	return v.data.StartedBy != nil
}

func (v *RoutesClientSetMaintenanceResults) StartedBy() string {
	if v.data.StartedBy == nil {
		return ""
	}
	return *v.data.StartedBy
}

func (v RoutesClient) SetMaintenance(ctx context.Context, host string, default_route bool, reason string, back_at string) (*RoutesClientSetMaintenanceResults, error) {
	args := RoutesSetMaintenanceArgs{}
	args.data.Host = &host
	args.data.DefaultRoute = &default_route
	args.data.Reason = &reason
	args.data.BackAt = &back_at

	var ret routesSetMaintenanceResultsData

	err := v.Call(ctx, "setMaintenance", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &RoutesClientSetMaintenanceResults{client: v.Client, data: ret}, nil
}

type RoutesClientClearMaintenanceResults struct {
	client rpc.Client
	data   routesClearMaintenanceResultsData
}

func (v *RoutesClientClearMaintenanceResults) HasRoute() bool {
	return v.data.Route != nil
}

func (v *RoutesClientClearMaintenanceResults) Route() string {
	if v.data.Route == nil {
		return ""
	}
	return *v.data.Route
}

func (v *RoutesClientClearMaintenanceResults) HasChanged() bool {
	return v.data.Changed != nil
}

func (v *RoutesClientClearMaintenanceResults) Changed() bool {
	if v.data.Changed == nil {
		return false
	}
	return *v.data.Changed
}

func (v RoutesClient) ClearMaintenance(ctx context.Context, host string, default_route bool) (*RoutesClientClearMaintenanceResults, error) {
	args := RoutesClearMaintenanceArgs{}
	args.data.Host = &host
	args.data.DefaultRoute = &default_route

	var ret routesClearMaintenanceResultsData

	err := v.Call(ctx, "clearMaintenance", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &RoutesClientClearMaintenanceResults{client: v.Client, data: ret}, nil
}

type RoutesClientListResults struct {
	client rpc.Client
	data   routesListResultsData
}

func (v *RoutesClientListResults) HasRoutes() bool {
	return v.data.Routes != nil
}

func (v *RoutesClientListResults) Routes() []*RouteInfo {
	if v.data.Routes == nil {
		return nil
	}
	return *v.data.Routes
}

func (v RoutesClient) List(ctx context.Context, app string) (*RoutesClientListResults, error) {
	args := RoutesListArgs{}
	args.data.App = &app

	var ret routesListResultsData

	err := v.Call(ctx, "list", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &RoutesClientListResults{client: v.Client, data: ret}, nil
}

type RoutesClientShowResults struct {
	client rpc.Client
	data   routesShowResultsData
}

func (v *RoutesClientShowResults) HasRoute() bool {
	return v.data.Route != nil
}

func (v *RoutesClientShowResults) Route() *RouteInfo {
	return v.data.Route
}

func (v RoutesClient) Show(ctx context.Context, host string, default_route bool) (*RoutesClientShowResults, error) {
	args := RoutesShowArgs{}
	args.data.Host = &host
	args.data.DefaultRoute = &default_route

	var ret routesShowResultsData

	err := v.Call(ctx, "show", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &RoutesClientShowResults{client: v.Client, data: ret}, nil
}

type RoutesClientSetResults struct {
	client rpc.Client
	data   routesSetResultsData
}

func (v *RoutesClientSetResults) HasRoute() bool {
	return v.data.Route != nil
}

func (v *RoutesClientSetResults) Route() *RouteInfo {
	return v.data.Route
}

func (v RoutesClient) Set(ctx context.Context, host string, app string, service string) (*RoutesClientSetResults, error) {
	args := RoutesSetArgs{}
	args.data.Host = &host
	args.data.App = &app
	args.data.Service = &service

	var ret routesSetResultsData

	err := v.Call(ctx, "set", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &RoutesClientSetResults{client: v.Client, data: ret}, nil
}

type RoutesClientRemoveResults struct {
	client rpc.Client
	data   routesRemoveResultsData
}

func (v RoutesClient) Remove(ctx context.Context, host string) (*RoutesClientRemoveResults, error) {
	args := RoutesRemoveArgs{}
	args.data.Host = &host

	var ret routesRemoveResultsData

	err := v.Call(ctx, "remove", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &RoutesClientRemoveResults{client: v.Client, data: ret}, nil
}

type RoutesClientSetDefaultResults struct {
	client rpc.Client
	data   routesSetDefaultResultsData
}

func (v *RoutesClientSetDefaultResults) HasRoute() bool {
	return v.data.Route != nil
}

func (v *RoutesClientSetDefaultResults) Route() *RouteInfo {
	return v.data.Route
}

func (v RoutesClient) SetDefault(ctx context.Context, app string) (*RoutesClientSetDefaultResults, error) {
	args := RoutesSetDefaultArgs{}
	args.data.App = &app

	var ret routesSetDefaultResultsData

	err := v.Call(ctx, "setDefault", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &RoutesClientSetDefaultResults{client: v.Client, data: ret}, nil
}

type RoutesClientUnsetDefaultResults struct {
	client rpc.Client
	data   routesUnsetDefaultResultsData
}

func (v *RoutesClientUnsetDefaultResults) HasChanged() bool {
	return v.data.Changed != nil
}

func (v *RoutesClientUnsetDefaultResults) Changed() bool {
	if v.data.Changed == nil {
		return false
	}
	return *v.data.Changed
}

func (v *RoutesClientUnsetDefaultResults) HasApp() bool {
	return v.data.App != nil
}

func (v *RoutesClientUnsetDefaultResults) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v RoutesClient) UnsetDefault(ctx context.Context) (*RoutesClientUnsetDefaultResults, error) {
	args := RoutesUnsetDefaultArgs{}

	var ret routesUnsetDefaultResultsData

	err := v.Call(ctx, "unsetDefault", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &RoutesClientUnsetDefaultResults{client: v.Client, data: ret}, nil
}

type RoutesClientProtectResults struct {
	client rpc.Client
	data   routesProtectResultsData
}

func (v *RoutesClientProtectResults) HasRoute() bool {
	return v.data.Route != nil
}

func (v *RoutesClientProtectResults) Route() *RouteInfo {
	return v.data.Route
}

func (v RoutesClient) Protect(ctx context.Context, host string, default_route bool, provider string, claim_mappings []*RouteClaimMapping, ignore_password_claim_mappings bool) (*RoutesClientProtectResults, error) {
	args := RoutesProtectArgs{}
	args.data.Host = &host
	args.data.DefaultRoute = &default_route
	args.data.Provider = &provider
	x := slices.Clone(claim_mappings)
	args.data.ClaimMappings = &x
	args.data.IgnorePasswordClaimMappings = &ignore_password_claim_mappings

	var ret routesProtectResultsData

	err := v.Call(ctx, "protect", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &RoutesClientProtectResults{client: v.Client, data: ret}, nil
}

type RoutesClientUnprotectResults struct {
	client rpc.Client
	data   routesUnprotectResultsData
}

func (v *RoutesClientUnprotectResults) HasRoute() bool {
	return v.data.Route != nil
}

func (v *RoutesClientUnprotectResults) Route() *RouteInfo {
	return v.data.Route
}

func (v RoutesClient) Unprotect(ctx context.Context, host string, default_route bool) (*RoutesClientUnprotectResults, error) {
	args := RoutesUnprotectArgs{}
	args.data.Host = &host
	args.data.DefaultRoute = &default_route

	var ret routesUnprotectResultsData

	err := v.Call(ctx, "unprotect", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &RoutesClientUnprotectResults{client: v.Client, data: ret}, nil
}

type RoutesClientWafResults struct {
	client rpc.Client
	data   routesWafResultsData
}

func (v *RoutesClientWafResults) HasRoute() bool {
	return v.data.Route != nil
}

func (v *RoutesClientWafResults) Route() *RouteInfo {
	return v.data.Route
}

func (v RoutesClient) Waf(ctx context.Context, host string, default_route bool, level int64) (*RoutesClientWafResults, error) {
	args := RoutesWafArgs{}
	args.data.Host = &host
	args.data.DefaultRoute = &default_route
	args.data.Level = &level

	var ret routesWafResultsData

	err := v.Call(ctx, "waf", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &RoutesClientWafResults{client: v.Client, data: ret}, nil
}

type RoutesClientTimeoutResults struct {
	client rpc.Client
	data   routesTimeoutResultsData
}

func (v *RoutesClientTimeoutResults) HasRoute() bool {
	return v.data.Route != nil
}

func (v *RoutesClientTimeoutResults) Route() *RouteInfo {
	return v.data.Route
}

func (v RoutesClient) Timeout(ctx context.Context, host string, default_route bool, timeout string) (*RoutesClientTimeoutResults, error) {
	args := RoutesTimeoutArgs{}
	args.data.Host = &host
	args.data.DefaultRoute = &default_route
	args.data.Timeout = &timeout

	var ret routesTimeoutResultsData

	err := v.Call(ctx, "timeout", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &RoutesClientTimeoutResults{client: v.Client, data: ret}, nil
}

type RoutesClientTlsCheckResults struct {
	client rpc.Client
	data   routesTlsCheckResultsData
}

func (v *RoutesClientTlsCheckResults) HasRoute() bool {
	return v.data.Route != nil
}

func (v *RoutesClientTlsCheckResults) Route() *RouteInfo {
	return v.data.Route
}

func (v RoutesClient) TlsCheck(ctx context.Context, host string, path string) (*RoutesClientTlsCheckResults, error) {
	args := RoutesTlsCheckArgs{}
	args.data.Host = &host
	args.data.Path = &path

	var ret routesTlsCheckResultsData

	err := v.Call(ctx, "tlsCheck", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &RoutesClientTlsCheckResults{client: v.Client, data: ret}, nil
}
