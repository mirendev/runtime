package ingress_v1alpha

import (
	"context"
	"encoding/json"

	"github.com/fxamacker/cbor/v2"
	rpc "miren.dev/runtime/pkg/rpc"
)

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

type Routes interface {
	SetMaintenance(ctx context.Context, state *RoutesSetMaintenance) error
	ClearMaintenance(ctx context.Context, state *RoutesClearMaintenance) error
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
