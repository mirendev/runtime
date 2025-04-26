package build_v1alpha

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/fxamacker/cbor/v2"
	rpc "miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/stream"
)

type StatusUpdate interface {
	Which() string
	Message() string
	SetMessage(string)
	Buildkit() []byte
	SetBuildkit([]byte)
}

type statusUpdate struct {
	U_Message  *string `cbor:"1,keyasint,omitempty" json:"message,omitempty"`
	U_Buildkit *[]byte `cbor:"2,keyasint,omitempty" json:"buildkit,omitempty"`
}

func (v *statusUpdate) Which() string {
	if v.U_Message != nil {
		return "message"
	}
	if v.U_Buildkit != nil {
		return "buildkit"
	}
	return ""
}

func (v *statusUpdate) Message() string {
	if v.U_Message == nil {
		return ""
	}
	return *v.U_Message
}

func (v *statusUpdate) SetMessage(val string) {
	v.U_Buildkit = nil
	v.U_Message = &val
}

func (v *statusUpdate) Buildkit() []byte {
	if v.U_Buildkit == nil {
		return nil
	}
	return *v.U_Buildkit
}

func (v *statusUpdate) SetBuildkit(val []byte) {
	v.U_Message = nil
	v.U_Buildkit = &val
}

type statusData struct {
	Kind *string `cbor:"0,keyasint,omitempty" json:"kind,omitempty"`
	statusUpdate
}

type Status struct {
	data statusData
}

func (v *Status) HasKind() bool {
	return v.data.Kind != nil
}

func (v *Status) Kind() string {
	if v.data.Kind == nil {
		return ""
	}
	return *v.data.Kind
}

func (v *Status) SetKind(kind string) {
	v.data.Kind = &kind
}

func (v *Status) Update() StatusUpdate {
	return &v.data.statusUpdate
}

func (v *Status) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *Status) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *Status) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *Status) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type streamRecvArgsData struct {
	Count *int32 `cbor:"0,keyasint,omitempty" json:"count,omitempty"`
}

type StreamRecvArgs struct {
	call *rpc.Call
	data streamRecvArgsData
}

func (v *StreamRecvArgs) HasCount() bool {
	return v.data.Count != nil
}

func (v *StreamRecvArgs) Count() int32 {
	if v.data.Count == nil {
		return 0
	}
	return *v.data.Count
}

func (v *StreamRecvArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *StreamRecvArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *StreamRecvArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *StreamRecvArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type streamRecvResultsData struct {
	Data *[]byte `cbor:"0,keyasint,omitempty" json:"data,omitempty"`
}

type StreamRecvResults struct {
	call *rpc.Call
	data streamRecvResultsData
}

func (v *StreamRecvResults) SetData(data []byte) {
	x := slices.Clone(data)
	v.data.Data = &x
}

func (v *StreamRecvResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *StreamRecvResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *StreamRecvResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *StreamRecvResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type StreamRecv struct {
	*rpc.Call
	args    StreamRecvArgs
	results StreamRecvResults
}

func (t *StreamRecv) Args() *StreamRecvArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *StreamRecv) Results() *StreamRecvResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type Stream interface {
	Recv(ctx context.Context, state *StreamRecv) error
}

type reexportStream struct {
	client *rpc.Client
}

func (_ reexportStream) Recv(ctx context.Context, state *StreamRecv) error {
	panic("not implemented")
}

func (t reexportStream) CapabilityClient() *rpc.Client {
	return t.client
}

func AdaptStream(t Stream) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "recv",
			InterfaceName: "Stream",
			Index:         0,
			Handler: func(ctx context.Context, call *rpc.Call) error {
				return t.Recv(ctx, &StreamRecv{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type StreamClient struct {
	*rpc.Client
}

func (c StreamClient) Export() Stream {
	return reexportStream{client: c.Client}
}

type StreamClientRecvResults struct {
	client *rpc.Client
	data   streamRecvResultsData
}

func (v *StreamClientRecvResults) HasData() bool {
	return v.data.Data != nil
}

func (v *StreamClientRecvResults) Data() []byte {
	if v.data.Data == nil {
		return nil
	}
	return *v.data.Data
}

func (v StreamClient) Recv(ctx context.Context, count int32) (*StreamClientRecvResults, error) {
	args := StreamRecvArgs{}
	args.data.Count = &count

	var ret streamRecvResultsData

	err := v.Client.Call(ctx, "recv", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &StreamClientRecvResults{client: v.Client, data: ret}, nil
}

type builderBuildFromTarArgsData struct {
	Application *string         `cbor:"0,keyasint,omitempty" json:"application,omitempty"`
	Tardata     *rpc.Capability `cbor:"1,keyasint,omitempty" json:"tardata,omitempty"`
	Status      *rpc.Capability `cbor:"2,keyasint,omitempty" json:"status,omitempty"`
}

type BuilderBuildFromTarArgs struct {
	call *rpc.Call
	data builderBuildFromTarArgsData
}

func (v *BuilderBuildFromTarArgs) HasApplication() bool {
	return v.data.Application != nil
}

func (v *BuilderBuildFromTarArgs) Application() string {
	if v.data.Application == nil {
		return ""
	}
	return *v.data.Application
}

func (v *BuilderBuildFromTarArgs) HasTardata() bool {
	return v.data.Tardata != nil
}

func (v *BuilderBuildFromTarArgs) Tardata() *stream.RecvStreamClient[[]byte] {
	if v.data.Tardata == nil {
		return nil
	}
	return &stream.RecvStreamClient[[]byte]{Client: v.call.NewClient(v.data.Tardata)}
}

func (v *BuilderBuildFromTarArgs) HasStatus() bool {
	return v.data.Status != nil
}

func (v *BuilderBuildFromTarArgs) Status() *stream.SendStreamClient[*Status] {
	if v.data.Status == nil {
		return nil
	}
	return &stream.SendStreamClient[*Status]{Client: v.call.NewClient(v.data.Status)}
}

func (v *BuilderBuildFromTarArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *BuilderBuildFromTarArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *BuilderBuildFromTarArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *BuilderBuildFromTarArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type builderBuildFromTarResultsData struct {
	Version *string `cbor:"0,keyasint,omitempty" json:"version,omitempty"`
}

type BuilderBuildFromTarResults struct {
	call *rpc.Call
	data builderBuildFromTarResultsData
}

func (v *BuilderBuildFromTarResults) SetVersion(version string) {
	v.data.Version = &version
}

func (v *BuilderBuildFromTarResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *BuilderBuildFromTarResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *BuilderBuildFromTarResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *BuilderBuildFromTarResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type BuilderBuildFromTar struct {
	*rpc.Call
	args    BuilderBuildFromTarArgs
	results BuilderBuildFromTarResults
}

func (t *BuilderBuildFromTar) Args() *BuilderBuildFromTarArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *BuilderBuildFromTar) Results() *BuilderBuildFromTarResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type Builder interface {
	BuildFromTar(ctx context.Context, state *BuilderBuildFromTar) error
}

type reexportBuilder struct {
	client *rpc.Client
}

func (_ reexportBuilder) BuildFromTar(ctx context.Context, state *BuilderBuildFromTar) error {
	panic("not implemented")
}

func (t reexportBuilder) CapabilityClient() *rpc.Client {
	return t.client
}

func AdaptBuilder(t Builder) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "buildFromTar",
			InterfaceName: "Builder",
			Index:         0,
			Handler: func(ctx context.Context, call *rpc.Call) error {
				return t.BuildFromTar(ctx, &BuilderBuildFromTar{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type BuilderClient struct {
	*rpc.Client
}

func (c BuilderClient) Export() Builder {
	return reexportBuilder{client: c.Client}
}

type BuilderClientBuildFromTarResults struct {
	client *rpc.Client
	data   builderBuildFromTarResultsData
}

func (v *BuilderClientBuildFromTarResults) HasVersion() bool {
	return v.data.Version != nil
}

func (v *BuilderClientBuildFromTarResults) Version() string {
	if v.data.Version == nil {
		return ""
	}
	return *v.data.Version
}

func (v BuilderClient) BuildFromTar(ctx context.Context, application string, tardata stream.RecvStream[[]byte], status stream.SendStream[*Status]) (*BuilderClientBuildFromTarResults, error) {
	args := BuilderBuildFromTarArgs{}
	caps := map[rpc.OID]*rpc.InlineCapability{}
	args.data.Application = &application
	{
		ic, oid, c := v.Client.NewInlineCapability(stream.AdaptRecvStream[[]byte](tardata), tardata)
		args.data.Tardata = c
		caps[oid] = ic
	}
	{
		ic, oid, c := v.Client.NewInlineCapability(stream.AdaptSendStream[*Status](status), status)
		args.data.Status = c
		caps[oid] = ic
	}

	var ret builderBuildFromTarResultsData

	err := v.Client.CallWithCaps(ctx, "buildFromTar", &args, &ret, caps)
	if err != nil {
		return nil, err
	}

	return &BuilderClientBuildFromTarResults{client: v.Client, data: ret}, nil
}
