package dataset

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/fxamacker/cbor/v2"
	rpc "miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/standard"
)

type dataSetInfoData struct {
	Id      *string   `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
	Name    *string   `cbor:"1,keyasint,omitempty" json:"name,omitempty"`
	Size    *int64    `cbor:"2,keyasint,omitempty" json:"size,omitempty"`
	Formats *[]string `cbor:"3,keyasint,omitempty" json:"formats,omitempty"`
}

type DataSetInfo struct {
	data dataSetInfoData
}

func (v *DataSetInfo) HasId() bool {
	return v.data.Id != nil
}

func (v *DataSetInfo) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *DataSetInfo) SetId(id string) {
	v.data.Id = &id
}

func (v *DataSetInfo) HasName() bool {
	return v.data.Name != nil
}

func (v *DataSetInfo) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *DataSetInfo) SetName(name string) {
	v.data.Name = &name
}

func (v *DataSetInfo) HasSize() bool {
	return v.data.Size != nil
}

func (v *DataSetInfo) Size() int64 {
	if v.data.Size == nil {
		return 0
	}
	return *v.data.Size
}

func (v *DataSetInfo) SetSize(size int64) {
	v.data.Size = &size
}

func (v *DataSetInfo) HasFormats() bool {
	return v.data.Formats != nil
}

func (v *DataSetInfo) Formats() []string {
	if v.data.Formats == nil {
		return nil
	}
	return *v.data.Formats
}

func (v *DataSetInfo) SetFormats(formats []string) {
	x := slices.Clone(formats)
	v.data.Formats = &x
}

func (v *DataSetInfo) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DataSetInfo) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DataSetInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DataSetInfo) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type segmentLayoutData struct {
	_       struct{} `cbor:",toarray"`
	Extents []Extent `cbor:"0,keyasint,omitempty" json:"extents,omitempty"`
}

type SegmentLayout struct {
	data segmentLayoutData
}

func (v *SegmentLayout) HasExtents() bool {
	return true
}

func (v *SegmentLayout) Extents() []Extent {
	return v.data.Extents
}

func (v *SegmentLayout) SetExtents(extents []Extent) {
	x := slices.Clone(extents)
	v.data.Extents = x
}

func (v *SegmentLayout) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SegmentLayout) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SegmentLayout) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SegmentLayout) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type extentData struct {
	_       struct{} `cbor:",toarray"`
	Lba     uint64   `cbor:"0,keyasint,omitempty" json:"lba,omitempty"`
	Blocks  uint32   `cbor:"1,keyasint,omitempty" json:"blocks,omitempty"`
	Size    uint32   `cbor:"2,keyasint,omitempty" json:"size,omitempty"`
	Offset  uint32   `cbor:"3,keyasint,omitempty" json:"offset,omitempty"`
	RawSize uint32   `cbor:"4,keyasint,omitempty" json:"raw_size,omitempty"`
}

type Extent struct {
	data extentData
}

func (v *Extent) HasLba() bool {
	return true
}

func (v *Extent) Lba() uint64 {
	return v.data.Lba
}

func (v *Extent) SetLba(lba uint64) {
	v.data.Lba = lba
}

func (v *Extent) HasBlocks() bool {
	return true
}

func (v *Extent) Blocks() uint32 {
	return v.data.Blocks
}

func (v *Extent) SetBlocks(blocks uint32) {
	v.data.Blocks = blocks
}

func (v *Extent) HasSize() bool {
	return true
}

func (v *Extent) Size() uint32 {
	return v.data.Size
}

func (v *Extent) SetSize(size uint32) {
	v.data.Size = size
}

func (v *Extent) HasOffset() bool {
	return true
}

func (v *Extent) Offset() uint32 {
	return v.data.Offset
}

func (v *Extent) SetOffset(offset uint32) {
	v.data.Offset = offset
}

func (v *Extent) HasRawSize() bool {
	return true
}

func (v *Extent) RawSize() uint32 {
	return v.data.RawSize
}

func (v *Extent) SetRawSize(raw_size uint32) {
	v.data.RawSize = raw_size
}

func (v *Extent) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *Extent) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *Extent) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *Extent) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type dataPathAccessData struct {
	Url *string            `cbor:"0,keyasint,omitempty" json:"url,omitempty"`
	Ttl *standard.Duration `cbor:"1,keyasint,omitempty" json:"ttl,omitempty"`
}

type DataPathAccess struct {
	data dataPathAccessData
}

func (v *DataPathAccess) HasUrl() bool {
	return v.data.Url != nil
}

func (v *DataPathAccess) Url() string {
	if v.data.Url == nil {
		return ""
	}
	return *v.data.Url
}

func (v *DataPathAccess) SetUrl(url string) {
	v.data.Url = &url
}

func (v *DataPathAccess) HasTtl() bool {
	return v.data.Ttl != nil
}

func (v *DataPathAccess) Ttl() *standard.Duration {
	return v.data.Ttl
}

func (v *DataPathAccess) SetTtl(ttl *standard.Duration) {
	v.data.Ttl = ttl
}

func (v *DataPathAccess) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DataPathAccess) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DataPathAccess) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DataPathAccess) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type segmentReaderReadAtArgsData struct {
	Offset *int64 `cbor:"0,keyasint,omitempty" json:"offset,omitempty"`
	Size   *int64 `cbor:"1,keyasint,omitempty" json:"size,omitempty"`
}

type SegmentReaderReadAtArgs struct {
	call rpc.Call
	data segmentReaderReadAtArgsData
}

func (v *SegmentReaderReadAtArgs) HasOffset() bool {
	return v.data.Offset != nil
}

func (v *SegmentReaderReadAtArgs) Offset() int64 {
	if v.data.Offset == nil {
		return 0
	}
	return *v.data.Offset
}

func (v *SegmentReaderReadAtArgs) HasSize() bool {
	return v.data.Size != nil
}

func (v *SegmentReaderReadAtArgs) Size() int64 {
	if v.data.Size == nil {
		return 0
	}
	return *v.data.Size
}

func (v *SegmentReaderReadAtArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SegmentReaderReadAtArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SegmentReaderReadAtArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SegmentReaderReadAtArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type segmentReaderReadAtResultsData struct {
	Data *[]byte `cbor:"0,keyasint,omitempty" json:"data,omitempty"`
}

type SegmentReaderReadAtResults struct {
	call rpc.Call
	data segmentReaderReadAtResultsData
}

func (v *SegmentReaderReadAtResults) SetData(data []byte) {
	x := slices.Clone(data)
	v.data.Data = &x
}

func (v *SegmentReaderReadAtResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SegmentReaderReadAtResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SegmentReaderReadAtResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SegmentReaderReadAtResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type segmentReaderCloseArgsData struct{}

type SegmentReaderCloseArgs struct {
	call rpc.Call
	data segmentReaderCloseArgsData
}

func (v *SegmentReaderCloseArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SegmentReaderCloseArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SegmentReaderCloseArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SegmentReaderCloseArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type segmentReaderCloseResultsData struct{}

type SegmentReaderCloseResults struct {
	call rpc.Call
	data segmentReaderCloseResultsData
}

func (v *SegmentReaderCloseResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SegmentReaderCloseResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SegmentReaderCloseResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SegmentReaderCloseResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type segmentReaderLayoutArgsData struct{}

type SegmentReaderLayoutArgs struct {
	call rpc.Call
	data segmentReaderLayoutArgsData
}

func (v *SegmentReaderLayoutArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SegmentReaderLayoutArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SegmentReaderLayoutArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SegmentReaderLayoutArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type segmentReaderLayoutResultsData struct {
	Layout *SegmentLayout `cbor:"0,keyasint,omitempty" json:"layout,omitempty"`
}

type SegmentReaderLayoutResults struct {
	call rpc.Call
	data segmentReaderLayoutResultsData
}

func (v *SegmentReaderLayoutResults) SetLayout(layout *SegmentLayout) {
	v.data.Layout = layout
}

func (v *SegmentReaderLayoutResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SegmentReaderLayoutResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SegmentReaderLayoutResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SegmentReaderLayoutResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type segmentReaderDataPathArgsData struct{}

type SegmentReaderDataPathArgs struct {
	call rpc.Call
	data segmentReaderDataPathArgsData
}

func (v *SegmentReaderDataPathArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SegmentReaderDataPathArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SegmentReaderDataPathArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SegmentReaderDataPathArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type segmentReaderDataPathResultsData struct {
	DataPath *DataPathAccess `cbor:"0,keyasint,omitempty" json:"dataPath,omitempty"`
}

type SegmentReaderDataPathResults struct {
	call rpc.Call
	data segmentReaderDataPathResultsData
}

func (v *SegmentReaderDataPathResults) SetDataPath(dataPath *DataPathAccess) {
	v.data.DataPath = dataPath
}

func (v *SegmentReaderDataPathResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SegmentReaderDataPathResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SegmentReaderDataPathResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SegmentReaderDataPathResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type SegmentReaderReadAt struct {
	rpc.Call
	args    SegmentReaderReadAtArgs
	results SegmentReaderReadAtResults
}

func (t *SegmentReaderReadAt) Args() *SegmentReaderReadAtArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *SegmentReaderReadAt) Results() *SegmentReaderReadAtResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type SegmentReaderClose struct {
	rpc.Call
	args    SegmentReaderCloseArgs
	results SegmentReaderCloseResults
}

func (t *SegmentReaderClose) Args() *SegmentReaderCloseArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *SegmentReaderClose) Results() *SegmentReaderCloseResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type SegmentReaderLayout struct {
	rpc.Call
	args    SegmentReaderLayoutArgs
	results SegmentReaderLayoutResults
}

func (t *SegmentReaderLayout) Args() *SegmentReaderLayoutArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *SegmentReaderLayout) Results() *SegmentReaderLayoutResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type SegmentReaderDataPath struct {
	rpc.Call
	args    SegmentReaderDataPathArgs
	results SegmentReaderDataPathResults
}

func (t *SegmentReaderDataPath) Args() *SegmentReaderDataPathArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *SegmentReaderDataPath) Results() *SegmentReaderDataPathResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type SegmentReader interface {
	ReadAt(ctx context.Context, state *SegmentReaderReadAt) error
	Close(ctx context.Context, state *SegmentReaderClose) error
	Layout(ctx context.Context, state *SegmentReaderLayout) error
	DataPath(ctx context.Context, state *SegmentReaderDataPath) error
}

type reexportSegmentReader struct {
	client rpc.Client
}

func (_ reexportSegmentReader) ReadAt(ctx context.Context, state *SegmentReaderReadAt) error {
	panic("not implemented")
}

func (_ reexportSegmentReader) Close(ctx context.Context, state *SegmentReaderClose) error {
	panic("not implemented")
}

func (_ reexportSegmentReader) Layout(ctx context.Context, state *SegmentReaderLayout) error {
	panic("not implemented")
}

func (_ reexportSegmentReader) DataPath(ctx context.Context, state *SegmentReaderDataPath) error {
	panic("not implemented")
}

func (t reexportSegmentReader) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptSegmentReader(t SegmentReader) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "readAt",
			InterfaceName: "SegmentReader",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.ReadAt(ctx, &SegmentReaderReadAt{Call: call})
			},
		},
		{
			Name:          "close",
			InterfaceName: "SegmentReader",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Close(ctx, &SegmentReaderClose{Call: call})
			},
		},
		{
			Name:          "layout",
			InterfaceName: "SegmentReader",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Layout(ctx, &SegmentReaderLayout{Call: call})
			},
		},
		{
			Name:          "dataPath",
			InterfaceName: "SegmentReader",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.DataPath(ctx, &SegmentReaderDataPath{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type SegmentReaderClient struct {
	rpc.Client
}

func NewSegmentReaderClient(client rpc.Client) *SegmentReaderClient {
	return &SegmentReaderClient{Client: client}
}

func (c SegmentReaderClient) Export() SegmentReader {
	return reexportSegmentReader{client: c.Client}
}

type SegmentReaderClientReadAtResults struct {
	client rpc.Client
	data   segmentReaderReadAtResultsData
}

func (v *SegmentReaderClientReadAtResults) HasData() bool {
	return v.data.Data != nil
}

func (v *SegmentReaderClientReadAtResults) Data() []byte {
	if v.data.Data == nil {
		return nil
	}
	return *v.data.Data
}

func (v SegmentReaderClient) ReadAt(ctx context.Context, offset int64, size int64) (*SegmentReaderClientReadAtResults, error) {
	args := SegmentReaderReadAtArgs{}
	args.data.Offset = &offset
	args.data.Size = &size

	var ret segmentReaderReadAtResultsData

	err := v.Client.Call(ctx, "readAt", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &SegmentReaderClientReadAtResults{client: v.Client, data: ret}, nil
}

type SegmentReaderClientCloseResults struct {
	client rpc.Client
	data   segmentReaderCloseResultsData
}

func (v SegmentReaderClient) Close(ctx context.Context) (*SegmentReaderClientCloseResults, error) {
	args := SegmentReaderCloseArgs{}

	var ret segmentReaderCloseResultsData

	err := v.Client.Call(ctx, "close", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &SegmentReaderClientCloseResults{client: v.Client, data: ret}, nil
}

type SegmentReaderClientLayoutResults struct {
	client rpc.Client
	data   segmentReaderLayoutResultsData
}

func (v *SegmentReaderClientLayoutResults) HasLayout() bool {
	return v.data.Layout != nil
}

func (v *SegmentReaderClientLayoutResults) Layout() *SegmentLayout {
	return v.data.Layout
}

func (v SegmentReaderClient) Layout(ctx context.Context) (*SegmentReaderClientLayoutResults, error) {
	args := SegmentReaderLayoutArgs{}

	var ret segmentReaderLayoutResultsData

	err := v.Client.Call(ctx, "layout", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &SegmentReaderClientLayoutResults{client: v.Client, data: ret}, nil
}

type SegmentReaderClientDataPathResults struct {
	client rpc.Client
	data   segmentReaderDataPathResultsData
}

func (v *SegmentReaderClientDataPathResults) HasDataPath() bool {
	return v.data.DataPath != nil
}

func (v *SegmentReaderClientDataPathResults) DataPath() *DataPathAccess {
	return v.data.DataPath
}

func (v SegmentReaderClient) DataPath(ctx context.Context) (*SegmentReaderClientDataPathResults, error) {
	args := SegmentReaderDataPathArgs{}

	var ret segmentReaderDataPathResultsData

	err := v.Client.Call(ctx, "dataPath", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &SegmentReaderClientDataPathResults{client: v.Client, data: ret}, nil
}

type segmentWriterWriteAtArgsData struct {
	Offset *int64  `cbor:"0,keyasint,omitempty" json:"offset,omitempty"`
	Data   *[]byte `cbor:"1,keyasint,omitempty" json:"data,omitempty"`
}

type SegmentWriterWriteAtArgs struct {
	call rpc.Call
	data segmentWriterWriteAtArgsData
}

func (v *SegmentWriterWriteAtArgs) HasOffset() bool {
	return v.data.Offset != nil
}

func (v *SegmentWriterWriteAtArgs) Offset() int64 {
	if v.data.Offset == nil {
		return 0
	}
	return *v.data.Offset
}

func (v *SegmentWriterWriteAtArgs) HasData() bool {
	return v.data.Data != nil
}

func (v *SegmentWriterWriteAtArgs) Data() []byte {
	if v.data.Data == nil {
		return nil
	}
	return *v.data.Data
}

func (v *SegmentWriterWriteAtArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SegmentWriterWriteAtArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SegmentWriterWriteAtArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SegmentWriterWriteAtArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type segmentWriterWriteAtResultsData struct {
	Count *int64 `cbor:"0,keyasint,omitempty" json:"count,omitempty"`
}

type SegmentWriterWriteAtResults struct {
	call rpc.Call
	data segmentWriterWriteAtResultsData
}

func (v *SegmentWriterWriteAtResults) SetCount(count int64) {
	v.data.Count = &count
}

func (v *SegmentWriterWriteAtResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SegmentWriterWriteAtResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SegmentWriterWriteAtResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SegmentWriterWriteAtResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type segmentWriterCloseArgsData struct{}

type SegmentWriterCloseArgs struct {
	call rpc.Call
	data segmentWriterCloseArgsData
}

func (v *SegmentWriterCloseArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SegmentWriterCloseArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SegmentWriterCloseArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SegmentWriterCloseArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type segmentWriterCloseResultsData struct{}

type SegmentWriterCloseResults struct {
	call rpc.Call
	data segmentWriterCloseResultsData
}

func (v *SegmentWriterCloseResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SegmentWriterCloseResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SegmentWriterCloseResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SegmentWriterCloseResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type SegmentWriterWriteAt struct {
	rpc.Call
	args    SegmentWriterWriteAtArgs
	results SegmentWriterWriteAtResults
}

func (t *SegmentWriterWriteAt) Args() *SegmentWriterWriteAtArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *SegmentWriterWriteAt) Results() *SegmentWriterWriteAtResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type SegmentWriterClose struct {
	rpc.Call
	args    SegmentWriterCloseArgs
	results SegmentWriterCloseResults
}

func (t *SegmentWriterClose) Args() *SegmentWriterCloseArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *SegmentWriterClose) Results() *SegmentWriterCloseResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type SegmentWriter interface {
	WriteAt(ctx context.Context, state *SegmentWriterWriteAt) error
	Close(ctx context.Context, state *SegmentWriterClose) error
}

type reexportSegmentWriter struct {
	client rpc.Client
}

func (_ reexportSegmentWriter) WriteAt(ctx context.Context, state *SegmentWriterWriteAt) error {
	panic("not implemented")
}

func (_ reexportSegmentWriter) Close(ctx context.Context, state *SegmentWriterClose) error {
	panic("not implemented")
}

func (t reexportSegmentWriter) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptSegmentWriter(t SegmentWriter) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "writeAt",
			InterfaceName: "SegmentWriter",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.WriteAt(ctx, &SegmentWriterWriteAt{Call: call})
			},
		},
		{
			Name:          "close",
			InterfaceName: "SegmentWriter",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Close(ctx, &SegmentWriterClose{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type SegmentWriterClient struct {
	rpc.Client
}

func NewSegmentWriterClient(client rpc.Client) *SegmentWriterClient {
	return &SegmentWriterClient{Client: client}
}

func (c SegmentWriterClient) Export() SegmentWriter {
	return reexportSegmentWriter{client: c.Client}
}

type SegmentWriterClientWriteAtResults struct {
	client rpc.Client
	data   segmentWriterWriteAtResultsData
}

func (v *SegmentWriterClientWriteAtResults) HasCount() bool {
	return v.data.Count != nil
}

func (v *SegmentWriterClientWriteAtResults) Count() int64 {
	if v.data.Count == nil {
		return 0
	}
	return *v.data.Count
}

func (v SegmentWriterClient) WriteAt(ctx context.Context, offset int64, data []byte) (*SegmentWriterClientWriteAtResults, error) {
	args := SegmentWriterWriteAtArgs{}
	args.data.Offset = &offset
	args.data.Data = &data

	var ret segmentWriterWriteAtResultsData

	err := v.Client.Call(ctx, "writeAt", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &SegmentWriterClientWriteAtResults{client: v.Client, data: ret}, nil
}

type SegmentWriterClientCloseResults struct {
	client rpc.Client
	data   segmentWriterCloseResultsData
}

func (v SegmentWriterClient) Close(ctx context.Context) (*SegmentWriterClientCloseResults, error) {
	args := SegmentWriterCloseArgs{}

	var ret segmentWriterCloseResultsData

	err := v.Client.Call(ctx, "close", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &SegmentWriterClientCloseResults{client: v.Client, data: ret}, nil
}

type dataSetGetInfoArgsData struct{}

type DataSetGetInfoArgs struct {
	call rpc.Call
	data dataSetGetInfoArgsData
}

func (v *DataSetGetInfoArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DataSetGetInfoArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DataSetGetInfoArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DataSetGetInfoArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type dataSetGetInfoResultsData struct {
	Info *DataSetInfo `cbor:"0,keyasint,omitempty" json:"info,omitempty"`
}

type DataSetGetInfoResults struct {
	call rpc.Call
	data dataSetGetInfoResultsData
}

func (v *DataSetGetInfoResults) SetInfo(info *DataSetInfo) {
	v.data.Info = info
}

func (v *DataSetGetInfoResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DataSetGetInfoResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DataSetGetInfoResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DataSetGetInfoResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type dataSetListSegmentsArgsData struct{}

type DataSetListSegmentsArgs struct {
	call rpc.Call
	data dataSetListSegmentsArgsData
}

func (v *DataSetListSegmentsArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DataSetListSegmentsArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DataSetListSegmentsArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DataSetListSegmentsArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type dataSetListSegmentsResultsData struct {
	Segments *[]string `cbor:"0,keyasint,omitempty" json:"segments,omitempty"`
}

type DataSetListSegmentsResults struct {
	call rpc.Call
	data dataSetListSegmentsResultsData
}

func (v *DataSetListSegmentsResults) SetSegments(segments []string) {
	x := slices.Clone(segments)
	v.data.Segments = &x
}

func (v *DataSetListSegmentsResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DataSetListSegmentsResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DataSetListSegmentsResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DataSetListSegmentsResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type dataSetReadSegmentArgsData struct {
	Id *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
}

type DataSetReadSegmentArgs struct {
	call rpc.Call
	data dataSetReadSegmentArgsData
}

func (v *DataSetReadSegmentArgs) HasId() bool {
	return v.data.Id != nil
}

func (v *DataSetReadSegmentArgs) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *DataSetReadSegmentArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DataSetReadSegmentArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DataSetReadSegmentArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DataSetReadSegmentArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type dataSetReadSegmentResultsData struct {
	Reader *rpc.Capability `cbor:"0,keyasint,omitempty" json:"reader,omitempty"`
}

type DataSetReadSegmentResults struct {
	call rpc.Call
	data dataSetReadSegmentResultsData
}

func (v *DataSetReadSegmentResults) SetReader(reader SegmentReader) {
	v.data.Reader = v.call.NewCapability(AdaptSegmentReader(reader))
}

func (v *DataSetReadSegmentResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DataSetReadSegmentResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DataSetReadSegmentResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DataSetReadSegmentResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type dataSetNewSegmentArgsData struct {
	Id     *string        `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
	Layout *SegmentLayout `cbor:"1,keyasint,omitempty" json:"layout,omitempty"`
}

type DataSetNewSegmentArgs struct {
	call rpc.Call
	data dataSetNewSegmentArgsData
}

func (v *DataSetNewSegmentArgs) HasId() bool {
	return v.data.Id != nil
}

func (v *DataSetNewSegmentArgs) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *DataSetNewSegmentArgs) HasLayout() bool {
	return v.data.Layout != nil
}

func (v *DataSetNewSegmentArgs) Layout() *SegmentLayout {
	return v.data.Layout
}

func (v *DataSetNewSegmentArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DataSetNewSegmentArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DataSetNewSegmentArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DataSetNewSegmentArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type dataSetNewSegmentResultsData struct {
	Writer *rpc.Capability `cbor:"0,keyasint,omitempty" json:"writer,omitempty"`
}

type DataSetNewSegmentResults struct {
	call rpc.Call
	data dataSetNewSegmentResultsData
}

func (v *DataSetNewSegmentResults) SetWriter(writer SegmentWriter) {
	v.data.Writer = v.call.NewCapability(AdaptSegmentWriter(writer))
}

func (v *DataSetNewSegmentResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DataSetNewSegmentResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DataSetNewSegmentResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DataSetNewSegmentResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type dataSetReadBytesArgsData struct {
	Offset *int64 `cbor:"0,keyasint,omitempty" json:"offset,omitempty"`
	Count  *int64 `cbor:"1,keyasint,omitempty" json:"count,omitempty"`
}

type DataSetReadBytesArgs struct {
	call rpc.Call
	data dataSetReadBytesArgsData
}

func (v *DataSetReadBytesArgs) HasOffset() bool {
	return v.data.Offset != nil
}

func (v *DataSetReadBytesArgs) Offset() int64 {
	if v.data.Offset == nil {
		return 0
	}
	return *v.data.Offset
}

func (v *DataSetReadBytesArgs) HasCount() bool {
	return v.data.Count != nil
}

func (v *DataSetReadBytesArgs) Count() int64 {
	if v.data.Count == nil {
		return 0
	}
	return *v.data.Count
}

func (v *DataSetReadBytesArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DataSetReadBytesArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DataSetReadBytesArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DataSetReadBytesArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type dataSetReadBytesResultsData struct {
	Data *[]byte `cbor:"0,keyasint,omitempty" json:"data,omitempty"`
}

type DataSetReadBytesResults struct {
	call rpc.Call
	data dataSetReadBytesResultsData
}

func (v *DataSetReadBytesResults) SetData(data []byte) {
	x := slices.Clone(data)
	v.data.Data = &x
}

func (v *DataSetReadBytesResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DataSetReadBytesResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DataSetReadBytesResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DataSetReadBytesResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type DataSetGetInfo struct {
	rpc.Call
	args    DataSetGetInfoArgs
	results DataSetGetInfoResults
}

func (t *DataSetGetInfo) Args() *DataSetGetInfoArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *DataSetGetInfo) Results() *DataSetGetInfoResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type DataSetListSegments struct {
	rpc.Call
	args    DataSetListSegmentsArgs
	results DataSetListSegmentsResults
}

func (t *DataSetListSegments) Args() *DataSetListSegmentsArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *DataSetListSegments) Results() *DataSetListSegmentsResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type DataSetReadSegment struct {
	rpc.Call
	args    DataSetReadSegmentArgs
	results DataSetReadSegmentResults
}

func (t *DataSetReadSegment) Args() *DataSetReadSegmentArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *DataSetReadSegment) Results() *DataSetReadSegmentResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type DataSetNewSegment struct {
	rpc.Call
	args    DataSetNewSegmentArgs
	results DataSetNewSegmentResults
}

func (t *DataSetNewSegment) Args() *DataSetNewSegmentArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *DataSetNewSegment) Results() *DataSetNewSegmentResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type DataSetReadBytes struct {
	rpc.Call
	args    DataSetReadBytesArgs
	results DataSetReadBytesResults
}

func (t *DataSetReadBytes) Args() *DataSetReadBytesArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *DataSetReadBytes) Results() *DataSetReadBytesResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type DataSet interface {
	GetInfo(ctx context.Context, state *DataSetGetInfo) error
	ListSegments(ctx context.Context, state *DataSetListSegments) error
	ReadSegment(ctx context.Context, state *DataSetReadSegment) error
	NewSegment(ctx context.Context, state *DataSetNewSegment) error
	ReadBytes(ctx context.Context, state *DataSetReadBytes) error
}

type reexportDataSet struct {
	client rpc.Client
}

func (_ reexportDataSet) GetInfo(ctx context.Context, state *DataSetGetInfo) error {
	panic("not implemented")
}

func (_ reexportDataSet) ListSegments(ctx context.Context, state *DataSetListSegments) error {
	panic("not implemented")
}

func (_ reexportDataSet) ReadSegment(ctx context.Context, state *DataSetReadSegment) error {
	panic("not implemented")
}

func (_ reexportDataSet) NewSegment(ctx context.Context, state *DataSetNewSegment) error {
	panic("not implemented")
}

func (_ reexportDataSet) ReadBytes(ctx context.Context, state *DataSetReadBytes) error {
	panic("not implemented")
}

func (t reexportDataSet) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptDataSet(t DataSet) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "getInfo",
			InterfaceName: "DataSet",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.GetInfo(ctx, &DataSetGetInfo{Call: call})
			},
		},
		{
			Name:          "listSegments",
			InterfaceName: "DataSet",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.ListSegments(ctx, &DataSetListSegments{Call: call})
			},
		},
		{
			Name:          "readSegment",
			InterfaceName: "DataSet",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.ReadSegment(ctx, &DataSetReadSegment{Call: call})
			},
		},
		{
			Name:          "newSegment",
			InterfaceName: "DataSet",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.NewSegment(ctx, &DataSetNewSegment{Call: call})
			},
		},
		{
			Name:          "readBytes",
			InterfaceName: "DataSet",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.ReadBytes(ctx, &DataSetReadBytes{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type DataSetClient struct {
	rpc.Client
}

func NewDataSetClient(client rpc.Client) *DataSetClient {
	return &DataSetClient{Client: client}
}

func (c DataSetClient) Export() DataSet {
	return reexportDataSet{client: c.Client}
}

type DataSetClientGetInfoResults struct {
	client rpc.Client
	data   dataSetGetInfoResultsData
}

func (v *DataSetClientGetInfoResults) HasInfo() bool {
	return v.data.Info != nil
}

func (v *DataSetClientGetInfoResults) Info() *DataSetInfo {
	return v.data.Info
}

func (v DataSetClient) GetInfo(ctx context.Context) (*DataSetClientGetInfoResults, error) {
	args := DataSetGetInfoArgs{}

	var ret dataSetGetInfoResultsData

	err := v.Client.Call(ctx, "getInfo", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &DataSetClientGetInfoResults{client: v.Client, data: ret}, nil
}

type DataSetClientListSegmentsResults struct {
	client rpc.Client
	data   dataSetListSegmentsResultsData
}

func (v *DataSetClientListSegmentsResults) HasSegments() bool {
	return v.data.Segments != nil
}

func (v *DataSetClientListSegmentsResults) Segments() []string {
	if v.data.Segments == nil {
		return nil
	}
	return *v.data.Segments
}

func (v DataSetClient) ListSegments(ctx context.Context) (*DataSetClientListSegmentsResults, error) {
	args := DataSetListSegmentsArgs{}

	var ret dataSetListSegmentsResultsData

	err := v.Client.Call(ctx, "listSegments", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &DataSetClientListSegmentsResults{client: v.Client, data: ret}, nil
}

type DataSetClientReadSegmentResults struct {
	client rpc.Client
	data   dataSetReadSegmentResultsData
}

func (v *DataSetClientReadSegmentResults) Reader() *SegmentReaderClient {
	return &SegmentReaderClient{
		Client: v.client.NewClient(v.data.Reader),
	}
}

func (v DataSetClient) ReadSegment(ctx context.Context, id string) (*DataSetClientReadSegmentResults, error) {
	args := DataSetReadSegmentArgs{}
	args.data.Id = &id

	var ret dataSetReadSegmentResultsData

	err := v.Client.Call(ctx, "readSegment", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &DataSetClientReadSegmentResults{client: v.Client, data: ret}, nil
}

type DataSetClientNewSegmentResults struct {
	client rpc.Client
	data   dataSetNewSegmentResultsData
}

func (v *DataSetClientNewSegmentResults) Writer() *SegmentWriterClient {
	return &SegmentWriterClient{
		Client: v.client.NewClient(v.data.Writer),
	}
}

func (v DataSetClient) NewSegment(ctx context.Context, id string, layout *SegmentLayout) (*DataSetClientNewSegmentResults, error) {
	args := DataSetNewSegmentArgs{}
	args.data.Id = &id
	args.data.Layout = layout

	var ret dataSetNewSegmentResultsData

	err := v.Client.Call(ctx, "newSegment", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &DataSetClientNewSegmentResults{client: v.Client, data: ret}, nil
}

type DataSetClientReadBytesResults struct {
	client rpc.Client
	data   dataSetReadBytesResultsData
}

func (v *DataSetClientReadBytesResults) HasData() bool {
	return v.data.Data != nil
}

func (v *DataSetClientReadBytesResults) Data() []byte {
	if v.data.Data == nil {
		return nil
	}
	return *v.data.Data
}

func (v DataSetClient) ReadBytes(ctx context.Context, offset int64, count int64) (*DataSetClientReadBytesResults, error) {
	args := DataSetReadBytesArgs{}
	args.data.Offset = &offset
	args.data.Count = &count

	var ret dataSetReadBytesResultsData

	err := v.Client.Call(ctx, "readBytes", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &DataSetClientReadBytesResults{client: v.Client, data: ret}, nil
}

type dataSetsListArgsData struct{}

type DataSetsListArgs struct {
	call rpc.Call
	data dataSetsListArgsData
}

func (v *DataSetsListArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DataSetsListArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DataSetsListArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DataSetsListArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type dataSetsListResultsData struct {
	Datasets *[]*DataSetInfo `cbor:"0,keyasint,omitempty" json:"datasets,omitempty"`
}

type DataSetsListResults struct {
	call rpc.Call
	data dataSetsListResultsData
}

func (v *DataSetsListResults) SetDatasets(datasets []*DataSetInfo) {
	x := slices.Clone(datasets)
	v.data.Datasets = &x
}

func (v *DataSetsListResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DataSetsListResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DataSetsListResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DataSetsListResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type dataSetsCreateArgsData struct {
	Info *DataSetInfo `cbor:"0,keyasint,omitempty" json:"info,omitempty"`
}

type DataSetsCreateArgs struct {
	call rpc.Call
	data dataSetsCreateArgsData
}

func (v *DataSetsCreateArgs) HasInfo() bool {
	return v.data.Info != nil
}

func (v *DataSetsCreateArgs) Info() *DataSetInfo {
	return v.data.Info
}

func (v *DataSetsCreateArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DataSetsCreateArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DataSetsCreateArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DataSetsCreateArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type dataSetsCreateResultsData struct {
	Dataset *rpc.Capability `cbor:"0,keyasint,omitempty" json:"dataset,omitempty"`
}

type DataSetsCreateResults struct {
	call rpc.Call
	data dataSetsCreateResultsData
}

func (v *DataSetsCreateResults) SetDataset(dataset DataSet) {
	v.data.Dataset = v.call.NewCapability(AdaptDataSet(dataset))
}

func (v *DataSetsCreateResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DataSetsCreateResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DataSetsCreateResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DataSetsCreateResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type dataSetsGetArgsData struct {
	Id *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
}

type DataSetsGetArgs struct {
	call rpc.Call
	data dataSetsGetArgsData
}

func (v *DataSetsGetArgs) HasId() bool {
	return v.data.Id != nil
}

func (v *DataSetsGetArgs) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *DataSetsGetArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DataSetsGetArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DataSetsGetArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DataSetsGetArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type dataSetsGetResultsData struct {
	Dataset *rpc.Capability `cbor:"0,keyasint,omitempty" json:"dataset,omitempty"`
}

type DataSetsGetResults struct {
	call rpc.Call
	data dataSetsGetResultsData
}

func (v *DataSetsGetResults) SetDataset(dataset DataSet) {
	v.data.Dataset = v.call.NewCapability(AdaptDataSet(dataset))
}

func (v *DataSetsGetResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DataSetsGetResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DataSetsGetResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DataSetsGetResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type dataSetsDeleteArgsData struct {
	Id *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
}

type DataSetsDeleteArgs struct {
	call rpc.Call
	data dataSetsDeleteArgsData
}

func (v *DataSetsDeleteArgs) HasId() bool {
	return v.data.Id != nil
}

func (v *DataSetsDeleteArgs) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *DataSetsDeleteArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DataSetsDeleteArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DataSetsDeleteArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DataSetsDeleteArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type dataSetsDeleteResultsData struct{}

type DataSetsDeleteResults struct {
	call rpc.Call
	data dataSetsDeleteResultsData
}

func (v *DataSetsDeleteResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DataSetsDeleteResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DataSetsDeleteResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DataSetsDeleteResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type DataSetsList struct {
	rpc.Call
	args    DataSetsListArgs
	results DataSetsListResults
}

func (t *DataSetsList) Args() *DataSetsListArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *DataSetsList) Results() *DataSetsListResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type DataSetsCreate struct {
	rpc.Call
	args    DataSetsCreateArgs
	results DataSetsCreateResults
}

func (t *DataSetsCreate) Args() *DataSetsCreateArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *DataSetsCreate) Results() *DataSetsCreateResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type DataSetsGet struct {
	rpc.Call
	args    DataSetsGetArgs
	results DataSetsGetResults
}

func (t *DataSetsGet) Args() *DataSetsGetArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *DataSetsGet) Results() *DataSetsGetResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type DataSetsDelete struct {
	rpc.Call
	args    DataSetsDeleteArgs
	results DataSetsDeleteResults
}

func (t *DataSetsDelete) Args() *DataSetsDeleteArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *DataSetsDelete) Results() *DataSetsDeleteResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type DataSets interface {
	List(ctx context.Context, state *DataSetsList) error
	Create(ctx context.Context, state *DataSetsCreate) error
	Get(ctx context.Context, state *DataSetsGet) error
	Delete(ctx context.Context, state *DataSetsDelete) error
}

type reexportDataSets struct {
	client rpc.Client
}

func (_ reexportDataSets) List(ctx context.Context, state *DataSetsList) error {
	panic("not implemented")
}

func (_ reexportDataSets) Create(ctx context.Context, state *DataSetsCreate) error {
	panic("not implemented")
}

func (_ reexportDataSets) Get(ctx context.Context, state *DataSetsGet) error {
	panic("not implemented")
}

func (_ reexportDataSets) Delete(ctx context.Context, state *DataSetsDelete) error {
	panic("not implemented")
}

func (t reexportDataSets) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptDataSets(t DataSets) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "list",
			InterfaceName: "DataSets",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.List(ctx, &DataSetsList{Call: call})
			},
		},
		{
			Name:          "create",
			InterfaceName: "DataSets",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Create(ctx, &DataSetsCreate{Call: call})
			},
		},
		{
			Name:          "get",
			InterfaceName: "DataSets",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Get(ctx, &DataSetsGet{Call: call})
			},
		},
		{
			Name:          "delete",
			InterfaceName: "DataSets",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Delete(ctx, &DataSetsDelete{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type DataSetsClient struct {
	rpc.Client
}

func NewDataSetsClient(client rpc.Client) *DataSetsClient {
	return &DataSetsClient{Client: client}
}

func (c DataSetsClient) Export() DataSets {
	return reexportDataSets{client: c.Client}
}

type DataSetsClientListResults struct {
	client rpc.Client
	data   dataSetsListResultsData
}

func (v *DataSetsClientListResults) HasDatasets() bool {
	return v.data.Datasets != nil
}

func (v *DataSetsClientListResults) Datasets() []*DataSetInfo {
	if v.data.Datasets == nil {
		return nil
	}
	return *v.data.Datasets
}

func (v DataSetsClient) List(ctx context.Context) (*DataSetsClientListResults, error) {
	args := DataSetsListArgs{}

	var ret dataSetsListResultsData

	err := v.Client.Call(ctx, "list", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &DataSetsClientListResults{client: v.Client, data: ret}, nil
}

type DataSetsClientCreateResults struct {
	client rpc.Client
	data   dataSetsCreateResultsData
}

func (v *DataSetsClientCreateResults) Dataset() *DataSetClient {
	return &DataSetClient{
		Client: v.client.NewClient(v.data.Dataset),
	}
}

func (v DataSetsClient) Create(ctx context.Context, info *DataSetInfo) (*DataSetsClientCreateResults, error) {
	args := DataSetsCreateArgs{}
	args.data.Info = info

	var ret dataSetsCreateResultsData

	err := v.Client.Call(ctx, "create", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &DataSetsClientCreateResults{client: v.Client, data: ret}, nil
}

type DataSetsClientGetResults struct {
	client rpc.Client
	data   dataSetsGetResultsData
}

func (v *DataSetsClientGetResults) Dataset() *DataSetClient {
	return &DataSetClient{
		Client: v.client.NewClient(v.data.Dataset),
	}
}

func (v DataSetsClient) Get(ctx context.Context, id string) (*DataSetsClientGetResults, error) {
	args := DataSetsGetArgs{}
	args.data.Id = &id

	var ret dataSetsGetResultsData

	err := v.Client.Call(ctx, "get", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &DataSetsClientGetResults{client: v.Client, data: ret}, nil
}

type DataSetsClientDeleteResults struct {
	client rpc.Client
	data   dataSetsDeleteResultsData
}

func (v DataSetsClient) Delete(ctx context.Context, id string) (*DataSetsClientDeleteResults, error) {
	args := DataSetsDeleteArgs{}
	args.data.Id = &id

	var ret dataSetsDeleteResultsData

	err := v.Client.Call(ctx, "delete", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &DataSetsClientDeleteResults{client: v.Client, data: ret}, nil
}
