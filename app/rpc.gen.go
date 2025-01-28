package app

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/fxamacker/cbor/v2"
	rpc "miren.dev/runtime/pkg/rpc"
)

type namedValueData struct {
	Key   *string `cbor:"0,keyasint,omitempty" json:"key,omitempty"`
	Value *string `cbor:"1,keyasint,omitempty" json:"value,omitempty"`
}

type NamedValue struct {
	data namedValueData
}

func (v *NamedValue) HasKey() bool {
	return v.data.Key != nil
}

func (v *NamedValue) Key() string {
	if v.data.Key == nil {
		return ""
	}
	return *v.data.Key
}

func (v *NamedValue) SetKey(key string) {
	v.data.Key = &key
}

func (v *NamedValue) HasValue() bool {
	return v.data.Value != nil
}

func (v *NamedValue) Value() string {
	if v.data.Value == nil {
		return ""
	}
	return *v.data.Value
}

func (v *NamedValue) SetValue(value string) {
	v.data.Value = &value
}

func (v *NamedValue) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *NamedValue) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *NamedValue) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *NamedValue) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type crudNewArgsData struct {
	Name *string `cbor:"0,keyasint,omitempty" json:"name,omitempty"`
}

type CrudNewArgs struct {
	call *rpc.Call
	data crudNewArgsData
}

func (v *CrudNewArgs) HasName() bool {
	return v.data.Name != nil
}

func (v *CrudNewArgs) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *CrudNewArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CrudNewArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CrudNewArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CrudNewArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type crudNewResultsData struct {
	Id *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
}

type CrudNewResults struct {
	call *rpc.Call
	data crudNewResultsData
}

func (v *CrudNewResults) SetId(id string) {
	v.data.Id = &id
}

func (v *CrudNewResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CrudNewResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CrudNewResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CrudNewResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type crudAddEnvArgsData struct {
	App     *string        `cbor:"0,keyasint,omitempty" json:"app,omitempty"`
	Envvars *[]*NamedValue `cbor:"1,keyasint,omitempty" json:"envvars,omitempty"`
}

type CrudAddEnvArgs struct {
	call *rpc.Call
	data crudAddEnvArgsData
}

func (v *CrudAddEnvArgs) HasApp() bool {
	return v.data.App != nil
}

func (v *CrudAddEnvArgs) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *CrudAddEnvArgs) HasEnvvars() bool {
	return v.data.Envvars != nil
}

func (v *CrudAddEnvArgs) Envvars() []*NamedValue {
	if v.data.Envvars == nil {
		return nil
	}
	return *v.data.Envvars
}

func (v *CrudAddEnvArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CrudAddEnvArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CrudAddEnvArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CrudAddEnvArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type crudAddEnvResultsData struct {
	VersionId *string `cbor:"0,keyasint,omitempty" json:"versionId,omitempty"`
}

type CrudAddEnvResults struct {
	call *rpc.Call
	data crudAddEnvResultsData
}

func (v *CrudAddEnvResults) SetVersionId(versionId string) {
	v.data.VersionId = &versionId
}

func (v *CrudAddEnvResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CrudAddEnvResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CrudAddEnvResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CrudAddEnvResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type CrudNew struct {
	*rpc.Call
	args    CrudNewArgs
	results CrudNewResults
}

func (t *CrudNew) Args() *CrudNewArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *CrudNew) Results() *CrudNewResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type CrudAddEnv struct {
	*rpc.Call
	args    CrudAddEnvArgs
	results CrudAddEnvResults
}

func (t *CrudAddEnv) Args() *CrudAddEnvArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *CrudAddEnv) Results() *CrudAddEnvResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type Crud interface {
	New(ctx context.Context, state *CrudNew) error
	AddEnv(ctx context.Context, state *CrudAddEnv) error
}

type reexportCrud struct {
	client *rpc.Client
}

func (_ reexportCrud) New(ctx context.Context, state *CrudNew) error {
	panic("not implemented")
}

func (_ reexportCrud) AddEnv(ctx context.Context, state *CrudAddEnv) error {
	panic("not implemented")
}

func (t reexportCrud) CapabilityClient() *rpc.Client {
	return t.client
}

func AdaptCrud(t Crud) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "new",
			InterfaceName: "Crud",
			Index:         0,
			Handler: func(ctx context.Context, call *rpc.Call) error {
				return t.New(ctx, &CrudNew{Call: call})
			},
		},
		{
			Name:          "addEnv",
			InterfaceName: "Crud",
			Index:         0,
			Handler: func(ctx context.Context, call *rpc.Call) error {
				return t.AddEnv(ctx, &CrudAddEnv{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type CrudClient struct {
	*rpc.Client
}

func (c CrudClient) Export() Crud {
	return reexportCrud{client: c.Client}
}

type CrudClientNewResults struct {
	client *rpc.Client
	data   crudNewResultsData
}

func (v *CrudClientNewResults) HasId() bool {
	return v.data.Id != nil
}

func (v *CrudClientNewResults) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v CrudClient) New(ctx context.Context, name string) (*CrudClientNewResults, error) {
	args := CrudNewArgs{}
	args.data.Name = &name

	var ret crudNewResultsData

	err := v.Client.Call(ctx, "new", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &CrudClientNewResults{client: v.Client, data: ret}, nil
}

type CrudClientAddEnvResults struct {
	client *rpc.Client
	data   crudAddEnvResultsData
}

func (v *CrudClientAddEnvResults) HasVersionId() bool {
	return v.data.VersionId != nil
}

func (v *CrudClientAddEnvResults) VersionId() string {
	if v.data.VersionId == nil {
		return ""
	}
	return *v.data.VersionId
}

func (v CrudClient) AddEnv(ctx context.Context, app string, envvars []*NamedValue) (*CrudClientAddEnvResults, error) {
	args := CrudAddEnvArgs{}
	args.data.App = &app
	x := slices.Clone(envvars)
	args.data.Envvars = &x

	var ret crudAddEnvResultsData

	err := v.Client.Call(ctx, "addEnv", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &CrudClientAddEnvResults{client: v.Client, data: ret}, nil
}
