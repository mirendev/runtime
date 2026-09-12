package server_v1alpha

import (
	"context"
	"encoding/json"

	"github.com/fxamacker/cbor/v2"
	rpc "miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/standard"
)

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
