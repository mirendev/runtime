package admin_v1alpha

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/fxamacker/cbor/v2"
	rpc "miren.dev/runtime/pkg/rpc"
)

type adminCallResultData struct {
	Result    *string `cbor:"0,keyasint,omitempty" json:"result,omitempty"`
	Error     *string `cbor:"1,keyasint,omitempty" json:"error,omitempty"`
	ErrorCode *int32  `cbor:"2,keyasint,omitempty" json:"error_code,omitempty"`
}

type AdminCallResult struct {
	data adminCallResultData
}

func (v *AdminCallResult) HasResult() bool {
	return v.data.Result != nil
}

func (v *AdminCallResult) Result() string {
	if v.data.Result == nil {
		return ""
	}
	return *v.data.Result
}

func (v *AdminCallResult) SetResult(result string) {
	v.data.Result = &result
}

func (v *AdminCallResult) HasError() bool {
	return v.data.Error != nil
}

func (v *AdminCallResult) Error() string {
	if v.data.Error == nil {
		return ""
	}
	return *v.data.Error
}

func (v *AdminCallResult) SetError(error string) {
	v.data.Error = &error
}

func (v *AdminCallResult) HasErrorCode() bool {
	return v.data.ErrorCode != nil
}

func (v *AdminCallResult) ErrorCode() int32 {
	if v.data.ErrorCode == nil {
		return 0
	}
	return *v.data.ErrorCode
}

func (v *AdminCallResult) SetErrorCode(error_code int32) {
	v.data.ErrorCode = &error_code
}

func (v *AdminCallResult) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *AdminCallResult) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *AdminCallResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *AdminCallResult) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type adminMethodData struct {
	Name        *string              `cbor:"0,keyasint,omitempty" json:"name,omitempty"`
	Description *string              `cbor:"1,keyasint,omitempty" json:"description,omitempty"`
	Params      *[]*AdminMethodParam `cbor:"2,keyasint,omitempty" json:"params,omitempty"`
	Category    *string              `cbor:"3,keyasint,omitempty" json:"category,omitempty"`
	SafeRead    *bool                `cbor:"4,keyasint,omitempty" json:"safe_read,omitempty"`
}

type AdminMethod struct {
	data adminMethodData
}

func (v *AdminMethod) HasName() bool {
	return v.data.Name != nil
}

func (v *AdminMethod) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *AdminMethod) SetName(name string) {
	v.data.Name = &name
}

func (v *AdminMethod) HasDescription() bool {
	return v.data.Description != nil
}

func (v *AdminMethod) Description() string {
	if v.data.Description == nil {
		return ""
	}
	return *v.data.Description
}

func (v *AdminMethod) SetDescription(description string) {
	v.data.Description = &description
}

func (v *AdminMethod) HasParams() bool {
	return v.data.Params != nil
}

func (v *AdminMethod) Params() []*AdminMethodParam {
	if v.data.Params == nil {
		return nil
	}
	return *v.data.Params
}

func (v *AdminMethod) SetParams(params []*AdminMethodParam) {
	x := slices.Clone(params)
	v.data.Params = &x
}

func (v *AdminMethod) HasCategory() bool {
	return v.data.Category != nil
}

func (v *AdminMethod) Category() string {
	if v.data.Category == nil {
		return ""
	}
	return *v.data.Category
}

func (v *AdminMethod) SetCategory(category string) {
	v.data.Category = &category
}

func (v *AdminMethod) HasSafeRead() bool {
	return v.data.SafeRead != nil
}

func (v *AdminMethod) SafeRead() bool {
	if v.data.SafeRead == nil {
		return false
	}
	return *v.data.SafeRead
}

func (v *AdminMethod) SetSafeRead(safe_read bool) {
	v.data.SafeRead = &safe_read
}

func (v *AdminMethod) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *AdminMethod) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *AdminMethod) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *AdminMethod) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type adminMethodParamData struct {
	Name      *string `cbor:"0,keyasint,omitempty" json:"name,omitempty"`
	ParamType *string `cbor:"1,keyasint,omitempty" json:"param_type,omitempty"`
	Required  *bool   `cbor:"2,keyasint,omitempty" json:"required,omitempty"`
}

type AdminMethodParam struct {
	data adminMethodParamData
}

func (v *AdminMethodParam) HasName() bool {
	return v.data.Name != nil
}

func (v *AdminMethodParam) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *AdminMethodParam) SetName(name string) {
	v.data.Name = &name
}

func (v *AdminMethodParam) HasParamType() bool {
	return v.data.ParamType != nil
}

func (v *AdminMethodParam) ParamType() string {
	if v.data.ParamType == nil {
		return ""
	}
	return *v.data.ParamType
}

func (v *AdminMethodParam) SetParamType(param_type string) {
	v.data.ParamType = &param_type
}

func (v *AdminMethodParam) HasRequired() bool {
	return v.data.Required != nil
}

func (v *AdminMethodParam) Required() bool {
	if v.data.Required == nil {
		return false
	}
	return *v.data.Required
}

func (v *AdminMethodParam) SetRequired(required bool) {
	v.data.Required = &required
}

func (v *AdminMethodParam) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *AdminMethodParam) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *AdminMethodParam) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *AdminMethodParam) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type adminInvokeArgsData struct {
	App    *string `cbor:"0,keyasint,omitempty" json:"app,omitempty"`
	Method *string `cbor:"1,keyasint,omitempty" json:"method,omitempty"`
	Params *string `cbor:"2,keyasint,omitempty" json:"params,omitempty"`
}

type AdminInvokeArgs struct {
	call rpc.Call
	data adminInvokeArgsData
}

func (v *AdminInvokeArgs) HasApp() bool {
	return v.data.App != nil
}

func (v *AdminInvokeArgs) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *AdminInvokeArgs) HasMethod() bool {
	return v.data.Method != nil
}

func (v *AdminInvokeArgs) Method() string {
	if v.data.Method == nil {
		return ""
	}
	return *v.data.Method
}

func (v *AdminInvokeArgs) HasParams() bool {
	return v.data.Params != nil
}

func (v *AdminInvokeArgs) Params() string {
	if v.data.Params == nil {
		return ""
	}
	return *v.data.Params
}

func (v *AdminInvokeArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *AdminInvokeArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *AdminInvokeArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *AdminInvokeArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type adminInvokeResultsData struct {
	Result *AdminCallResult `cbor:"0,keyasint,omitempty" json:"result,omitempty"`
}

type AdminInvokeResults struct {
	call rpc.Call
	data adminInvokeResultsData
}

func (v *AdminInvokeResults) SetResult(result *AdminCallResult) {
	v.data.Result = result
}

func (v *AdminInvokeResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *AdminInvokeResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *AdminInvokeResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *AdminInvokeResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type adminListMethodsArgsData struct {
	App *string `cbor:"0,keyasint,omitempty" json:"app,omitempty"`
}

type AdminListMethodsArgs struct {
	call rpc.Call
	data adminListMethodsArgsData
}

func (v *AdminListMethodsArgs) HasApp() bool {
	return v.data.App != nil
}

func (v *AdminListMethodsArgs) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *AdminListMethodsArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *AdminListMethodsArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *AdminListMethodsArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *AdminListMethodsArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type adminListMethodsResultsData struct {
	Methods *[]*AdminMethod `cbor:"0,keyasint,omitempty" json:"methods,omitempty"`
	Error   *string         `cbor:"1,keyasint,omitempty" json:"error,omitempty"`
}

type AdminListMethodsResults struct {
	call rpc.Call
	data adminListMethodsResultsData
}

func (v *AdminListMethodsResults) SetMethods(methods []*AdminMethod) {
	x := slices.Clone(methods)
	v.data.Methods = &x
}

func (v *AdminListMethodsResults) SetError(error string) {
	v.data.Error = &error
}

func (v *AdminListMethodsResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *AdminListMethodsResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *AdminListMethodsResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *AdminListMethodsResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type adminDescribeMethodsArgsData struct {
	App     *string   `cbor:"0,keyasint,omitempty" json:"app,omitempty"`
	Methods *[]string `cbor:"1,keyasint,omitempty" json:"methods,omitempty"`
}

type AdminDescribeMethodsArgs struct {
	call rpc.Call
	data adminDescribeMethodsArgsData
}

func (v *AdminDescribeMethodsArgs) HasApp() bool {
	return v.data.App != nil
}

func (v *AdminDescribeMethodsArgs) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *AdminDescribeMethodsArgs) HasMethods() bool {
	return v.data.Methods != nil
}

func (v *AdminDescribeMethodsArgs) Methods() []string {
	if v.data.Methods == nil {
		return nil
	}
	return *v.data.Methods
}

func (v *AdminDescribeMethodsArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *AdminDescribeMethodsArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *AdminDescribeMethodsArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *AdminDescribeMethodsArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type adminDescribeMethodsResultsData struct {
	Methods *[]*AdminMethod `cbor:"0,keyasint,omitempty" json:"methods,omitempty"`
	Error   *string         `cbor:"1,keyasint,omitempty" json:"error,omitempty"`
}

type AdminDescribeMethodsResults struct {
	call rpc.Call
	data adminDescribeMethodsResultsData
}

func (v *AdminDescribeMethodsResults) SetMethods(methods []*AdminMethod) {
	x := slices.Clone(methods)
	v.data.Methods = &x
}

func (v *AdminDescribeMethodsResults) SetError(error string) {
	v.data.Error = &error
}

func (v *AdminDescribeMethodsResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *AdminDescribeMethodsResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *AdminDescribeMethodsResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *AdminDescribeMethodsResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type AdminInvoke struct {
	rpc.Call
	args    AdminInvokeArgs
	results AdminInvokeResults
}

func (t *AdminInvoke) Args() *AdminInvokeArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *AdminInvoke) Results() *AdminInvokeResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type AdminListMethods struct {
	rpc.Call
	args    AdminListMethodsArgs
	results AdminListMethodsResults
}

func (t *AdminListMethods) Args() *AdminListMethodsArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *AdminListMethods) Results() *AdminListMethodsResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type AdminDescribeMethods struct {
	rpc.Call
	args    AdminDescribeMethodsArgs
	results AdminDescribeMethodsResults
}

func (t *AdminDescribeMethods) Args() *AdminDescribeMethodsArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *AdminDescribeMethods) Results() *AdminDescribeMethodsResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type Admin interface {
	Invoke(ctx context.Context, state *AdminInvoke) error
	ListMethods(ctx context.Context, state *AdminListMethods) error
	DescribeMethods(ctx context.Context, state *AdminDescribeMethods) error
}

type reexportAdmin struct {
	client rpc.Client
}

func (reexportAdmin) Invoke(ctx context.Context, state *AdminInvoke) error {
	panic("not implemented")
}

func (reexportAdmin) ListMethods(ctx context.Context, state *AdminListMethods) error {
	panic("not implemented")
}

func (reexportAdmin) DescribeMethods(ctx context.Context, state *AdminDescribeMethods) error {
	panic("not implemented")
}

func (t reexportAdmin) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptAdmin(t Admin) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "Invoke",
			InterfaceName: "Admin",
			Index:         0,
			Public:        false,
			Params:        []string{"app", "method", "params"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Invoke(ctx, &AdminInvoke{Call: call})
			},
		},
		{
			Name:          "ListMethods",
			InterfaceName: "Admin",
			Index:         1,
			Public:        false,
			Params:        []string{"app"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.ListMethods(ctx, &AdminListMethods{Call: call})
			},
		},
		{
			Name:          "DescribeMethods",
			InterfaceName: "Admin",
			Index:         2,
			Public:        false,
			Params:        []string{"app", "methods"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.DescribeMethods(ctx, &AdminDescribeMethods{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type AdminClient struct {
	rpc.Client
}

func NewAdminClient(client rpc.Client) *AdminClient {
	return &AdminClient{Client: client}
}

func (c AdminClient) Export() Admin {
	return reexportAdmin{client: c.Client}
}

type AdminClientInvokeResults struct {
	client rpc.Client
	data   adminInvokeResultsData
}

func (v *AdminClientInvokeResults) HasResult() bool {
	return v.data.Result != nil
}

func (v *AdminClientInvokeResults) Result() *AdminCallResult {
	return v.data.Result
}

func (v AdminClient) Invoke(ctx context.Context, app string, method string, params string) (*AdminClientInvokeResults, error) {
	args := AdminInvokeArgs{}
	args.data.App = &app
	args.data.Method = &method
	args.data.Params = &params

	var ret adminInvokeResultsData

	err := v.Call(ctx, "Invoke", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &AdminClientInvokeResults{client: v.Client, data: ret}, nil
}

type AdminClientListMethodsResults struct {
	client rpc.Client
	data   adminListMethodsResultsData
}

func (v *AdminClientListMethodsResults) HasMethods() bool {
	return v.data.Methods != nil
}

func (v *AdminClientListMethodsResults) Methods() []*AdminMethod {
	if v.data.Methods == nil {
		return nil
	}
	return *v.data.Methods
}

func (v *AdminClientListMethodsResults) HasError() bool {
	return v.data.Error != nil
}

func (v *AdminClientListMethodsResults) Error() string {
	if v.data.Error == nil {
		return ""
	}
	return *v.data.Error
}

func (v AdminClient) ListMethods(ctx context.Context, app string) (*AdminClientListMethodsResults, error) {
	args := AdminListMethodsArgs{}
	args.data.App = &app

	var ret adminListMethodsResultsData

	err := v.Call(ctx, "ListMethods", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &AdminClientListMethodsResults{client: v.Client, data: ret}, nil
}

type AdminClientDescribeMethodsResults struct {
	client rpc.Client
	data   adminDescribeMethodsResultsData
}

func (v *AdminClientDescribeMethodsResults) HasMethods() bool {
	return v.data.Methods != nil
}

func (v *AdminClientDescribeMethodsResults) Methods() []*AdminMethod {
	if v.data.Methods == nil {
		return nil
	}
	return *v.data.Methods
}

func (v *AdminClientDescribeMethodsResults) HasError() bool {
	return v.data.Error != nil
}

func (v *AdminClientDescribeMethodsResults) Error() string {
	if v.data.Error == nil {
		return ""
	}
	return *v.data.Error
}

func (v AdminClient) DescribeMethods(ctx context.Context, app string, methods []string) (*AdminClientDescribeMethodsResults, error) {
	args := AdminDescribeMethodsArgs{}
	args.data.App = &app
	x := slices.Clone(methods)
	args.data.Methods = &x

	var ret adminDescribeMethodsResultsData

	err := v.Call(ctx, "DescribeMethods", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &AdminClientDescribeMethodsResults{client: v.Client, data: ret}, nil
}
