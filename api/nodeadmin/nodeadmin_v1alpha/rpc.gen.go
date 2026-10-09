package nodeadmin_v1alpha

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/fxamacker/cbor/v2"
	rpc "miren.dev/runtime/pkg/rpc"
)

type nodeAdminInstallDiskAcceleratorArgsData struct {
	Image *string `cbor:"0,keyasint,omitempty" json:"image,omitempty"`
	Force *bool   `cbor:"1,keyasint,omitempty" json:"force,omitempty"`
}

type NodeAdminInstallDiskAcceleratorArgs struct {
	call rpc.Call
	data nodeAdminInstallDiskAcceleratorArgsData
}

func (v *NodeAdminInstallDiskAcceleratorArgs) HasImage() bool {
	return v.data.Image != nil
}

func (v *NodeAdminInstallDiskAcceleratorArgs) Image() string {
	if v.data.Image == nil {
		return ""
	}
	return *v.data.Image
}

func (v *NodeAdminInstallDiskAcceleratorArgs) HasForce() bool {
	return v.data.Force != nil
}

func (v *NodeAdminInstallDiskAcceleratorArgs) Force() bool {
	if v.data.Force == nil {
		return false
	}
	return *v.data.Force
}

func (v *NodeAdminInstallDiskAcceleratorArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *NodeAdminInstallDiskAcceleratorArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *NodeAdminInstallDiskAcceleratorArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *NodeAdminInstallDiskAcceleratorArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type nodeAdminInstallDiskAcceleratorResultsData struct {
	KernelRelease *string `cbor:"0,keyasint,omitempty" json:"kernel_release,omitempty"`
	LbdVersion    *string `cbor:"1,keyasint,omitempty" json:"lbd_version,omitempty"`
	Error         *string `cbor:"2,keyasint,omitempty" json:"error,omitempty"`
}

type NodeAdminInstallDiskAcceleratorResults struct {
	call rpc.Call
	data nodeAdminInstallDiskAcceleratorResultsData
}

func (v *NodeAdminInstallDiskAcceleratorResults) SetKernelRelease(kernel_release string) {
	v.data.KernelRelease = &kernel_release
}

func (v *NodeAdminInstallDiskAcceleratorResults) SetLbdVersion(lbd_version string) {
	v.data.LbdVersion = &lbd_version
}

func (v *NodeAdminInstallDiskAcceleratorResults) SetError(error string) {
	v.data.Error = &error
}

func (v *NodeAdminInstallDiskAcceleratorResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *NodeAdminInstallDiskAcceleratorResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *NodeAdminInstallDiskAcceleratorResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *NodeAdminInstallDiskAcceleratorResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type nodeAdminQueryArgsData struct {
	Expression *string `cbor:"0,keyasint,omitempty" json:"expression,omitempty"`
}

type NodeAdminQueryArgs struct {
	call rpc.Call
	data nodeAdminQueryArgsData
}

func (v *NodeAdminQueryArgs) HasExpression() bool {
	return v.data.Expression != nil
}

func (v *NodeAdminQueryArgs) Expression() string {
	if v.data.Expression == nil {
		return ""
	}
	return *v.data.Expression
}

func (v *NodeAdminQueryArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *NodeAdminQueryArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *NodeAdminQueryArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *NodeAdminQueryArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type nodeAdminQueryResultsData struct {
	Data           *[]byte `cbor:"0,keyasint,omitempty" json:"data,omitempty"`
	Error          *string `cbor:"1,keyasint,omitempty" json:"error,omitempty"`
	EngineRevision *string `cbor:"2,keyasint,omitempty" json:"engine_revision,omitempty"`
}

type NodeAdminQueryResults struct {
	call rpc.Call
	data nodeAdminQueryResultsData
}

func (v *NodeAdminQueryResults) SetData(data []byte) {
	x := slices.Clone(data)
	v.data.Data = &x
}

func (v *NodeAdminQueryResults) SetError(error string) {
	v.data.Error = &error
}

func (v *NodeAdminQueryResults) SetEngineRevision(engine_revision string) {
	v.data.EngineRevision = &engine_revision
}

func (v *NodeAdminQueryResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *NodeAdminQueryResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *NodeAdminQueryResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *NodeAdminQueryResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type nodeAdminQueryInfoArgsData struct{}

type NodeAdminQueryInfoArgs struct {
	call rpc.Call
	data nodeAdminQueryInfoArgsData
}

func (v *NodeAdminQueryInfoArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *NodeAdminQueryInfoArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *NodeAdminQueryInfoArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *NodeAdminQueryInfoArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type nodeAdminQueryInfoResultsData struct {
	EngineRevision *string `cbor:"0,keyasint,omitempty" json:"engine_revision,omitempty"`
	Reference      *string `cbor:"1,keyasint,omitempty" json:"reference,omitempty"`
	Error          *string `cbor:"2,keyasint,omitempty" json:"error,omitempty"`
}

type NodeAdminQueryInfoResults struct {
	call rpc.Call
	data nodeAdminQueryInfoResultsData
}

func (v *NodeAdminQueryInfoResults) SetEngineRevision(engine_revision string) {
	v.data.EngineRevision = &engine_revision
}

func (v *NodeAdminQueryInfoResults) SetReference(reference string) {
	v.data.Reference = &reference
}

func (v *NodeAdminQueryInfoResults) SetError(error string) {
	v.data.Error = &error
}

func (v *NodeAdminQueryInfoResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *NodeAdminQueryInfoResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *NodeAdminQueryInfoResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *NodeAdminQueryInfoResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type nodeAdminValidateQueryArgsData struct {
	Expression *string `cbor:"0,keyasint,omitempty" json:"expression,omitempty"`
}

type NodeAdminValidateQueryArgs struct {
	call rpc.Call
	data nodeAdminValidateQueryArgsData
}

func (v *NodeAdminValidateQueryArgs) HasExpression() bool {
	return v.data.Expression != nil
}

func (v *NodeAdminValidateQueryArgs) Expression() string {
	if v.data.Expression == nil {
		return ""
	}
	return *v.data.Expression
}

func (v *NodeAdminValidateQueryArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *NodeAdminValidateQueryArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *NodeAdminValidateQueryArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *NodeAdminValidateQueryArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type nodeAdminValidateQueryResultsData struct {
	EngineRevision *string `cbor:"0,keyasint,omitempty" json:"engine_revision,omitempty"`
	Valid          *bool   `cbor:"1,keyasint,omitempty" json:"valid,omitempty"`
	Error          *string `cbor:"2,keyasint,omitempty" json:"error,omitempty"`
}

type NodeAdminValidateQueryResults struct {
	call rpc.Call
	data nodeAdminValidateQueryResultsData
}

func (v *NodeAdminValidateQueryResults) SetEngineRevision(engine_revision string) {
	v.data.EngineRevision = &engine_revision
}

func (v *NodeAdminValidateQueryResults) SetValid(valid bool) {
	v.data.Valid = &valid
}

func (v *NodeAdminValidateQueryResults) SetError(error string) {
	v.data.Error = &error
}

func (v *NodeAdminValidateQueryResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *NodeAdminValidateQueryResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *NodeAdminValidateQueryResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *NodeAdminValidateQueryResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type NodeAdminInstallDiskAccelerator struct {
	rpc.Call
	args    NodeAdminInstallDiskAcceleratorArgs
	results NodeAdminInstallDiskAcceleratorResults
}

func (t *NodeAdminInstallDiskAccelerator) Args() *NodeAdminInstallDiskAcceleratorArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *NodeAdminInstallDiskAccelerator) Results() *NodeAdminInstallDiskAcceleratorResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type NodeAdminQuery struct {
	rpc.Call
	args    NodeAdminQueryArgs
	results NodeAdminQueryResults
}

func (t *NodeAdminQuery) Args() *NodeAdminQueryArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *NodeAdminQuery) Results() *NodeAdminQueryResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type NodeAdminQueryInfo struct {
	rpc.Call
	args    NodeAdminQueryInfoArgs
	results NodeAdminQueryInfoResults
}

func (t *NodeAdminQueryInfo) Args() *NodeAdminQueryInfoArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *NodeAdminQueryInfo) Results() *NodeAdminQueryInfoResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type NodeAdminValidateQuery struct {
	rpc.Call
	args    NodeAdminValidateQueryArgs
	results NodeAdminValidateQueryResults
}

func (t *NodeAdminValidateQuery) Args() *NodeAdminValidateQueryArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *NodeAdminValidateQuery) Results() *NodeAdminValidateQueryResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type NodeAdmin interface {
	InstallDiskAccelerator(ctx context.Context, state *NodeAdminInstallDiskAccelerator) error
	Query(ctx context.Context, state *NodeAdminQuery) error
	QueryInfo(ctx context.Context, state *NodeAdminQueryInfo) error
	ValidateQuery(ctx context.Context, state *NodeAdminValidateQuery) error
}

type reexportNodeAdmin struct {
	client rpc.Client
}

func (reexportNodeAdmin) InstallDiskAccelerator(ctx context.Context, state *NodeAdminInstallDiskAccelerator) error {
	panic("not implemented")
}

func (reexportNodeAdmin) Query(ctx context.Context, state *NodeAdminQuery) error {
	panic("not implemented")
}

func (reexportNodeAdmin) QueryInfo(ctx context.Context, state *NodeAdminQueryInfo) error {
	panic("not implemented")
}

func (reexportNodeAdmin) ValidateQuery(ctx context.Context, state *NodeAdminValidateQuery) error {
	panic("not implemented")
}

func (t reexportNodeAdmin) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptNodeAdmin(t NodeAdmin) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "install_disk_accelerator",
			InterfaceName: "NodeAdmin",
			Index:         0,
			Public:        false,
			Params:        []string{"image", "force"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.InstallDiskAccelerator(ctx, &NodeAdminInstallDiskAccelerator{Call: call})
			},
		},
		{
			Name:          "query",
			InterfaceName: "NodeAdmin",
			Index:         1,
			Public:        false,
			Params:        []string{"expression"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Query(ctx, &NodeAdminQuery{Call: call})
			},
		},
		{
			Name:          "query_info",
			InterfaceName: "NodeAdmin",
			Index:         2,
			Public:        false,
			Params:        []string{},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.QueryInfo(ctx, &NodeAdminQueryInfo{Call: call})
			},
		},
		{
			Name:          "validate_query",
			InterfaceName: "NodeAdmin",
			Index:         3,
			Public:        false,
			Params:        []string{"expression"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.ValidateQuery(ctx, &NodeAdminValidateQuery{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type NodeAdminClient struct {
	rpc.Client
}

func NewNodeAdminClient(client rpc.Client) *NodeAdminClient {
	return &NodeAdminClient{Client: client}
}

func (c NodeAdminClient) Export() NodeAdmin {
	return reexportNodeAdmin{client: c.Client}
}

type NodeAdminClientInstallDiskAcceleratorResults struct {
	client rpc.Client
	data   nodeAdminInstallDiskAcceleratorResultsData
}

func (v *NodeAdminClientInstallDiskAcceleratorResults) HasKernelRelease() bool {
	return v.data.KernelRelease != nil
}

func (v *NodeAdminClientInstallDiskAcceleratorResults) KernelRelease() string {
	if v.data.KernelRelease == nil {
		return ""
	}
	return *v.data.KernelRelease
}

func (v *NodeAdminClientInstallDiskAcceleratorResults) HasLbdVersion() bool {
	return v.data.LbdVersion != nil
}

func (v *NodeAdminClientInstallDiskAcceleratorResults) LbdVersion() string {
	if v.data.LbdVersion == nil {
		return ""
	}
	return *v.data.LbdVersion
}

func (v *NodeAdminClientInstallDiskAcceleratorResults) HasError() bool {
	return v.data.Error != nil
}

func (v *NodeAdminClientInstallDiskAcceleratorResults) Error() string {
	if v.data.Error == nil {
		return ""
	}
	return *v.data.Error
}

func (v NodeAdminClient) InstallDiskAccelerator(ctx context.Context, image string, force bool) (*NodeAdminClientInstallDiskAcceleratorResults, error) {
	args := NodeAdminInstallDiskAcceleratorArgs{}
	args.data.Image = &image
	args.data.Force = &force

	var ret nodeAdminInstallDiskAcceleratorResultsData

	err := v.Call(ctx, "install_disk_accelerator", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &NodeAdminClientInstallDiskAcceleratorResults{client: v.Client, data: ret}, nil
}

type NodeAdminClientQueryResults struct {
	client rpc.Client
	data   nodeAdminQueryResultsData
}

func (v *NodeAdminClientQueryResults) HasData() bool {
	return v.data.Data != nil
}

func (v *NodeAdminClientQueryResults) Data() []byte {
	if v.data.Data == nil {
		return nil
	}
	return *v.data.Data
}

func (v *NodeAdminClientQueryResults) HasError() bool {
	return v.data.Error != nil
}

func (v *NodeAdminClientQueryResults) Error() string {
	if v.data.Error == nil {
		return ""
	}
	return *v.data.Error
}

func (v *NodeAdminClientQueryResults) HasEngineRevision() bool {
	return v.data.EngineRevision != nil
}

func (v *NodeAdminClientQueryResults) EngineRevision() string {
	if v.data.EngineRevision == nil {
		return ""
	}
	return *v.data.EngineRevision
}

func (v NodeAdminClient) Query(ctx context.Context, expression string) (*NodeAdminClientQueryResults, error) {
	args := NodeAdminQueryArgs{}
	args.data.Expression = &expression

	var ret nodeAdminQueryResultsData

	err := v.Call(ctx, "query", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &NodeAdminClientQueryResults{client: v.Client, data: ret}, nil
}

type NodeAdminClientQueryInfoResults struct {
	client rpc.Client
	data   nodeAdminQueryInfoResultsData
}

func (v *NodeAdminClientQueryInfoResults) HasEngineRevision() bool {
	return v.data.EngineRevision != nil
}

func (v *NodeAdminClientQueryInfoResults) EngineRevision() string {
	if v.data.EngineRevision == nil {
		return ""
	}
	return *v.data.EngineRevision
}

func (v *NodeAdminClientQueryInfoResults) HasReference() bool {
	return v.data.Reference != nil
}

func (v *NodeAdminClientQueryInfoResults) Reference() string {
	if v.data.Reference == nil {
		return ""
	}
	return *v.data.Reference
}

func (v *NodeAdminClientQueryInfoResults) HasError() bool {
	return v.data.Error != nil
}

func (v *NodeAdminClientQueryInfoResults) Error() string {
	if v.data.Error == nil {
		return ""
	}
	return *v.data.Error
}

func (v NodeAdminClient) QueryInfo(ctx context.Context) (*NodeAdminClientQueryInfoResults, error) {
	args := NodeAdminQueryInfoArgs{}

	var ret nodeAdminQueryInfoResultsData

	err := v.Call(ctx, "query_info", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &NodeAdminClientQueryInfoResults{client: v.Client, data: ret}, nil
}

type NodeAdminClientValidateQueryResults struct {
	client rpc.Client
	data   nodeAdminValidateQueryResultsData
}

func (v *NodeAdminClientValidateQueryResults) HasEngineRevision() bool {
	return v.data.EngineRevision != nil
}

func (v *NodeAdminClientValidateQueryResults) EngineRevision() string {
	if v.data.EngineRevision == nil {
		return ""
	}
	return *v.data.EngineRevision
}

func (v *NodeAdminClientValidateQueryResults) HasValid() bool {
	return v.data.Valid != nil
}

func (v *NodeAdminClientValidateQueryResults) Valid() bool {
	if v.data.Valid == nil {
		return false
	}
	return *v.data.Valid
}

func (v *NodeAdminClientValidateQueryResults) HasError() bool {
	return v.data.Error != nil
}

func (v *NodeAdminClientValidateQueryResults) Error() string {
	if v.data.Error == nil {
		return ""
	}
	return *v.data.Error
}

func (v NodeAdminClient) ValidateQuery(ctx context.Context, expression string) (*NodeAdminClientValidateQueryResults, error) {
	args := NodeAdminValidateQueryArgs{}
	args.data.Expression = &expression

	var ret nodeAdminValidateQueryResultsData

	err := v.Call(ctx, "validate_query", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &NodeAdminClientValidateQueryResults{client: v.Client, data: ret}, nil
}
