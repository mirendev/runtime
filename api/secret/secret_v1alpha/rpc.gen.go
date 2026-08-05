package secret_v1alpha

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/fxamacker/cbor/v2"
	rpc "miren.dev/runtime/pkg/rpc"
)

type secretVersionInfoData struct {
	Version   *string `cbor:"0,keyasint,omitempty" json:"version,omitempty"`
	State     *string `cbor:"1,keyasint,omitempty" json:"state,omitempty"`
	CreatedAt *int64  `cbor:"2,keyasint,omitempty" json:"created_at,omitempty"`
	Current   *bool   `cbor:"3,keyasint,omitempty" json:"current,omitempty"`
}

type SecretVersionInfo struct {
	data secretVersionInfoData
}

func (v *SecretVersionInfo) HasVersion() bool {
	return v.data.Version != nil
}

func (v *SecretVersionInfo) Version() string {
	if v.data.Version == nil {
		return ""
	}
	return *v.data.Version
}

func (v *SecretVersionInfo) SetVersion(version string) {
	v.data.Version = &version
}

func (v *SecretVersionInfo) HasState() bool {
	return v.data.State != nil
}

func (v *SecretVersionInfo) State() string {
	if v.data.State == nil {
		return ""
	}
	return *v.data.State
}

func (v *SecretVersionInfo) SetState(state string) {
	v.data.State = &state
}

func (v *SecretVersionInfo) HasCreatedAt() bool {
	return v.data.CreatedAt != nil
}

func (v *SecretVersionInfo) CreatedAt() int64 {
	if v.data.CreatedAt == nil {
		return 0
	}
	return *v.data.CreatedAt
}

func (v *SecretVersionInfo) SetCreatedAt(created_at int64) {
	v.data.CreatedAt = &created_at
}

func (v *SecretVersionInfo) HasCurrent() bool {
	return v.data.Current != nil
}

func (v *SecretVersionInfo) Current() bool {
	if v.data.Current == nil {
		return false
	}
	return *v.data.Current
}

func (v *SecretVersionInfo) SetCurrent(current bool) {
	v.data.Current = &current
}

func (v *SecretVersionInfo) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SecretVersionInfo) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SecretVersionInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SecretVersionInfo) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type secretInfoData struct {
	Path           *string               `cbor:"0,keyasint,omitempty" json:"path,omitempty"`
	Backend        *string               `cbor:"1,keyasint,omitempty" json:"backend,omitempty"`
	CurrentVersion *string               `cbor:"2,keyasint,omitempty" json:"current_version,omitempty"`
	Versions       *[]*SecretVersionInfo `cbor:"3,keyasint,omitempty" json:"versions,omitempty"`
}

type SecretInfo struct {
	data secretInfoData
}

func (v *SecretInfo) HasPath() bool {
	return v.data.Path != nil
}

func (v *SecretInfo) Path() string {
	if v.data.Path == nil {
		return ""
	}
	return *v.data.Path
}

func (v *SecretInfo) SetPath(path string) {
	v.data.Path = &path
}

func (v *SecretInfo) HasBackend() bool {
	return v.data.Backend != nil
}

func (v *SecretInfo) Backend() string {
	if v.data.Backend == nil {
		return ""
	}
	return *v.data.Backend
}

func (v *SecretInfo) SetBackend(backend string) {
	v.data.Backend = &backend
}

func (v *SecretInfo) HasCurrentVersion() bool {
	return v.data.CurrentVersion != nil
}

func (v *SecretInfo) CurrentVersion() string {
	if v.data.CurrentVersion == nil {
		return ""
	}
	return *v.data.CurrentVersion
}

func (v *SecretInfo) SetCurrentVersion(current_version string) {
	v.data.CurrentVersion = &current_version
}

func (v *SecretInfo) HasVersions() bool {
	return v.data.Versions != nil
}

func (v *SecretInfo) Versions() []*SecretVersionInfo {
	if v.data.Versions == nil {
		return nil
	}
	return *v.data.Versions
}

func (v *SecretInfo) SetVersions(versions []*SecretVersionInfo) {
	x := slices.Clone(versions)
	v.data.Versions = &x
}

func (v *SecretInfo) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SecretInfo) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SecretInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SecretInfo) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type secretsSetArgsData struct {
	Backend *string `cbor:"0,keyasint,omitempty" json:"backend,omitempty"`
	Path    *string `cbor:"1,keyasint,omitempty" json:"path,omitempty"`
	Value   *[]byte `cbor:"2,keyasint,omitempty" json:"value,omitempty"`
}

type SecretsSetArgs struct {
	call rpc.Call
	data secretsSetArgsData
}

func (v *SecretsSetArgs) HasBackend() bool {
	return v.data.Backend != nil
}

func (v *SecretsSetArgs) Backend() string {
	if v.data.Backend == nil {
		return ""
	}
	return *v.data.Backend
}

func (v *SecretsSetArgs) HasPath() bool {
	return v.data.Path != nil
}

func (v *SecretsSetArgs) Path() string {
	if v.data.Path == nil {
		return ""
	}
	return *v.data.Path
}

func (v *SecretsSetArgs) HasValue() bool {
	return v.data.Value != nil
}

func (v *SecretsSetArgs) Value() []byte {
	if v.data.Value == nil {
		return nil
	}
	return *v.data.Value
}

func (v *SecretsSetArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SecretsSetArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SecretsSetArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SecretsSetArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type secretsSetResultsData struct {
	Version   *string `cbor:"0,keyasint,omitempty" json:"version,omitempty"`
	Unchanged *bool   `cbor:"1,keyasint,omitempty" json:"unchanged,omitempty"`
}

type SecretsSetResults struct {
	call rpc.Call
	data secretsSetResultsData
}

func (v *SecretsSetResults) SetVersion(version string) {
	v.data.Version = &version
}

func (v *SecretsSetResults) SetUnchanged(unchanged bool) {
	v.data.Unchanged = &unchanged
}

func (v *SecretsSetResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SecretsSetResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SecretsSetResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SecretsSetResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type secretsListArgsData struct {
	Backend *string `cbor:"0,keyasint,omitempty" json:"backend,omitempty"`
}

type SecretsListArgs struct {
	call rpc.Call
	data secretsListArgsData
}

func (v *SecretsListArgs) HasBackend() bool {
	return v.data.Backend != nil
}

func (v *SecretsListArgs) Backend() string {
	if v.data.Backend == nil {
		return ""
	}
	return *v.data.Backend
}

func (v *SecretsListArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SecretsListArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SecretsListArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SecretsListArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type secretsListResultsData struct {
	Secrets *[]*SecretInfo `cbor:"0,keyasint,omitempty" json:"secrets,omitempty"`
}

type SecretsListResults struct {
	call rpc.Call
	data secretsListResultsData
}

func (v *SecretsListResults) SetSecrets(secrets []*SecretInfo) {
	x := slices.Clone(secrets)
	v.data.Secrets = &x
}

func (v *SecretsListResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SecretsListResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SecretsListResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SecretsListResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type secretsListVersionsArgsData struct {
	Backend *string `cbor:"0,keyasint,omitempty" json:"backend,omitempty"`
	Path    *string `cbor:"1,keyasint,omitempty" json:"path,omitempty"`
}

type SecretsListVersionsArgs struct {
	call rpc.Call
	data secretsListVersionsArgsData
}

func (v *SecretsListVersionsArgs) HasBackend() bool {
	return v.data.Backend != nil
}

func (v *SecretsListVersionsArgs) Backend() string {
	if v.data.Backend == nil {
		return ""
	}
	return *v.data.Backend
}

func (v *SecretsListVersionsArgs) HasPath() bool {
	return v.data.Path != nil
}

func (v *SecretsListVersionsArgs) Path() string {
	if v.data.Path == nil {
		return ""
	}
	return *v.data.Path
}

func (v *SecretsListVersionsArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SecretsListVersionsArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SecretsListVersionsArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SecretsListVersionsArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type secretsListVersionsResultsData struct {
	Secret *SecretInfo `cbor:"0,keyasint,omitempty" json:"secret,omitempty"`
}

type SecretsListVersionsResults struct {
	call rpc.Call
	data secretsListVersionsResultsData
}

func (v *SecretsListVersionsResults) SetSecret(secret *SecretInfo) {
	v.data.Secret = secret
}

func (v *SecretsListVersionsResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SecretsListVersionsResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SecretsListVersionsResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SecretsListVersionsResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type secretsSetStateArgsData struct {
	Backend *string `cbor:"0,keyasint,omitempty" json:"backend,omitempty"`
	Ref     *string `cbor:"1,keyasint,omitempty" json:"ref,omitempty"`
	State   *string `cbor:"2,keyasint,omitempty" json:"state,omitempty"`
}

type SecretsSetStateArgs struct {
	call rpc.Call
	data secretsSetStateArgsData
}

func (v *SecretsSetStateArgs) HasBackend() bool {
	return v.data.Backend != nil
}

func (v *SecretsSetStateArgs) Backend() string {
	if v.data.Backend == nil {
		return ""
	}
	return *v.data.Backend
}

func (v *SecretsSetStateArgs) HasRef() bool {
	return v.data.Ref != nil
}

func (v *SecretsSetStateArgs) Ref() string {
	if v.data.Ref == nil {
		return ""
	}
	return *v.data.Ref
}

func (v *SecretsSetStateArgs) HasState() bool {
	return v.data.State != nil
}

func (v *SecretsSetStateArgs) State() string {
	if v.data.State == nil {
		return ""
	}
	return *v.data.State
}

func (v *SecretsSetStateArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SecretsSetStateArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SecretsSetStateArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SecretsSetStateArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type secretsSetStateResultsData struct{}

type SecretsSetStateResults struct {
	call rpc.Call
	data secretsSetStateResultsData
}

func (v *SecretsSetStateResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SecretsSetStateResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SecretsSetStateResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SecretsSetStateResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type SecretsSet struct {
	rpc.Call
	args    SecretsSetArgs
	results SecretsSetResults
}

func (t *SecretsSet) Args() *SecretsSetArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *SecretsSet) Results() *SecretsSetResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type SecretsList struct {
	rpc.Call
	args    SecretsListArgs
	results SecretsListResults
}

func (t *SecretsList) Args() *SecretsListArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *SecretsList) Results() *SecretsListResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type SecretsListVersions struct {
	rpc.Call
	args    SecretsListVersionsArgs
	results SecretsListVersionsResults
}

func (t *SecretsListVersions) Args() *SecretsListVersionsArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *SecretsListVersions) Results() *SecretsListVersionsResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type SecretsSetState struct {
	rpc.Call
	args    SecretsSetStateArgs
	results SecretsSetStateResults
}

func (t *SecretsSetState) Args() *SecretsSetStateArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *SecretsSetState) Results() *SecretsSetStateResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type Secrets interface {
	Set(ctx context.Context, state *SecretsSet) error
	List(ctx context.Context, state *SecretsList) error
	ListVersions(ctx context.Context, state *SecretsListVersions) error
	SetState(ctx context.Context, state *SecretsSetState) error
}

type reexportSecrets struct {
	client rpc.Client
}

func (reexportSecrets) Set(ctx context.Context, state *SecretsSet) error {
	panic("not implemented")
}

func (reexportSecrets) List(ctx context.Context, state *SecretsList) error {
	panic("not implemented")
}

func (reexportSecrets) ListVersions(ctx context.Context, state *SecretsListVersions) error {
	panic("not implemented")
}

func (reexportSecrets) SetState(ctx context.Context, state *SecretsSetState) error {
	panic("not implemented")
}

func (t reexportSecrets) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptSecrets(t Secrets) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "set",
			InterfaceName: "Secrets",
			Index:         0,
			Public:        false,
			Params:        []string{"backend", "path", "value"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Set(ctx, &SecretsSet{Call: call})
			},
		},
		{
			Name:          "list",
			InterfaceName: "Secrets",
			Index:         0,
			Public:        false,
			Params:        []string{"backend"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.List(ctx, &SecretsList{Call: call})
			},
		},
		{
			Name:          "listVersions",
			InterfaceName: "Secrets",
			Index:         0,
			Public:        false,
			Params:        []string{"backend", "path"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.ListVersions(ctx, &SecretsListVersions{Call: call})
			},
		},
		{
			Name:          "setState",
			InterfaceName: "Secrets",
			Index:         0,
			Public:        false,
			Params:        []string{"backend", "ref", "state"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.SetState(ctx, &SecretsSetState{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type SecretsClient struct {
	rpc.Client
}

func NewSecretsClient(client rpc.Client) *SecretsClient {
	return &SecretsClient{Client: client}
}

func (c SecretsClient) Export() Secrets {
	return reexportSecrets{client: c.Client}
}

type SecretsClientSetResults struct {
	client rpc.Client
	data   secretsSetResultsData
}

func (v *SecretsClientSetResults) HasVersion() bool {
	return v.data.Version != nil
}

func (v *SecretsClientSetResults) Version() string {
	if v.data.Version == nil {
		return ""
	}
	return *v.data.Version
}

func (v *SecretsClientSetResults) HasUnchanged() bool {
	return v.data.Unchanged != nil
}

func (v *SecretsClientSetResults) Unchanged() bool {
	if v.data.Unchanged == nil {
		return false
	}
	return *v.data.Unchanged
}

func (v SecretsClient) Set(ctx context.Context, backend string, path string, value []byte) (*SecretsClientSetResults, error) {
	args := SecretsSetArgs{}
	args.data.Backend = &backend
	args.data.Path = &path
	args.data.Value = &value

	var ret secretsSetResultsData

	err := v.Call(ctx, "set", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &SecretsClientSetResults{client: v.Client, data: ret}, nil
}

type SecretsClientListResults struct {
	client rpc.Client
	data   secretsListResultsData
}

func (v *SecretsClientListResults) HasSecrets() bool {
	return v.data.Secrets != nil
}

func (v *SecretsClientListResults) Secrets() []*SecretInfo {
	if v.data.Secrets == nil {
		return nil
	}
	return *v.data.Secrets
}

func (v SecretsClient) List(ctx context.Context, backend string) (*SecretsClientListResults, error) {
	args := SecretsListArgs{}
	args.data.Backend = &backend

	var ret secretsListResultsData

	err := v.Call(ctx, "list", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &SecretsClientListResults{client: v.Client, data: ret}, nil
}

type SecretsClientListVersionsResults struct {
	client rpc.Client
	data   secretsListVersionsResultsData
}

func (v *SecretsClientListVersionsResults) HasSecret() bool {
	return v.data.Secret != nil
}

func (v *SecretsClientListVersionsResults) Secret() *SecretInfo {
	return v.data.Secret
}

func (v SecretsClient) ListVersions(ctx context.Context, backend string, path string) (*SecretsClientListVersionsResults, error) {
	args := SecretsListVersionsArgs{}
	args.data.Backend = &backend
	args.data.Path = &path

	var ret secretsListVersionsResultsData

	err := v.Call(ctx, "listVersions", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &SecretsClientListVersionsResults{client: v.Client, data: ret}, nil
}

type SecretsClientSetStateResults struct {
	client rpc.Client
	data   secretsSetStateResultsData
}

func (v SecretsClient) SetState(ctx context.Context, backend string, ref string, state string) (*SecretsClientSetStateResults, error) {
	args := SecretsSetStateArgs{}
	args.data.Backend = &backend
	args.data.Ref = &ref
	args.data.State = &state

	var ret secretsSetStateResultsData

	err := v.Call(ctx, "setState", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &SecretsClientSetStateResults{client: v.Client, data: ret}, nil
}
