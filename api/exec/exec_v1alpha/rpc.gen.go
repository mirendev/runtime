package exec_v1alpha

import (
	"context"
	"encoding/json"

	"github.com/fxamacker/cbor/v2"
	rpc "miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/stream"
)

type sandboxExecExecArgsData struct {
	Id      *string         `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
	Command *string         `cbor:"1,keyasint,omitempty" json:"command,omitempty"`
	Input   *rpc.Capability `cbor:"2,keyasint,omitempty" json:"input,omitempty"`
	Output  *rpc.Capability `cbor:"3,keyasint,omitempty" json:"output,omitempty"`
}

type SandboxExecExecArgs struct {
	call *rpc.Call
	data sandboxExecExecArgsData
}

func (v *SandboxExecExecArgs) HasId() bool {
	return v.data.Id != nil
}

func (v *SandboxExecExecArgs) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
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

func (v SandboxExecClient) Exec(ctx context.Context, id string, command string, input stream.RecvStream[[]byte], output stream.SendStream[[]byte]) (*SandboxExecClientExecResults, error) {
	args := SandboxExecExecArgs{}
	args.data.Id = &id
	args.data.Command = &command
	args.data.Input = v.Client.NewCapability(stream.AdaptRecvStream[[]byte](input), input)
	args.data.Output = v.Client.NewCapability(stream.AdaptSendStream[[]byte](output), output)

	var ret sandboxExecExecResultsData

	err := v.Client.Call(ctx, "exec", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &SandboxExecClientExecResults{client: v.Client, data: ret}, nil
}
