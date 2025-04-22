package exec_v1alpha

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
	WinSize  *WindowSize `cbor:"2,keyasint,omitempty" json:"win_size,omitempty"`
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

func (v *ShellOptions) HasWinSize() bool {
	return v.data.WinSize != nil
}

func (v *ShellOptions) WinSize() *WindowSize {
	return v.data.WinSize
}

func (v *ShellOptions) SetWinSize(win_size *WindowSize) {
	v.data.WinSize = win_size
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

type sandboxExecExecArgsData struct {
	Category      *string         `cbor:"0,keyasint,omitempty" json:"category,omitempty"`
	Value         *string         `cbor:"1,keyasint,omitempty" json:"value,omitempty"`
	Command       *string         `cbor:"2,keyasint,omitempty" json:"command,omitempty"`
	Options       *ShellOptions   `cbor:"3,keyasint,omitempty" json:"options,omitempty"`
	Input         *rpc.Capability `cbor:"4,keyasint,omitempty" json:"input,omitempty"`
	Output        *rpc.Capability `cbor:"5,keyasint,omitempty" json:"output,omitempty"`
	WindowUpdates *rpc.Capability `cbor:"6,keyasint,omitempty" json:"window_updates,omitempty"`
}

type SandboxExecExecArgs struct {
	call *rpc.Call
	data sandboxExecExecArgsData
}

func (v *SandboxExecExecArgs) HasCategory() bool {
	return v.data.Category != nil
}

func (v *SandboxExecExecArgs) Category() string {
	if v.data.Category == nil {
		return ""
	}
	return *v.data.Category
}

func (v *SandboxExecExecArgs) HasValue() bool {
	return v.data.Value != nil
}

func (v *SandboxExecExecArgs) Value() string {
	if v.data.Value == nil {
		return ""
	}
	return *v.data.Value
}

func (v *SandboxExecExecArgs) HasCommand() bool {
	return v.data.Command != nil
}

func (v *SandboxExecExecArgs) Command() string {
	if v.data.Command == nil {
		return ""
	}
	return *v.data.Command
}

func (v *SandboxExecExecArgs) HasOptions() bool {
	return v.data.Options != nil
}

func (v *SandboxExecExecArgs) Options() *ShellOptions {
	return v.data.Options
}

func (v *SandboxExecExecArgs) HasInput() bool {
	return v.data.Input != nil
}

func (v *SandboxExecExecArgs) Input() *stream.RecvStreamClient[[]byte] {
	if v.data.Input == nil {
		return nil
	}
	return &stream.RecvStreamClient[[]byte]{Client: v.call.NewClient(v.data.Input)}
}

func (v *SandboxExecExecArgs) HasOutput() bool {
	return v.data.Output != nil
}

func (v *SandboxExecExecArgs) Output() *stream.SendStreamClient[[]byte] {
	if v.data.Output == nil {
		return nil
	}
	return &stream.SendStreamClient[[]byte]{Client: v.call.NewClient(v.data.Output)}
}

func (v *SandboxExecExecArgs) HasWindowUpdates() bool {
	return v.data.WindowUpdates != nil
}

func (v *SandboxExecExecArgs) WindowUpdates() *stream.RecvStreamClient[*WindowSize] {
	if v.data.WindowUpdates == nil {
		return nil
	}
	return &stream.RecvStreamClient[*WindowSize]{Client: v.call.NewClient(v.data.WindowUpdates)}
}

func (v *SandboxExecExecArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SandboxExecExecArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SandboxExecExecArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SandboxExecExecArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type sandboxExecExecResultsData struct {
	Code *int32 `cbor:"0,keyasint,omitempty" json:"code,omitempty"`
}

type SandboxExecExecResults struct {
	call *rpc.Call
	data sandboxExecExecResultsData
}

func (v *SandboxExecExecResults) SetCode(code int32) {
	v.data.Code = &code
}

func (v *SandboxExecExecResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SandboxExecExecResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SandboxExecExecResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SandboxExecExecResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type SandboxExecExec struct {
	*rpc.Call
	args    SandboxExecExecArgs
	results SandboxExecExecResults
}

func (t *SandboxExecExec) Args() *SandboxExecExecArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *SandboxExecExec) Results() *SandboxExecExecResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type SandboxExec interface {
	Exec(ctx context.Context, state *SandboxExecExec) error
}

type reexportSandboxExec struct {
	client *rpc.Client
}

func (_ reexportSandboxExec) Exec(ctx context.Context, state *SandboxExecExec) error {
	panic("not implemented")
}

func (t reexportSandboxExec) CapabilityClient() *rpc.Client {
	return t.client
}

func AdaptSandboxExec(t SandboxExec) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "exec",
			InterfaceName: "SandboxExec",
			Index:         0,
			Handler: func(ctx context.Context, call *rpc.Call) error {
				return t.Exec(ctx, &SandboxExecExec{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type SandboxExecClient struct {
	*rpc.Client
}

func (c SandboxExecClient) Export() SandboxExec {
	return reexportSandboxExec{client: c.Client}
}

type SandboxExecClientExecResults struct {
	client *rpc.Client
	data   sandboxExecExecResultsData
}

func (v *SandboxExecClientExecResults) HasCode() bool {
	return v.data.Code != nil
}

func (v *SandboxExecClientExecResults) Code() int32 {
	if v.data.Code == nil {
		return 0
	}
	return *v.data.Code
}

func (v SandboxExecClient) Exec(ctx context.Context, category string, value string, command string, options *ShellOptions, input stream.RecvStream[[]byte], output stream.SendStream[[]byte], window_updates stream.RecvStream[*WindowSize]) (*SandboxExecClientExecResults, error) {
	args := SandboxExecExecArgs{}
	args.data.Category = &category
	args.data.Value = &value
	args.data.Command = &command
	args.data.Options = options
	args.data.Input = v.Client.NewCapability(stream.AdaptRecvStream[[]byte](input), input)
	args.data.Output = v.Client.NewCapability(stream.AdaptSendStream[[]byte](output), output)
	args.data.WindowUpdates = v.Client.NewCapability(stream.AdaptRecvStream[*WindowSize](window_updates), window_updates)

	var ret sandboxExecExecResultsData

	err := v.Client.Call(ctx, "exec", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &SandboxExecClientExecResults{client: v.Client, data: ret}, nil
}
