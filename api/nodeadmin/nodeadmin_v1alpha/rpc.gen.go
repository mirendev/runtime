package nodeadmin_v1alpha

import (
	"context"
	"encoding/json"

	"github.com/fxamacker/cbor/v2"
	rpc "miren.dev/runtime/pkg/rpc"
)

type nodeAdminInstallDiskAcceleratorArgsData struct {
	Image *string `cbor:"0,keyasint,omitempty" json:"image,omitempty"`
	Force *bool   `cbor:"1,keyasint,omitempty" json:"force,omitempty"`
}

type NodeAdminInstallDiskAcceleratorArgs struct {
	call rpc.Call
	data nodeAdminInstallDiskAcceleratorArgsData
}

func (v *NodeAdminInstallDiskAcceleratorArgs) HasImage() bool {
	return v.data.Image != nil
}

func (v *NodeAdminInstallDiskAcceleratorArgs) Image() string {
	if v.data.Image == nil {
		return ""
	}
	return *v.data.Image
}

func (v *NodeAdminInstallDiskAcceleratorArgs) HasForce() bool {
	return v.data.Force != nil
}

func (v *NodeAdminInstallDiskAcceleratorArgs) Force() bool {
	if v.data.Force == nil {
		return false
	}
	return *v.data.Force
}

func (v *NodeAdminInstallDiskAcceleratorArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *NodeAdminInstallDiskAcceleratorArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *NodeAdminInstallDiskAcceleratorArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *NodeAdminInstallDiskAcceleratorArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type nodeAdminInstallDiskAcceleratorResultsData struct {
	KernelRelease *string `cbor:"0,keyasint,omitempty" json:"kernel_release,omitempty"`
	LbdVersion    *string `cbor:"1,keyasint,omitempty" json:"lbd_version,omitempty"`
	Error         *string `cbor:"2,keyasint,omitempty" json:"error,omitempty"`
}

type NodeAdminInstallDiskAcceleratorResults struct {
	call rpc.Call
	data nodeAdminInstallDiskAcceleratorResultsData
}

func (v *NodeAdminInstallDiskAcceleratorResults) SetKernelRelease(kernel_release string) {
	v.data.KernelRelease = &kernel_release
}

func (v *NodeAdminInstallDiskAcceleratorResults) SetLbdVersion(lbd_version string) {
	v.data.LbdVersion = &lbd_version
}

func (v *NodeAdminInstallDiskAcceleratorResults) SetError(error string) {
	v.data.Error = &error
}

func (v *NodeAdminInstallDiskAcceleratorResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *NodeAdminInstallDiskAcceleratorResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *NodeAdminInstallDiskAcceleratorResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *NodeAdminInstallDiskAcceleratorResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type NodeAdminInstallDiskAccelerator struct {
	rpc.Call
	args    NodeAdminInstallDiskAcceleratorArgs
	results NodeAdminInstallDiskAcceleratorResults
}

func (t *NodeAdminInstallDiskAccelerator) Args() *NodeAdminInstallDiskAcceleratorArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *NodeAdminInstallDiskAccelerator) Results() *NodeAdminInstallDiskAcceleratorResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type NodeAdmin interface {
	InstallDiskAccelerator(ctx context.Context, state *NodeAdminInstallDiskAccelerator) error
}

type reexportNodeAdmin struct {
	client rpc.Client
}

func (reexportNodeAdmin) InstallDiskAccelerator(ctx context.Context, state *NodeAdminInstallDiskAccelerator) error {
	panic("not implemented")
}

func (t reexportNodeAdmin) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptNodeAdmin(t NodeAdmin) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "install_disk_accelerator",
			InterfaceName: "NodeAdmin",
			Index:         0,
			Public:        false,
			Params:        []string{"image", "force"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.InstallDiskAccelerator(ctx, &NodeAdminInstallDiskAccelerator{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type NodeAdminClient struct {
	rpc.Client
}

func NewNodeAdminClient(client rpc.Client) *NodeAdminClient {
	return &NodeAdminClient{Client: client}
}

func (c NodeAdminClient) Export() NodeAdmin {
	return reexportNodeAdmin{client: c.Client}
}

type NodeAdminClientInstallDiskAcceleratorResults struct {
	client rpc.Client
	data   nodeAdminInstallDiskAcceleratorResultsData
}

func (v *NodeAdminClientInstallDiskAcceleratorResults) HasKernelRelease() bool {
	return v.data.KernelRelease != nil
}

func (v *NodeAdminClientInstallDiskAcceleratorResults) KernelRelease() string {
	if v.data.KernelRelease == nil {
		return ""
	}
	return *v.data.KernelRelease
}

func (v *NodeAdminClientInstallDiskAcceleratorResults) HasLbdVersion() bool {
	return v.data.LbdVersion != nil
}

func (v *NodeAdminClientInstallDiskAcceleratorResults) LbdVersion() string {
	if v.data.LbdVersion == nil {
		return ""
	}
	return *v.data.LbdVersion
}

func (v *NodeAdminClientInstallDiskAcceleratorResults) HasError() bool {
	return v.data.Error != nil
}

func (v *NodeAdminClientInstallDiskAcceleratorResults) Error() string {
	if v.data.Error == nil {
		return ""
	}
	return *v.data.Error
}

func (v NodeAdminClient) InstallDiskAccelerator(ctx context.Context, image string, force bool) (*NodeAdminClientInstallDiskAcceleratorResults, error) {
	args := NodeAdminInstallDiskAcceleratorArgs{}
	args.data.Image = &image
	args.data.Force = &force

	var ret nodeAdminInstallDiskAcceleratorResultsData

	err := v.Call(ctx, "install_disk_accelerator", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &NodeAdminClientInstallDiskAcceleratorResults{client: v.Client, data: ret}, nil
}
