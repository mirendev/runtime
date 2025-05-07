package generic

import (
	"context"
	"encoding/json"

	"github.com/fxamacker/cbor/v2"
	rpc "miren.dev/runtime/pkg/rpc"
)

type containerData[T any] struct {
	Value       *T      `cbor:"0,keyasint,omitempty" json:"value,omitempty"`
	Description *string `cbor:"1,keyasint,omitempty" json:"description,omitempty"`
}

type Container[T any] struct {
	data containerData[T]
}

func (v *Container[T]) HasValue() bool {
	return v.data.Value != nil
}

func (v *Container[T]) Value() *T {
	return v.data.Value
}

func (v *Container[T]) SetValue(value *T) {
	v.data.Value = value
}

func (v *Container[T]) HasDescription() bool {
	return v.data.Description != nil
}

func (v *Container[T]) Description() string {
	if v.data.Description == nil {
		return ""
	}
	return *v.data.Description
}

func (v *Container[T]) SetDescription(description string) {
	v.data.Description = &description
}

func (v *Container[T]) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *Container[T]) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *Container[T]) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *Container[T]) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type readerReadArgsData[T any] struct {
	Value *Container[T] `cbor:"0,keyasint,omitempty" json:"value,omitempty"`
}

type ReaderReadArgs[T any] struct {
	call rpc.Call
	data readerReadArgsData[T]
}

func (v *ReaderReadArgs[T]) HasValue() bool {
	return v.data.Value != nil
}

func (v *ReaderReadArgs[T]) Value() *Container[T] {
	return v.data.Value
}

func (v *ReaderReadArgs[T]) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ReaderReadArgs[T]) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ReaderReadArgs[T]) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ReaderReadArgs[T]) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type readerReadResultsData[T any] struct{}

type ReaderReadResults[T any] struct {
	call rpc.Call
	data readerReadResultsData[T]
}

func (v *ReaderReadResults[T]) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ReaderReadResults[T]) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ReaderReadResults[T]) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ReaderReadResults[T]) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type ReaderRead[T any] struct {
	rpc.Call
	args    ReaderReadArgs[T]
	results ReaderReadResults[T]
}

func (t *ReaderRead[T]) Args() *ReaderReadArgs[T] {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *ReaderRead[T]) Results() *ReaderReadResults[T] {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type Reader[T any] interface {
	Read(ctx context.Context, state *ReaderRead[T]) error
}

type reexportReader[T any] struct {
	client rpc.Client
}

func (_ reexportReader[T]) Read(ctx context.Context, state *ReaderRead[T]) error {
	panic("not implemented")
}

func (t reexportReader[T]) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptReader[T any](t Reader[T]) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "read",
			InterfaceName: "Reader",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Read(ctx, &ReaderRead[T]{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type ReaderClient[T any] struct {
	rpc.Client
}

func NewReaderClient[T any](client rpc.Client) *ReaderClient[T] {
	return &ReaderClient[T]{Client: client}
}

func (c ReaderClient[T]) Export() Reader[T] {
	return reexportReader[T]{client: c.Client}
}

type ReaderClientReadResults[T any] struct {
	client rpc.Client
	data   readerReadResultsData[T]
}

func (v ReaderClient[T]) Read(ctx context.Context, value *Container[T]) (*ReaderClientReadResults[T], error) {
	args := ReaderReadArgs[T]{}
	args.data.Value = value

	var ret readerReadResultsData[T]

	err := v.Client.Call(ctx, "read", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &ReaderClientReadResults[T]{client: v.Client, data: ret}, nil
}
