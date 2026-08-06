package rest

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/fxamacker/cbor/v2"
	rpc "miren.dev/runtime/pkg/rpc"
)

type widgetData struct {
	Id   *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
	Size *int32  `cbor:"1,keyasint,omitempty" json:"size,omitempty"`
}

type Widget struct {
	data widgetData
}

func (v *Widget) HasId() bool {
	return v.data.Id != nil
}

func (v *Widget) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *Widget) SetId(id string) {
	v.data.Id = &id
}

func (v *Widget) HasSize() bool {
	return v.data.Size != nil
}

func (v *Widget) Size() int32 {
	if v.data.Size == nil {
		return 0
	}
	return *v.data.Size
}

func (v *Widget) SetSize(size int32) {
	v.data.Size = &size
}

func (v *Widget) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *Widget) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *Widget) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *Widget) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type widgetsGetArgsData struct {
	Id *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
}

type WidgetsGetArgs struct {
	call rpc.Call
	data widgetsGetArgsData
}

func (v *WidgetsGetArgs) HasId() bool {
	return v.data.Id != nil
}

func (v *WidgetsGetArgs) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *WidgetsGetArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *WidgetsGetArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *WidgetsGetArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *WidgetsGetArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type widgetsGetResultsData struct {
	Widget *Widget `cbor:"0,keyasint,omitempty" json:"widget,omitempty"`
}

type WidgetsGetResults struct {
	call rpc.Call
	data widgetsGetResultsData
}

func (v *WidgetsGetResults) SetWidget(widget *Widget) {
	v.data.Widget = widget
}

func (v *WidgetsGetResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *WidgetsGetResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *WidgetsGetResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *WidgetsGetResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type widgetsCreateArgsData struct {
	Id   *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
	Size *int32  `cbor:"1,keyasint,omitempty" json:"size,omitempty"`
}

type WidgetsCreateArgs struct {
	call rpc.Call
	data widgetsCreateArgsData
}

func (v *WidgetsCreateArgs) HasId() bool {
	return v.data.Id != nil
}

func (v *WidgetsCreateArgs) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *WidgetsCreateArgs) HasSize() bool {
	return v.data.Size != nil
}

func (v *WidgetsCreateArgs) Size() int32 {
	if v.data.Size == nil {
		return 0
	}
	return *v.data.Size
}

func (v *WidgetsCreateArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *WidgetsCreateArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *WidgetsCreateArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *WidgetsCreateArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type widgetsCreateResultsData struct {
	Widget *Widget `cbor:"0,keyasint,omitempty" json:"widget,omitempty"`
}

type WidgetsCreateResults struct {
	call rpc.Call
	data widgetsCreateResultsData
}

func (v *WidgetsCreateResults) SetWidget(widget *Widget) {
	v.data.Widget = widget
}

func (v *WidgetsCreateResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *WidgetsCreateResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *WidgetsCreateResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *WidgetsCreateResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type widgetsDeleteArgsData struct {
	Id *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
}

type WidgetsDeleteArgs struct {
	call rpc.Call
	data widgetsDeleteArgsData
}

func (v *WidgetsDeleteArgs) HasId() bool {
	return v.data.Id != nil
}

func (v *WidgetsDeleteArgs) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *WidgetsDeleteArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *WidgetsDeleteArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *WidgetsDeleteArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *WidgetsDeleteArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type widgetsDeleteResultsData struct{}

type WidgetsDeleteResults struct {
	call rpc.Call
	data widgetsDeleteResultsData
}

func (v *WidgetsDeleteResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *WidgetsDeleteResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *WidgetsDeleteResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *WidgetsDeleteResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type widgetsSearchArgsData struct {
	Prefix *string `cbor:"0,keyasint,omitempty" json:"prefix,omitempty"`
	Limit  *int32  `cbor:"1,keyasint,omitempty" json:"limit,omitempty"`
	Active *bool   `cbor:"2,keyasint,omitempty" json:"active,omitempty"`
}

type WidgetsSearchArgs struct {
	call rpc.Call
	data widgetsSearchArgsData
}

func (v *WidgetsSearchArgs) HasPrefix() bool {
	return v.data.Prefix != nil
}

func (v *WidgetsSearchArgs) Prefix() string {
	if v.data.Prefix == nil {
		return ""
	}
	return *v.data.Prefix
}

func (v *WidgetsSearchArgs) HasLimit() bool {
	return v.data.Limit != nil
}

func (v *WidgetsSearchArgs) Limit() int32 {
	if v.data.Limit == nil {
		return 0
	}
	return *v.data.Limit
}

func (v *WidgetsSearchArgs) HasActive() bool {
	return v.data.Active != nil
}

func (v *WidgetsSearchArgs) Active() bool {
	if v.data.Active == nil {
		return false
	}
	return *v.data.Active
}

func (v *WidgetsSearchArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *WidgetsSearchArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *WidgetsSearchArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *WidgetsSearchArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type widgetsSearchResultsData struct {
	Widgets *[]*Widget `cbor:"0,keyasint,omitempty" json:"widgets,omitempty"`
}

type WidgetsSearchResults struct {
	call rpc.Call
	data widgetsSearchResultsData
}

func (v *WidgetsSearchResults) SetWidgets(widgets []*Widget) {
	x := slices.Clone(widgets)
	v.data.Widgets = &x
}

func (v *WidgetsSearchResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *WidgetsSearchResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *WidgetsSearchResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *WidgetsSearchResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type widgetsInternalOnlyArgsData struct {
	Id *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
}

type WidgetsInternalOnlyArgs struct {
	call rpc.Call
	data widgetsInternalOnlyArgsData
}

func (v *WidgetsInternalOnlyArgs) HasId() bool {
	return v.data.Id != nil
}

func (v *WidgetsInternalOnlyArgs) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *WidgetsInternalOnlyArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *WidgetsInternalOnlyArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *WidgetsInternalOnlyArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *WidgetsInternalOnlyArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type widgetsInternalOnlyResultsData struct {
	Ok *bool `cbor:"0,keyasint,omitempty" json:"ok,omitempty"`
}

type WidgetsInternalOnlyResults struct {
	call rpc.Call
	data widgetsInternalOnlyResultsData
}

func (v *WidgetsInternalOnlyResults) SetOk(ok bool) {
	v.data.Ok = &ok
}

func (v *WidgetsInternalOnlyResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *WidgetsInternalOnlyResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *WidgetsInternalOnlyResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *WidgetsInternalOnlyResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type WidgetsGet struct {
	rpc.Call
	args    WidgetsGetArgs
	results WidgetsGetResults
}

func (t *WidgetsGet) Args() *WidgetsGetArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *WidgetsGet) Results() *WidgetsGetResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type WidgetsCreate struct {
	rpc.Call
	args    WidgetsCreateArgs
	results WidgetsCreateResults
}

func (t *WidgetsCreate) Args() *WidgetsCreateArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *WidgetsCreate) Results() *WidgetsCreateResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type WidgetsDelete struct {
	rpc.Call
	args    WidgetsDeleteArgs
	results WidgetsDeleteResults
}

func (t *WidgetsDelete) Args() *WidgetsDeleteArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *WidgetsDelete) Results() *WidgetsDeleteResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type WidgetsSearch struct {
	rpc.Call
	args    WidgetsSearchArgs
	results WidgetsSearchResults
}

func (t *WidgetsSearch) Args() *WidgetsSearchArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *WidgetsSearch) Results() *WidgetsSearchResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type WidgetsInternalOnly struct {
	rpc.Call
	args    WidgetsInternalOnlyArgs
	results WidgetsInternalOnlyResults
}

func (t *WidgetsInternalOnly) Args() *WidgetsInternalOnlyArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *WidgetsInternalOnly) Results() *WidgetsInternalOnlyResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type Widgets interface {
	Get(ctx context.Context, state *WidgetsGet) error
	Create(ctx context.Context, state *WidgetsCreate) error
	Delete(ctx context.Context, state *WidgetsDelete) error
	Search(ctx context.Context, state *WidgetsSearch) error
	InternalOnly(ctx context.Context, state *WidgetsInternalOnly) error
}

type reexportWidgets struct {
	client rpc.Client
}

func (reexportWidgets) Get(ctx context.Context, state *WidgetsGet) error {
	panic("not implemented")
}

func (reexportWidgets) Create(ctx context.Context, state *WidgetsCreate) error {
	panic("not implemented")
}

func (reexportWidgets) Delete(ctx context.Context, state *WidgetsDelete) error {
	panic("not implemented")
}

func (reexportWidgets) Search(ctx context.Context, state *WidgetsSearch) error {
	panic("not implemented")
}

func (reexportWidgets) InternalOnly(ctx context.Context, state *WidgetsInternalOnly) error {
	panic("not implemented")
}

func (t reexportWidgets) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptWidgets(t Widgets) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "get",
			InterfaceName: "Widgets",
			Index:         0,
			Public:        false,
			Params:        []string{"id"},
			HTTP: &rpc.HTTPBinding{
				Verb:       "GET",
				Path:       "/api/v1/widgets/{id}",
				Body:       "",
				PathParams: []string{"id"},
			},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Get(ctx, &WidgetsGet{Call: call})
			},
		},
		{
			Name:          "create",
			InterfaceName: "Widgets",
			Index:         1,
			Public:        false,
			Params:        []string{"id", "size"},
			HTTP: &rpc.HTTPBinding{
				Verb:       "POST",
				Path:       "/api/v1/widgets",
				Body:       "*",
				PathParams: []string{},
			},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Create(ctx, &WidgetsCreate{Call: call})
			},
		},
		{
			Name:          "delete",
			InterfaceName: "Widgets",
			Index:         2,
			Public:        false,
			Params:        []string{"id"},
			HTTP: &rpc.HTTPBinding{
				Verb:       "DELETE",
				Path:       "/api/v1/widgets/{id}",
				Body:       "",
				PathParams: []string{"id"},
			},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Delete(ctx, &WidgetsDelete{Call: call})
			},
		},
		{
			Name:          "search",
			InterfaceName: "Widgets",
			Index:         4,
			Public:        false,
			Params:        []string{"prefix", "limit", "active"},
			HTTP: &rpc.HTTPBinding{
				Verb:       "GET",
				Path:       "/api/v1/widgets",
				Body:       "",
				PathParams: []string{},
				Query: []rpc.HTTPParam{
					{Name: "prefix", Kind: "string"},
					{Name: "limit", Kind: "int"},
					{Name: "active", Kind: "bool"},
				},
			},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Search(ctx, &WidgetsSearch{Call: call})
			},
		},
		{
			Name:          "internalOnly",
			InterfaceName: "Widgets",
			Index:         3,
			Public:        false,
			Params:        []string{"id"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.InternalOnly(ctx, &WidgetsInternalOnly{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type WidgetsClient struct {
	rpc.Client
}

func NewWidgetsClient(client rpc.Client) *WidgetsClient {
	return &WidgetsClient{Client: client}
}

func (c WidgetsClient) Export() Widgets {
	return reexportWidgets{client: c.Client}
}

type WidgetsClientGetResults struct {
	client rpc.Client
	data   widgetsGetResultsData
}

func (v *WidgetsClientGetResults) HasWidget() bool {
	return v.data.Widget != nil
}

func (v *WidgetsClientGetResults) Widget() *Widget {
	return v.data.Widget
}

func (v WidgetsClient) Get(ctx context.Context, id string) (*WidgetsClientGetResults, error) {
	args := WidgetsGetArgs{}
	args.data.Id = &id

	var ret widgetsGetResultsData

	err := v.Call(ctx, "get", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &WidgetsClientGetResults{client: v.Client, data: ret}, nil
}

type WidgetsClientCreateResults struct {
	client rpc.Client
	data   widgetsCreateResultsData
}

func (v *WidgetsClientCreateResults) HasWidget() bool {
	return v.data.Widget != nil
}

func (v *WidgetsClientCreateResults) Widget() *Widget {
	return v.data.Widget
}

func (v WidgetsClient) Create(ctx context.Context, id string, size int32) (*WidgetsClientCreateResults, error) {
	args := WidgetsCreateArgs{}
	args.data.Id = &id
	args.data.Size = &size

	var ret widgetsCreateResultsData

	err := v.Call(ctx, "create", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &WidgetsClientCreateResults{client: v.Client, data: ret}, nil
}

type WidgetsClientDeleteResults struct {
	client rpc.Client
	data   widgetsDeleteResultsData
}

func (v WidgetsClient) Delete(ctx context.Context, id string) (*WidgetsClientDeleteResults, error) {
	args := WidgetsDeleteArgs{}
	args.data.Id = &id

	var ret widgetsDeleteResultsData

	err := v.Call(ctx, "delete", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &WidgetsClientDeleteResults{client: v.Client, data: ret}, nil
}

type WidgetsClientSearchResults struct {
	client rpc.Client
	data   widgetsSearchResultsData
}

func (v *WidgetsClientSearchResults) HasWidgets() bool {
	return v.data.Widgets != nil
}

func (v *WidgetsClientSearchResults) Widgets() []*Widget {
	if v.data.Widgets == nil {
		return nil
	}
	return *v.data.Widgets
}

func (v WidgetsClient) Search(ctx context.Context, prefix string, limit int32, active bool) (*WidgetsClientSearchResults, error) {
	args := WidgetsSearchArgs{}
	args.data.Prefix = &prefix
	args.data.Limit = &limit
	args.data.Active = &active

	var ret widgetsSearchResultsData

	err := v.Call(ctx, "search", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &WidgetsClientSearchResults{client: v.Client, data: ret}, nil
}

type WidgetsClientInternalOnlyResults struct {
	client rpc.Client
	data   widgetsInternalOnlyResultsData
}

func (v *WidgetsClientInternalOnlyResults) HasOk() bool {
	return v.data.Ok != nil
}

func (v *WidgetsClientInternalOnlyResults) Ok() bool {
	if v.data.Ok == nil {
		return false
	}
	return *v.data.Ok
}

func (v WidgetsClient) InternalOnly(ctx context.Context, id string) (*WidgetsClientInternalOnlyResults, error) {
	args := WidgetsInternalOnlyArgs{}
	args.data.Id = &id

	var ret widgetsInternalOnlyResultsData

	err := v.Call(ctx, "internalOnly", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &WidgetsClientInternalOnlyResults{client: v.Client, data: ret}, nil
}
