package debug_v1alpha

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/fxamacker/cbor/v2"
	rpc "miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/standard"
)

type iPLeaseData struct {
	Ip         *string             `cbor:"0,keyasint,omitempty" json:"ip,omitempty"`
	Subnet     *string             `cbor:"1,keyasint,omitempty" json:"subnet,omitempty"`
	Reserved   *bool               `cbor:"2,keyasint,omitempty" json:"reserved,omitempty"`
	ReleasedAt *standard.Timestamp `cbor:"3,keyasint,omitempty" json:"released_at,omitempty"`
}

type IPLease struct {
	data iPLeaseData
}

func (v *IPLease) HasIp() bool {
	return v.data.Ip != nil
}

func (v *IPLease) Ip() string {
	if v.data.Ip == nil {
		return ""
	}
	return *v.data.Ip
}

func (v *IPLease) SetIp(ip string) {
	v.data.Ip = &ip
}

func (v *IPLease) HasSubnet() bool {
	return v.data.Subnet != nil
}

func (v *IPLease) Subnet() string {
	if v.data.Subnet == nil {
		return ""
	}
	return *v.data.Subnet
}

func (v *IPLease) SetSubnet(subnet string) {
	v.data.Subnet = &subnet
}

func (v *IPLease) HasReserved() bool {
	return v.data.Reserved != nil
}

func (v *IPLease) Reserved() bool {
	if v.data.Reserved == nil {
		return false
	}
	return *v.data.Reserved
}

func (v *IPLease) SetReserved(reserved bool) {
	v.data.Reserved = &reserved
}

func (v *IPLease) HasReleasedAt() bool {
	return v.data.ReleasedAt != nil
}

func (v *IPLease) ReleasedAt() *standard.Timestamp {
	return v.data.ReleasedAt
}

func (v *IPLease) SetReleasedAt(released_at *standard.Timestamp) {
	v.data.ReleasedAt = released_at
}

func (v *IPLease) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *IPLease) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *IPLease) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *IPLease) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type subnetStatusData struct {
	Subnet   *string `cbor:"0,keyasint,omitempty" json:"subnet,omitempty"`
	Total    *int32  `cbor:"1,keyasint,omitempty" json:"total,omitempty"`
	Reserved *int32  `cbor:"2,keyasint,omitempty" json:"reserved,omitempty"`
	Released *int32  `cbor:"3,keyasint,omitempty" json:"released,omitempty"`
	Stuck    *int32  `cbor:"4,keyasint,omitempty" json:"stuck,omitempty"`
	Capacity *int32  `cbor:"5,keyasint,omitempty" json:"capacity,omitempty"`
}

type SubnetStatus struct {
	data subnetStatusData
}

func (v *SubnetStatus) HasSubnet() bool {
	return v.data.Subnet != nil
}

func (v *SubnetStatus) Subnet() string {
	if v.data.Subnet == nil {
		return ""
	}
	return *v.data.Subnet
}

func (v *SubnetStatus) SetSubnet(subnet string) {
	v.data.Subnet = &subnet
}

func (v *SubnetStatus) HasTotal() bool {
	return v.data.Total != nil
}

func (v *SubnetStatus) Total() int32 {
	if v.data.Total == nil {
		return 0
	}
	return *v.data.Total
}

func (v *SubnetStatus) SetTotal(total int32) {
	v.data.Total = &total
}

func (v *SubnetStatus) HasReserved() bool {
	return v.data.Reserved != nil
}

func (v *SubnetStatus) Reserved() int32 {
	if v.data.Reserved == nil {
		return 0
	}
	return *v.data.Reserved
}

func (v *SubnetStatus) SetReserved(reserved int32) {
	v.data.Reserved = &reserved
}

func (v *SubnetStatus) HasReleased() bool {
	return v.data.Released != nil
}

func (v *SubnetStatus) Released() int32 {
	if v.data.Released == nil {
		return 0
	}
	return *v.data.Released
}

func (v *SubnetStatus) SetReleased(released int32) {
	v.data.Released = &released
}

func (v *SubnetStatus) HasStuck() bool {
	return v.data.Stuck != nil
}

func (v *SubnetStatus) Stuck() int32 {
	if v.data.Stuck == nil {
		return 0
	}
	return *v.data.Stuck
}

func (v *SubnetStatus) SetStuck(stuck int32) {
	v.data.Stuck = &stuck
}

func (v *SubnetStatus) HasCapacity() bool {
	return v.data.Capacity != nil
}

func (v *SubnetStatus) Capacity() int32 {
	if v.data.Capacity == nil {
		return 0
	}
	return *v.data.Capacity
}

func (v *SubnetStatus) SetCapacity(capacity int32) {
	v.data.Capacity = &capacity
}

func (v *SubnetStatus) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SubnetStatus) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SubnetStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SubnetStatus) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type iPAllocListLeasesArgsData struct {
	Subnet       *string `cbor:"0,keyasint,omitempty" json:"subnet,omitempty"`
	ReservedOnly *bool   `cbor:"1,keyasint,omitempty" json:"reserved_only,omitempty"`
	ReleasedOnly *bool   `cbor:"2,keyasint,omitempty" json:"released_only,omitempty"`
	StuckOnly    *bool   `cbor:"3,keyasint,omitempty" json:"stuck_only,omitempty"`
}

type IPAllocListLeasesArgs struct {
	call rpc.Call
	data iPAllocListLeasesArgsData
}

func (v *IPAllocListLeasesArgs) HasSubnet() bool {
	return v.data.Subnet != nil
}

func (v *IPAllocListLeasesArgs) Subnet() string {
	if v.data.Subnet == nil {
		return ""
	}
	return *v.data.Subnet
}

func (v *IPAllocListLeasesArgs) HasReservedOnly() bool {
	return v.data.ReservedOnly != nil
}

func (v *IPAllocListLeasesArgs) ReservedOnly() bool {
	if v.data.ReservedOnly == nil {
		return false
	}
	return *v.data.ReservedOnly
}

func (v *IPAllocListLeasesArgs) HasReleasedOnly() bool {
	return v.data.ReleasedOnly != nil
}

func (v *IPAllocListLeasesArgs) ReleasedOnly() bool {
	if v.data.ReleasedOnly == nil {
		return false
	}
	return *v.data.ReleasedOnly
}

func (v *IPAllocListLeasesArgs) HasStuckOnly() bool {
	return v.data.StuckOnly != nil
}

func (v *IPAllocListLeasesArgs) StuckOnly() bool {
	if v.data.StuckOnly == nil {
		return false
	}
	return *v.data.StuckOnly
}

func (v *IPAllocListLeasesArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *IPAllocListLeasesArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *IPAllocListLeasesArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *IPAllocListLeasesArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type iPAllocListLeasesResultsData struct {
	Leases *[]*IPLease `cbor:"0,keyasint,omitempty" json:"leases,omitempty"`
}

type IPAllocListLeasesResults struct {
	call rpc.Call
	data iPAllocListLeasesResultsData
}

func (v *IPAllocListLeasesResults) SetLeases(leases []*IPLease) {
	x := slices.Clone(leases)
	v.data.Leases = &x
}

func (v *IPAllocListLeasesResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *IPAllocListLeasesResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *IPAllocListLeasesResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *IPAllocListLeasesResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type iPAllocStatusArgsData struct{}

type IPAllocStatusArgs struct {
	call rpc.Call
	data iPAllocStatusArgsData
}

func (v *IPAllocStatusArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *IPAllocStatusArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *IPAllocStatusArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *IPAllocStatusArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type iPAllocStatusResultsData struct {
	Subnets *[]*SubnetStatus `cbor:"0,keyasint,omitempty" json:"subnets,omitempty"`
}

type IPAllocStatusResults struct {
	call rpc.Call
	data iPAllocStatusResultsData
}

func (v *IPAllocStatusResults) SetSubnets(subnets []*SubnetStatus) {
	x := slices.Clone(subnets)
	v.data.Subnets = &x
}

func (v *IPAllocStatusResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *IPAllocStatusResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *IPAllocStatusResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *IPAllocStatusResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type iPAllocReleaseIPArgsData struct {
	Ip *string `cbor:"0,keyasint,omitempty" json:"ip,omitempty"`
}

type IPAllocReleaseIPArgs struct {
	call rpc.Call
	data iPAllocReleaseIPArgsData
}

func (v *IPAllocReleaseIPArgs) HasIp() bool {
	return v.data.Ip != nil
}

func (v *IPAllocReleaseIPArgs) Ip() string {
	if v.data.Ip == nil {
		return ""
	}
	return *v.data.Ip
}

func (v *IPAllocReleaseIPArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *IPAllocReleaseIPArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *IPAllocReleaseIPArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *IPAllocReleaseIPArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type iPAllocReleaseIPResultsData struct {
	Released *bool `cbor:"0,keyasint,omitempty" json:"released,omitempty"`
}

type IPAllocReleaseIPResults struct {
	call rpc.Call
	data iPAllocReleaseIPResultsData
}

func (v *IPAllocReleaseIPResults) SetReleased(released bool) {
	v.data.Released = &released
}

func (v *IPAllocReleaseIPResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *IPAllocReleaseIPResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *IPAllocReleaseIPResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *IPAllocReleaseIPResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type iPAllocReleaseSubnetArgsData struct {
	Subnet *string `cbor:"0,keyasint,omitempty" json:"subnet,omitempty"`
}

type IPAllocReleaseSubnetArgs struct {
	call rpc.Call
	data iPAllocReleaseSubnetArgsData
}

func (v *IPAllocReleaseSubnetArgs) HasSubnet() bool {
	return v.data.Subnet != nil
}

func (v *IPAllocReleaseSubnetArgs) Subnet() string {
	if v.data.Subnet == nil {
		return ""
	}
	return *v.data.Subnet
}

func (v *IPAllocReleaseSubnetArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *IPAllocReleaseSubnetArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *IPAllocReleaseSubnetArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *IPAllocReleaseSubnetArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type iPAllocReleaseSubnetResultsData struct {
	Count *int32 `cbor:"0,keyasint,omitempty" json:"count,omitempty"`
}

type IPAllocReleaseSubnetResults struct {
	call rpc.Call
	data iPAllocReleaseSubnetResultsData
}

func (v *IPAllocReleaseSubnetResults) SetCount(count int32) {
	v.data.Count = &count
}

func (v *IPAllocReleaseSubnetResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *IPAllocReleaseSubnetResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *IPAllocReleaseSubnetResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *IPAllocReleaseSubnetResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type iPAllocReleaseAllArgsData struct{}

type IPAllocReleaseAllArgs struct {
	call rpc.Call
	data iPAllocReleaseAllArgsData
}

func (v *IPAllocReleaseAllArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *IPAllocReleaseAllArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *IPAllocReleaseAllArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *IPAllocReleaseAllArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type iPAllocReleaseAllResultsData struct {
	Count *int32 `cbor:"0,keyasint,omitempty" json:"count,omitempty"`
}

type IPAllocReleaseAllResults struct {
	call rpc.Call
	data iPAllocReleaseAllResultsData
}

func (v *IPAllocReleaseAllResults) SetCount(count int32) {
	v.data.Count = &count
}

func (v *IPAllocReleaseAllResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *IPAllocReleaseAllResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *IPAllocReleaseAllResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *IPAllocReleaseAllResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type iPAllocGcArgsData struct {
	Subnet *string `cbor:"0,keyasint,omitempty" json:"subnet,omitempty"`
	DryRun *bool   `cbor:"1,keyasint,omitempty" json:"dry_run,omitempty"`
}

type IPAllocGcArgs struct {
	call rpc.Call
	data iPAllocGcArgsData
}

func (v *IPAllocGcArgs) HasSubnet() bool {
	return v.data.Subnet != nil
}

func (v *IPAllocGcArgs) Subnet() string {
	if v.data.Subnet == nil {
		return ""
	}
	return *v.data.Subnet
}

func (v *IPAllocGcArgs) HasDryRun() bool {
	return v.data.DryRun != nil
}

func (v *IPAllocGcArgs) DryRun() bool {
	if v.data.DryRun == nil {
		return false
	}
	return *v.data.DryRun
}

func (v *IPAllocGcArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *IPAllocGcArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *IPAllocGcArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *IPAllocGcArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type iPAllocGcResultsData struct {
	OrphanedIps   *[]string `cbor:"0,keyasint,omitempty" json:"orphaned_ips,omitempty"`
	ReleasedCount *int32    `cbor:"1,keyasint,omitempty" json:"released_count,omitempty"`
}

type IPAllocGcResults struct {
	call rpc.Call
	data iPAllocGcResultsData
}

func (v *IPAllocGcResults) SetOrphanedIps(orphaned_ips []string) {
	x := slices.Clone(orphaned_ips)
	v.data.OrphanedIps = &x
}

func (v *IPAllocGcResults) SetReleasedCount(released_count int32) {
	v.data.ReleasedCount = &released_count
}

func (v *IPAllocGcResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *IPAllocGcResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *IPAllocGcResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *IPAllocGcResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type IPAllocListLeases struct {
	rpc.Call
	args    IPAllocListLeasesArgs
	results IPAllocListLeasesResults
}

func (t *IPAllocListLeases) Args() *IPAllocListLeasesArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *IPAllocListLeases) Results() *IPAllocListLeasesResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type IPAllocStatus struct {
	rpc.Call
	args    IPAllocStatusArgs
	results IPAllocStatusResults
}

func (t *IPAllocStatus) Args() *IPAllocStatusArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *IPAllocStatus) Results() *IPAllocStatusResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type IPAllocReleaseIP struct {
	rpc.Call
	args    IPAllocReleaseIPArgs
	results IPAllocReleaseIPResults
}

func (t *IPAllocReleaseIP) Args() *IPAllocReleaseIPArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *IPAllocReleaseIP) Results() *IPAllocReleaseIPResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type IPAllocReleaseSubnet struct {
	rpc.Call
	args    IPAllocReleaseSubnetArgs
	results IPAllocReleaseSubnetResults
}

func (t *IPAllocReleaseSubnet) Args() *IPAllocReleaseSubnetArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *IPAllocReleaseSubnet) Results() *IPAllocReleaseSubnetResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type IPAllocReleaseAll struct {
	rpc.Call
	args    IPAllocReleaseAllArgs
	results IPAllocReleaseAllResults
}

func (t *IPAllocReleaseAll) Args() *IPAllocReleaseAllArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *IPAllocReleaseAll) Results() *IPAllocReleaseAllResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type IPAllocGc struct {
	rpc.Call
	args    IPAllocGcArgs
	results IPAllocGcResults
}

func (t *IPAllocGc) Args() *IPAllocGcArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *IPAllocGc) Results() *IPAllocGcResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type IPAlloc interface {
	ListLeases(ctx context.Context, state *IPAllocListLeases) error
	Status(ctx context.Context, state *IPAllocStatus) error
	ReleaseIP(ctx context.Context, state *IPAllocReleaseIP) error
	ReleaseSubnet(ctx context.Context, state *IPAllocReleaseSubnet) error
	ReleaseAll(ctx context.Context, state *IPAllocReleaseAll) error
	Gc(ctx context.Context, state *IPAllocGc) error
}

type reexportIPAlloc struct {
	client rpc.Client
}

func (reexportIPAlloc) ListLeases(ctx context.Context, state *IPAllocListLeases) error {
	panic("not implemented")
}

func (reexportIPAlloc) Status(ctx context.Context, state *IPAllocStatus) error {
	panic("not implemented")
}

func (reexportIPAlloc) ReleaseIP(ctx context.Context, state *IPAllocReleaseIP) error {
	panic("not implemented")
}

func (reexportIPAlloc) ReleaseSubnet(ctx context.Context, state *IPAllocReleaseSubnet) error {
	panic("not implemented")
}

func (reexportIPAlloc) ReleaseAll(ctx context.Context, state *IPAllocReleaseAll) error {
	panic("not implemented")
}

func (reexportIPAlloc) Gc(ctx context.Context, state *IPAllocGc) error {
	panic("not implemented")
}

func (t reexportIPAlloc) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptIPAlloc(t IPAlloc) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "listLeases",
			InterfaceName: "IPAlloc",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.ListLeases(ctx, &IPAllocListLeases{Call: call})
			},
		},
		{
			Name:          "status",
			InterfaceName: "IPAlloc",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Status(ctx, &IPAllocStatus{Call: call})
			},
		},
		{
			Name:          "releaseIP",
			InterfaceName: "IPAlloc",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.ReleaseIP(ctx, &IPAllocReleaseIP{Call: call})
			},
		},
		{
			Name:          "releaseSubnet",
			InterfaceName: "IPAlloc",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.ReleaseSubnet(ctx, &IPAllocReleaseSubnet{Call: call})
			},
		},
		{
			Name:          "releaseAll",
			InterfaceName: "IPAlloc",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.ReleaseAll(ctx, &IPAllocReleaseAll{Call: call})
			},
		},
		{
			Name:          "gc",
			InterfaceName: "IPAlloc",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Gc(ctx, &IPAllocGc{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type IPAllocClient struct {
	rpc.Client
}

func NewIPAllocClient(client rpc.Client) *IPAllocClient {
	return &IPAllocClient{Client: client}
}

func (c IPAllocClient) Export() IPAlloc {
	return reexportIPAlloc{client: c.Client}
}

type IPAllocClientListLeasesResults struct {
	client rpc.Client
	data   iPAllocListLeasesResultsData
}

func (v *IPAllocClientListLeasesResults) HasLeases() bool {
	return v.data.Leases != nil
}

func (v *IPAllocClientListLeasesResults) Leases() []*IPLease {
	if v.data.Leases == nil {
		return nil
	}
	return *v.data.Leases
}

func (v IPAllocClient) ListLeases(ctx context.Context, subnet string, reserved_only bool, released_only bool, stuck_only bool) (*IPAllocClientListLeasesResults, error) {
	args := IPAllocListLeasesArgs{}
	args.data.Subnet = &subnet
	args.data.ReservedOnly = &reserved_only
	args.data.ReleasedOnly = &released_only
	args.data.StuckOnly = &stuck_only

	var ret iPAllocListLeasesResultsData

	err := v.Call(ctx, "listLeases", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &IPAllocClientListLeasesResults{client: v.Client, data: ret}, nil
}

type IPAllocClientStatusResults struct {
	client rpc.Client
	data   iPAllocStatusResultsData
}

func (v *IPAllocClientStatusResults) HasSubnets() bool {
	return v.data.Subnets != nil
}

func (v *IPAllocClientStatusResults) Subnets() []*SubnetStatus {
	if v.data.Subnets == nil {
		return nil
	}
	return *v.data.Subnets
}

func (v IPAllocClient) Status(ctx context.Context) (*IPAllocClientStatusResults, error) {
	args := IPAllocStatusArgs{}

	var ret iPAllocStatusResultsData

	err := v.Call(ctx, "status", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &IPAllocClientStatusResults{client: v.Client, data: ret}, nil
}

type IPAllocClientReleaseIPResults struct {
	client rpc.Client
	data   iPAllocReleaseIPResultsData
}

func (v *IPAllocClientReleaseIPResults) HasReleased() bool {
	return v.data.Released != nil
}

func (v *IPAllocClientReleaseIPResults) Released() bool {
	if v.data.Released == nil {
		return false
	}
	return *v.data.Released
}

func (v IPAllocClient) ReleaseIP(ctx context.Context, ip string) (*IPAllocClientReleaseIPResults, error) {
	args := IPAllocReleaseIPArgs{}
	args.data.Ip = &ip

	var ret iPAllocReleaseIPResultsData

	err := v.Call(ctx, "releaseIP", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &IPAllocClientReleaseIPResults{client: v.Client, data: ret}, nil
}

type IPAllocClientReleaseSubnetResults struct {
	client rpc.Client
	data   iPAllocReleaseSubnetResultsData
}

func (v *IPAllocClientReleaseSubnetResults) HasCount() bool {
	return v.data.Count != nil
}

func (v *IPAllocClientReleaseSubnetResults) Count() int32 {
	if v.data.Count == nil {
		return 0
	}
	return *v.data.Count
}

func (v IPAllocClient) ReleaseSubnet(ctx context.Context, subnet string) (*IPAllocClientReleaseSubnetResults, error) {
	args := IPAllocReleaseSubnetArgs{}
	args.data.Subnet = &subnet

	var ret iPAllocReleaseSubnetResultsData

	err := v.Call(ctx, "releaseSubnet", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &IPAllocClientReleaseSubnetResults{client: v.Client, data: ret}, nil
}

type IPAllocClientReleaseAllResults struct {
	client rpc.Client
	data   iPAllocReleaseAllResultsData
}

func (v *IPAllocClientReleaseAllResults) HasCount() bool {
	return v.data.Count != nil
}

func (v *IPAllocClientReleaseAllResults) Count() int32 {
	if v.data.Count == nil {
		return 0
	}
	return *v.data.Count
}

func (v IPAllocClient) ReleaseAll(ctx context.Context) (*IPAllocClientReleaseAllResults, error) {
	args := IPAllocReleaseAllArgs{}

	var ret iPAllocReleaseAllResultsData

	err := v.Call(ctx, "releaseAll", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &IPAllocClientReleaseAllResults{client: v.Client, data: ret}, nil
}

type IPAllocClientGcResults struct {
	client rpc.Client
	data   iPAllocGcResultsData
}

func (v *IPAllocClientGcResults) HasOrphanedIps() bool {
	return v.data.OrphanedIps != nil
}

func (v *IPAllocClientGcResults) OrphanedIps() []string {
	if v.data.OrphanedIps == nil {
		return nil
	}
	return *v.data.OrphanedIps
}

func (v *IPAllocClientGcResults) HasReleasedCount() bool {
	return v.data.ReleasedCount != nil
}

func (v *IPAllocClientGcResults) ReleasedCount() int32 {
	if v.data.ReleasedCount == nil {
		return 0
	}
	return *v.data.ReleasedCount
}

func (v IPAllocClient) Gc(ctx context.Context, subnet string, dry_run bool) (*IPAllocClientGcResults, error) {
	args := IPAllocGcArgs{}
	args.data.Subnet = &subnet
	args.data.DryRun = &dry_run

	var ret iPAllocGcResultsData

	err := v.Call(ctx, "gc", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &IPAllocClientGcResults{client: v.Client, data: ret}, nil
}
