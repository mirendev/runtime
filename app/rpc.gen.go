package app

import (
	"context"
	"encoding/json"

	"github.com/fxamacker/cbor/v2"
	rpc "miren.dev/runtime/pkg/rpc"
)

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

type Crud interface {
	New(ctx context.Context, state *CrudNew) error
}

type reexportCrud struct {
	client *rpc.Client
}

func (_ reexportCrud) New(ctx context.Context, state *CrudNew) error {
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
