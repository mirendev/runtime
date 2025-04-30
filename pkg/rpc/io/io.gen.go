package io

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/fxamacker/cbor/v2"
	rpc "miren.dev/runtime/pkg/rpc"
)

type readerReadArgsData struct {
	Count *int32 `cbor:"0,keyasint,omitempty" json:"count,omitempty"`
}

type ReaderReadArgs struct {
	call *rpc.NetworkCall
	data readerReadArgsData
}

func (v *ReaderReadArgs) HasCount() bool {
	return v.data.Count != nil
}

func (v *ReaderReadArgs) Count() int32 {
	if v.data.Count == nil {
		return 0
	}
	return *v.data.Count
}

func (v *ReaderReadArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ReaderReadArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ReaderReadArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ReaderReadArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type readerReadResultsData struct {
	Data *[]byte `cbor:"0,keyasint,omitempty" json:"data,omitempty"`
}

type ReaderReadResults struct {
	call *rpc.NetworkCall
	data readerReadResultsData
}

func (v *ReaderReadResults) SetData(data []byte) {
	x := slices.Clone(data)
	v.data.Data = &x
}

func (v *ReaderReadResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ReaderReadResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ReaderReadResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ReaderReadResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type ReaderRead struct {
	*rpc.NetworkCall
	args    ReaderReadArgs
	results ReaderReadResults
}

func (t *ReaderRead) Args() *ReaderReadArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.NetworkCall
	t.NetworkCall.Args(args)
	return args
}

func (t *ReaderRead) Results() *ReaderReadResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.NetworkCall
	t.NetworkCall.Results(results)
	return results
}

type Reader interface {
	Read(ctx context.Context, state *ReaderRead) error
}

type reexportReader struct {
	client *rpc.NetworkClient
}

func (_ reexportReader) Read(ctx context.Context, state *ReaderRead) error {
	panic("not implemented")
}

func (t reexportReader) CapabilityClient() *rpc.NetworkClient {
	return t.client
}

func AdaptReader(t Reader) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "Read",
			InterfaceName: "Reader",
			Index:         0,
			Handler: func(ctx context.Context, call *rpc.NetworkCall) error {
				return t.Read(ctx, &ReaderRead{NetworkCall: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type ReaderClient struct {
	*rpc.NetworkClient
}

func (c ReaderClient) Export() Reader {
	return reexportReader{client: c.NetworkClient}
}

type ReaderClientReadResults struct {
	client *rpc.NetworkClient
	data   readerReadResultsData
}

func (v *ReaderClientReadResults) HasData() bool {
	return v.data.Data != nil
}

func (v *ReaderClientReadResults) Data() []byte {
	if v.data.Data == nil {
		return nil
	}
	return *v.data.Data
}

func (v ReaderClient) Read(ctx context.Context, count int32) (*ReaderClientReadResults, error) {
	args := ReaderReadArgs{}
	args.data.Count = &count

	var ret readerReadResultsData

	err := v.NetworkClient.Call(ctx, "Read", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &ReaderClientReadResults{client: v.NetworkClient, data: ret}, nil
}

type readerAtReadAtArgsData struct {
	Count  *int32 `cbor:"0,keyasint,omitempty" json:"count,omitempty"`
	Offset *int64 `cbor:"1,keyasint,omitempty" json:"offset,omitempty"`
}

type ReaderAtReadAtArgs struct {
	call *rpc.NetworkCall
	data readerAtReadAtArgsData
}

func (v *ReaderAtReadAtArgs) HasCount() bool {
	return v.data.Count != nil
}

func (v *ReaderAtReadAtArgs) Count() int32 {
	if v.data.Count == nil {
		return 0
	}
	return *v.data.Count
}

func (v *ReaderAtReadAtArgs) HasOffset() bool {
	return v.data.Offset != nil
}

func (v *ReaderAtReadAtArgs) Offset() int64 {
	if v.data.Offset == nil {
		return 0
	}
	return *v.data.Offset
}

func (v *ReaderAtReadAtArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ReaderAtReadAtArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ReaderAtReadAtArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ReaderAtReadAtArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type readerAtReadAtResultsData struct {
	Data *[]byte `cbor:"0,keyasint,omitempty" json:"data,omitempty"`
}

type ReaderAtReadAtResults struct {
	call *rpc.NetworkCall
	data readerAtReadAtResultsData
}

func (v *ReaderAtReadAtResults) SetData(data []byte) {
	x := slices.Clone(data)
	v.data.Data = &x
}

func (v *ReaderAtReadAtResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ReaderAtReadAtResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ReaderAtReadAtResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ReaderAtReadAtResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type ReaderAtReadAt struct {
	*rpc.NetworkCall
	args    ReaderAtReadAtArgs
	results ReaderAtReadAtResults
}

func (t *ReaderAtReadAt) Args() *ReaderAtReadAtArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.NetworkCall
	t.NetworkCall.Args(args)
	return args
}

func (t *ReaderAtReadAt) Results() *ReaderAtReadAtResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.NetworkCall
	t.NetworkCall.Results(results)
	return results
}

type ReaderAt interface {
	ReadAt(ctx context.Context, state *ReaderAtReadAt) error
}

type reexportReaderAt struct {
	client *rpc.NetworkClient
}

func (_ reexportReaderAt) ReadAt(ctx context.Context, state *ReaderAtReadAt) error {
	panic("not implemented")
}

func (t reexportReaderAt) CapabilityClient() *rpc.NetworkClient {
	return t.client
}

func AdaptReaderAt(t ReaderAt) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "ReadAt",
			InterfaceName: "ReaderAt",
			Index:         0,
			Handler: func(ctx context.Context, call *rpc.NetworkCall) error {
				return t.ReadAt(ctx, &ReaderAtReadAt{NetworkCall: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type ReaderAtClient struct {
	*rpc.NetworkClient
}

func (c ReaderAtClient) Export() ReaderAt {
	return reexportReaderAt{client: c.NetworkClient}
}

type ReaderAtClientReadAtResults struct {
	client *rpc.NetworkClient
	data   readerAtReadAtResultsData
}

func (v *ReaderAtClientReadAtResults) HasData() bool {
	return v.data.Data != nil
}

func (v *ReaderAtClientReadAtResults) Data() []byte {
	if v.data.Data == nil {
		return nil
	}
	return *v.data.Data
}

func (v ReaderAtClient) ReadAt(ctx context.Context, count int32, offset int64) (*ReaderAtClientReadAtResults, error) {
	args := ReaderAtReadAtArgs{}
	args.data.Count = &count
	args.data.Offset = &offset

	var ret readerAtReadAtResultsData

	err := v.NetworkClient.Call(ctx, "ReadAt", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &ReaderAtClientReadAtResults{client: v.NetworkClient, data: ret}, nil
}

type writerWriteArgsData struct {
	Data *[]byte `cbor:"0,keyasint,omitempty" json:"data,omitempty"`
}

type WriterWriteArgs struct {
	call *rpc.NetworkCall
	data writerWriteArgsData
}

func (v *WriterWriteArgs) HasData() bool {
	return v.data.Data != nil
}

func (v *WriterWriteArgs) Data() []byte {
	if v.data.Data == nil {
		return nil
	}
	return *v.data.Data
}

func (v *WriterWriteArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *WriterWriteArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *WriterWriteArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *WriterWriteArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type writerWriteResultsData struct {
	Count *int32 `cbor:"0,keyasint,omitempty" json:"count,omitempty"`
}

type WriterWriteResults struct {
	call *rpc.NetworkCall
	data writerWriteResultsData
}

func (v *WriterWriteResults) SetCount(count int32) {
	v.data.Count = &count
}

func (v *WriterWriteResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *WriterWriteResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *WriterWriteResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *WriterWriteResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type WriterWrite struct {
	*rpc.NetworkCall
	args    WriterWriteArgs
	results WriterWriteResults
}

func (t *WriterWrite) Args() *WriterWriteArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.NetworkCall
	t.NetworkCall.Args(args)
	return args
}

func (t *WriterWrite) Results() *WriterWriteResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.NetworkCall
	t.NetworkCall.Results(results)
	return results
}

type Writer interface {
	Write(ctx context.Context, state *WriterWrite) error
}

type reexportWriter struct {
	client *rpc.NetworkClient
}

func (_ reexportWriter) Write(ctx context.Context, state *WriterWrite) error {
	panic("not implemented")
}

func (t reexportWriter) CapabilityClient() *rpc.NetworkClient {
	return t.client
}

func AdaptWriter(t Writer) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "Write",
			InterfaceName: "Writer",
			Index:         0,
			Handler: func(ctx context.Context, call *rpc.NetworkCall) error {
				return t.Write(ctx, &WriterWrite{NetworkCall: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type WriterClient struct {
	*rpc.NetworkClient
}

func (c WriterClient) Export() Writer {
	return reexportWriter{client: c.NetworkClient}
}

type WriterClientWriteResults struct {
	client *rpc.NetworkClient
	data   writerWriteResultsData
}

func (v *WriterClientWriteResults) HasCount() bool {
	return v.data.Count != nil
}

func (v *WriterClientWriteResults) Count() int32 {
	if v.data.Count == nil {
		return 0
	}
	return *v.data.Count
}

func (v WriterClient) Write(ctx context.Context, data []byte) (*WriterClientWriteResults, error) {
	args := WriterWriteArgs{}
	args.data.Data = &data

	var ret writerWriteResultsData

	err := v.NetworkClient.Call(ctx, "Write", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &WriterClientWriteResults{client: v.NetworkClient, data: ret}, nil
}
