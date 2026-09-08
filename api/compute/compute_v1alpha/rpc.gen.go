package compute_v1alpha

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/fxamacker/cbor/v2"
	rpc "miren.dev/runtime/pkg/rpc"
)

type sandboxInfoData struct {
	Id             *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
	ShortId        *string `cbor:"1,keyasint,omitempty" json:"short_id,omitempty"`
	App            *string `cbor:"2,keyasint,omitempty" json:"app,omitempty"`
	Version        *string `cbor:"3,keyasint,omitempty" json:"version,omitempty"`
	VersionShortId *string `cbor:"4,keyasint,omitempty" json:"version_short_id,omitempty"`
	Service        *string `cbor:"5,keyasint,omitempty" json:"service,omitempty"`
	Pool           *string `cbor:"6,keyasint,omitempty" json:"pool,omitempty"`
	PoolShortId    *string `cbor:"7,keyasint,omitempty" json:"pool_short_id,omitempty"`
	Address        *string `cbor:"8,keyasint,omitempty" json:"address,omitempty"`
	Runner         *string `cbor:"9,keyasint,omitempty" json:"runner,omitempty"`
	Status         *string `cbor:"10,keyasint,omitempty" json:"status,omitempty"`
	CreatedAt      *int64  `cbor:"11,keyasint,omitempty" json:"created_at,omitempty"`
	UpdatedAt      *int64  `cbor:"12,keyasint,omitempty" json:"updated_at,omitempty"`
}

type SandboxInfo struct {
	data sandboxInfoData
}

func (v *SandboxInfo) HasId() bool {
	return v.data.Id != nil
}

func (v *SandboxInfo) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *SandboxInfo) SetId(id string) {
	v.data.Id = &id
}

func (v *SandboxInfo) HasShortId() bool {
	return v.data.ShortId != nil
}

func (v *SandboxInfo) ShortId() string {
	if v.data.ShortId == nil {
		return ""
	}
	return *v.data.ShortId
}

func (v *SandboxInfo) SetShortId(short_id string) {
	v.data.ShortId = &short_id
}

func (v *SandboxInfo) HasApp() bool {
	return v.data.App != nil
}

func (v *SandboxInfo) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *SandboxInfo) SetApp(app string) {
	v.data.App = &app
}

func (v *SandboxInfo) HasVersion() bool {
	return v.data.Version != nil
}

func (v *SandboxInfo) Version() string {
	if v.data.Version == nil {
		return ""
	}
	return *v.data.Version
}

func (v *SandboxInfo) SetVersion(version string) {
	v.data.Version = &version
}

func (v *SandboxInfo) HasVersionShortId() bool {
	return v.data.VersionShortId != nil
}

func (v *SandboxInfo) VersionShortId() string {
	if v.data.VersionShortId == nil {
		return ""
	}
	return *v.data.VersionShortId
}

func (v *SandboxInfo) SetVersionShortId(version_short_id string) {
	v.data.VersionShortId = &version_short_id
}

func (v *SandboxInfo) HasService() bool {
	return v.data.Service != nil
}

func (v *SandboxInfo) Service() string {
	if v.data.Service == nil {
		return ""
	}
	return *v.data.Service
}

func (v *SandboxInfo) SetService(service string) {
	v.data.Service = &service
}

func (v *SandboxInfo) HasPool() bool {
	return v.data.Pool != nil
}

func (v *SandboxInfo) Pool() string {
	if v.data.Pool == nil {
		return ""
	}
	return *v.data.Pool
}

func (v *SandboxInfo) SetPool(pool string) {
	v.data.Pool = &pool
}

func (v *SandboxInfo) HasPoolShortId() bool {
	return v.data.PoolShortId != nil
}

func (v *SandboxInfo) PoolShortId() string {
	if v.data.PoolShortId == nil {
		return ""
	}
	return *v.data.PoolShortId
}

func (v *SandboxInfo) SetPoolShortId(pool_short_id string) {
	v.data.PoolShortId = &pool_short_id
}

func (v *SandboxInfo) HasAddress() bool {
	return v.data.Address != nil
}

func (v *SandboxInfo) Address() string {
	if v.data.Address == nil {
		return ""
	}
	return *v.data.Address
}

func (v *SandboxInfo) SetAddress(address string) {
	v.data.Address = &address
}

func (v *SandboxInfo) HasRunner() bool {
	return v.data.Runner != nil
}

func (v *SandboxInfo) Runner() string {
	if v.data.Runner == nil {
		return ""
	}
	return *v.data.Runner
}

func (v *SandboxInfo) SetRunner(runner string) {
	v.data.Runner = &runner
}

func (v *SandboxInfo) HasStatus() bool {
	return v.data.Status != nil
}

func (v *SandboxInfo) Status() string {
	if v.data.Status == nil {
		return ""
	}
	return *v.data.Status
}

func (v *SandboxInfo) SetStatus(status string) {
	v.data.Status = &status
}

func (v *SandboxInfo) HasCreatedAt() bool {
	return v.data.CreatedAt != nil
}

func (v *SandboxInfo) CreatedAt() int64 {
	if v.data.CreatedAt == nil {
		return 0
	}
	return *v.data.CreatedAt
}

func (v *SandboxInfo) SetCreatedAt(created_at int64) {
	v.data.CreatedAt = &created_at
}

func (v *SandboxInfo) HasUpdatedAt() bool {
	return v.data.UpdatedAt != nil
}

func (v *SandboxInfo) UpdatedAt() int64 {
	if v.data.UpdatedAt == nil {
		return 0
	}
	return *v.data.UpdatedAt
}

func (v *SandboxInfo) SetUpdatedAt(updated_at int64) {
	v.data.UpdatedAt = &updated_at
}

func (v *SandboxInfo) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SandboxInfo) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SandboxInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SandboxInfo) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type sandboxesListArgsData struct{}

type SandboxesListArgs struct {
	call rpc.Call
	data sandboxesListArgsData
}

func (v *SandboxesListArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SandboxesListArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SandboxesListArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SandboxesListArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type sandboxesListResultsData struct {
	Sandboxes *[]*SandboxInfo `cbor:"0,keyasint,omitempty" json:"sandboxes,omitempty"`
}

type SandboxesListResults struct {
	call rpc.Call
	data sandboxesListResultsData
}

func (v *SandboxesListResults) SetSandboxes(sandboxes []*SandboxInfo) {
	x := slices.Clone(sandboxes)
	v.data.Sandboxes = &x
}

func (v *SandboxesListResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SandboxesListResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SandboxesListResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SandboxesListResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type SandboxesList struct {
	rpc.Call
	args    SandboxesListArgs
	results SandboxesListResults
}

func (t *SandboxesList) Args() *SandboxesListArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *SandboxesList) Results() *SandboxesListResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type Sandboxes interface {
	List(ctx context.Context, state *SandboxesList) error
}

type reexportSandboxes struct {
	client rpc.Client
}

func (reexportSandboxes) List(ctx context.Context, state *SandboxesList) error {
	panic("not implemented")
}

func (t reexportSandboxes) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptSandboxes(t Sandboxes) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "list",
			InterfaceName: "Sandboxes",
			Index:         0,
			Public:        false,
			Params:        []string{},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.List(ctx, &SandboxesList{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type SandboxesClient struct {
	rpc.Client
}

func NewSandboxesClient(client rpc.Client) *SandboxesClient {
	return &SandboxesClient{Client: client}
}

func (c SandboxesClient) Export() Sandboxes {
	return reexportSandboxes{client: c.Client}
}

type SandboxesClientListResults struct {
	client rpc.Client
	data   sandboxesListResultsData
}

func (v *SandboxesClientListResults) HasSandboxes() bool {
	return v.data.Sandboxes != nil
}

func (v *SandboxesClientListResults) Sandboxes() []*SandboxInfo {
	if v.data.Sandboxes == nil {
		return nil
	}
	return *v.data.Sandboxes
}

func (v SandboxesClient) List(ctx context.Context) (*SandboxesClientListResults, error) {
	args := SandboxesListArgs{}

	var ret sandboxesListResultsData

	err := v.Call(ctx, "list", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &SandboxesClientListResults{client: v.Client, data: ret}, nil
}
