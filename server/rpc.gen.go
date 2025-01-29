package server

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/fxamacker/cbor/v2"
	rpc "miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/standard"
)

type cpuUsageData struct {
	Start *standard.Timestamp `cbor:"0,keyasint,omitempty" json:"start,omitempty"`
	Cores *float64            `cbor:"1,keyasint,omitempty" json:"cores,omitempty"`
}

type CpuUsage struct {
	data cpuUsageData
}

func (v *CpuUsage) HasStart() bool {
	return v.data.Start != nil
}

func (v *CpuUsage) Start() *standard.Timestamp {
	return v.data.Start
}

func (v *CpuUsage) SetStart(start *standard.Timestamp) {
	v.data.Start = start
}

func (v *CpuUsage) HasCores() bool {
	return v.data.Cores != nil
}

func (v *CpuUsage) Cores() float64 {
	if v.data.Cores == nil {
		return 0
	}
	return *v.data.Cores
}

func (v *CpuUsage) SetCores(cores float64) {
	v.data.Cores = &cores
}

func (v *CpuUsage) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CpuUsage) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CpuUsage) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CpuUsage) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type memoryUsageData struct {
	Timestamp *standard.Timestamp `cbor:"0,keyasint,omitempty" json:"timestamp,omitempty"`
	Bytes     *int64              `cbor:"1,keyasint,omitempty" json:"bytes,omitempty"`
}

type MemoryUsage struct {
	data memoryUsageData
}

func (v *MemoryUsage) HasTimestamp() bool {
	return v.data.Timestamp != nil
}

func (v *MemoryUsage) Timestamp() *standard.Timestamp {
	return v.data.Timestamp
}

func (v *MemoryUsage) SetTimestamp(timestamp *standard.Timestamp) {
	v.data.Timestamp = timestamp
}

func (v *MemoryUsage) HasBytes() bool {
	return v.data.Bytes != nil
}

func (v *MemoryUsage) Bytes() int64 {
	if v.data.Bytes == nil {
		return 0
	}
	return *v.data.Bytes
}

func (v *MemoryUsage) SetBytes(bytes int64) {
	v.data.Bytes = &bytes
}

func (v *MemoryUsage) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *MemoryUsage) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *MemoryUsage) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *MemoryUsage) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type poolStatusData struct {
	Name      *string          `cbor:"0,keyasint,omitempty" json:"name,omitempty"`
	Windows   *[]*WindowStatus `cbor:"1,keyasint,omitempty" json:"windows,omitempty"`
	Idle      *int32           `cbor:"2,keyasint,omitempty" json:"idle,omitempty"`
	IdleUsage *int64           `cbor:"3,keyasint,omitempty" json:"idle_usage,omitempty"`
}

type PoolStatus struct {
	data poolStatusData
}

func (v *PoolStatus) HasName() bool {
	return v.data.Name != nil
}

func (v *PoolStatus) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *PoolStatus) SetName(name string) {
	v.data.Name = &name
}

func (v *PoolStatus) HasWindows() bool {
	return v.data.Windows != nil
}

func (v *PoolStatus) Windows() []*WindowStatus {
	if v.data.Windows == nil {
		return nil
	}
	return *v.data.Windows
}

func (v *PoolStatus) SetWindows(windows []*WindowStatus) {
	x := slices.Clone(windows)
	v.data.Windows = &x
}

func (v *PoolStatus) HasIdle() bool {
	return v.data.Idle != nil
}

func (v *PoolStatus) Idle() int32 {
	if v.data.Idle == nil {
		return 0
	}
	return *v.data.Idle
}

func (v *PoolStatus) SetIdle(idle int32) {
	v.data.Idle = &idle
}

func (v *PoolStatus) HasIdleUsage() bool {
	return v.data.IdleUsage != nil
}

func (v *PoolStatus) IdleUsage() int64 {
	if v.data.IdleUsage == nil {
		return 0
	}
	return *v.data.IdleUsage
}

func (v *PoolStatus) SetIdleUsage(idleUsage int64) {
	v.data.IdleUsage = &idleUsage
}

func (v *PoolStatus) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *PoolStatus) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *PoolStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *PoolStatus) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type windowStatusData struct {
	Version *string `cbor:"0,keyasint,omitempty" json:"version,omitempty"`
	Leases  *int32  `cbor:"1,keyasint,omitempty" json:"leases,omitempty"`
	Usage   *int64  `cbor:"2,keyasint,omitempty" json:"usage,omitempty"`
}

type WindowStatus struct {
	data windowStatusData
}

func (v *WindowStatus) HasVersion() bool {
	return v.data.Version != nil
}

func (v *WindowStatus) Version() string {
	if v.data.Version == nil {
		return ""
	}
	return *v.data.Version
}

func (v *WindowStatus) SetVersion(version string) {
	v.data.Version = &version
}

func (v *WindowStatus) HasLeases() bool {
	return v.data.Leases != nil
}

func (v *WindowStatus) Leases() int32 {
	if v.data.Leases == nil {
		return 0
	}
	return *v.data.Leases
}

func (v *WindowStatus) SetLeases(leases int32) {
	v.data.Leases = &leases
}

func (v *WindowStatus) HasUsage() bool {
	return v.data.Usage != nil
}

func (v *WindowStatus) Usage() int64 {
	if v.data.Usage == nil {
		return 0
	}
	return *v.data.Usage
}

func (v *WindowStatus) SetUsage(usage int64) {
	v.data.Usage = &usage
}

func (v *WindowStatus) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *WindowStatus) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *WindowStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *WindowStatus) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type applicationStatusData struct {
	Name           *string             `cbor:"0,keyasint,omitempty" json:"name,omitempty"`
	Pools          *[]*PoolStatus      `cbor:"1,keyasint,omitempty" json:"pools,omitempty"`
	LastMinCPU     *float64            `cbor:"2,keyasint,omitempty" json:"last_min_c_p_u,omitempty"`
	LastHourCPU    *float64            `cbor:"3,keyasint,omitempty" json:"last_hour_c_p_u,omitempty"`
	LastDayCPU     *float64            `cbor:"4,keyasint,omitempty" json:"last_day_c_p_u,omitempty"`
	CpuOverHour    *[]*CpuUsage        `cbor:"5,keyasint,omitempty" json:"cpu_over_hour,omitempty"`
	MemoryOverHour *[]*MemoryUsage     `cbor:"6,keyasint,omitempty" json:"memory_over_hour,omitempty"`
	ActiveVersion  *string             `cbor:"7,keyasint,omitempty" json:"active_version,omitempty"`
	LastDeploy     *standard.Timestamp `cbor:"8,keyasint,omitempty" json:"last_deploy,omitempty"`
}

type ApplicationStatus struct {
	data applicationStatusData
}

func (v *ApplicationStatus) HasName() bool {
	return v.data.Name != nil
}

func (v *ApplicationStatus) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *ApplicationStatus) SetName(name string) {
	v.data.Name = &name
}

func (v *ApplicationStatus) HasPools() bool {
	return v.data.Pools != nil
}

func (v *ApplicationStatus) Pools() []*PoolStatus {
	if v.data.Pools == nil {
		return nil
	}
	return *v.data.Pools
}

func (v *ApplicationStatus) SetPools(pools []*PoolStatus) {
	x := slices.Clone(pools)
	v.data.Pools = &x
}

func (v *ApplicationStatus) HasLastMinCPU() bool {
	return v.data.LastMinCPU != nil
}

func (v *ApplicationStatus) LastMinCPU() float64 {
	if v.data.LastMinCPU == nil {
		return 0
	}
	return *v.data.LastMinCPU
}

func (v *ApplicationStatus) SetLastMinCPU(lastMinCPU float64) {
	v.data.LastMinCPU = &lastMinCPU
}

func (v *ApplicationStatus) HasLastHourCPU() bool {
	return v.data.LastHourCPU != nil
}

func (v *ApplicationStatus) LastHourCPU() float64 {
	if v.data.LastHourCPU == nil {
		return 0
	}
	return *v.data.LastHourCPU
}

func (v *ApplicationStatus) SetLastHourCPU(lastHourCPU float64) {
	v.data.LastHourCPU = &lastHourCPU
}

func (v *ApplicationStatus) HasLastDayCPU() bool {
	return v.data.LastDayCPU != nil
}

func (v *ApplicationStatus) LastDayCPU() float64 {
	if v.data.LastDayCPU == nil {
		return 0
	}
	return *v.data.LastDayCPU
}

func (v *ApplicationStatus) SetLastDayCPU(lastDayCPU float64) {
	v.data.LastDayCPU = &lastDayCPU
}

func (v *ApplicationStatus) HasCpuOverHour() bool {
	return v.data.CpuOverHour != nil
}

func (v *ApplicationStatus) CpuOverHour() []*CpuUsage {
	if v.data.CpuOverHour == nil {
		return nil
	}
	return *v.data.CpuOverHour
}

func (v *ApplicationStatus) SetCpuOverHour(cpuOverHour []*CpuUsage) {
	x := slices.Clone(cpuOverHour)
	v.data.CpuOverHour = &x
}

func (v *ApplicationStatus) HasMemoryOverHour() bool {
	return v.data.MemoryOverHour != nil
}

func (v *ApplicationStatus) MemoryOverHour() []*MemoryUsage {
	if v.data.MemoryOverHour == nil {
		return nil
	}
	return *v.data.MemoryOverHour
}

func (v *ApplicationStatus) SetMemoryOverHour(memoryOverHour []*MemoryUsage) {
	x := slices.Clone(memoryOverHour)
	v.data.MemoryOverHour = &x
}

func (v *ApplicationStatus) HasActiveVersion() bool {
	return v.data.ActiveVersion != nil
}

func (v *ApplicationStatus) ActiveVersion() string {
	if v.data.ActiveVersion == nil {
		return ""
	}
	return *v.data.ActiveVersion
}

func (v *ApplicationStatus) SetActiveVersion(activeVersion string) {
	v.data.ActiveVersion = &activeVersion
}

func (v *ApplicationStatus) HasLastDeploy() bool {
	return v.data.LastDeploy != nil
}

func (v *ApplicationStatus) LastDeploy() *standard.Timestamp {
	return v.data.LastDeploy
}

func (v *ApplicationStatus) SetLastDeploy(lastDeploy *standard.Timestamp) {
	v.data.LastDeploy = lastDeploy
}

func (v *ApplicationStatus) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ApplicationStatus) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ApplicationStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ApplicationStatus) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type appInfoAppInfoArgsData struct {
	Application *string `cbor:"0,keyasint,omitempty" json:"application,omitempty"`
}

type AppInfoAppInfoArgs struct {
	call *rpc.Call
	data appInfoAppInfoArgsData
}

func (v *AppInfoAppInfoArgs) HasApplication() bool {
	return v.data.Application != nil
}

func (v *AppInfoAppInfoArgs) Application() string {
	if v.data.Application == nil {
		return ""
	}
	return *v.data.Application
}

func (v *AppInfoAppInfoArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *AppInfoAppInfoArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *AppInfoAppInfoArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *AppInfoAppInfoArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type appInfoAppInfoResultsData struct {
	Status *ApplicationStatus `cbor:"0,keyasint,omitempty" json:"status,omitempty"`
}

type AppInfoAppInfoResults struct {
	call *rpc.Call
	data appInfoAppInfoResultsData
}

func (v *AppInfoAppInfoResults) SetStatus(status *ApplicationStatus) {
	v.data.Status = status
}

func (v *AppInfoAppInfoResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *AppInfoAppInfoResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *AppInfoAppInfoResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *AppInfoAppInfoResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type AppInfoAppInfo struct {
	*rpc.Call
	args    AppInfoAppInfoArgs
	results AppInfoAppInfoResults
}

func (t *AppInfoAppInfo) Args() *AppInfoAppInfoArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *AppInfoAppInfo) Results() *AppInfoAppInfoResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type AppInfo interface {
	AppInfo(ctx context.Context, state *AppInfoAppInfo) error
}

type reexportAppInfo struct {
	client *rpc.Client
}

func (_ reexportAppInfo) AppInfo(ctx context.Context, state *AppInfoAppInfo) error {
	panic("not implemented")
}

func (t reexportAppInfo) CapabilityClient() *rpc.Client {
	return t.client
}

func AdaptAppInfo(t AppInfo) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "appInfo",
			InterfaceName: "AppInfo",
			Index:         0,
			Handler: func(ctx context.Context, call *rpc.Call) error {
				return t.AppInfo(ctx, &AppInfoAppInfo{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type AppInfoClient struct {
	*rpc.Client
}

func (c AppInfoClient) Export() AppInfo {
	return reexportAppInfo{client: c.Client}
}

type AppInfoClientAppInfoResults struct {
	client *rpc.Client
	data   appInfoAppInfoResultsData
}

func (v *AppInfoClientAppInfoResults) HasStatus() bool {
	return v.data.Status != nil
}

func (v *AppInfoClientAppInfoResults) Status() *ApplicationStatus {
	return v.data.Status
}

func (v AppInfoClient) AppInfo(ctx context.Context, application string) (*AppInfoClientAppInfoResults, error) {
	args := AppInfoAppInfoArgs{}
	args.data.Application = &application

	var ret appInfoAppInfoResultsData

	err := v.Client.Call(ctx, "appInfo", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &AppInfoClientAppInfoResults{client: v.Client, data: ret}, nil
}
