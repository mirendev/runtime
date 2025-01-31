package app

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/fxamacker/cbor/v2"
	rpc "miren.dev/runtime/pkg/rpc"
)

type configurationData struct {
	EnvVars     *[]*NamedValue `cbor:"0,keyasint,omitempty" json:"env_vars,omitempty"`
	Concurrency *int32         `cbor:"1,keyasint,omitempty" json:"concurrency,omitempty"`
}

type Configuration struct {
	data configurationData
}

func (v *Configuration) HasEnvVars() bool {
	return v.data.EnvVars != nil
}

func (v *Configuration) EnvVars() []*NamedValue {
	if v.data.EnvVars == nil {
		return nil
	}
	return *v.data.EnvVars
}

func (v *Configuration) SetEnvVars(env_vars []*NamedValue) {
	x := slices.Clone(env_vars)
	v.data.EnvVars = &x
}

func (v *Configuration) HasConcurrency() bool {
	return v.data.Concurrency != nil
}

func (v *Configuration) Concurrency() int32 {
	if v.data.Concurrency == nil {
		return 0
	}
	return *v.data.Concurrency
}

func (v *Configuration) SetConcurrency(concurrency int32) {
	v.data.Concurrency = &concurrency
}

func (v *Configuration) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *Configuration) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *Configuration) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *Configuration) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type namedValueData struct {
	Key       *string `cbor:"0,keyasint,omitempty" json:"key,omitempty"`
	Value     *string `cbor:"1,keyasint,omitempty" json:"value,omitempty"`
	Sensitive *bool   `cbor:"2,keyasint,omitempty" json:"sensitive,omitempty"`
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

func (v *NamedValue) HasSensitive() bool {
	return v.data.Sensitive != nil
}

func (v *NamedValue) Sensitive() bool {
	if v.data.Sensitive == nil {
		return false
	}
	return *v.data.Sensitive
}

func (v *NamedValue) SetSensitive(sensitive bool) {
	v.data.Sensitive = &sensitive
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

type crudSetConfigurationArgsData struct {
	App           *string        `cbor:"0,keyasint,omitempty" json:"app,omitempty"`
	Configuration *Configuration `cbor:"1,keyasint,omitempty" json:"configuration,omitempty"`
}

type CrudSetConfigurationArgs struct {
	call *rpc.Call
	data crudSetConfigurationArgsData
}

func (v *CrudSetConfigurationArgs) HasApp() bool {
	return v.data.App != nil
}

func (v *CrudSetConfigurationArgs) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *CrudSetConfigurationArgs) HasConfiguration() bool {
	return v.data.Configuration != nil
}

func (v *CrudSetConfigurationArgs) Configuration() *Configuration {
	return v.data.Configuration
}

func (v *CrudSetConfigurationArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CrudSetConfigurationArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CrudSetConfigurationArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CrudSetConfigurationArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type crudSetConfigurationResultsData struct {
	VersionId *string `cbor:"0,keyasint,omitempty" json:"versionId,omitempty"`
}

type CrudSetConfigurationResults struct {
	call *rpc.Call
	data crudSetConfigurationResultsData
}

func (v *CrudSetConfigurationResults) SetVersionId(versionId string) {
	v.data.VersionId = &versionId
}

func (v *CrudSetConfigurationResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CrudSetConfigurationResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CrudSetConfigurationResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CrudSetConfigurationResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type crudGetConfigurationArgsData struct {
	App *string `cbor:"0,keyasint,omitempty" json:"app,omitempty"`
}

type CrudGetConfigurationArgs struct {
	call *rpc.Call
	data crudGetConfigurationArgsData
}

func (v *CrudGetConfigurationArgs) HasApp() bool {
	return v.data.App != nil
}

func (v *CrudGetConfigurationArgs) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *CrudGetConfigurationArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CrudGetConfigurationArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CrudGetConfigurationArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CrudGetConfigurationArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type crudGetConfigurationResultsData struct {
	Configuration *Configuration `cbor:"0,keyasint,omitempty" json:"configuration,omitempty"`
	VersionId     *string        `cbor:"1,keyasint,omitempty" json:"versionId,omitempty"`
}

type CrudGetConfigurationResults struct {
	call *rpc.Call
	data crudGetConfigurationResultsData
}

func (v *CrudGetConfigurationResults) SetConfiguration(configuration *Configuration) {
	v.data.Configuration = configuration
}

func (v *CrudGetConfigurationResults) SetVersionId(versionId string) {
	v.data.VersionId = &versionId
}

func (v *CrudGetConfigurationResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CrudGetConfigurationResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CrudGetConfigurationResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CrudGetConfigurationResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type crudSetHostArgsData struct {
	App  *string `cbor:"0,keyasint,omitempty" json:"app,omitempty"`
	Host *string `cbor:"1,keyasint,omitempty" json:"host,omitempty"`
}

type CrudSetHostArgs struct {
	call *rpc.Call
	data crudSetHostArgsData
}

func (v *CrudSetHostArgs) HasApp() bool {
	return v.data.App != nil
}

func (v *CrudSetHostArgs) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *CrudSetHostArgs) HasHost() bool {
	return v.data.Host != nil
}

func (v *CrudSetHostArgs) Host() string {
	if v.data.Host == nil {
		return ""
	}
	return *v.data.Host
}

func (v *CrudSetHostArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CrudSetHostArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CrudSetHostArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CrudSetHostArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type crudSetHostResultsData struct{}

type CrudSetHostResults struct {
	call *rpc.Call
	data crudSetHostResultsData
}

func (v *CrudSetHostResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CrudSetHostResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CrudSetHostResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CrudSetHostResults) UnmarshalJSON(data []byte) error {
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

type CrudSetConfiguration struct {
	*rpc.Call
	args    CrudSetConfigurationArgs
	results CrudSetConfigurationResults
}

func (t *CrudSetConfiguration) Args() *CrudSetConfigurationArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *CrudSetConfiguration) Results() *CrudSetConfigurationResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type CrudGetConfiguration struct {
	*rpc.Call
	args    CrudGetConfigurationArgs
	results CrudGetConfigurationResults
}

func (t *CrudGetConfiguration) Args() *CrudGetConfigurationArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *CrudGetConfiguration) Results() *CrudGetConfigurationResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type CrudSetHost struct {
	*rpc.Call
	args    CrudSetHostArgs
	results CrudSetHostResults
}

func (t *CrudSetHost) Args() *CrudSetHostArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *CrudSetHost) Results() *CrudSetHostResults {
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
	SetConfiguration(ctx context.Context, state *CrudSetConfiguration) error
	GetConfiguration(ctx context.Context, state *CrudGetConfiguration) error
	SetHost(ctx context.Context, state *CrudSetHost) error
}

type reexportCrud struct {
	client *rpc.Client
}

func (_ reexportCrud) New(ctx context.Context, state *CrudNew) error {
	panic("not implemented")
}

func (_ reexportCrud) SetConfiguration(ctx context.Context, state *CrudSetConfiguration) error {
	panic("not implemented")
}

func (_ reexportCrud) GetConfiguration(ctx context.Context, state *CrudGetConfiguration) error {
	panic("not implemented")
}

func (_ reexportCrud) SetHost(ctx context.Context, state *CrudSetHost) error {
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
			Name:          "setConfiguration",
			InterfaceName: "Crud",
			Index:         0,
			Handler: func(ctx context.Context, call *rpc.Call) error {
				return t.SetConfiguration(ctx, &CrudSetConfiguration{Call: call})
			},
		},
		{
			Name:          "getConfiguration",
			InterfaceName: "Crud",
			Index:         0,
			Handler: func(ctx context.Context, call *rpc.Call) error {
				return t.GetConfiguration(ctx, &CrudGetConfiguration{Call: call})
			},
		},
		{
			Name:          "setHost",
			InterfaceName: "Crud",
			Index:         0,
			Handler: func(ctx context.Context, call *rpc.Call) error {
				return t.SetHost(ctx, &CrudSetHost{Call: call})
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

type CrudClientSetConfigurationResults struct {
	client *rpc.Client
	data   crudSetConfigurationResultsData
}

func (v *CrudClientSetConfigurationResults) HasVersionId() bool {
	return v.data.VersionId != nil
}

func (v *CrudClientSetConfigurationResults) VersionId() string {
	if v.data.VersionId == nil {
		return ""
	}
	return *v.data.VersionId
}

func (v CrudClient) SetConfiguration(ctx context.Context, app string, configuration *Configuration) (*CrudClientSetConfigurationResults, error) {
	args := CrudSetConfigurationArgs{}
	args.data.App = &app
	args.data.Configuration = configuration

	var ret crudSetConfigurationResultsData

	err := v.Client.Call(ctx, "setConfiguration", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &CrudClientSetConfigurationResults{client: v.Client, data: ret}, nil
}

type CrudClientGetConfigurationResults struct {
	client *rpc.Client
	data   crudGetConfigurationResultsData
}

func (v *CrudClientGetConfigurationResults) HasConfiguration() bool {
	return v.data.Configuration != nil
}

func (v *CrudClientGetConfigurationResults) Configuration() *Configuration {
	return v.data.Configuration
}

func (v *CrudClientGetConfigurationResults) HasVersionId() bool {
	return v.data.VersionId != nil
}

func (v *CrudClientGetConfigurationResults) VersionId() string {
	if v.data.VersionId == nil {
		return ""
	}
	return *v.data.VersionId
}

func (v CrudClient) GetConfiguration(ctx context.Context, app string) (*CrudClientGetConfigurationResults, error) {
	args := CrudGetConfigurationArgs{}
	args.data.App = &app

	var ret crudGetConfigurationResultsData

	err := v.Client.Call(ctx, "getConfiguration", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &CrudClientGetConfigurationResults{client: v.Client, data: ret}, nil
}

type CrudClientSetHostResults struct {
	client *rpc.Client
	data   crudSetHostResultsData
}

func (v CrudClient) SetHost(ctx context.Context, app string, host string) (*CrudClientSetHostResults, error) {
	args := CrudSetHostArgs{}
	args.data.App = &app
	args.data.Host = &host

	var ret crudSetHostResultsData

	err := v.Client.Call(ctx, "setHost", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &CrudClientSetHostResults{client: v.Client, data: ret}, nil
}
