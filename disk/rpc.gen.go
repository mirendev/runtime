package disk

import (
	"context"
	"encoding/json"

	"github.com/fxamacker/cbor/v2"
	rpc "miren.dev/runtime/pkg/rpc"
)

type diskStatusData struct {
	BlocksWritten   *int64 `cbor:"0,keyasint,omitempty" json:"blocks_written,omitempty"`
	BlocksRead      *int64 `cbor:"1,keyasint,omitempty" json:"blocks_read,omitempty"`
	Iops            *int64 `cbor:"2,keyasint,omitempty" json:"iops,omitempty"`
	SegmentsWritten *int64 `cbor:"3,keyasint,omitempty" json:"segments_written,omitempty"`
}

type DiskStatus struct {
	data diskStatusData
}

func (v *DiskStatus) HasBlocksWritten() bool {
	return v.data.BlocksWritten != nil
}

func (v *DiskStatus) BlocksWritten() int64 {
	if v.data.BlocksWritten == nil {
		return 0
	}
	return *v.data.BlocksWritten
}

func (v *DiskStatus) SetBlocksWritten(blocks_written int64) {
	v.data.BlocksWritten = &blocks_written
}

func (v *DiskStatus) HasBlocksRead() bool {
	return v.data.BlocksRead != nil
}

func (v *DiskStatus) BlocksRead() int64 {
	if v.data.BlocksRead == nil {
		return 0
	}
	return *v.data.BlocksRead
}

func (v *DiskStatus) SetBlocksRead(blocks_read int64) {
	v.data.BlocksRead = &blocks_read
}

func (v *DiskStatus) HasIops() bool {
	return v.data.Iops != nil
}

func (v *DiskStatus) Iops() int64 {
	if v.data.Iops == nil {
		return 0
	}
	return *v.data.Iops
}

func (v *DiskStatus) SetIops(iops int64) {
	v.data.Iops = &iops
}

func (v *DiskStatus) HasSegmentsWritten() bool {
	return v.data.SegmentsWritten != nil
}

func (v *DiskStatus) SegmentsWritten() int64 {
	if v.data.SegmentsWritten == nil {
		return 0
	}
	return *v.data.SegmentsWritten
}

func (v *DiskStatus) SetSegmentsWritten(segments_written int64) {
	v.data.SegmentsWritten = &segments_written
}

func (v *DiskStatus) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DiskStatus) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DiskStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DiskStatus) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type diskManagementUnmountArgsData struct {
	Id *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
}

type DiskManagementUnmountArgs struct {
	call rpc.Call
	data diskManagementUnmountArgsData
}

func (v *DiskManagementUnmountArgs) HasId() bool {
	return v.data.Id != nil
}

func (v *DiskManagementUnmountArgs) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *DiskManagementUnmountArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DiskManagementUnmountArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DiskManagementUnmountArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DiskManagementUnmountArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type diskManagementUnmountResultsData struct{}

type DiskManagementUnmountResults struct {
	call rpc.Call
	data diskManagementUnmountResultsData
}

func (v *DiskManagementUnmountResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DiskManagementUnmountResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DiskManagementUnmountResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DiskManagementUnmountResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type diskManagementStatusArgsData struct {
	Id *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
}

type DiskManagementStatusArgs struct {
	call rpc.Call
	data diskManagementStatusArgsData
}

func (v *DiskManagementStatusArgs) HasId() bool {
	return v.data.Id != nil
}

func (v *DiskManagementStatusArgs) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *DiskManagementStatusArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DiskManagementStatusArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DiskManagementStatusArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DiskManagementStatusArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type diskManagementStatusResultsData struct {
	Status *DiskStatus `cbor:"0,keyasint,omitempty" json:"status,omitempty"`
}

type DiskManagementStatusResults struct {
	call rpc.Call
	data diskManagementStatusResultsData
}

func (v *DiskManagementStatusResults) SetStatus(status *DiskStatus) {
	v.data.Status = status
}

func (v *DiskManagementStatusResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DiskManagementStatusResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DiskManagementStatusResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DiskManagementStatusResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type DiskManagementUnmount struct {
	rpc.Call
	args    DiskManagementUnmountArgs
	results DiskManagementUnmountResults
}

func (t *DiskManagementUnmount) Args() *DiskManagementUnmountArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *DiskManagementUnmount) Results() *DiskManagementUnmountResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type DiskManagementStatus struct {
	rpc.Call
	args    DiskManagementStatusArgs
	results DiskManagementStatusResults
}

func (t *DiskManagementStatus) Args() *DiskManagementStatusArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *DiskManagementStatus) Results() *DiskManagementStatusResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type DiskManagement interface {
	Unmount(ctx context.Context, state *DiskManagementUnmount) error
	Status(ctx context.Context, state *DiskManagementStatus) error
}

type reexportDiskManagement struct {
	client rpc.Client
}

func (_ reexportDiskManagement) Unmount(ctx context.Context, state *DiskManagementUnmount) error {
	panic("not implemented")
}

func (_ reexportDiskManagement) Status(ctx context.Context, state *DiskManagementStatus) error {
	panic("not implemented")
}

func (t reexportDiskManagement) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptDiskManagement(t DiskManagement) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "unmount",
			InterfaceName: "DiskManagement",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Unmount(ctx, &DiskManagementUnmount{Call: call})
			},
		},
		{
			Name:          "status",
			InterfaceName: "DiskManagement",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Status(ctx, &DiskManagementStatus{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type DiskManagementClient struct {
	rpc.Client
}

func NewDiskManagementClient(client rpc.Client) *DiskManagementClient {
	return &DiskManagementClient{Client: client}
}

func (c DiskManagementClient) Export() DiskManagement {
	return reexportDiskManagement{client: c.Client}
}

type DiskManagementClientUnmountResults struct {
	client rpc.Client
	data   diskManagementUnmountResultsData
}

func (v DiskManagementClient) Unmount(ctx context.Context, id string) (*DiskManagementClientUnmountResults, error) {
	args := DiskManagementUnmountArgs{}
	args.data.Id = &id

	var ret diskManagementUnmountResultsData

	err := v.Call(ctx, "unmount", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &DiskManagementClientUnmountResults{client: v.Client, data: ret}, nil
}

type DiskManagementClientStatusResults struct {
	client rpc.Client
	data   diskManagementStatusResultsData
}

func (v *DiskManagementClientStatusResults) HasStatus() bool {
	return v.data.Status != nil
}

func (v *DiskManagementClientStatusResults) Status() *DiskStatus {
	return v.data.Status
}

func (v DiskManagementClient) Status(ctx context.Context, id string) (*DiskManagementClientStatusResults, error) {
	args := DiskManagementStatusArgs{}
	args.data.Id = &id

	var ret diskManagementStatusResultsData

	err := v.Call(ctx, "status", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &DiskManagementClientStatusResults{client: v.Client, data: ret}, nil
}
