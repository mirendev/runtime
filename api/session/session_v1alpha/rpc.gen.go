package session_v1alpha

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/fxamacker/cbor/v2"
	rpc "miren.dev/runtime/pkg/rpc"
)

type sessionInfoData struct {
	Id                    *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
	App                   *string `cbor:"1,keyasint,omitempty" json:"app,omitempty"`
	Version               *string `cbor:"2,keyasint,omitempty" json:"version,omitempty"`
	Service               *string `cbor:"3,keyasint,omitempty" json:"service,omitempty"`
	Group                 *string `cbor:"4,keyasint,omitempty" json:"group,omitempty"`
	MaxSessionsPerSandbox *int64  `cbor:"5,keyasint,omitempty" json:"max_sessions_per_sandbox,omitempty"`
	DesiredState          *string `cbor:"6,keyasint,omitempty" json:"desired_state,omitempty"`
	Phase                 *string `cbor:"7,keyasint,omitempty" json:"phase,omitempty"`
	Sandbox               *string `cbor:"8,keyasint,omitempty" json:"sandbox,omitempty"`
	Failure               *string `cbor:"9,keyasint,omitempty" json:"failure,omitempty"`
	IdleTimeoutSeconds    *int64  `cbor:"10,keyasint,omitempty" json:"idle_timeout_seconds,omitempty"`
	Activity              *string `cbor:"11,keyasint,omitempty" json:"activity,omitempty"`
}

type SessionInfo struct {
	data sessionInfoData
}

func (v *SessionInfo) HasId() bool {
	return v.data.Id != nil
}

func (v *SessionInfo) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *SessionInfo) SetId(id string) {
	v.data.Id = &id
}

func (v *SessionInfo) HasApp() bool {
	return v.data.App != nil
}

func (v *SessionInfo) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *SessionInfo) SetApp(app string) {
	v.data.App = &app
}

func (v *SessionInfo) HasVersion() bool {
	return v.data.Version != nil
}

func (v *SessionInfo) Version() string {
	if v.data.Version == nil {
		return ""
	}
	return *v.data.Version
}

func (v *SessionInfo) SetVersion(version string) {
	v.data.Version = &version
}

func (v *SessionInfo) HasService() bool {
	return v.data.Service != nil
}

func (v *SessionInfo) Service() string {
	if v.data.Service == nil {
		return ""
	}
	return *v.data.Service
}

func (v *SessionInfo) SetService(service string) {
	v.data.Service = &service
}

func (v *SessionInfo) HasGroup() bool {
	return v.data.Group != nil
}

func (v *SessionInfo) Group() string {
	if v.data.Group == nil {
		return ""
	}
	return *v.data.Group
}

func (v *SessionInfo) SetGroup(group string) {
	v.data.Group = &group
}

func (v *SessionInfo) HasMaxSessionsPerSandbox() bool {
	return v.data.MaxSessionsPerSandbox != nil
}

func (v *SessionInfo) MaxSessionsPerSandbox() int64 {
	if v.data.MaxSessionsPerSandbox == nil {
		return 0
	}
	return *v.data.MaxSessionsPerSandbox
}

func (v *SessionInfo) SetMaxSessionsPerSandbox(max_sessions_per_sandbox int64) {
	v.data.MaxSessionsPerSandbox = &max_sessions_per_sandbox
}

func (v *SessionInfo) HasDesiredState() bool {
	return v.data.DesiredState != nil
}

func (v *SessionInfo) DesiredState() string {
	if v.data.DesiredState == nil {
		return ""
	}
	return *v.data.DesiredState
}

func (v *SessionInfo) SetDesiredState(desired_state string) {
	v.data.DesiredState = &desired_state
}

func (v *SessionInfo) HasPhase() bool {
	return v.data.Phase != nil
}

func (v *SessionInfo) Phase() string {
	if v.data.Phase == nil {
		return ""
	}
	return *v.data.Phase
}

func (v *SessionInfo) SetPhase(phase string) {
	v.data.Phase = &phase
}

func (v *SessionInfo) HasSandbox() bool {
	return v.data.Sandbox != nil
}

func (v *SessionInfo) Sandbox() string {
	if v.data.Sandbox == nil {
		return ""
	}
	return *v.data.Sandbox
}

func (v *SessionInfo) SetSandbox(sandbox string) {
	v.data.Sandbox = &sandbox
}

func (v *SessionInfo) HasFailure() bool {
	return v.data.Failure != nil
}

func (v *SessionInfo) Failure() string {
	if v.data.Failure == nil {
		return ""
	}
	return *v.data.Failure
}

func (v *SessionInfo) SetFailure(failure string) {
	v.data.Failure = &failure
}

func (v *SessionInfo) HasIdleTimeoutSeconds() bool {
	return v.data.IdleTimeoutSeconds != nil
}

func (v *SessionInfo) IdleTimeoutSeconds() int64 {
	if v.data.IdleTimeoutSeconds == nil {
		return 0
	}
	return *v.data.IdleTimeoutSeconds
}

func (v *SessionInfo) SetIdleTimeoutSeconds(idle_timeout_seconds int64) {
	v.data.IdleTimeoutSeconds = &idle_timeout_seconds
}

func (v *SessionInfo) HasActivity() bool {
	return v.data.Activity != nil
}

func (v *SessionInfo) Activity() string {
	if v.data.Activity == nil {
		return ""
	}
	return *v.data.Activity
}

func (v *SessionInfo) SetActivity(activity string) {
	v.data.Activity = &activity
}

func (v *SessionInfo) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SessionInfo) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SessionInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SessionInfo) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type sessionsCreateArgsData struct {
	App                   *string `cbor:"0,keyasint,omitempty" json:"app,omitempty"`
	Name                  *string `cbor:"1,keyasint,omitempty" json:"name,omitempty"`
	Service               *string `cbor:"2,keyasint,omitempty" json:"service,omitempty"`
	Group                 *string `cbor:"3,keyasint,omitempty" json:"group,omitempty"`
	MaxSessionsPerSandbox *int64  `cbor:"4,keyasint,omitempty" json:"max_sessions_per_sandbox,omitempty"`
	IdleTimeoutSeconds    *int64  `cbor:"5,keyasint,omitempty" json:"idle_timeout_seconds,omitempty"`
}

type SessionsCreateArgs struct {
	call rpc.Call
	data sessionsCreateArgsData
}

func (v *SessionsCreateArgs) HasApp() bool {
	return v.data.App != nil
}

func (v *SessionsCreateArgs) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *SessionsCreateArgs) HasName() bool {
	return v.data.Name != nil
}

func (v *SessionsCreateArgs) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *SessionsCreateArgs) HasService() bool {
	return v.data.Service != nil
}

func (v *SessionsCreateArgs) Service() string {
	if v.data.Service == nil {
		return ""
	}
	return *v.data.Service
}

func (v *SessionsCreateArgs) HasGroup() bool {
	return v.data.Group != nil
}

func (v *SessionsCreateArgs) Group() string {
	if v.data.Group == nil {
		return ""
	}
	return *v.data.Group
}

func (v *SessionsCreateArgs) HasMaxSessionsPerSandbox() bool {
	return v.data.MaxSessionsPerSandbox != nil
}

func (v *SessionsCreateArgs) MaxSessionsPerSandbox() int64 {
	if v.data.MaxSessionsPerSandbox == nil {
		return 0
	}
	return *v.data.MaxSessionsPerSandbox
}

func (v *SessionsCreateArgs) HasIdleTimeoutSeconds() bool {
	return v.data.IdleTimeoutSeconds != nil
}

func (v *SessionsCreateArgs) IdleTimeoutSeconds() int64 {
	if v.data.IdleTimeoutSeconds == nil {
		return 0
	}
	return *v.data.IdleTimeoutSeconds
}

func (v *SessionsCreateArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SessionsCreateArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SessionsCreateArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SessionsCreateArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type sessionsCreateResultsData struct {
	Session *SessionInfo `cbor:"0,keyasint,omitempty" json:"session,omitempty"`
}

type SessionsCreateResults struct {
	call rpc.Call
	data sessionsCreateResultsData
}

func (v *SessionsCreateResults) SetSession(session *SessionInfo) {
	v.data.Session = session
}

func (v *SessionsCreateResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SessionsCreateResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SessionsCreateResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SessionsCreateResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type sessionsListArgsData struct {
	App *string `cbor:"0,keyasint,omitempty" json:"app,omitempty"`
}

type SessionsListArgs struct {
	call rpc.Call
	data sessionsListArgsData
}

func (v *SessionsListArgs) HasApp() bool {
	return v.data.App != nil
}

func (v *SessionsListArgs) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *SessionsListArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SessionsListArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SessionsListArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SessionsListArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type sessionsListResultsData struct {
	Sessions *[]*SessionInfo `cbor:"0,keyasint,omitempty" json:"sessions,omitempty"`
}

type SessionsListResults struct {
	call rpc.Call
	data sessionsListResultsData
}

func (v *SessionsListResults) SetSessions(sessions []*SessionInfo) {
	x := slices.Clone(sessions)
	v.data.Sessions = &x
}

func (v *SessionsListResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SessionsListResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SessionsListResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SessionsListResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type sessionsGetArgsData struct {
	App  *string `cbor:"0,keyasint,omitempty" json:"app,omitempty"`
	Name *string `cbor:"1,keyasint,omitempty" json:"name,omitempty"`
}

type SessionsGetArgs struct {
	call rpc.Call
	data sessionsGetArgsData
}

func (v *SessionsGetArgs) HasApp() bool {
	return v.data.App != nil
}

func (v *SessionsGetArgs) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *SessionsGetArgs) HasName() bool {
	return v.data.Name != nil
}

func (v *SessionsGetArgs) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *SessionsGetArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SessionsGetArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SessionsGetArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SessionsGetArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type sessionsGetResultsData struct {
	Session *SessionInfo `cbor:"0,keyasint,omitempty" json:"session,omitempty"`
}

type SessionsGetResults struct {
	call rpc.Call
	data sessionsGetResultsData
}

func (v *SessionsGetResults) SetSession(session *SessionInfo) {
	v.data.Session = session
}

func (v *SessionsGetResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SessionsGetResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SessionsGetResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SessionsGetResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type sessionsSetDesiredStateArgsData struct {
	App          *string `cbor:"0,keyasint,omitempty" json:"app,omitempty"`
	Name         *string `cbor:"1,keyasint,omitempty" json:"name,omitempty"`
	DesiredState *string `cbor:"2,keyasint,omitempty" json:"desired_state,omitempty"`
}

type SessionsSetDesiredStateArgs struct {
	call rpc.Call
	data sessionsSetDesiredStateArgsData
}

func (v *SessionsSetDesiredStateArgs) HasApp() bool {
	return v.data.App != nil
}

func (v *SessionsSetDesiredStateArgs) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *SessionsSetDesiredStateArgs) HasName() bool {
	return v.data.Name != nil
}

func (v *SessionsSetDesiredStateArgs) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *SessionsSetDesiredStateArgs) HasDesiredState() bool {
	return v.data.DesiredState != nil
}

func (v *SessionsSetDesiredStateArgs) DesiredState() string {
	if v.data.DesiredState == nil {
		return ""
	}
	return *v.data.DesiredState
}

func (v *SessionsSetDesiredStateArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SessionsSetDesiredStateArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SessionsSetDesiredStateArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SessionsSetDesiredStateArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type sessionsSetDesiredStateResultsData struct {
	Session *SessionInfo `cbor:"0,keyasint,omitempty" json:"session,omitempty"`
}

type SessionsSetDesiredStateResults struct {
	call rpc.Call
	data sessionsSetDesiredStateResultsData
}

func (v *SessionsSetDesiredStateResults) SetSession(session *SessionInfo) {
	v.data.Session = session
}

func (v *SessionsSetDesiredStateResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SessionsSetDesiredStateResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SessionsSetDesiredStateResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SessionsSetDesiredStateResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type sessionsDeleteArgsData struct {
	App  *string `cbor:"0,keyasint,omitempty" json:"app,omitempty"`
	Name *string `cbor:"1,keyasint,omitempty" json:"name,omitempty"`
}

type SessionsDeleteArgs struct {
	call rpc.Call
	data sessionsDeleteArgsData
}

func (v *SessionsDeleteArgs) HasApp() bool {
	return v.data.App != nil
}

func (v *SessionsDeleteArgs) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *SessionsDeleteArgs) HasName() bool {
	return v.data.Name != nil
}

func (v *SessionsDeleteArgs) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *SessionsDeleteArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SessionsDeleteArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SessionsDeleteArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SessionsDeleteArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type sessionsDeleteResultsData struct{}

type SessionsDeleteResults struct {
	call rpc.Call
	data sessionsDeleteResultsData
}

func (v *SessionsDeleteResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SessionsDeleteResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SessionsDeleteResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SessionsDeleteResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type SessionsCreate struct {
	rpc.Call
	args    SessionsCreateArgs
	results SessionsCreateResults
}

func (t *SessionsCreate) Args() *SessionsCreateArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *SessionsCreate) Results() *SessionsCreateResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type SessionsList struct {
	rpc.Call
	args    SessionsListArgs
	results SessionsListResults
}

func (t *SessionsList) Args() *SessionsListArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *SessionsList) Results() *SessionsListResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type SessionsGet struct {
	rpc.Call
	args    SessionsGetArgs
	results SessionsGetResults
}

func (t *SessionsGet) Args() *SessionsGetArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *SessionsGet) Results() *SessionsGetResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type SessionsSetDesiredState struct {
	rpc.Call
	args    SessionsSetDesiredStateArgs
	results SessionsSetDesiredStateResults
}

func (t *SessionsSetDesiredState) Args() *SessionsSetDesiredStateArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *SessionsSetDesiredState) Results() *SessionsSetDesiredStateResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type SessionsDelete struct {
	rpc.Call
	args    SessionsDeleteArgs
	results SessionsDeleteResults
}

func (t *SessionsDelete) Args() *SessionsDeleteArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *SessionsDelete) Results() *SessionsDeleteResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type Sessions interface {
	Create(ctx context.Context, state *SessionsCreate) error
	List(ctx context.Context, state *SessionsList) error
	Get(ctx context.Context, state *SessionsGet) error
	SetDesiredState(ctx context.Context, state *SessionsSetDesiredState) error
	Delete(ctx context.Context, state *SessionsDelete) error
}

type reexportSessions struct {
	client rpc.Client
}

func (reexportSessions) Create(ctx context.Context, state *SessionsCreate) error {
	panic("not implemented")
}

func (reexportSessions) List(ctx context.Context, state *SessionsList) error {
	panic("not implemented")
}

func (reexportSessions) Get(ctx context.Context, state *SessionsGet) error {
	panic("not implemented")
}

func (reexportSessions) SetDesiredState(ctx context.Context, state *SessionsSetDesiredState) error {
	panic("not implemented")
}

func (reexportSessions) Delete(ctx context.Context, state *SessionsDelete) error {
	panic("not implemented")
}

func (t reexportSessions) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptSessions(t Sessions) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "create",
			InterfaceName: "Sessions",
			Index:         0,
			Public:        false,
			Params:        []string{"app", "name", "service", "group", "max_sessions_per_sandbox", "idle_timeout_seconds"},
			HTTP: &rpc.HTTPBinding{
				Verb:       "POST",
				Path:       "/api/v1/apps/{app}/sessions",
				Body:       "*",
				PathParams: []string{"app"},
			},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Create(ctx, &SessionsCreate{Call: call})
			},
		},
		{
			Name:          "list",
			InterfaceName: "Sessions",
			Index:         0,
			Public:        false,
			Params:        []string{"app"},
			HTTP: &rpc.HTTPBinding{
				Verb:       "GET",
				Path:       "/api/v1/apps/{app}/sessions",
				Body:       "",
				PathParams: []string{"app"},
			},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.List(ctx, &SessionsList{Call: call})
			},
		},
		{
			Name:          "get",
			InterfaceName: "Sessions",
			Index:         0,
			Public:        false,
			Params:        []string{"app", "name"},
			HTTP: &rpc.HTTPBinding{
				Verb:       "GET",
				Path:       "/api/v1/apps/{app}/sessions/{name}",
				Body:       "",
				PathParams: []string{"app", "name"},
			},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Get(ctx, &SessionsGet{Call: call})
			},
		},
		{
			Name:          "setDesiredState",
			InterfaceName: "Sessions",
			Index:         0,
			Public:        false,
			Params:        []string{"app", "name", "desired_state"},
			HTTP: &rpc.HTTPBinding{
				Verb:       "PUT",
				Path:       "/api/v1/apps/{app}/sessions/{name}/desired-state",
				Body:       "*",
				PathParams: []string{"app", "name"},
			},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.SetDesiredState(ctx, &SessionsSetDesiredState{Call: call})
			},
		},
		{
			Name:          "delete",
			InterfaceName: "Sessions",
			Index:         0,
			Public:        false,
			Params:        []string{"app", "name"},
			HTTP: &rpc.HTTPBinding{
				Verb:       "DELETE",
				Path:       "/api/v1/apps/{app}/sessions/{name}",
				Body:       "",
				PathParams: []string{"app", "name"},
			},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Delete(ctx, &SessionsDelete{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type SessionsClient struct {
	rpc.Client
}

func NewSessionsClient(client rpc.Client) *SessionsClient {
	return &SessionsClient{Client: client}
}

func (c SessionsClient) Export() Sessions {
	return reexportSessions{client: c.Client}
}

type SessionsClientCreateResults struct {
	client rpc.Client
	data   sessionsCreateResultsData
}

func (v *SessionsClientCreateResults) HasSession() bool {
	return v.data.Session != nil
}

func (v *SessionsClientCreateResults) Session() *SessionInfo {
	return v.data.Session
}

func (v SessionsClient) Create(ctx context.Context, app string, name string, service string, group string, max_sessions_per_sandbox int64, idle_timeout_seconds int64) (*SessionsClientCreateResults, error) {
	args := SessionsCreateArgs{}
	args.data.App = &app
	args.data.Name = &name
	args.data.Service = &service
	args.data.Group = &group
	args.data.MaxSessionsPerSandbox = &max_sessions_per_sandbox
	args.data.IdleTimeoutSeconds = &idle_timeout_seconds

	var ret sessionsCreateResultsData

	err := v.Call(ctx, "create", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &SessionsClientCreateResults{client: v.Client, data: ret}, nil
}

type SessionsClientListResults struct {
	client rpc.Client
	data   sessionsListResultsData
}

func (v *SessionsClientListResults) HasSessions() bool {
	return v.data.Sessions != nil
}

func (v *SessionsClientListResults) Sessions() []*SessionInfo {
	if v.data.Sessions == nil {
		return nil
	}
	return *v.data.Sessions
}

func (v SessionsClient) List(ctx context.Context, app string) (*SessionsClientListResults, error) {
	args := SessionsListArgs{}
	args.data.App = &app

	var ret sessionsListResultsData

	err := v.Call(ctx, "list", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &SessionsClientListResults{client: v.Client, data: ret}, nil
}

type SessionsClientGetResults struct {
	client rpc.Client
	data   sessionsGetResultsData
}

func (v *SessionsClientGetResults) HasSession() bool {
	return v.data.Session != nil
}

func (v *SessionsClientGetResults) Session() *SessionInfo {
	return v.data.Session
}

func (v SessionsClient) Get(ctx context.Context, app string, name string) (*SessionsClientGetResults, error) {
	args := SessionsGetArgs{}
	args.data.App = &app
	args.data.Name = &name

	var ret sessionsGetResultsData

	err := v.Call(ctx, "get", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &SessionsClientGetResults{client: v.Client, data: ret}, nil
}

type SessionsClientSetDesiredStateResults struct {
	client rpc.Client
	data   sessionsSetDesiredStateResultsData
}

func (v *SessionsClientSetDesiredStateResults) HasSession() bool {
	return v.data.Session != nil
}

func (v *SessionsClientSetDesiredStateResults) Session() *SessionInfo {
	return v.data.Session
}

func (v SessionsClient) SetDesiredState(ctx context.Context, app string, name string, desired_state string) (*SessionsClientSetDesiredStateResults, error) {
	args := SessionsSetDesiredStateArgs{}
	args.data.App = &app
	args.data.Name = &name
	args.data.DesiredState = &desired_state

	var ret sessionsSetDesiredStateResultsData

	err := v.Call(ctx, "setDesiredState", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &SessionsClientSetDesiredStateResults{client: v.Client, data: ret}, nil
}

type SessionsClientDeleteResults struct {
	client rpc.Client
	data   sessionsDeleteResultsData
}

func (v SessionsClient) Delete(ctx context.Context, app string, name string) (*SessionsClientDeleteResults, error) {
	args := SessionsDeleteArgs{}
	args.data.App = &app
	args.data.Name = &name

	var ret sessionsDeleteResultsData

	err := v.Call(ctx, "delete", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &SessionsClientDeleteResults{client: v.Client, data: ret}, nil
}
