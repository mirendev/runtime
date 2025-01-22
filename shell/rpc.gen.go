package shell

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/fxamacker/cbor/v2"
	rpc "miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/stream"
)

type windowSizeData struct {
	Height *int32 `cbor:"0,keyasint,omitempty" json:"height,omitempty"`
	Width  *int32 `cbor:"1,keyasint,omitempty" json:"width,omitempty"`
}

type WindowSize struct {
	data windowSizeData
}

func (v *WindowSize) HasHeight() bool {
	return v.data.Height != nil
}

func (v *WindowSize) Height() int32 {
	if v.data.Height == nil {
		return 0
	}
	return *v.data.Height
}

func (v *WindowSize) SetHeight(height int32) {
	v.data.Height = &height
}

func (v *WindowSize) HasWidth() bool {
	return v.data.Width != nil
}

func (v *WindowSize) Width() int32 {
	if v.data.Width == nil {
		return 0
	}
	return *v.data.Width
}

func (v *WindowSize) SetWidth(width int32) {
	v.data.Width = &width
}

func (v *WindowSize) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *WindowSize) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *WindowSize) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *WindowSize) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type shellOptionsData struct {
	Terminal *bool       `cbor:"0,keyasint,omitempty" json:"terminal,omitempty"`
	Command  *[]string   `cbor:"1,keyasint,omitempty" json:"command,omitempty"`
	Win_size *WindowSize `cbor:"2,keyasint,omitempty" json:"win_size,omitempty"`
	Env      *[]string   `cbor:"3,keyasint,omitempty" json:"env,omitempty"`
	Pool     *string     `cbor:"4,keyasint,omitempty" json:"pool,omitempty"`
}

type ShellOptions struct {
	data shellOptionsData
}

func (v *ShellOptions) HasTerminal() bool {
	return v.data.Terminal != nil
}

func (v *ShellOptions) Terminal() bool {
	if v.data.Terminal == nil {
		return false
	}
	return *v.data.Terminal
}

func (v *ShellOptions) SetTerminal(terminal bool) {
	v.data.Terminal = &terminal
}

func (v *ShellOptions) HasCommand() bool {
	return v.data.Command != nil
}

func (v *ShellOptions) Command() []string {
	if v.data.Command == nil {
		return nil
	}
	return *v.data.Command
}

func (v *ShellOptions) SetCommand(command []string) {
	x := slices.Clone(command)
	v.data.Command = &x
}

func (v *ShellOptions) HasWin_size() bool {
	return v.data.Win_size != nil
}

func (v *ShellOptions) Win_size() *WindowSize {
	return v.data.Win_size
}

func (v *ShellOptions) SetWin_size(win_size *WindowSize) {
	v.data.Win_size = win_size
}

func (v *ShellOptions) HasEnv() bool {
	return v.data.Env != nil
}

func (v *ShellOptions) Env() []string {
	if v.data.Env == nil {
		return nil
	}
	return *v.data.Env
}

func (v *ShellOptions) SetEnv(env []string) {
	x := slices.Clone(env)
	v.data.Env = &x
}

func (v *ShellOptions) HasPool() bool {
	return v.data.Pool != nil
}

func (v *ShellOptions) Pool() string {
	if v.data.Pool == nil {
		return ""
	}
	return *v.data.Pool
}

func (v *ShellOptions) SetPool(pool string) {
	v.data.Pool = &pool
}

func (v *ShellOptions) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ShellOptions) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ShellOptions) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ShellOptions) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type shellAccessOpenArgsData struct {
	Application    *string         `cbor:"0,keyasint,omitempty" json:"application,omitempty"`
	Options        *ShellOptions   `cbor:"1,keyasint,omitempty" json:"options,omitempty"`
	Input          *rpc.Capability `cbor:"2,keyasint,omitempty" json:"input,omitempty"`
	Output         *rpc.Capability `cbor:"3,keyasint,omitempty" json:"output,omitempty"`
	Window_updates *rpc.Capability `cbor:"4,keyasint,omitempty" json:"window_updates,omitempty"`
}

type ShellAccessOpenArgs struct {
	call *rpc.Call
	data shellAccessOpenArgsData
}

func (v *ShellAccessOpenArgs) HasApplication() bool {
	return v.data.Application != nil
}

func (v *ShellAccessOpenArgs) Application() string {
	if v.data.Application == nil {
		return ""
	}
	return *v.data.Application
}

func (v *ShellAccessOpenArgs) HasOptions() bool {
	return v.data.Options != nil
}

func (v *ShellAccessOpenArgs) Options() *ShellOptions {
	return v.data.Options
}

func (v *ShellAccessOpenArgs) HasInput() bool {
	return v.data.Input != nil
}

func (v *ShellAccessOpenArgs) Input() *stream.RecvStreamClient[[]byte] {
	if v.data.Input == nil {
		return nil
	}
	return &stream.RecvStreamClient[[]byte]{Client: v.call.NewClient(v.data.Input)}
}

func (v *ShellAccessOpenArgs) HasOutput() bool {
	return v.data.Output != nil
}

func (v *ShellAccessOpenArgs) Output() *stream.SendStreamClient[[]byte] {
	if v.data.Output == nil {
		return nil
	}
	return &stream.SendStreamClient[[]byte]{Client: v.call.NewClient(v.data.Output)}
}

func (v *ShellAccessOpenArgs) HasWindow_updates() bool {
	return v.data.Window_updates != nil
}

func (v *ShellAccessOpenArgs) Window_updates() *stream.RecvStreamClient[*WindowSize] {
	if v.data.Window_updates == nil {
		return nil
	}
	return &stream.RecvStreamClient[*WindowSize]{Client: v.call.NewClient(v.data.Window_updates)}
}

func (v *ShellAccessOpenArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ShellAccessOpenArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ShellAccessOpenArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ShellAccessOpenArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type shellAccessOpenResultsData struct {
	Status *int32 `cbor:"0,keyasint,omitempty" json:"status,omitempty"`
}

type ShellAccessOpenResults struct {
	call *rpc.Call
	data shellAccessOpenResultsData
}

func (v *ShellAccessOpenResults) SetStatus(status int32) {
	v.data.Status = &status
}

func (v *ShellAccessOpenResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ShellAccessOpenResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ShellAccessOpenResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ShellAccessOpenResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type ShellAccessOpen struct {
	*rpc.Call
	args    ShellAccessOpenArgs
	results ShellAccessOpenResults
}

func (t *ShellAccessOpen) Args() *ShellAccessOpenArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *ShellAccessOpen) Results() *ShellAccessOpenResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type ShellAccess interface {
	Open(ctx context.Context, state *ShellAccessOpen) error
}

type reexportShellAccess struct {
	client *rpc.Client
}

func (_ reexportShellAccess) Open(ctx context.Context, state *ShellAccessOpen) error {
	panic("not implemented")
}

func (t reexportShellAccess) CapabilityClient() *rpc.Client {
	return t.client
}

func AdaptShellAccess(t ShellAccess) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "open",
			InterfaceName: "ShellAccess",
			Index:         0,
			Handler: func(ctx context.Context, call *rpc.Call) error {
				return t.Open(ctx, &ShellAccessOpen{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type ShellAccessClient struct {
	*rpc.Client
}

func (c ShellAccessClient) Export() ShellAccess {
	return reexportShellAccess{client: c.Client}
}

type ShellAccessClientOpenResults struct {
	client *rpc.Client
	data   shellAccessOpenResultsData
}

func (v *ShellAccessClientOpenResults) HasStatus() bool {
	return v.data.Status != nil
}

func (v *ShellAccessClientOpenResults) Status() int32 {
	if v.data.Status == nil {
		return 0
	}
	return *v.data.Status
}

func (v ShellAccessClient) Open(ctx context.Context, application string, options *ShellOptions, input stream.RecvStream[[]byte], output stream.SendStream[[]byte], window_updates stream.RecvStream[*WindowSize]) (*ShellAccessClientOpenResults, error) {
	args := ShellAccessOpenArgs{}
	args.data.Application = &application
	args.data.Options = options
	args.data.Input = v.Client.NewCapability(stream.AdaptRecvStream[[]byte](input), input)
	args.data.Output = v.Client.NewCapability(stream.AdaptSendStream[[]byte](output), output)
	args.data.Window_updates = v.Client.NewCapability(stream.AdaptRecvStream[*WindowSize](window_updates), window_updates)

	var ret shellAccessOpenResultsData

	err := v.Client.Call(ctx, "open", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &ShellAccessClientOpenResults{client: v.Client, data: ret}, nil
}
