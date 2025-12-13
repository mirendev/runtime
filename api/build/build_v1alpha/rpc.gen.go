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
	Error() string
	SetError(string)
}

type statusUpdate struct {
	U_Message  *string `cbor:"1,keyasint,omitempty" json:"message,omitempty"`
	U_Buildkit *[]byte `cbor:"2,keyasint,omitempty" json:"buildkit,omitempty"`
	U_Error    *string `cbor:"3,keyasint,omitempty" json:"error,omitempty"`
}

func (v *statusUpdate) Which() string {
	if v.U_Message != nil {
		return "message"
	}
	if v.U_Buildkit != nil {
		return "buildkit"
	}
	if v.U_Error != nil {
		return "error"
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
	v.U_Error = nil
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
	v.U_Error = nil
	v.U_Buildkit = &val
}

func (v *statusUpdate) Error() string {
	if v.U_Error == nil {
		return ""
	}
	return *v.U_Error
}

func (v *statusUpdate) SetError(val string) {
	v.U_Message = nil
	v.U_Buildkit = nil
	v.U_Error = &val
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

type accessInfoData struct {
	Hostnames       *[]string `cbor:"0,keyasint,omitempty" json:"hostnames,omitempty"`
	DefaultRoute    *bool     `cbor:"1,keyasint,omitempty" json:"default_route,omitempty"`
	ClusterHostname *string   `cbor:"2,keyasint,omitempty" json:"cluster_hostname,omitempty"`
}

type AccessInfo struct {
	data accessInfoData
}

func (v *AccessInfo) HasHostnames() bool {
	return v.data.Hostnames != nil
}

func (v *AccessInfo) Hostnames() *[]string {
	return v.data.Hostnames
}

func (v *AccessInfo) SetHostnames(hostnames *[]string) {
	v.data.Hostnames = hostnames
}

func (v *AccessInfo) HasDefaultRoute() bool {
	return v.data.DefaultRoute != nil
}

func (v *AccessInfo) DefaultRoute() bool {
	if v.data.DefaultRoute == nil {
		return false
	}
	return *v.data.DefaultRoute
}

func (v *AccessInfo) SetDefaultRoute(default_route bool) {
	v.data.DefaultRoute = &default_route
}

func (v *AccessInfo) HasClusterHostname() bool {
	return v.data.ClusterHostname != nil
}

func (v *AccessInfo) ClusterHostname() string {
	if v.data.ClusterHostname == nil {
		return ""
	}
	return *v.data.ClusterHostname
}

func (v *AccessInfo) SetClusterHostname(cluster_hostname string) {
	v.data.ClusterHostname = &cluster_hostname
}

func (v *AccessInfo) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *AccessInfo) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *AccessInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *AccessInfo) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type serviceInfoData struct {
	Name    *string `cbor:"0,keyasint,omitempty" json:"name,omitempty"`
	Command *string `cbor:"1,keyasint,omitempty" json:"command,omitempty"`
	Source  *string `cbor:"2,keyasint,omitempty" json:"source,omitempty"`
}

type ServiceInfo struct {
	data serviceInfoData
}

func (v *ServiceInfo) HasName() bool {
	return v.data.Name != nil
}

func (v *ServiceInfo) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *ServiceInfo) SetName(name string) {
	v.data.Name = &name
}

func (v *ServiceInfo) HasCommand() bool {
	return v.data.Command != nil
}

func (v *ServiceInfo) Command() string {
	if v.data.Command == nil {
		return ""
	}
	return *v.data.Command
}

func (v *ServiceInfo) SetCommand(command string) {
	v.data.Command = &command
}

func (v *ServiceInfo) HasSource() bool {
	return v.data.Source != nil
}

func (v *ServiceInfo) Source() string {
	if v.data.Source == nil {
		return ""
	}
	return *v.data.Source
}

func (v *ServiceInfo) SetSource(source string) {
	v.data.Source = &source
}

func (v *ServiceInfo) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ServiceInfo) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ServiceInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ServiceInfo) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type detectionEventData struct {
	Kind    *string `cbor:"0,keyasint,omitempty" json:"kind,omitempty"`
	Name    *string `cbor:"1,keyasint,omitempty" json:"name,omitempty"`
	Message *string `cbor:"2,keyasint,omitempty" json:"message,omitempty"`
}

type DetectionEvent struct {
	data detectionEventData
}

func (v *DetectionEvent) HasKind() bool {
	return v.data.Kind != nil
}

func (v *DetectionEvent) Kind() string {
	if v.data.Kind == nil {
		return ""
	}
	return *v.data.Kind
}

func (v *DetectionEvent) SetKind(kind string) {
	v.data.Kind = &kind
}

func (v *DetectionEvent) HasName() bool {
	return v.data.Name != nil
}

func (v *DetectionEvent) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *DetectionEvent) SetName(name string) {
	v.data.Name = &name
}

func (v *DetectionEvent) HasMessage() bool {
	return v.data.Message != nil
}

func (v *DetectionEvent) Message() string {
	if v.data.Message == nil {
		return ""
	}
	return *v.data.Message
}

func (v *DetectionEvent) SetMessage(message string) {
	v.data.Message = &message
}

func (v *DetectionEvent) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DetectionEvent) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DetectionEvent) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DetectionEvent) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type analysisResultData struct {
	Stack           *string           `cbor:"0,keyasint,omitempty" json:"stack,omitempty"`
	Services        *[]ServiceInfo    `cbor:"1,keyasint,omitempty" json:"services,omitempty"`
	WorkingDir      *string           `cbor:"2,keyasint,omitempty" json:"working_dir,omitempty"`
	Entrypoint      *string           `cbor:"3,keyasint,omitempty" json:"entrypoint,omitempty"`
	AppName         *string           `cbor:"4,keyasint,omitempty" json:"app_name,omitempty"`
	BuildDockerfile *string           `cbor:"5,keyasint,omitempty" json:"build_dockerfile,omitempty"`
	EnvVars         *[]string         `cbor:"6,keyasint,omitempty" json:"env_vars,omitempty"`
	Events          *[]DetectionEvent `cbor:"7,keyasint,omitempty" json:"events,omitempty"`
}

type AnalysisResult struct {
	data analysisResultData
}

func (v *AnalysisResult) HasStack() bool {
	return v.data.Stack != nil
}

func (v *AnalysisResult) Stack() string {
	if v.data.Stack == nil {
		return ""
	}
	return *v.data.Stack
}

func (v *AnalysisResult) SetStack(stack string) {
	v.data.Stack = &stack
}

func (v *AnalysisResult) HasServices() bool {
	return v.data.Services != nil
}

func (v *AnalysisResult) Services() *[]ServiceInfo {
	return v.data.Services
}

func (v *AnalysisResult) SetServices(services *[]ServiceInfo) {
	v.data.Services = services
}

func (v *AnalysisResult) HasWorkingDir() bool {
	return v.data.WorkingDir != nil
}

func (v *AnalysisResult) WorkingDir() string {
	if v.data.WorkingDir == nil {
		return ""
	}
	return *v.data.WorkingDir
}

func (v *AnalysisResult) SetWorkingDir(working_dir string) {
	v.data.WorkingDir = &working_dir
}

func (v *AnalysisResult) HasEntrypoint() bool {
	return v.data.Entrypoint != nil
}

func (v *AnalysisResult) Entrypoint() string {
	if v.data.Entrypoint == nil {
		return ""
	}
	return *v.data.Entrypoint
}

func (v *AnalysisResult) SetEntrypoint(entrypoint string) {
	v.data.Entrypoint = &entrypoint
}

func (v *AnalysisResult) HasAppName() bool {
	return v.data.AppName != nil
}

func (v *AnalysisResult) AppName() string {
	if v.data.AppName == nil {
		return ""
	}
	return *v.data.AppName
}

func (v *AnalysisResult) SetAppName(app_name string) {
	v.data.AppName = &app_name
}

func (v *AnalysisResult) HasBuildDockerfile() bool {
	return v.data.BuildDockerfile != nil
}

func (v *AnalysisResult) BuildDockerfile() string {
	if v.data.BuildDockerfile == nil {
		return ""
	}
	return *v.data.BuildDockerfile
}

func (v *AnalysisResult) SetBuildDockerfile(build_dockerfile string) {
	v.data.BuildDockerfile = &build_dockerfile
}

func (v *AnalysisResult) HasEnvVars() bool {
	return v.data.EnvVars != nil
}

func (v *AnalysisResult) EnvVars() *[]string {
	return v.data.EnvVars
}

func (v *AnalysisResult) SetEnvVars(env_vars *[]string) {
	v.data.EnvVars = env_vars
}

func (v *AnalysisResult) HasEvents() bool {
	return v.data.Events != nil
}

func (v *AnalysisResult) Events() *[]DetectionEvent {
	return v.data.Events
}

func (v *AnalysisResult) SetEvents(events *[]DetectionEvent) {
	v.data.Events = events
}

func (v *AnalysisResult) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *AnalysisResult) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *AnalysisResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *AnalysisResult) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type streamRecvArgsData struct {
	Count *int32 `cbor:"0,keyasint,omitempty" json:"count,omitempty"`
}

type StreamRecvArgs struct {
	call rpc.Call
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
	call rpc.Call
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
	rpc.Call
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
	client rpc.Client
}

func (reexportStream) Recv(ctx context.Context, state *StreamRecv) error {
	panic("not implemented")
}

func (t reexportStream) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptStream(t Stream) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "recv",
			InterfaceName: "Stream",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Recv(ctx, &StreamRecv{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type StreamClient struct {
	rpc.Client
}

func NewStreamClient(client rpc.Client) *StreamClient {
	return &StreamClient{Client: client}
}

func (c StreamClient) Export() Stream {
	return reexportStream{client: c.Client}
}

type StreamClientRecvResults struct {
	client rpc.Client
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

	err := v.Call(ctx, "recv", &args, &ret)
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
	call rpc.Call
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
	Version    *string      `cbor:"0,keyasint,omitempty" json:"version,omitempty"`
	AccessInfo **AccessInfo `cbor:"1,keyasint,omitempty" json:"access_info,omitempty"`
}

type BuilderBuildFromTarResults struct {
	call rpc.Call
	data builderBuildFromTarResultsData
}

func (v *BuilderBuildFromTarResults) SetVersion(version string) {
	v.data.Version = &version
}

func (v *BuilderBuildFromTarResults) SetAccessInfo(access_info **AccessInfo) {
	v.data.AccessInfo = access_info
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

type builderAnalyzeAppArgsData struct {
	Tardata *rpc.Capability `cbor:"0,keyasint,omitempty" json:"tardata,omitempty"`
}

type BuilderAnalyzeAppArgs struct {
	call rpc.Call
	data builderAnalyzeAppArgsData
}

func (v *BuilderAnalyzeAppArgs) HasTardata() bool {
	return v.data.Tardata != nil
}

func (v *BuilderAnalyzeAppArgs) Tardata() *stream.RecvStreamClient[[]byte] {
	if v.data.Tardata == nil {
		return nil
	}
	return &stream.RecvStreamClient[[]byte]{Client: v.call.NewClient(v.data.Tardata)}
}

func (v *BuilderAnalyzeAppArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *BuilderAnalyzeAppArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *BuilderAnalyzeAppArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *BuilderAnalyzeAppArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type builderAnalyzeAppResultsData struct {
	Result **AnalysisResult `cbor:"0,keyasint,omitempty" json:"result,omitempty"`
}

type BuilderAnalyzeAppResults struct {
	call rpc.Call
	data builderAnalyzeAppResultsData
}

func (v *BuilderAnalyzeAppResults) SetResult(result **AnalysisResult) {
	v.data.Result = result
}

func (v *BuilderAnalyzeAppResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *BuilderAnalyzeAppResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *BuilderAnalyzeAppResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *BuilderAnalyzeAppResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type BuilderBuildFromTar struct {
	rpc.Call
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

type BuilderAnalyzeApp struct {
	rpc.Call
	args    BuilderAnalyzeAppArgs
	results BuilderAnalyzeAppResults
}

func (t *BuilderAnalyzeApp) Args() *BuilderAnalyzeAppArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *BuilderAnalyzeApp) Results() *BuilderAnalyzeAppResults {
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
	AnalyzeApp(ctx context.Context, state *BuilderAnalyzeApp) error
}

type reexportBuilder struct {
	client rpc.Client
}

func (reexportBuilder) BuildFromTar(ctx context.Context, state *BuilderBuildFromTar) error {
	panic("not implemented")
}

func (reexportBuilder) AnalyzeApp(ctx context.Context, state *BuilderAnalyzeApp) error {
	panic("not implemented")
}

func (t reexportBuilder) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptBuilder(t Builder) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "buildFromTar",
			InterfaceName: "Builder",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.BuildFromTar(ctx, &BuilderBuildFromTar{Call: call})
			},
		},
		{
			Name:          "analyzeApp",
			InterfaceName: "Builder",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.AnalyzeApp(ctx, &BuilderAnalyzeApp{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type BuilderClient struct {
	rpc.Client
}

func NewBuilderClient(client rpc.Client) *BuilderClient {
	return &BuilderClient{Client: client}
}

func (c BuilderClient) Export() Builder {
	return reexportBuilder{client: c.Client}
}

type BuilderClientBuildFromTarResults struct {
	client rpc.Client
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

func (v *BuilderClientBuildFromTarResults) HasAccessInfo() bool {
	return v.data.AccessInfo != nil
}

func (v *BuilderClientBuildFromTarResults) AccessInfo() *AccessInfo {
	return *v.data.AccessInfo
}

func (v BuilderClient) BuildFromTar(ctx context.Context, application string, tardata stream.RecvStream[[]byte], status stream.SendStream[*Status]) (*BuilderClientBuildFromTarResults, error) {
	args := BuilderBuildFromTarArgs{}
	caps := map[rpc.OID]*rpc.InlineCapability{}
	args.data.Application = &application
	{
		ic, oid, c := v.NewInlineCapability(stream.AdaptRecvStream[[]byte](tardata), tardata)
		args.data.Tardata = c
		caps[oid] = ic
	}
	{
		ic, oid, c := v.NewInlineCapability(stream.AdaptSendStream[*Status](status), status)
		args.data.Status = c
		caps[oid] = ic
	}

	var ret builderBuildFromTarResultsData

	err := v.CallWithCaps(ctx, "buildFromTar", &args, &ret, caps)
	if err != nil {
		return nil, err
	}

	return &BuilderClientBuildFromTarResults{client: v.Client, data: ret}, nil
}

type BuilderClientAnalyzeAppResults struct {
	client rpc.Client
	data   builderAnalyzeAppResultsData
}

func (v *BuilderClientAnalyzeAppResults) HasResult() bool {
	return v.data.Result != nil
}

func (v *BuilderClientAnalyzeAppResults) Result() *AnalysisResult {
	return *v.data.Result
}

func (v BuilderClient) AnalyzeApp(ctx context.Context, tardata stream.RecvStream[[]byte]) (*BuilderClientAnalyzeAppResults, error) {
	args := BuilderAnalyzeAppArgs{}
	caps := map[rpc.OID]*rpc.InlineCapability{}
	{
		ic, oid, c := v.NewInlineCapability(stream.AdaptRecvStream[[]byte](tardata), tardata)
		args.data.Tardata = c
		caps[oid] = ic
	}

	var ret builderAnalyzeAppResultsData

	err := v.CallWithCaps(ctx, "analyzeApp", &args, &ret, caps)
	if err != nil {
		return nil, err
	}

	return &BuilderClientAnalyzeAppResults{client: v.Client, data: ret}, nil
}
