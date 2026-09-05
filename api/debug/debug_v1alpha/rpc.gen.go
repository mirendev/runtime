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
	SandboxId  *string             `cbor:"4,keyasint,omitempty" json:"sandbox_id,omitempty"`
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

func (v *IPLease) HasSandboxId() bool {
	return v.data.SandboxId != nil
}

func (v *IPLease) SandboxId() string {
	if v.data.SandboxId == nil {
		return ""
	}
	return *v.data.SandboxId
}

func (v *IPLease) SetSandboxId(sandbox_id string) {
	v.data.SandboxId = &sandbox_id
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
	Capacity *int32  `cbor:"4,keyasint,omitempty" json:"capacity,omitempty"`
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

type cloudSyncEventData struct {
	Kind    *string             `cbor:"0,keyasint,omitempty" json:"kind,omitempty"`
	At      *standard.Timestamp `cbor:"1,keyasint,omitempty" json:"at,omitempty"`
	Summary *string             `cbor:"2,keyasint,omitempty" json:"summary,omitempty"`
}

type CloudSyncEvent struct {
	data cloudSyncEventData
}

func (v *CloudSyncEvent) HasKind() bool {
	return v.data.Kind != nil
}

func (v *CloudSyncEvent) Kind() string {
	if v.data.Kind == nil {
		return ""
	}
	return *v.data.Kind
}

func (v *CloudSyncEvent) SetKind(kind string) {
	v.data.Kind = &kind
}

func (v *CloudSyncEvent) HasAt() bool {
	return v.data.At != nil
}

func (v *CloudSyncEvent) At() *standard.Timestamp {
	return v.data.At
}

func (v *CloudSyncEvent) SetAt(at *standard.Timestamp) {
	v.data.At = at
}

func (v *CloudSyncEvent) HasSummary() bool {
	return v.data.Summary != nil
}

func (v *CloudSyncEvent) Summary() string {
	if v.data.Summary == nil {
		return ""
	}
	return *v.data.Summary
}

func (v *CloudSyncEvent) SetSummary(summary string) {
	v.data.Summary = &summary
}

func (v *CloudSyncEvent) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CloudSyncEvent) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CloudSyncEvent) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CloudSyncEvent) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type cloudSyncFactData struct {
	Name  *string `cbor:"0,keyasint,omitempty" json:"name,omitempty"`
	Value *string `cbor:"1,keyasint,omitempty" json:"value,omitempty"`
}

type CloudSyncFact struct {
	data cloudSyncFactData
}

func (v *CloudSyncFact) HasName() bool {
	return v.data.Name != nil
}

func (v *CloudSyncFact) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *CloudSyncFact) SetName(name string) {
	v.data.Name = &name
}

func (v *CloudSyncFact) HasValue() bool {
	return v.data.Value != nil
}

func (v *CloudSyncFact) Value() string {
	if v.data.Value == nil {
		return ""
	}
	return *v.data.Value
}

func (v *CloudSyncFact) SetValue(value string) {
	v.data.Value = &value
}

func (v *CloudSyncFact) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CloudSyncFact) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CloudSyncFact) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CloudSyncFact) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type cloudSyncReportData struct {
	State   *string            `cbor:"0,keyasint,omitempty" json:"state,omitempty"`
	Summary *string            `cbor:"1,keyasint,omitempty" json:"summary,omitempty"`
	Facts   *[]*CloudSyncFact  `cbor:"2,keyasint,omitempty" json:"facts,omitempty"`
	Events  *[]*CloudSyncEvent `cbor:"3,keyasint,omitempty" json:"events,omitempty"`
}

type CloudSyncReport struct {
	data cloudSyncReportData
}

func (v *CloudSyncReport) HasState() bool {
	return v.data.State != nil
}

func (v *CloudSyncReport) State() string {
	if v.data.State == nil {
		return ""
	}
	return *v.data.State
}

func (v *CloudSyncReport) SetState(state string) {
	v.data.State = &state
}

func (v *CloudSyncReport) HasSummary() bool {
	return v.data.Summary != nil
}

func (v *CloudSyncReport) Summary() string {
	if v.data.Summary == nil {
		return ""
	}
	return *v.data.Summary
}

func (v *CloudSyncReport) SetSummary(summary string) {
	v.data.Summary = &summary
}

func (v *CloudSyncReport) HasFacts() bool {
	return v.data.Facts != nil
}

func (v *CloudSyncReport) Facts() []*CloudSyncFact {
	if v.data.Facts == nil {
		return nil
	}
	return *v.data.Facts
}

func (v *CloudSyncReport) SetFacts(facts []*CloudSyncFact) {
	x := slices.Clone(facts)
	v.data.Facts = &x
}

func (v *CloudSyncReport) HasEvents() bool {
	return v.data.Events != nil
}

func (v *CloudSyncReport) Events() []*CloudSyncEvent {
	if v.data.Events == nil {
		return nil
	}
	return *v.data.Events
}

func (v *CloudSyncReport) SetEvents(events []*CloudSyncEvent) {
	x := slices.Clone(events)
	v.data.Events = &x
}

func (v *CloudSyncReport) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CloudSyncReport) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CloudSyncReport) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CloudSyncReport) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type cloudSyncGetStatusArgsData struct{}

type CloudSyncGetStatusArgs struct {
	call rpc.Call
	data cloudSyncGetStatusArgsData
}

func (v *CloudSyncGetStatusArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CloudSyncGetStatusArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CloudSyncGetStatusArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CloudSyncGetStatusArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type cloudSyncGetStatusResultsData struct {
	Report *CloudSyncReport `cbor:"0,keyasint,omitempty" json:"report,omitempty"`
}

type CloudSyncGetStatusResults struct {
	call rpc.Call
	data cloudSyncGetStatusResultsData
}

func (v *CloudSyncGetStatusResults) SetReport(report *CloudSyncReport) {
	v.data.Report = report
}

func (v *CloudSyncGetStatusResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CloudSyncGetStatusResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CloudSyncGetStatusResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CloudSyncGetStatusResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type CloudSyncGetStatus struct {
	rpc.Call
	args    CloudSyncGetStatusArgs
	results CloudSyncGetStatusResults
}

func (t *CloudSyncGetStatus) Args() *CloudSyncGetStatusArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *CloudSyncGetStatus) Results() *CloudSyncGetStatusResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type CloudSync interface {
	GetStatus(ctx context.Context, state *CloudSyncGetStatus) error
}

type reexportCloudSync struct {
	client rpc.Client
}

func (reexportCloudSync) GetStatus(ctx context.Context, state *CloudSyncGetStatus) error {
	panic("not implemented")
}

func (t reexportCloudSync) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptCloudSync(t CloudSync) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "getStatus",
			InterfaceName: "CloudSync",
			Index:         0,
			Public:        false,
			Params:        []string{},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.GetStatus(ctx, &CloudSyncGetStatus{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type CloudSyncClient struct {
	rpc.Client
}

func NewCloudSyncClient(client rpc.Client) *CloudSyncClient {
	return &CloudSyncClient{Client: client}
}

func (c CloudSyncClient) Export() CloudSync {
	return reexportCloudSync{client: c.Client}
}

type CloudSyncClientGetStatusResults struct {
	client rpc.Client
	data   cloudSyncGetStatusResultsData
}

func (v *CloudSyncClientGetStatusResults) HasReport() bool {
	return v.data.Report != nil
}

func (v *CloudSyncClientGetStatusResults) Report() *CloudSyncReport {
	return v.data.Report
}

func (v CloudSyncClient) GetStatus(ctx context.Context) (*CloudSyncClientGetStatusResults, error) {
	args := CloudSyncGetStatusArgs{}

	var ret cloudSyncGetStatusResultsData

	err := v.Call(ctx, "getStatus", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &CloudSyncClientGetStatusResults{client: v.Client, data: ret}, nil
}

type netDBListLeasesArgsData struct {
	Subnet       *string `cbor:"0,keyasint,omitempty" json:"subnet,omitempty"`
	ReservedOnly *bool   `cbor:"1,keyasint,omitempty" json:"reserved_only,omitempty"`
	ReleasedOnly *bool   `cbor:"2,keyasint,omitempty" json:"released_only,omitempty"`
}

type NetDBListLeasesArgs struct {
	call rpc.Call
	data netDBListLeasesArgsData
}

func (v *NetDBListLeasesArgs) HasSubnet() bool {
	return v.data.Subnet != nil
}

func (v *NetDBListLeasesArgs) Subnet() string {
	if v.data.Subnet == nil {
		return ""
	}
	return *v.data.Subnet
}

func (v *NetDBListLeasesArgs) HasReservedOnly() bool {
	return v.data.ReservedOnly != nil
}

func (v *NetDBListLeasesArgs) ReservedOnly() bool {
	if v.data.ReservedOnly == nil {
		return false
	}
	return *v.data.ReservedOnly
}

func (v *NetDBListLeasesArgs) HasReleasedOnly() bool {
	return v.data.ReleasedOnly != nil
}

func (v *NetDBListLeasesArgs) ReleasedOnly() bool {
	if v.data.ReleasedOnly == nil {
		return false
	}
	return *v.data.ReleasedOnly
}

func (v *NetDBListLeasesArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *NetDBListLeasesArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *NetDBListLeasesArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *NetDBListLeasesArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type netDBListLeasesResultsData struct {
	Leases *[]*IPLease `cbor:"0,keyasint,omitempty" json:"leases,omitempty"`
}

type NetDBListLeasesResults struct {
	call rpc.Call
	data netDBListLeasesResultsData
}

func (v *NetDBListLeasesResults) SetLeases(leases []*IPLease) {
	x := slices.Clone(leases)
	v.data.Leases = &x
}

func (v *NetDBListLeasesResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *NetDBListLeasesResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *NetDBListLeasesResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *NetDBListLeasesResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type netDBStatusArgsData struct{}

type NetDBStatusArgs struct {
	call rpc.Call
	data netDBStatusArgsData
}

func (v *NetDBStatusArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *NetDBStatusArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *NetDBStatusArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *NetDBStatusArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type netDBStatusResultsData struct {
	Subnets *[]*SubnetStatus `cbor:"0,keyasint,omitempty" json:"subnets,omitempty"`
}

type NetDBStatusResults struct {
	call rpc.Call
	data netDBStatusResultsData
}

func (v *NetDBStatusResults) SetSubnets(subnets []*SubnetStatus) {
	x := slices.Clone(subnets)
	v.data.Subnets = &x
}

func (v *NetDBStatusResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *NetDBStatusResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *NetDBStatusResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *NetDBStatusResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type netDBReleaseIPArgsData struct {
	Ip *string `cbor:"0,keyasint,omitempty" json:"ip,omitempty"`
}

type NetDBReleaseIPArgs struct {
	call rpc.Call
	data netDBReleaseIPArgsData
}

func (v *NetDBReleaseIPArgs) HasIp() bool {
	return v.data.Ip != nil
}

func (v *NetDBReleaseIPArgs) Ip() string {
	if v.data.Ip == nil {
		return ""
	}
	return *v.data.Ip
}

func (v *NetDBReleaseIPArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *NetDBReleaseIPArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *NetDBReleaseIPArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *NetDBReleaseIPArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type netDBReleaseIPResultsData struct {
	Released *bool `cbor:"0,keyasint,omitempty" json:"released,omitempty"`
}

type NetDBReleaseIPResults struct {
	call rpc.Call
	data netDBReleaseIPResultsData
}

func (v *NetDBReleaseIPResults) SetReleased(released bool) {
	v.data.Released = &released
}

func (v *NetDBReleaseIPResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *NetDBReleaseIPResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *NetDBReleaseIPResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *NetDBReleaseIPResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type netDBReleaseSubnetArgsData struct {
	Subnet *string `cbor:"0,keyasint,omitempty" json:"subnet,omitempty"`
}

type NetDBReleaseSubnetArgs struct {
	call rpc.Call
	data netDBReleaseSubnetArgsData
}

func (v *NetDBReleaseSubnetArgs) HasSubnet() bool {
	return v.data.Subnet != nil
}

func (v *NetDBReleaseSubnetArgs) Subnet() string {
	if v.data.Subnet == nil {
		return ""
	}
	return *v.data.Subnet
}

func (v *NetDBReleaseSubnetArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *NetDBReleaseSubnetArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *NetDBReleaseSubnetArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *NetDBReleaseSubnetArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type netDBReleaseSubnetResultsData struct {
	Count *int32 `cbor:"0,keyasint,omitempty" json:"count,omitempty"`
}

type NetDBReleaseSubnetResults struct {
	call rpc.Call
	data netDBReleaseSubnetResultsData
}

func (v *NetDBReleaseSubnetResults) SetCount(count int32) {
	v.data.Count = &count
}

func (v *NetDBReleaseSubnetResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *NetDBReleaseSubnetResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *NetDBReleaseSubnetResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *NetDBReleaseSubnetResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type netDBReleaseAllArgsData struct{}

type NetDBReleaseAllArgs struct {
	call rpc.Call
	data netDBReleaseAllArgsData
}

func (v *NetDBReleaseAllArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *NetDBReleaseAllArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *NetDBReleaseAllArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *NetDBReleaseAllArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type netDBReleaseAllResultsData struct {
	Count *int32 `cbor:"0,keyasint,omitempty" json:"count,omitempty"`
}

type NetDBReleaseAllResults struct {
	call rpc.Call
	data netDBReleaseAllResultsData
}

func (v *NetDBReleaseAllResults) SetCount(count int32) {
	v.data.Count = &count
}

func (v *NetDBReleaseAllResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *NetDBReleaseAllResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *NetDBReleaseAllResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *NetDBReleaseAllResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type netDBGcArgsData struct {
	Subnet *string `cbor:"0,keyasint,omitempty" json:"subnet,omitempty"`
	DryRun *bool   `cbor:"1,keyasint,omitempty" json:"dry_run,omitempty"`
}

type NetDBGcArgs struct {
	call rpc.Call
	data netDBGcArgsData
}

func (v *NetDBGcArgs) HasSubnet() bool {
	return v.data.Subnet != nil
}

func (v *NetDBGcArgs) Subnet() string {
	if v.data.Subnet == nil {
		return ""
	}
	return *v.data.Subnet
}

func (v *NetDBGcArgs) HasDryRun() bool {
	return v.data.DryRun != nil
}

func (v *NetDBGcArgs) DryRun() bool {
	if v.data.DryRun == nil {
		return false
	}
	return *v.data.DryRun
}

func (v *NetDBGcArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *NetDBGcArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *NetDBGcArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *NetDBGcArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type netDBGcResultsData struct {
	OrphanedIps   *[]string `cbor:"0,keyasint,omitempty" json:"orphaned_ips,omitempty"`
	ReleasedCount *int32    `cbor:"1,keyasint,omitempty" json:"released_count,omitempty"`
}

type NetDBGcResults struct {
	call rpc.Call
	data netDBGcResultsData
}

func (v *NetDBGcResults) SetOrphanedIps(orphaned_ips []string) {
	x := slices.Clone(orphaned_ips)
	v.data.OrphanedIps = &x
}

func (v *NetDBGcResults) SetReleasedCount(released_count int32) {
	v.data.ReleasedCount = &released_count
}

func (v *NetDBGcResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *NetDBGcResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *NetDBGcResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *NetDBGcResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type NetDBListLeases struct {
	rpc.Call
	args    NetDBListLeasesArgs
	results NetDBListLeasesResults
}

func (t *NetDBListLeases) Args() *NetDBListLeasesArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *NetDBListLeases) Results() *NetDBListLeasesResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type NetDBStatus struct {
	rpc.Call
	args    NetDBStatusArgs
	results NetDBStatusResults
}

func (t *NetDBStatus) Args() *NetDBStatusArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *NetDBStatus) Results() *NetDBStatusResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type NetDBReleaseIP struct {
	rpc.Call
	args    NetDBReleaseIPArgs
	results NetDBReleaseIPResults
}

func (t *NetDBReleaseIP) Args() *NetDBReleaseIPArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *NetDBReleaseIP) Results() *NetDBReleaseIPResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type NetDBReleaseSubnet struct {
	rpc.Call
	args    NetDBReleaseSubnetArgs
	results NetDBReleaseSubnetResults
}

func (t *NetDBReleaseSubnet) Args() *NetDBReleaseSubnetArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *NetDBReleaseSubnet) Results() *NetDBReleaseSubnetResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type NetDBReleaseAll struct {
	rpc.Call
	args    NetDBReleaseAllArgs
	results NetDBReleaseAllResults
}

func (t *NetDBReleaseAll) Args() *NetDBReleaseAllArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *NetDBReleaseAll) Results() *NetDBReleaseAllResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type NetDBGc struct {
	rpc.Call
	args    NetDBGcArgs
	results NetDBGcResults
}

func (t *NetDBGc) Args() *NetDBGcArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *NetDBGc) Results() *NetDBGcResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type NetDB interface {
	ListLeases(ctx context.Context, state *NetDBListLeases) error
	Status(ctx context.Context, state *NetDBStatus) error
	ReleaseIP(ctx context.Context, state *NetDBReleaseIP) error
	ReleaseSubnet(ctx context.Context, state *NetDBReleaseSubnet) error
	ReleaseAll(ctx context.Context, state *NetDBReleaseAll) error
	Gc(ctx context.Context, state *NetDBGc) error
}

type reexportNetDB struct {
	client rpc.Client
}

func (reexportNetDB) ListLeases(ctx context.Context, state *NetDBListLeases) error {
	panic("not implemented")
}

func (reexportNetDB) Status(ctx context.Context, state *NetDBStatus) error {
	panic("not implemented")
}

func (reexportNetDB) ReleaseIP(ctx context.Context, state *NetDBReleaseIP) error {
	panic("not implemented")
}

func (reexportNetDB) ReleaseSubnet(ctx context.Context, state *NetDBReleaseSubnet) error {
	panic("not implemented")
}

func (reexportNetDB) ReleaseAll(ctx context.Context, state *NetDBReleaseAll) error {
	panic("not implemented")
}

func (reexportNetDB) Gc(ctx context.Context, state *NetDBGc) error {
	panic("not implemented")
}

func (t reexportNetDB) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptNetDB(t NetDB) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "listLeases",
			InterfaceName: "NetDB",
			Index:         0,
			Public:        false,
			Params:        []string{"subnet", "reserved_only", "released_only"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.ListLeases(ctx, &NetDBListLeases{Call: call})
			},
		},
		{
			Name:          "status",
			InterfaceName: "NetDB",
			Index:         0,
			Public:        false,
			Params:        []string{},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Status(ctx, &NetDBStatus{Call: call})
			},
		},
		{
			Name:          "releaseIP",
			InterfaceName: "NetDB",
			Index:         0,
			Public:        false,
			Params:        []string{"ip"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.ReleaseIP(ctx, &NetDBReleaseIP{Call: call})
			},
		},
		{
			Name:          "releaseSubnet",
			InterfaceName: "NetDB",
			Index:         0,
			Public:        false,
			Params:        []string{"subnet"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.ReleaseSubnet(ctx, &NetDBReleaseSubnet{Call: call})
			},
		},
		{
			Name:          "releaseAll",
			InterfaceName: "NetDB",
			Index:         0,
			Public:        false,
			Params:        []string{},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.ReleaseAll(ctx, &NetDBReleaseAll{Call: call})
			},
		},
		{
			Name:          "gc",
			InterfaceName: "NetDB",
			Index:         0,
			Public:        false,
			Params:        []string{"subnet", "dry_run"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Gc(ctx, &NetDBGc{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type NetDBClient struct {
	rpc.Client
}

func NewNetDBClient(client rpc.Client) *NetDBClient {
	return &NetDBClient{Client: client}
}

func (c NetDBClient) Export() NetDB {
	return reexportNetDB{client: c.Client}
}

type NetDBClientListLeasesResults struct {
	client rpc.Client
	data   netDBListLeasesResultsData
}

func (v *NetDBClientListLeasesResults) HasLeases() bool {
	return v.data.Leases != nil
}

func (v *NetDBClientListLeasesResults) Leases() []*IPLease {
	if v.data.Leases == nil {
		return nil
	}
	return *v.data.Leases
}

func (v NetDBClient) ListLeases(ctx context.Context, subnet string, reserved_only bool, released_only bool) (*NetDBClientListLeasesResults, error) {
	args := NetDBListLeasesArgs{}
	args.data.Subnet = &subnet
	args.data.ReservedOnly = &reserved_only
	args.data.ReleasedOnly = &released_only

	var ret netDBListLeasesResultsData

	err := v.Call(ctx, "listLeases", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &NetDBClientListLeasesResults{client: v.Client, data: ret}, nil
}

type NetDBClientStatusResults struct {
	client rpc.Client
	data   netDBStatusResultsData
}

func (v *NetDBClientStatusResults) HasSubnets() bool {
	return v.data.Subnets != nil
}

func (v *NetDBClientStatusResults) Subnets() []*SubnetStatus {
	if v.data.Subnets == nil {
		return nil
	}
	return *v.data.Subnets
}

func (v NetDBClient) Status(ctx context.Context) (*NetDBClientStatusResults, error) {
	args := NetDBStatusArgs{}

	var ret netDBStatusResultsData

	err := v.Call(ctx, "status", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &NetDBClientStatusResults{client: v.Client, data: ret}, nil
}

type NetDBClientReleaseIPResults struct {
	client rpc.Client
	data   netDBReleaseIPResultsData
}

func (v *NetDBClientReleaseIPResults) HasReleased() bool {
	return v.data.Released != nil
}

func (v *NetDBClientReleaseIPResults) Released() bool {
	if v.data.Released == nil {
		return false
	}
	return *v.data.Released
}

func (v NetDBClient) ReleaseIP(ctx context.Context, ip string) (*NetDBClientReleaseIPResults, error) {
	args := NetDBReleaseIPArgs{}
	args.data.Ip = &ip

	var ret netDBReleaseIPResultsData

	err := v.Call(ctx, "releaseIP", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &NetDBClientReleaseIPResults{client: v.Client, data: ret}, nil
}

type NetDBClientReleaseSubnetResults struct {
	client rpc.Client
	data   netDBReleaseSubnetResultsData
}

func (v *NetDBClientReleaseSubnetResults) HasCount() bool {
	return v.data.Count != nil
}

func (v *NetDBClientReleaseSubnetResults) Count() int32 {
	if v.data.Count == nil {
		return 0
	}
	return *v.data.Count
}

func (v NetDBClient) ReleaseSubnet(ctx context.Context, subnet string) (*NetDBClientReleaseSubnetResults, error) {
	args := NetDBReleaseSubnetArgs{}
	args.data.Subnet = &subnet

	var ret netDBReleaseSubnetResultsData

	err := v.Call(ctx, "releaseSubnet", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &NetDBClientReleaseSubnetResults{client: v.Client, data: ret}, nil
}

type NetDBClientReleaseAllResults struct {
	client rpc.Client
	data   netDBReleaseAllResultsData
}

func (v *NetDBClientReleaseAllResults) HasCount() bool {
	return v.data.Count != nil
}

func (v *NetDBClientReleaseAllResults) Count() int32 {
	if v.data.Count == nil {
		return 0
	}
	return *v.data.Count
}

func (v NetDBClient) ReleaseAll(ctx context.Context) (*NetDBClientReleaseAllResults, error) {
	args := NetDBReleaseAllArgs{}

	var ret netDBReleaseAllResultsData

	err := v.Call(ctx, "releaseAll", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &NetDBClientReleaseAllResults{client: v.Client, data: ret}, nil
}

type NetDBClientGcResults struct {
	client rpc.Client
	data   netDBGcResultsData
}

func (v *NetDBClientGcResults) HasOrphanedIps() bool {
	return v.data.OrphanedIps != nil
}

func (v *NetDBClientGcResults) OrphanedIps() []string {
	if v.data.OrphanedIps == nil {
		return nil
	}
	return *v.data.OrphanedIps
}

func (v *NetDBClientGcResults) HasReleasedCount() bool {
	return v.data.ReleasedCount != nil
}

func (v *NetDBClientGcResults) ReleasedCount() int32 {
	if v.data.ReleasedCount == nil {
		return 0
	}
	return *v.data.ReleasedCount
}

func (v NetDBClient) Gc(ctx context.Context, subnet string, dry_run bool) (*NetDBClientGcResults, error) {
	args := NetDBGcArgs{}
	args.data.Subnet = &subnet
	args.data.DryRun = &dry_run

	var ret netDBGcResultsData

	err := v.Call(ctx, "gc", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &NetDBClientGcResults{client: v.Client, data: ret}, nil
}
