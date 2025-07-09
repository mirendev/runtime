package stream

import (
	"context"
	"encoding/json"

	"github.com/fxamacker/cbor/v2"
	rpc "miren.dev/runtime/pkg/rpc"
)

type sendStreamSendArgsData[T any] struct {
	Value *T `cbor:"0,keyasint,omitempty" json:"value,omitempty"`
}

type SendStreamSendArgs[T any] struct {
	call rpc.Call
	data sendStreamSendArgsData[T]
}

func (v *SendStreamSendArgs[T]) HasValue() bool {
	return v.data.Value != nil
}

func (v *SendStreamSendArgs[T]) Value() T {
	if v.data.Value == nil {
		return rpc.Zero[T]()
	}
	return *v.data.Value
}

func (v *SendStreamSendArgs[T]) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SendStreamSendArgs[T]) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SendStreamSendArgs[T]) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SendStreamSendArgs[T]) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type sendStreamSendResultsData[T any] struct {
	Count *int32 `cbor:"0,keyasint,omitempty" json:"count,omitempty"`
}

type SendStreamSendResults[T any] struct {
	call rpc.Call
	data sendStreamSendResultsData[T]
}

func (v *SendStreamSendResults[T]) SetCount(count int32) {
	v.data.Count = &count
}

func (v *SendStreamSendResults[T]) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SendStreamSendResults[T]) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SendStreamSendResults[T]) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SendStreamSendResults[T]) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type SendStreamSend[T any] struct {
	rpc.Call
	args    SendStreamSendArgs[T]
	results SendStreamSendResults[T]
}

func (t *SendStreamSend[T]) Args() *SendStreamSendArgs[T] {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *SendStreamSend[T]) Results() *SendStreamSendResults[T] {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type SendStream[T any] interface {
	Send(ctx context.Context, state *SendStreamSend[T]) error
}

type reexportSendStream[T any] struct {
	client rpc.Client
}

func (_ reexportSendStream[T]) Send(ctx context.Context, state *SendStreamSend[T]) error {
	panic("not implemented")
}

func (t reexportSendStream[T]) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptSendStream[T any](t SendStream[T]) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "send",
			InterfaceName: "SendStream",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Send(ctx, &SendStreamSend[T]{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type SendStreamClient[T any] struct {
	rpc.Client
}

func NewSendStreamClient[T any](client rpc.Client) *SendStreamClient[T] {
	return &SendStreamClient[T]{Client: client}
}

func (c SendStreamClient[T]) Export() SendStream[T] {
	return reexportSendStream[T]{client: c.Client}
}

type SendStreamClientSendResults[T any] struct {
	client rpc.Client
	data   sendStreamSendResultsData[T]
}

func (v *SendStreamClientSendResults[T]) HasCount() bool {
	return v.data.Count != nil
}

func (v *SendStreamClientSendResults[T]) Count() int32 {
	if v.data.Count == nil {
		return 0
	}
	return *v.data.Count
}

func (v SendStreamClient[T]) Send(ctx context.Context, value T) (*SendStreamClientSendResults[T], error) {
	args := SendStreamSendArgs[T]{}
	args.data.Value = &value

	var ret sendStreamSendResultsData[T]

	err := v.Call(ctx, "send", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &SendStreamClientSendResults[T]{client: v.Client, data: ret}, nil
}

type recvStreamRecvArgsData[T any] struct {
	Count *int32 `cbor:"0,keyasint,omitempty" json:"count,omitempty"`
}

type RecvStreamRecvArgs[T any] struct {
	call rpc.Call
	data recvStreamRecvArgsData[T]
}

func (v *RecvStreamRecvArgs[T]) HasCount() bool {
	return v.data.Count != nil
}

func (v *RecvStreamRecvArgs[T]) Count() int32 {
	if v.data.Count == nil {
		return 0
	}
	return *v.data.Count
}

func (v *RecvStreamRecvArgs[T]) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RecvStreamRecvArgs[T]) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RecvStreamRecvArgs[T]) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RecvStreamRecvArgs[T]) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type recvStreamRecvResultsData[T any] struct {
	Value *T `cbor:"0,keyasint,omitempty" json:"value,omitempty"`
}

type RecvStreamRecvResults[T any] struct {
	call rpc.Call
	data recvStreamRecvResultsData[T]
}

func (v *RecvStreamRecvResults[T]) SetValue(value T) {
	v.data.Value = &value
}

func (v *RecvStreamRecvResults[T]) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RecvStreamRecvResults[T]) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RecvStreamRecvResults[T]) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RecvStreamRecvResults[T]) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type RecvStreamRecv[T any] struct {
	rpc.Call
	args    RecvStreamRecvArgs[T]
	results RecvStreamRecvResults[T]
}

func (t *RecvStreamRecv[T]) Args() *RecvStreamRecvArgs[T] {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *RecvStreamRecv[T]) Results() *RecvStreamRecvResults[T] {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type RecvStream[T any] interface {
	Recv(ctx context.Context, state *RecvStreamRecv[T]) error
}

type reexportRecvStream[T any] struct {
	client rpc.Client
}

func (_ reexportRecvStream[T]) Recv(ctx context.Context, state *RecvStreamRecv[T]) error {
	panic("not implemented")
}

func (t reexportRecvStream[T]) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptRecvStream[T any](t RecvStream[T]) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "recv",
			InterfaceName: "RecvStream",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Recv(ctx, &RecvStreamRecv[T]{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type RecvStreamClient[T any] struct {
	rpc.Client
}

func NewRecvStreamClient[T any](client rpc.Client) *RecvStreamClient[T] {
	return &RecvStreamClient[T]{Client: client}
}

func (c RecvStreamClient[T]) Export() RecvStream[T] {
	return reexportRecvStream[T]{client: c.Client}
}

type RecvStreamClientRecvResults[T any] struct {
	client rpc.Client
	data   recvStreamRecvResultsData[T]
}

func (v *RecvStreamClientRecvResults[T]) HasValue() bool {
	return v.data.Value != nil
}

func (v *RecvStreamClientRecvResults[T]) Value() T {
	if v.data.Value == nil {
		return rpc.Zero[T]()
	}
	return *v.data.Value
}

func (v RecvStreamClient[T]) Recv(ctx context.Context, count int32) (*RecvStreamClientRecvResults[T], error) {
	args := RecvStreamRecvArgs[T]{}
	args.data.Count = &count

	var ret recvStreamRecvResultsData[T]

	err := v.Call(ctx, "recv", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &RecvStreamClientRecvResults[T]{client: v.Client, data: ret}, nil
}
