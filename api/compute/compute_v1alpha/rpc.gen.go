package compute_v1alpha

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/fxamacker/cbor/v2"
	rpc "miren.dev/runtime/pkg/rpc"
)

type sandboxPoolInfoData struct {
	Id               *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
	ShortId          *string `cbor:"1,keyasint,omitempty" json:"short_id,omitempty"`
	App              *string `cbor:"2,keyasint,omitempty" json:"app,omitempty"`
	Version          *string `cbor:"3,keyasint,omitempty" json:"version,omitempty"`
	VersionShortId   *string `cbor:"4,keyasint,omitempty" json:"version_short_id,omitempty"`
	Service          *string `cbor:"5,keyasint,omitempty" json:"service,omitempty"`
	ScalingMode      *string `cbor:"6,keyasint,omitempty" json:"scaling_mode,omitempty"`
	DesiredInstances *int64  `cbor:"7,keyasint,omitempty" json:"desired_instances,omitempty"`
	CurrentInstances *int64  `cbor:"8,keyasint,omitempty" json:"current_instances,omitempty"`
	ReadyInstances   *int64  `cbor:"9,keyasint,omitempty" json:"ready_instances,omitempty"`
	CreatedAt        *int64  `cbor:"10,keyasint,omitempty" json:"created_at,omitempty"`
	UpdatedAt        *int64  `cbor:"11,keyasint,omitempty" json:"updated_at,omitempty"`
	Revision         *int64  `cbor:"12,keyasint,omitempty" json:"revision,omitempty"`
}

type SandboxPoolInfo struct {
	data sandboxPoolInfoData
}

func (v *SandboxPoolInfo) HasId() bool {
	return v.data.Id != nil
}

func (v *SandboxPoolInfo) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *SandboxPoolInfo) SetId(id string) {
	v.data.Id = &id
}

func (v *SandboxPoolInfo) HasShortId() bool {
	return v.data.ShortId != nil
}

func (v *SandboxPoolInfo) ShortId() string {
	if v.data.ShortId == nil {
		return ""
	}
	return *v.data.ShortId
}

func (v *SandboxPoolInfo) SetShortId(short_id string) {
	v.data.ShortId = &short_id
}

func (v *SandboxPoolInfo) HasApp() bool {
	return v.data.App != nil
}

func (v *SandboxPoolInfo) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *SandboxPoolInfo) SetApp(app string) {
	v.data.App = &app
}

func (v *SandboxPoolInfo) HasVersion() bool {
	return v.data.Version != nil
}

func (v *SandboxPoolInfo) Version() string {
	if v.data.Version == nil {
		return ""
	}
	return *v.data.Version
}

func (v *SandboxPoolInfo) SetVersion(version string) {
	v.data.Version = &version
}

func (v *SandboxPoolInfo) HasVersionShortId() bool {
	return v.data.VersionShortId != nil
}

func (v *SandboxPoolInfo) VersionShortId() string {
	if v.data.VersionShortId == nil {
		return ""
	}
	return *v.data.VersionShortId
}

func (v *SandboxPoolInfo) SetVersionShortId(version_short_id string) {
	v.data.VersionShortId = &version_short_id
}

func (v *SandboxPoolInfo) HasService() bool {
	return v.data.Service != nil
}

func (v *SandboxPoolInfo) Service() string {
	if v.data.Service == nil {
		return ""
	}
	return *v.data.Service
}

func (v *SandboxPoolInfo) SetService(service string) {
	v.data.Service = &service
}

func (v *SandboxPoolInfo) HasScalingMode() bool {
	return v.data.ScalingMode != nil
}

func (v *SandboxPoolInfo) ScalingMode() string {
	if v.data.ScalingMode == nil {
		return ""
	}
	return *v.data.ScalingMode
}

func (v *SandboxPoolInfo) SetScalingMode(scaling_mode string) {
	v.data.ScalingMode = &scaling_mode
}

func (v *SandboxPoolInfo) HasDesiredInstances() bool {
	return v.data.DesiredInstances != nil
}

func (v *SandboxPoolInfo) DesiredInstances() int64 {
	if v.data.DesiredInstances == nil {
		return 0
	}
	return *v.data.DesiredInstances
}

func (v *SandboxPoolInfo) SetDesiredInstances(desired_instances int64) {
	v.data.DesiredInstances = &desired_instances
}

func (v *SandboxPoolInfo) HasCurrentInstances() bool {
	return v.data.CurrentInstances != nil
}

func (v *SandboxPoolInfo) CurrentInstances() int64 {
	if v.data.CurrentInstances == nil {
		return 0
	}
	return *v.data.CurrentInstances
}

func (v *SandboxPoolInfo) SetCurrentInstances(current_instances int64) {
	v.data.CurrentInstances = &current_instances
}

func (v *SandboxPoolInfo) HasReadyInstances() bool {
	return v.data.ReadyInstances != nil
}

func (v *SandboxPoolInfo) ReadyInstances() int64 {
	if v.data.ReadyInstances == nil {
		return 0
	}
	return *v.data.ReadyInstances
}

func (v *SandboxPoolInfo) SetReadyInstances(ready_instances int64) {
	v.data.ReadyInstances = &ready_instances
}

func (v *SandboxPoolInfo) HasCreatedAt() bool {
	return v.data.CreatedAt != nil
}

func (v *SandboxPoolInfo) CreatedAt() int64 {
	if v.data.CreatedAt == nil {
		return 0
	}
	return *v.data.CreatedAt
}

func (v *SandboxPoolInfo) SetCreatedAt(created_at int64) {
	v.data.CreatedAt = &created_at
}

func (v *SandboxPoolInfo) HasUpdatedAt() bool {
	return v.data.UpdatedAt != nil
}

func (v *SandboxPoolInfo) UpdatedAt() int64 {
	if v.data.UpdatedAt == nil {
		return 0
	}
	return *v.data.UpdatedAt
}

func (v *SandboxPoolInfo) SetUpdatedAt(updated_at int64) {
	v.data.UpdatedAt = &updated_at
}

func (v *SandboxPoolInfo) HasRevision() bool {
	return v.data.Revision != nil
}

func (v *SandboxPoolInfo) Revision() int64 {
	if v.data.Revision == nil {
		return 0
	}
	return *v.data.Revision
}

func (v *SandboxPoolInfo) SetRevision(revision int64) {
	v.data.Revision = &revision
}

func (v *SandboxPoolInfo) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SandboxPoolInfo) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SandboxPoolInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SandboxPoolInfo) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

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

type sandboxSelectionData struct {
	Id             *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
	ShortId        *string `cbor:"1,keyasint,omitempty" json:"short_id,omitempty"`
	Service        *string `cbor:"2,keyasint,omitempty" json:"service,omitempty"`
	Version        *string `cbor:"3,keyasint,omitempty" json:"version,omitempty"`
	CandidateCount *int32  `cbor:"4,keyasint,omitempty" json:"candidate_count,omitempty"`
}

type SandboxSelection struct {
	data sandboxSelectionData
}

func (v *SandboxSelection) HasId() bool {
	return v.data.Id != nil
}

func (v *SandboxSelection) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *SandboxSelection) SetId(id string) {
	v.data.Id = &id
}

func (v *SandboxSelection) HasShortId() bool {
	return v.data.ShortId != nil
}

func (v *SandboxSelection) ShortId() string {
	if v.data.ShortId == nil {
		return ""
	}
	return *v.data.ShortId
}

func (v *SandboxSelection) SetShortId(short_id string) {
	v.data.ShortId = &short_id
}

func (v *SandboxSelection) HasService() bool {
	return v.data.Service != nil
}

func (v *SandboxSelection) Service() string {
	if v.data.Service == nil {
		return ""
	}
	return *v.data.Service
}

func (v *SandboxSelection) SetService(service string) {
	v.data.Service = &service
}

func (v *SandboxSelection) HasVersion() bool {
	return v.data.Version != nil
}

func (v *SandboxSelection) Version() string {
	if v.data.Version == nil {
		return ""
	}
	return *v.data.Version
}

func (v *SandboxSelection) SetVersion(version string) {
	v.data.Version = &version
}

func (v *SandboxSelection) HasCandidateCount() bool {
	return v.data.CandidateCount != nil
}

func (v *SandboxSelection) CandidateCount() int32 {
	if v.data.CandidateCount == nil {
		return 0
	}
	return *v.data.CandidateCount
}

func (v *SandboxSelection) SetCandidateCount(candidate_count int32) {
	v.data.CandidateCount = &candidate_count
}

func (v *SandboxSelection) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SandboxSelection) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SandboxSelection) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SandboxSelection) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type sandboxPoolsListArgsData struct {
	App *string `cbor:"0,keyasint,omitempty" json:"app,omitempty"`
}

type SandboxPoolsListArgs struct {
	call rpc.Call
	data sandboxPoolsListArgsData
}

func (v *SandboxPoolsListArgs) HasApp() bool {
	return v.data.App != nil
}

func (v *SandboxPoolsListArgs) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *SandboxPoolsListArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SandboxPoolsListArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SandboxPoolsListArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SandboxPoolsListArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type sandboxPoolsListResultsData struct {
	Pools *[]*SandboxPoolInfo `cbor:"0,keyasint,omitempty" json:"pools,omitempty"`
}

type SandboxPoolsListResults struct {
	call rpc.Call
	data sandboxPoolsListResultsData
}

func (v *SandboxPoolsListResults) SetPools(pools []*SandboxPoolInfo) {
	x := slices.Clone(pools)
	v.data.Pools = &x
}

func (v *SandboxPoolsListResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SandboxPoolsListResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SandboxPoolsListResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SandboxPoolsListResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type sandboxPoolsSetDesiredArgsData struct {
	Pool     *string `cbor:"0,keyasint,omitempty" json:"pool,omitempty"`
	Count    *int64  `cbor:"1,keyasint,omitempty" json:"count,omitempty"`
	Relative *bool   `cbor:"2,keyasint,omitempty" json:"relative,omitempty"`
	Revision *int64  `cbor:"3,keyasint,omitempty" json:"revision,omitempty"`
	RawId    *bool   `cbor:"4,keyasint,omitempty" json:"raw_id,omitempty"`
}

type SandboxPoolsSetDesiredArgs struct {
	call rpc.Call
	data sandboxPoolsSetDesiredArgsData
}

func (v *SandboxPoolsSetDesiredArgs) HasPool() bool {
	return v.data.Pool != nil
}

func (v *SandboxPoolsSetDesiredArgs) Pool() string {
	if v.data.Pool == nil {
		return ""
	}
	return *v.data.Pool
}

func (v *SandboxPoolsSetDesiredArgs) HasCount() bool {
	return v.data.Count != nil
}

func (v *SandboxPoolsSetDesiredArgs) Count() int64 {
	if v.data.Count == nil {
		return 0
	}
	return *v.data.Count
}

func (v *SandboxPoolsSetDesiredArgs) HasRelative() bool {
	return v.data.Relative != nil
}

func (v *SandboxPoolsSetDesiredArgs) Relative() bool {
	if v.data.Relative == nil {
		return false
	}
	return *v.data.Relative
}

func (v *SandboxPoolsSetDesiredArgs) HasRevision() bool {
	return v.data.Revision != nil
}

func (v *SandboxPoolsSetDesiredArgs) Revision() int64 {
	if v.data.Revision == nil {
		return 0
	}
	return *v.data.Revision
}

func (v *SandboxPoolsSetDesiredArgs) HasRawId() bool {
	return v.data.RawId != nil
}

func (v *SandboxPoolsSetDesiredArgs) RawId() bool {
	if v.data.RawId == nil {
		return false
	}
	return *v.data.RawId
}

func (v *SandboxPoolsSetDesiredArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SandboxPoolsSetDesiredArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SandboxPoolsSetDesiredArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SandboxPoolsSetDesiredArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type sandboxPoolsSetDesiredResultsData struct {
	PreviousDesired  *int64  `cbor:"0,keyasint,omitempty" json:"previous_desired,omitempty"`
	Desired          *int64  `cbor:"1,keyasint,omitempty" json:"desired,omitempty"`
	Changed          *bool   `cbor:"2,keyasint,omitempty" json:"changed,omitempty"`
	Revision         *int64  `cbor:"3,keyasint,omitempty" json:"revision,omitempty"`
	PoolId           *string `cbor:"4,keyasint,omitempty" json:"pool_id,omitempty"`
	PoolShortId      *string `cbor:"5,keyasint,omitempty" json:"pool_short_id,omitempty"`
	Service          *string `cbor:"6,keyasint,omitempty" json:"service,omitempty"`
	CurrentInstances *int64  `cbor:"7,keyasint,omitempty" json:"current_instances,omitempty"`
	ReadyInstances   *int64  `cbor:"8,keyasint,omitempty" json:"ready_instances,omitempty"`
}

type SandboxPoolsSetDesiredResults struct {
	call rpc.Call
	data sandboxPoolsSetDesiredResultsData
}

func (v *SandboxPoolsSetDesiredResults) SetPreviousDesired(previous_desired int64) {
	v.data.PreviousDesired = &previous_desired
}

func (v *SandboxPoolsSetDesiredResults) SetDesired(desired int64) {
	v.data.Desired = &desired
}

func (v *SandboxPoolsSetDesiredResults) SetChanged(changed bool) {
	v.data.Changed = &changed
}

func (v *SandboxPoolsSetDesiredResults) SetRevision(revision int64) {
	v.data.Revision = &revision
}

func (v *SandboxPoolsSetDesiredResults) SetPoolId(pool_id string) {
	v.data.PoolId = &pool_id
}

func (v *SandboxPoolsSetDesiredResults) SetPoolShortId(pool_short_id string) {
	v.data.PoolShortId = &pool_short_id
}

func (v *SandboxPoolsSetDesiredResults) SetService(service string) {
	v.data.Service = &service
}

func (v *SandboxPoolsSetDesiredResults) SetCurrentInstances(current_instances int64) {
	v.data.CurrentInstances = &current_instances
}

func (v *SandboxPoolsSetDesiredResults) SetReadyInstances(ready_instances int64) {
	v.data.ReadyInstances = &ready_instances
}

func (v *SandboxPoolsSetDesiredResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SandboxPoolsSetDesiredResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SandboxPoolsSetDesiredResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SandboxPoolsSetDesiredResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type SandboxPoolsList struct {
	rpc.Call
	args    SandboxPoolsListArgs
	results SandboxPoolsListResults
}

func (t *SandboxPoolsList) Args() *SandboxPoolsListArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *SandboxPoolsList) Results() *SandboxPoolsListResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type SandboxPoolsSetDesired struct {
	rpc.Call
	args    SandboxPoolsSetDesiredArgs
	results SandboxPoolsSetDesiredResults
}

func (t *SandboxPoolsSetDesired) Args() *SandboxPoolsSetDesiredArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *SandboxPoolsSetDesired) Results() *SandboxPoolsSetDesiredResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type SandboxPools interface {
	List(ctx context.Context, state *SandboxPoolsList) error
	SetDesired(ctx context.Context, state *SandboxPoolsSetDesired) error
}

type reexportSandboxPools struct {
	client rpc.Client
}

func (reexportSandboxPools) List(ctx context.Context, state *SandboxPoolsList) error {
	panic("not implemented")
}

func (reexportSandboxPools) SetDesired(ctx context.Context, state *SandboxPoolsSetDesired) error {
	panic("not implemented")
}

func (t reexportSandboxPools) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptSandboxPools(t SandboxPools) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "list",
			InterfaceName: "SandboxPools",
			Index:         0,
			Public:        false,
			Params:        []string{"app"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.List(ctx, &SandboxPoolsList{Call: call})
			},
		},
		{
			Name:          "setDesired",
			InterfaceName: "SandboxPools",
			Index:         0,
			Public:        false,
			Params:        []string{"pool", "count", "relative", "revision", "raw_id"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.SetDesired(ctx, &SandboxPoolsSetDesired{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type SandboxPoolsClient struct {
	rpc.Client
}

func NewSandboxPoolsClient(client rpc.Client) *SandboxPoolsClient {
	return &SandboxPoolsClient{Client: client}
}

func (c SandboxPoolsClient) Export() SandboxPools {
	return reexportSandboxPools{client: c.Client}
}

type SandboxPoolsClientListResults struct {
	client rpc.Client
	data   sandboxPoolsListResultsData
}

func (v *SandboxPoolsClientListResults) HasPools() bool {
	return v.data.Pools != nil
}

func (v *SandboxPoolsClientListResults) Pools() []*SandboxPoolInfo {
	if v.data.Pools == nil {
		return nil
	}
	return *v.data.Pools
}

func (v SandboxPoolsClient) List(ctx context.Context, app string) (*SandboxPoolsClientListResults, error) {
	args := SandboxPoolsListArgs{}
	args.data.App = &app

	var ret sandboxPoolsListResultsData

	err := v.Call(ctx, "list", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &SandboxPoolsClientListResults{client: v.Client, data: ret}, nil
}

type SandboxPoolsClientSetDesiredResults struct {
	client rpc.Client
	data   sandboxPoolsSetDesiredResultsData
}

func (v *SandboxPoolsClientSetDesiredResults) HasPreviousDesired() bool {
	return v.data.PreviousDesired != nil
}

func (v *SandboxPoolsClientSetDesiredResults) PreviousDesired() int64 {
	if v.data.PreviousDesired == nil {
		return 0
	}
	return *v.data.PreviousDesired
}

func (v *SandboxPoolsClientSetDesiredResults) HasDesired() bool {
	return v.data.Desired != nil
}

func (v *SandboxPoolsClientSetDesiredResults) Desired() int64 {
	if v.data.Desired == nil {
		return 0
	}
	return *v.data.Desired
}

func (v *SandboxPoolsClientSetDesiredResults) HasChanged() bool {
	return v.data.Changed != nil
}

func (v *SandboxPoolsClientSetDesiredResults) Changed() bool {
	if v.data.Changed == nil {
		return false
	}
	return *v.data.Changed
}

func (v *SandboxPoolsClientSetDesiredResults) HasRevision() bool {
	return v.data.Revision != nil
}

func (v *SandboxPoolsClientSetDesiredResults) Revision() int64 {
	if v.data.Revision == nil {
		return 0
	}
	return *v.data.Revision
}

func (v *SandboxPoolsClientSetDesiredResults) HasPoolId() bool {
	return v.data.PoolId != nil
}

func (v *SandboxPoolsClientSetDesiredResults) PoolId() string {
	if v.data.PoolId == nil {
		return ""
	}
	return *v.data.PoolId
}

func (v *SandboxPoolsClientSetDesiredResults) HasPoolShortId() bool {
	return v.data.PoolShortId != nil
}

func (v *SandboxPoolsClientSetDesiredResults) PoolShortId() string {
	if v.data.PoolShortId == nil {
		return ""
	}
	return *v.data.PoolShortId
}

func (v *SandboxPoolsClientSetDesiredResults) HasService() bool {
	return v.data.Service != nil
}

func (v *SandboxPoolsClientSetDesiredResults) Service() string {
	if v.data.Service == nil {
		return ""
	}
	return *v.data.Service
}

func (v *SandboxPoolsClientSetDesiredResults) HasCurrentInstances() bool {
	return v.data.CurrentInstances != nil
}

func (v *SandboxPoolsClientSetDesiredResults) CurrentInstances() int64 {
	if v.data.CurrentInstances == nil {
		return 0
	}
	return *v.data.CurrentInstances
}

func (v *SandboxPoolsClientSetDesiredResults) HasReadyInstances() bool {
	return v.data.ReadyInstances != nil
}

func (v *SandboxPoolsClientSetDesiredResults) ReadyInstances() int64 {
	if v.data.ReadyInstances == nil {
		return 0
	}
	return *v.data.ReadyInstances
}

func (v SandboxPoolsClient) SetDesired(ctx context.Context, pool string, count int64, relative bool, revision int64, raw_id bool) (*SandboxPoolsClientSetDesiredResults, error) {
	args := SandboxPoolsSetDesiredArgs{}
	args.data.Pool = &pool
	args.data.Count = &count
	args.data.Relative = &relative
	args.data.Revision = &revision
	args.data.RawId = &raw_id

	var ret sandboxPoolsSetDesiredResultsData

	err := v.Call(ctx, "setDesired", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &SandboxPoolsClientSetDesiredResults{client: v.Client, data: ret}, nil
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

type sandboxesStopArgsData struct {
	Sandbox *string `cbor:"0,keyasint,omitempty" json:"sandbox,omitempty"`
}

type SandboxesStopArgs struct {
	call rpc.Call
	data sandboxesStopArgsData
}

func (v *SandboxesStopArgs) HasSandbox() bool {
	return v.data.Sandbox != nil
}

func (v *SandboxesStopArgs) Sandbox() string {
	if v.data.Sandbox == nil {
		return ""
	}
	return *v.data.Sandbox
}

func (v *SandboxesStopArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SandboxesStopArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SandboxesStopArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SandboxesStopArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type sandboxesStopResultsData struct{}

type SandboxesStopResults struct {
	call rpc.Call
	data sandboxesStopResultsData
}

func (v *SandboxesStopResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SandboxesStopResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SandboxesStopResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SandboxesStopResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type sandboxesDeleteArgsData struct {
	Sandbox *string `cbor:"0,keyasint,omitempty" json:"sandbox,omitempty"`
}

type SandboxesDeleteArgs struct {
	call rpc.Call
	data sandboxesDeleteArgsData
}

func (v *SandboxesDeleteArgs) HasSandbox() bool {
	return v.data.Sandbox != nil
}

func (v *SandboxesDeleteArgs) Sandbox() string {
	if v.data.Sandbox == nil {
		return ""
	}
	return *v.data.Sandbox
}

func (v *SandboxesDeleteArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SandboxesDeleteArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SandboxesDeleteArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SandboxesDeleteArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type sandboxesDeleteResultsData struct{}

type SandboxesDeleteResults struct {
	call rpc.Call
	data sandboxesDeleteResultsData
}

func (v *SandboxesDeleteResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SandboxesDeleteResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SandboxesDeleteResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SandboxesDeleteResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type sandboxesSelectForAppArgsData struct {
	App     *string `cbor:"0,keyasint,omitempty" json:"app,omitempty"`
	Service *string `cbor:"1,keyasint,omitempty" json:"service,omitempty"`
}

type SandboxesSelectForAppArgs struct {
	call rpc.Call
	data sandboxesSelectForAppArgsData
}

func (v *SandboxesSelectForAppArgs) HasApp() bool {
	return v.data.App != nil
}

func (v *SandboxesSelectForAppArgs) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *SandboxesSelectForAppArgs) HasService() bool {
	return v.data.Service != nil
}

func (v *SandboxesSelectForAppArgs) Service() string {
	if v.data.Service == nil {
		return ""
	}
	return *v.data.Service
}

func (v *SandboxesSelectForAppArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SandboxesSelectForAppArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SandboxesSelectForAppArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SandboxesSelectForAppArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type sandboxesSelectForAppResultsData struct {
	Selection *SandboxSelection `cbor:"0,keyasint,omitempty" json:"selection,omitempty"`
	Failure   *string           `cbor:"1,keyasint,omitempty" json:"failure,omitempty"`
	Services  *[]string         `cbor:"2,keyasint,omitempty" json:"services,omitempty"`
}

type SandboxesSelectForAppResults struct {
	call rpc.Call
	data sandboxesSelectForAppResultsData
}

func (v *SandboxesSelectForAppResults) SetSelection(selection *SandboxSelection) {
	v.data.Selection = selection
}

func (v *SandboxesSelectForAppResults) SetFailure(failure string) {
	v.data.Failure = &failure
}

func (v *SandboxesSelectForAppResults) SetServices(services []string) {
	x := slices.Clone(services)
	v.data.Services = &x
}

func (v *SandboxesSelectForAppResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SandboxesSelectForAppResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SandboxesSelectForAppResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SandboxesSelectForAppResults) UnmarshalJSON(data []byte) error {
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

type SandboxesStop struct {
	rpc.Call
	args    SandboxesStopArgs
	results SandboxesStopResults
}

func (t *SandboxesStop) Args() *SandboxesStopArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *SandboxesStop) Results() *SandboxesStopResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type SandboxesDelete struct {
	rpc.Call
	args    SandboxesDeleteArgs
	results SandboxesDeleteResults
}

func (t *SandboxesDelete) Args() *SandboxesDeleteArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *SandboxesDelete) Results() *SandboxesDeleteResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type SandboxesSelectForApp struct {
	rpc.Call
	args    SandboxesSelectForAppArgs
	results SandboxesSelectForAppResults
}

func (t *SandboxesSelectForApp) Args() *SandboxesSelectForAppArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *SandboxesSelectForApp) Results() *SandboxesSelectForAppResults {
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
	Stop(ctx context.Context, state *SandboxesStop) error
	Delete(ctx context.Context, state *SandboxesDelete) error
	SelectForApp(ctx context.Context, state *SandboxesSelectForApp) error
}

type reexportSandboxes struct {
	client rpc.Client
}

func (reexportSandboxes) List(ctx context.Context, state *SandboxesList) error {
	panic("not implemented")
}

func (reexportSandboxes) Stop(ctx context.Context, state *SandboxesStop) error {
	panic("not implemented")
}

func (reexportSandboxes) Delete(ctx context.Context, state *SandboxesDelete) error {
	panic("not implemented")
}

func (reexportSandboxes) SelectForApp(ctx context.Context, state *SandboxesSelectForApp) error {
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
		{
			Name:          "stop",
			InterfaceName: "Sandboxes",
			Index:         0,
			Public:        false,
			Params:        []string{"sandbox"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Stop(ctx, &SandboxesStop{Call: call})
			},
		},
		{
			Name:          "delete",
			InterfaceName: "Sandboxes",
			Index:         0,
			Public:        false,
			Params:        []string{"sandbox"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Delete(ctx, &SandboxesDelete{Call: call})
			},
		},
		{
			Name:          "selectForApp",
			InterfaceName: "Sandboxes",
			Index:         0,
			Public:        false,
			Params:        []string{"app", "service"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.SelectForApp(ctx, &SandboxesSelectForApp{Call: call})
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

type SandboxesClientStopResults struct {
	client rpc.Client
	data   sandboxesStopResultsData
}

func (v SandboxesClient) Stop(ctx context.Context, sandbox string) (*SandboxesClientStopResults, error) {
	args := SandboxesStopArgs{}
	args.data.Sandbox = &sandbox

	var ret sandboxesStopResultsData

	err := v.Call(ctx, "stop", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &SandboxesClientStopResults{client: v.Client, data: ret}, nil
}

type SandboxesClientDeleteResults struct {
	client rpc.Client
	data   sandboxesDeleteResultsData
}

func (v SandboxesClient) Delete(ctx context.Context, sandbox string) (*SandboxesClientDeleteResults, error) {
	args := SandboxesDeleteArgs{}
	args.data.Sandbox = &sandbox

	var ret sandboxesDeleteResultsData

	err := v.Call(ctx, "delete", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &SandboxesClientDeleteResults{client: v.Client, data: ret}, nil
}

type SandboxesClientSelectForAppResults struct {
	client rpc.Client
	data   sandboxesSelectForAppResultsData
}

func (v *SandboxesClientSelectForAppResults) HasSelection() bool {
	return v.data.Selection != nil
}

func (v *SandboxesClientSelectForAppResults) Selection() *SandboxSelection {
	return v.data.Selection
}

func (v *SandboxesClientSelectForAppResults) HasFailure() bool {
	return v.data.Failure != nil
}

func (v *SandboxesClientSelectForAppResults) Failure() string {
	if v.data.Failure == nil {
		return ""
	}
	return *v.data.Failure
}

func (v *SandboxesClientSelectForAppResults) HasServices() bool {
	return v.data.Services != nil
}

func (v *SandboxesClientSelectForAppResults) Services() []string {
	if v.data.Services == nil {
		return nil
	}
	return *v.data.Services
}

func (v SandboxesClient) SelectForApp(ctx context.Context, app string, service string) (*SandboxesClientSelectForAppResults, error) {
	args := SandboxesSelectForAppArgs{}
	args.data.App = &app
	args.data.Service = &service

	var ret sandboxesSelectForAppResultsData

	err := v.Call(ctx, "selectForApp", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &SandboxesClientSelectForAppResults{client: v.Client, data: ret}, nil
}
