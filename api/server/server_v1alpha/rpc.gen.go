package server_v1alpha

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/fxamacker/cbor/v2"
	rpc "miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/standard"
)

type operationData struct {
	Id                  *string             `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
	Action              *string             `cbor:"1,keyasint,omitempty" json:"action,omitempty"`
	Phase               *string             `cbor:"2,keyasint,omitempty" json:"phase,omitempty"`
	RequestedBy         *string             `cbor:"3,keyasint,omitempty" json:"requested_by,omitempty"`
	TargetVersion       *string             `cbor:"4,keyasint,omitempty" json:"target_version,omitempty"`
	ResolvedVersion     *string             `cbor:"5,keyasint,omitempty" json:"resolved_version,omitempty"`
	ResolvedCommit      *string             `cbor:"6,keyasint,omitempty" json:"resolved_commit,omitempty"`
	ArtifactType        *string             `cbor:"7,keyasint,omitempty" json:"artifact_type,omitempty"`
	NoRollback          *bool               `cbor:"8,keyasint,omitempty" json:"no_rollback,omitempty"`
	ReadyTimeoutSeconds *int32              `cbor:"9,keyasint,omitempty" json:"ready_timeout_seconds,omitempty"`
	Error               *string             `cbor:"10,keyasint,omitempty" json:"error,omitempty"`
	Progress            *string             `cbor:"11,keyasint,omitempty" json:"progress,omitempty"`
	PreviousInstanceId  *string             `cbor:"12,keyasint,omitempty" json:"previous_instance_id,omitempty"`
	PreviousVersion     *string             `cbor:"13,keyasint,omitempty" json:"previous_version,omitempty"`
	PreviousCommit      *string             `cbor:"14,keyasint,omitempty" json:"previous_commit,omitempty"`
	NewInstanceId       *string             `cbor:"15,keyasint,omitempty" json:"new_instance_id,omitempty"`
	NewVersion          *string             `cbor:"16,keyasint,omitempty" json:"new_version,omitempty"`
	CreatedAt           *standard.Timestamp `cbor:"17,keyasint,omitempty" json:"created_at,omitempty"`
	UpdatedAt           *standard.Timestamp `cbor:"18,keyasint,omitempty" json:"updated_at,omitempty"`
	FinishedAt          *standard.Timestamp `cbor:"19,keyasint,omitempty" json:"finished_at,omitempty"`
	Record              *string             `cbor:"20,keyasint,omitempty" json:"record,omitempty"`
}

type Operation struct {
	data operationData
}

func (v *Operation) HasId() bool {
	return v.data.Id != nil
}

func (v *Operation) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *Operation) SetId(id string) {
	v.data.Id = &id
}

func (v *Operation) HasAction() bool {
	return v.data.Action != nil
}

func (v *Operation) Action() string {
	if v.data.Action == nil {
		return ""
	}
	return *v.data.Action
}

func (v *Operation) SetAction(action string) {
	v.data.Action = &action
}

func (v *Operation) HasPhase() bool {
	return v.data.Phase != nil
}

func (v *Operation) Phase() string {
	if v.data.Phase == nil {
		return ""
	}
	return *v.data.Phase
}

func (v *Operation) SetPhase(phase string) {
	v.data.Phase = &phase
}

func (v *Operation) HasRequestedBy() bool {
	return v.data.RequestedBy != nil
}

func (v *Operation) RequestedBy() string {
	if v.data.RequestedBy == nil {
		return ""
	}
	return *v.data.RequestedBy
}

func (v *Operation) SetRequestedBy(requested_by string) {
	v.data.RequestedBy = &requested_by
}

func (v *Operation) HasTargetVersion() bool {
	return v.data.TargetVersion != nil
}

func (v *Operation) TargetVersion() string {
	if v.data.TargetVersion == nil {
		return ""
	}
	return *v.data.TargetVersion
}

func (v *Operation) SetTargetVersion(target_version string) {
	v.data.TargetVersion = &target_version
}

func (v *Operation) HasResolvedVersion() bool {
	return v.data.ResolvedVersion != nil
}

func (v *Operation) ResolvedVersion() string {
	if v.data.ResolvedVersion == nil {
		return ""
	}
	return *v.data.ResolvedVersion
}

func (v *Operation) SetResolvedVersion(resolved_version string) {
	v.data.ResolvedVersion = &resolved_version
}

func (v *Operation) HasResolvedCommit() bool {
	return v.data.ResolvedCommit != nil
}

func (v *Operation) ResolvedCommit() string {
	if v.data.ResolvedCommit == nil {
		return ""
	}
	return *v.data.ResolvedCommit
}

func (v *Operation) SetResolvedCommit(resolved_commit string) {
	v.data.ResolvedCommit = &resolved_commit
}

func (v *Operation) HasArtifactType() bool {
	return v.data.ArtifactType != nil
}

func (v *Operation) ArtifactType() string {
	if v.data.ArtifactType == nil {
		return ""
	}
	return *v.data.ArtifactType
}

func (v *Operation) SetArtifactType(artifact_type string) {
	v.data.ArtifactType = &artifact_type
}

func (v *Operation) HasNoRollback() bool {
	return v.data.NoRollback != nil
}

func (v *Operation) NoRollback() bool {
	if v.data.NoRollback == nil {
		return false
	}
	return *v.data.NoRollback
}

func (v *Operation) SetNoRollback(no_rollback bool) {
	v.data.NoRollback = &no_rollback
}

func (v *Operation) HasReadyTimeoutSeconds() bool {
	return v.data.ReadyTimeoutSeconds != nil
}

func (v *Operation) ReadyTimeoutSeconds() int32 {
	if v.data.ReadyTimeoutSeconds == nil {
		return 0
	}
	return *v.data.ReadyTimeoutSeconds
}

func (v *Operation) SetReadyTimeoutSeconds(ready_timeout_seconds int32) {
	v.data.ReadyTimeoutSeconds = &ready_timeout_seconds
}

func (v *Operation) HasError() bool {
	return v.data.Error != nil
}

func (v *Operation) Error() string {
	if v.data.Error == nil {
		return ""
	}
	return *v.data.Error
}

func (v *Operation) SetError(error string) {
	v.data.Error = &error
}

func (v *Operation) HasProgress() bool {
	return v.data.Progress != nil
}

func (v *Operation) Progress() string {
	if v.data.Progress == nil {
		return ""
	}
	return *v.data.Progress
}

func (v *Operation) SetProgress(progress string) {
	v.data.Progress = &progress
}

func (v *Operation) HasPreviousInstanceId() bool {
	return v.data.PreviousInstanceId != nil
}

func (v *Operation) PreviousInstanceId() string {
	if v.data.PreviousInstanceId == nil {
		return ""
	}
	return *v.data.PreviousInstanceId
}

func (v *Operation) SetPreviousInstanceId(previous_instance_id string) {
	v.data.PreviousInstanceId = &previous_instance_id
}

func (v *Operation) HasPreviousVersion() bool {
	return v.data.PreviousVersion != nil
}

func (v *Operation) PreviousVersion() string {
	if v.data.PreviousVersion == nil {
		return ""
	}
	return *v.data.PreviousVersion
}

func (v *Operation) SetPreviousVersion(previous_version string) {
	v.data.PreviousVersion = &previous_version
}

func (v *Operation) HasPreviousCommit() bool {
	return v.data.PreviousCommit != nil
}

func (v *Operation) PreviousCommit() string {
	if v.data.PreviousCommit == nil {
		return ""
	}
	return *v.data.PreviousCommit
}

func (v *Operation) SetPreviousCommit(previous_commit string) {
	v.data.PreviousCommit = &previous_commit
}

func (v *Operation) HasNewInstanceId() bool {
	return v.data.NewInstanceId != nil
}

func (v *Operation) NewInstanceId() string {
	if v.data.NewInstanceId == nil {
		return ""
	}
	return *v.data.NewInstanceId
}

func (v *Operation) SetNewInstanceId(new_instance_id string) {
	v.data.NewInstanceId = &new_instance_id
}

func (v *Operation) HasNewVersion() bool {
	return v.data.NewVersion != nil
}

func (v *Operation) NewVersion() string {
	if v.data.NewVersion == nil {
		return ""
	}
	return *v.data.NewVersion
}

func (v *Operation) SetNewVersion(new_version string) {
	v.data.NewVersion = &new_version
}

func (v *Operation) HasCreatedAt() bool {
	return v.data.CreatedAt != nil
}

func (v *Operation) CreatedAt() *standard.Timestamp {
	return v.data.CreatedAt
}

func (v *Operation) SetCreatedAt(created_at *standard.Timestamp) {
	v.data.CreatedAt = created_at
}

func (v *Operation) HasUpdatedAt() bool {
	return v.data.UpdatedAt != nil
}

func (v *Operation) UpdatedAt() *standard.Timestamp {
	return v.data.UpdatedAt
}

func (v *Operation) SetUpdatedAt(updated_at *standard.Timestamp) {
	v.data.UpdatedAt = updated_at
}

func (v *Operation) HasFinishedAt() bool {
	return v.data.FinishedAt != nil
}

func (v *Operation) FinishedAt() *standard.Timestamp {
	return v.data.FinishedAt
}

func (v *Operation) SetFinishedAt(finished_at *standard.Timestamp) {
	v.data.FinishedAt = finished_at
}

func (v *Operation) HasRecord() bool {
	return v.data.Record != nil
}

func (v *Operation) Record() string {
	if v.data.Record == nil {
		return ""
	}
	return *v.data.Record
}

func (v *Operation) SetRecord(record string) {
	v.data.Record = &record
}

func (v *Operation) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *Operation) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *Operation) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *Operation) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type serverInfoVersionArgsData struct{}

type ServerInfoVersionArgs struct {
	call rpc.Call
	data serverInfoVersionArgsData
}

func (v *ServerInfoVersionArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ServerInfoVersionArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ServerInfoVersionArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ServerInfoVersionArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type serverInfoVersionResultsData struct {
	Version           *string             `cbor:"0,keyasint,omitempty" json:"version,omitempty"`
	Commit            *string             `cbor:"1,keyasint,omitempty" json:"commit,omitempty"`
	BuildDate         *standard.Timestamp `cbor:"2,keyasint,omitempty" json:"build_date,omitempty"`
	RuntimeInstanceId *string             `cbor:"3,keyasint,omitempty" json:"runtime_instance_id,omitempty"`
	StartedAt         *standard.Timestamp `cbor:"4,keyasint,omitempty" json:"started_at,omitempty"`
	Ready             *bool               `cbor:"5,keyasint,omitempty" json:"ready,omitempty"`
	InstallKind       *string             `cbor:"6,keyasint,omitempty" json:"install_kind,omitempty"`
}

type ServerInfoVersionResults struct {
	call rpc.Call
	data serverInfoVersionResultsData
}

func (v *ServerInfoVersionResults) SetVersion(version string) {
	v.data.Version = &version
}

func (v *ServerInfoVersionResults) SetCommit(commit string) {
	v.data.Commit = &commit
}

func (v *ServerInfoVersionResults) SetBuildDate(build_date *standard.Timestamp) {
	v.data.BuildDate = build_date
}

func (v *ServerInfoVersionResults) SetRuntimeInstanceId(runtime_instance_id string) {
	v.data.RuntimeInstanceId = &runtime_instance_id
}

func (v *ServerInfoVersionResults) SetStartedAt(started_at *standard.Timestamp) {
	v.data.StartedAt = started_at
}

func (v *ServerInfoVersionResults) SetReady(ready bool) {
	v.data.Ready = &ready
}

func (v *ServerInfoVersionResults) SetInstallKind(install_kind string) {
	v.data.InstallKind = &install_kind
}

func (v *ServerInfoVersionResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ServerInfoVersionResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ServerInfoVersionResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ServerInfoVersionResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type ServerInfoVersion struct {
	rpc.Call
	args    ServerInfoVersionArgs
	results ServerInfoVersionResults
}

func (t *ServerInfoVersion) Args() *ServerInfoVersionArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *ServerInfoVersion) Results() *ServerInfoVersionResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type ServerInfo interface {
	Version(ctx context.Context, state *ServerInfoVersion) error
}

type reexportServerInfo struct {
	client rpc.Client
}

func (reexportServerInfo) Version(ctx context.Context, state *ServerInfoVersion) error {
	panic("not implemented")
}

func (t reexportServerInfo) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptServerInfo(t ServerInfo) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "Version",
			InterfaceName: "ServerInfo",
			Index:         0,
			Public:        true,
			Params:        []string{},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Version(ctx, &ServerInfoVersion{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type ServerInfoClient struct {
	rpc.Client
}

func NewServerInfoClient(client rpc.Client) *ServerInfoClient {
	return &ServerInfoClient{Client: client}
}

func (c ServerInfoClient) Export() ServerInfo {
	return reexportServerInfo{client: c.Client}
}

type ServerInfoClientVersionResults struct {
	client rpc.Client
	data   serverInfoVersionResultsData
}

func (v *ServerInfoClientVersionResults) HasVersion() bool {
	return v.data.Version != nil
}

func (v *ServerInfoClientVersionResults) Version() string {
	if v.data.Version == nil {
		return ""
	}
	return *v.data.Version
}

func (v *ServerInfoClientVersionResults) HasCommit() bool {
	return v.data.Commit != nil
}

func (v *ServerInfoClientVersionResults) Commit() string {
	if v.data.Commit == nil {
		return ""
	}
	return *v.data.Commit
}

func (v *ServerInfoClientVersionResults) HasBuildDate() bool {
	return v.data.BuildDate != nil
}

func (v *ServerInfoClientVersionResults) BuildDate() *standard.Timestamp {
	return v.data.BuildDate
}

func (v *ServerInfoClientVersionResults) HasRuntimeInstanceId() bool {
	return v.data.RuntimeInstanceId != nil
}

func (v *ServerInfoClientVersionResults) RuntimeInstanceId() string {
	if v.data.RuntimeInstanceId == nil {
		return ""
	}
	return *v.data.RuntimeInstanceId
}

func (v *ServerInfoClientVersionResults) HasStartedAt() bool {
	return v.data.StartedAt != nil
}

func (v *ServerInfoClientVersionResults) StartedAt() *standard.Timestamp {
	return v.data.StartedAt
}

func (v *ServerInfoClientVersionResults) HasReady() bool {
	return v.data.Ready != nil
}

func (v *ServerInfoClientVersionResults) Ready() bool {
	if v.data.Ready == nil {
		return false
	}
	return *v.data.Ready
}

func (v *ServerInfoClientVersionResults) HasInstallKind() bool {
	return v.data.InstallKind != nil
}

func (v *ServerInfoClientVersionResults) InstallKind() string {
	if v.data.InstallKind == nil {
		return ""
	}
	return *v.data.InstallKind
}

func (v ServerInfoClient) Version(ctx context.Context) (*ServerInfoClientVersionResults, error) {
	args := ServerInfoVersionArgs{}

	var ret serverInfoVersionResultsData

	err := v.Call(ctx, "Version", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &ServerInfoClientVersionResults{client: v.Client, data: ret}, nil
}

type serverLifecycleListArgsData struct{}

type ServerLifecycleListArgs struct {
	call rpc.Call
	data serverLifecycleListArgsData
}

func (v *ServerLifecycleListArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ServerLifecycleListArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ServerLifecycleListArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ServerLifecycleListArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type serverLifecycleListResultsData struct {
	Operations *[]*Operation `cbor:"0,keyasint,omitempty" json:"operations,omitempty"`
}

type ServerLifecycleListResults struct {
	call rpc.Call
	data serverLifecycleListResultsData
}

func (v *ServerLifecycleListResults) SetOperations(operations []*Operation) {
	x := slices.Clone(operations)
	v.data.Operations = &x
}

func (v *ServerLifecycleListResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ServerLifecycleListResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ServerLifecycleListResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ServerLifecycleListResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type serverLifecycleGetArgsData struct {
	Id *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
}

type ServerLifecycleGetArgs struct {
	call rpc.Call
	data serverLifecycleGetArgsData
}

func (v *ServerLifecycleGetArgs) HasId() bool {
	return v.data.Id != nil
}

func (v *ServerLifecycleGetArgs) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *ServerLifecycleGetArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ServerLifecycleGetArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ServerLifecycleGetArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ServerLifecycleGetArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type serverLifecycleGetResultsData struct {
	Operation *Operation `cbor:"0,keyasint,omitempty" json:"operation,omitempty"`
}

type ServerLifecycleGetResults struct {
	call rpc.Call
	data serverLifecycleGetResultsData
}

func (v *ServerLifecycleGetResults) SetOperation(operation *Operation) {
	v.data.Operation = operation
}

func (v *ServerLifecycleGetResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ServerLifecycleGetResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ServerLifecycleGetResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ServerLifecycleGetResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type serverLifecycleStartArgsData struct {
	Id                  *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
	Action              *string `cbor:"1,keyasint,omitempty" json:"action,omitempty"`
	TargetVersion       *string `cbor:"2,keyasint,omitempty" json:"target_version,omitempty"`
	ArtifactType        *string `cbor:"3,keyasint,omitempty" json:"artifact_type,omitempty"`
	NoRollback          *bool   `cbor:"4,keyasint,omitempty" json:"no_rollback,omitempty"`
	ReadyTimeoutSeconds *int32  `cbor:"5,keyasint,omitempty" json:"ready_timeout_seconds,omitempty"`
}

type ServerLifecycleStartArgs struct {
	call rpc.Call
	data serverLifecycleStartArgsData
}

func (v *ServerLifecycleStartArgs) HasId() bool {
	return v.data.Id != nil
}

func (v *ServerLifecycleStartArgs) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *ServerLifecycleStartArgs) HasAction() bool {
	return v.data.Action != nil
}

func (v *ServerLifecycleStartArgs) Action() string {
	if v.data.Action == nil {
		return ""
	}
	return *v.data.Action
}

func (v *ServerLifecycleStartArgs) HasTargetVersion() bool {
	return v.data.TargetVersion != nil
}

func (v *ServerLifecycleStartArgs) TargetVersion() string {
	if v.data.TargetVersion == nil {
		return ""
	}
	return *v.data.TargetVersion
}

func (v *ServerLifecycleStartArgs) HasArtifactType() bool {
	return v.data.ArtifactType != nil
}

func (v *ServerLifecycleStartArgs) ArtifactType() string {
	if v.data.ArtifactType == nil {
		return ""
	}
	return *v.data.ArtifactType
}

func (v *ServerLifecycleStartArgs) HasNoRollback() bool {
	return v.data.NoRollback != nil
}

func (v *ServerLifecycleStartArgs) NoRollback() bool {
	if v.data.NoRollback == nil {
		return false
	}
	return *v.data.NoRollback
}

func (v *ServerLifecycleStartArgs) HasReadyTimeoutSeconds() bool {
	return v.data.ReadyTimeoutSeconds != nil
}

func (v *ServerLifecycleStartArgs) ReadyTimeoutSeconds() int32 {
	if v.data.ReadyTimeoutSeconds == nil {
		return 0
	}
	return *v.data.ReadyTimeoutSeconds
}

func (v *ServerLifecycleStartArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ServerLifecycleStartArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ServerLifecycleStartArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ServerLifecycleStartArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type serverLifecycleStartResultsData struct {
	Operation *Operation `cbor:"0,keyasint,omitempty" json:"operation,omitempty"`
}

type ServerLifecycleStartResults struct {
	call rpc.Call
	data serverLifecycleStartResultsData
}

func (v *ServerLifecycleStartResults) SetOperation(operation *Operation) {
	v.data.Operation = operation
}

func (v *ServerLifecycleStartResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ServerLifecycleStartResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ServerLifecycleStartResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ServerLifecycleStartResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type ServerLifecycleList struct {
	rpc.Call
	args    ServerLifecycleListArgs
	results ServerLifecycleListResults
}

func (t *ServerLifecycleList) Args() *ServerLifecycleListArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *ServerLifecycleList) Results() *ServerLifecycleListResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type ServerLifecycleGet struct {
	rpc.Call
	args    ServerLifecycleGetArgs
	results ServerLifecycleGetResults
}

func (t *ServerLifecycleGet) Args() *ServerLifecycleGetArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *ServerLifecycleGet) Results() *ServerLifecycleGetResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type ServerLifecycleStart struct {
	rpc.Call
	args    ServerLifecycleStartArgs
	results ServerLifecycleStartResults
}

func (t *ServerLifecycleStart) Args() *ServerLifecycleStartArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *ServerLifecycleStart) Results() *ServerLifecycleStartResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type ServerLifecycle interface {
	List(ctx context.Context, state *ServerLifecycleList) error
	Get(ctx context.Context, state *ServerLifecycleGet) error
	Start(ctx context.Context, state *ServerLifecycleStart) error
}

type reexportServerLifecycle struct {
	client rpc.Client
}

func (reexportServerLifecycle) List(ctx context.Context, state *ServerLifecycleList) error {
	panic("not implemented")
}

func (reexportServerLifecycle) Get(ctx context.Context, state *ServerLifecycleGet) error {
	panic("not implemented")
}

func (reexportServerLifecycle) Start(ctx context.Context, state *ServerLifecycleStart) error {
	panic("not implemented")
}

func (t reexportServerLifecycle) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptServerLifecycle(t ServerLifecycle) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "List",
			InterfaceName: "ServerLifecycle",
			Index:         0,
			Public:        false,
			Params:        []string{},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.List(ctx, &ServerLifecycleList{Call: call})
			},
		},
		{
			Name:          "Get",
			InterfaceName: "ServerLifecycle",
			Index:         1,
			Public:        false,
			Params:        []string{"id"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Get(ctx, &ServerLifecycleGet{Call: call})
			},
		},
		{
			Name:          "Start",
			InterfaceName: "ServerLifecycle",
			Index:         2,
			Public:        false,
			Params:        []string{"id", "action", "target_version", "artifact_type", "no_rollback", "ready_timeout_seconds"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Start(ctx, &ServerLifecycleStart{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type ServerLifecycleClient struct {
	rpc.Client
}

func NewServerLifecycleClient(client rpc.Client) *ServerLifecycleClient {
	return &ServerLifecycleClient{Client: client}
}

func (c ServerLifecycleClient) Export() ServerLifecycle {
	return reexportServerLifecycle{client: c.Client}
}

type ServerLifecycleClientListResults struct {
	client rpc.Client
	data   serverLifecycleListResultsData
}

func (v *ServerLifecycleClientListResults) HasOperations() bool {
	return v.data.Operations != nil
}

func (v *ServerLifecycleClientListResults) Operations() []*Operation {
	if v.data.Operations == nil {
		return nil
	}
	return *v.data.Operations
}

func (v ServerLifecycleClient) List(ctx context.Context) (*ServerLifecycleClientListResults, error) {
	args := ServerLifecycleListArgs{}

	var ret serverLifecycleListResultsData

	err := v.Call(ctx, "List", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &ServerLifecycleClientListResults{client: v.Client, data: ret}, nil
}

type ServerLifecycleClientGetResults struct {
	client rpc.Client
	data   serverLifecycleGetResultsData
}

func (v *ServerLifecycleClientGetResults) HasOperation() bool {
	return v.data.Operation != nil
}

func (v *ServerLifecycleClientGetResults) Operation() *Operation {
	return v.data.Operation
}

func (v ServerLifecycleClient) Get(ctx context.Context, id string) (*ServerLifecycleClientGetResults, error) {
	args := ServerLifecycleGetArgs{}
	args.data.Id = &id

	var ret serverLifecycleGetResultsData

	err := v.Call(ctx, "Get", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &ServerLifecycleClientGetResults{client: v.Client, data: ret}, nil
}

type ServerLifecycleClientStartResults struct {
	client rpc.Client
	data   serverLifecycleStartResultsData
}

func (v *ServerLifecycleClientStartResults) HasOperation() bool {
	return v.data.Operation != nil
}

func (v *ServerLifecycleClientStartResults) Operation() *Operation {
	return v.data.Operation
}

func (v ServerLifecycleClient) Start(ctx context.Context, id string, action string, target_version string, artifact_type string, no_rollback bool, ready_timeout_seconds int32) (*ServerLifecycleClientStartResults, error) {
	args := ServerLifecycleStartArgs{}
	args.data.Id = &id
	args.data.Action = &action
	args.data.TargetVersion = &target_version
	args.data.ArtifactType = &artifact_type
	args.data.NoRollback = &no_rollback
	args.data.ReadyTimeoutSeconds = &ready_timeout_seconds

	var ret serverLifecycleStartResultsData

	err := v.Call(ctx, "Start", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &ServerLifecycleClientStartResults{client: v.Client, data: ret}, nil
}
