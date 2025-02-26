package api

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
	Addons         *[]*AddonInstance   `cbor:"9,keyasint,omitempty" json:"addons,omitempty"`
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

func (v *ApplicationStatus) HasAddons() bool {
	return v.data.Addons != nil
}

func (v *ApplicationStatus) Addons() []*AddonInstance {
	if v.data.Addons == nil {
		return nil
	}
	return *v.data.Addons
}

func (v *ApplicationStatus) SetAddons(addons []*AddonInstance) {
	x := slices.Clone(addons)
	v.data.Addons = &x
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

type logEntryData struct {
	Timestamp *standard.Timestamp `cbor:"0,keyasint,omitempty" json:"timestamp,omitempty"`
	Line      *string             `cbor:"1,keyasint,omitempty" json:"line,omitempty"`
	Stream    *string             `cbor:"2,keyasint,omitempty" json:"stream,omitempty"`
}

type LogEntry struct {
	data logEntryData
}

func (v *LogEntry) HasTimestamp() bool {
	return v.data.Timestamp != nil
}

func (v *LogEntry) Timestamp() *standard.Timestamp {
	return v.data.Timestamp
}

func (v *LogEntry) SetTimestamp(timestamp *standard.Timestamp) {
	v.data.Timestamp = timestamp
}

func (v *LogEntry) HasLine() bool {
	return v.data.Line != nil
}

func (v *LogEntry) Line() string {
	if v.data.Line == nil {
		return ""
	}
	return *v.data.Line
}

func (v *LogEntry) SetLine(line string) {
	v.data.Line = &line
}

func (v *LogEntry) HasStream() bool {
	return v.data.Stream != nil
}

func (v *LogEntry) Stream() string {
	if v.data.Stream == nil {
		return ""
	}
	return *v.data.Stream
}

func (v *LogEntry) SetStream(stream string) {
	v.data.Stream = &stream
}

func (v *LogEntry) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *LogEntry) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *LogEntry) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *LogEntry) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type userInfoData struct {
	Subject *string `cbor:"0,keyasint,omitempty" json:"subject,omitempty"`
}

type UserInfo struct {
	data userInfoData
}

func (v *UserInfo) HasSubject() bool {
	return v.data.Subject != nil
}

func (v *UserInfo) Subject() string {
	if v.data.Subject == nil {
		return ""
	}
	return *v.data.Subject
}

func (v *UserInfo) SetSubject(subject string) {
	v.data.Subject = &subject
}

func (v *UserInfo) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *UserInfo) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *UserInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *UserInfo) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type diskConfigData struct {
	Id       *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
	Name     *string `cbor:"1,keyasint,omitempty" json:"name,omitempty"`
	Capacity *int64  `cbor:"2,keyasint,omitempty" json:"capacity,omitempty"`
}

type DiskConfig struct {
	data diskConfigData
}

func (v *DiskConfig) HasId() bool {
	return v.data.Id != nil
}

func (v *DiskConfig) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *DiskConfig) SetId(id string) {
	v.data.Id = &id
}

func (v *DiskConfig) HasName() bool {
	return v.data.Name != nil
}

func (v *DiskConfig) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *DiskConfig) SetName(name string) {
	v.data.Name = &name
}

func (v *DiskConfig) HasCapacity() bool {
	return v.data.Capacity != nil
}

func (v *DiskConfig) Capacity() int64 {
	if v.data.Capacity == nil {
		return 0
	}
	return *v.data.Capacity
}

func (v *DiskConfig) SetCapacity(capacity int64) {
	v.data.Capacity = &capacity
}

func (v *DiskConfig) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DiskConfig) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DiskConfig) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DiskConfig) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type addonInstanceData struct {
	Id    *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
	Name  *string `cbor:"1,keyasint,omitempty" json:"name,omitempty"`
	Addon *string `cbor:"2,keyasint,omitempty" json:"addon,omitempty"`
	Plan  *string `cbor:"3,keyasint,omitempty" json:"plan,omitempty"`
}

type AddonInstance struct {
	data addonInstanceData
}

func (v *AddonInstance) HasId() bool {
	return v.data.Id != nil
}

func (v *AddonInstance) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *AddonInstance) SetId(id string) {
	v.data.Id = &id
}

func (v *AddonInstance) HasName() bool {
	return v.data.Name != nil
}

func (v *AddonInstance) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *AddonInstance) SetName(name string) {
	v.data.Name = &name
}

func (v *AddonInstance) HasAddon() bool {
	return v.data.Addon != nil
}

func (v *AddonInstance) Addon() string {
	if v.data.Addon == nil {
		return ""
	}
	return *v.data.Addon
}

func (v *AddonInstance) SetAddon(addon string) {
	v.data.Addon = &addon
}

func (v *AddonInstance) HasPlan() bool {
	return v.data.Plan != nil
}

func (v *AddonInstance) Plan() string {
	if v.data.Plan == nil {
		return ""
	}
	return *v.data.Plan
}

func (v *AddonInstance) SetPlan(plan string) {
	v.data.Plan = &plan
}

func (v *AddonInstance) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *AddonInstance) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *AddonInstance) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *AddonInstance) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type userQueryWhoAmIArgsData struct{}

type UserQueryWhoAmIArgs struct {
	call *rpc.Call
	data userQueryWhoAmIArgsData
}

func (v *UserQueryWhoAmIArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *UserQueryWhoAmIArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *UserQueryWhoAmIArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *UserQueryWhoAmIArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type userQueryWhoAmIResultsData struct {
	Info *UserInfo `cbor:"0,keyasint,omitempty" json:"info,omitempty"`
}

type UserQueryWhoAmIResults struct {
	call *rpc.Call
	data userQueryWhoAmIResultsData
}

func (v *UserQueryWhoAmIResults) SetInfo(info *UserInfo) {
	v.data.Info = info
}

func (v *UserQueryWhoAmIResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *UserQueryWhoAmIResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *UserQueryWhoAmIResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *UserQueryWhoAmIResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type UserQueryWhoAmI struct {
	*rpc.Call
	args    UserQueryWhoAmIArgs
	results UserQueryWhoAmIResults
}

func (t *UserQueryWhoAmI) Args() *UserQueryWhoAmIArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *UserQueryWhoAmI) Results() *UserQueryWhoAmIResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type UserQuery interface {
	WhoAmI(ctx context.Context, state *UserQueryWhoAmI) error
}

type reexportUserQuery struct {
	client *rpc.Client
}

func (_ reexportUserQuery) WhoAmI(ctx context.Context, state *UserQueryWhoAmI) error {
	panic("not implemented")
}

func (t reexportUserQuery) CapabilityClient() *rpc.Client {
	return t.client
}

func AdaptUserQuery(t UserQuery) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "whoAmI",
			InterfaceName: "UserQuery",
			Index:         0,
			Handler: func(ctx context.Context, call *rpc.Call) error {
				return t.WhoAmI(ctx, &UserQueryWhoAmI{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type UserQueryClient struct {
	*rpc.Client
}

func (c UserQueryClient) Export() UserQuery {
	return reexportUserQuery{client: c.Client}
}

type UserQueryClientWhoAmIResults struct {
	client *rpc.Client
	data   userQueryWhoAmIResultsData
}

func (v *UserQueryClientWhoAmIResults) HasInfo() bool {
	return v.data.Info != nil
}

func (v *UserQueryClientWhoAmIResults) Info() *UserInfo {
	return v.data.Info
}

func (v UserQueryClient) WhoAmI(ctx context.Context) (*UserQueryClientWhoAmIResults, error) {
	args := UserQueryWhoAmIArgs{}

	var ret userQueryWhoAmIResultsData

	err := v.Client.Call(ctx, "whoAmI", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &UserQueryClientWhoAmIResults{client: v.Client, data: ret}, nil
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

type logsAppLogsArgsData struct {
	Application *string             `cbor:"0,keyasint,omitempty" json:"application,omitempty"`
	From        *standard.Timestamp `cbor:"1,keyasint,omitempty" json:"from,omitempty"`
	Follow      *bool               `cbor:"2,keyasint,omitempty" json:"follow,omitempty"`
}

type LogsAppLogsArgs struct {
	call *rpc.Call
	data logsAppLogsArgsData
}

func (v *LogsAppLogsArgs) HasApplication() bool {
	return v.data.Application != nil
}

func (v *LogsAppLogsArgs) Application() string {
	if v.data.Application == nil {
		return ""
	}
	return *v.data.Application
}

func (v *LogsAppLogsArgs) HasFrom() bool {
	return v.data.From != nil
}

func (v *LogsAppLogsArgs) From() *standard.Timestamp {
	return v.data.From
}

func (v *LogsAppLogsArgs) HasFollow() bool {
	return v.data.Follow != nil
}

func (v *LogsAppLogsArgs) Follow() bool {
	if v.data.Follow == nil {
		return false
	}
	return *v.data.Follow
}

func (v *LogsAppLogsArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *LogsAppLogsArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *LogsAppLogsArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *LogsAppLogsArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type logsAppLogsResultsData struct {
	Logs *[]*LogEntry `cbor:"0,keyasint,omitempty" json:"logs,omitempty"`
}

type LogsAppLogsResults struct {
	call *rpc.Call
	data logsAppLogsResultsData
}

func (v *LogsAppLogsResults) SetLogs(logs []*LogEntry) {
	x := slices.Clone(logs)
	v.data.Logs = &x
}

func (v *LogsAppLogsResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *LogsAppLogsResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *LogsAppLogsResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *LogsAppLogsResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type LogsAppLogs struct {
	*rpc.Call
	args    LogsAppLogsArgs
	results LogsAppLogsResults
}

func (t *LogsAppLogs) Args() *LogsAppLogsArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *LogsAppLogs) Results() *LogsAppLogsResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type Logs interface {
	AppLogs(ctx context.Context, state *LogsAppLogs) error
}

type reexportLogs struct {
	client *rpc.Client
}

func (_ reexportLogs) AppLogs(ctx context.Context, state *LogsAppLogs) error {
	panic("not implemented")
}

func (t reexportLogs) CapabilityClient() *rpc.Client {
	return t.client
}

func AdaptLogs(t Logs) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "appLogs",
			InterfaceName: "Logs",
			Index:         0,
			Handler: func(ctx context.Context, call *rpc.Call) error {
				return t.AppLogs(ctx, &LogsAppLogs{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type LogsClient struct {
	*rpc.Client
}

func (c LogsClient) Export() Logs {
	return reexportLogs{client: c.Client}
}

type LogsClientAppLogsResults struct {
	client *rpc.Client
	data   logsAppLogsResultsData
}

func (v *LogsClientAppLogsResults) HasLogs() bool {
	return v.data.Logs != nil
}

func (v *LogsClientAppLogsResults) Logs() []*LogEntry {
	if v.data.Logs == nil {
		return nil
	}
	return *v.data.Logs
}

func (v LogsClient) AppLogs(ctx context.Context, application string, from *standard.Timestamp, follow bool) (*LogsClientAppLogsResults, error) {
	args := LogsAppLogsArgs{}
	args.data.Application = &application
	args.data.From = from
	args.data.Follow = &follow

	var ret logsAppLogsResultsData

	err := v.Client.Call(ctx, "appLogs", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &LogsClientAppLogsResults{client: v.Client, data: ret}, nil
}

type disksNewArgsData struct {
	Name     *string `cbor:"0,keyasint,omitempty" json:"name,omitempty"`
	Capacity *int64  `cbor:"1,keyasint,omitempty" json:"capacity,omitempty"`
}

type DisksNewArgs struct {
	call *rpc.Call
	data disksNewArgsData
}

func (v *DisksNewArgs) HasName() bool {
	return v.data.Name != nil
}

func (v *DisksNewArgs) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *DisksNewArgs) HasCapacity() bool {
	return v.data.Capacity != nil
}

func (v *DisksNewArgs) Capacity() int64 {
	if v.data.Capacity == nil {
		return 0
	}
	return *v.data.Capacity
}

func (v *DisksNewArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DisksNewArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DisksNewArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DisksNewArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type disksNewResultsData struct {
	Id *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
}

type DisksNewResults struct {
	call *rpc.Call
	data disksNewResultsData
}

func (v *DisksNewResults) SetId(id string) {
	v.data.Id = &id
}

func (v *DisksNewResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DisksNewResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DisksNewResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DisksNewResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type disksGetByIdArgsData struct {
	Id *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
}

type DisksGetByIdArgs struct {
	call *rpc.Call
	data disksGetByIdArgsData
}

func (v *DisksGetByIdArgs) HasId() bool {
	return v.data.Id != nil
}

func (v *DisksGetByIdArgs) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *DisksGetByIdArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DisksGetByIdArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DisksGetByIdArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DisksGetByIdArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type disksGetByIdResultsData struct {
	Config *DiskConfig `cbor:"0,keyasint,omitempty" json:"config,omitempty"`
}

type DisksGetByIdResults struct {
	call *rpc.Call
	data disksGetByIdResultsData
}

func (v *DisksGetByIdResults) SetConfig(config *DiskConfig) {
	v.data.Config = config
}

func (v *DisksGetByIdResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DisksGetByIdResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DisksGetByIdResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DisksGetByIdResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type disksGetByNameArgsData struct {
	Name *string `cbor:"0,keyasint,omitempty" json:"name,omitempty"`
}

type DisksGetByNameArgs struct {
	call *rpc.Call
	data disksGetByNameArgsData
}

func (v *DisksGetByNameArgs) HasName() bool {
	return v.data.Name != nil
}

func (v *DisksGetByNameArgs) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *DisksGetByNameArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DisksGetByNameArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DisksGetByNameArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DisksGetByNameArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type disksGetByNameResultsData struct {
	Config *DiskConfig `cbor:"0,keyasint,omitempty" json:"config,omitempty"`
}

type DisksGetByNameResults struct {
	call *rpc.Call
	data disksGetByNameResultsData
}

func (v *DisksGetByNameResults) SetConfig(config *DiskConfig) {
	v.data.Config = config
}

func (v *DisksGetByNameResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DisksGetByNameResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DisksGetByNameResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DisksGetByNameResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type disksListArgsData struct{}

type DisksListArgs struct {
	call *rpc.Call
	data disksListArgsData
}

func (v *DisksListArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DisksListArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DisksListArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DisksListArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type disksListResultsData struct {
	Disks *[]*DiskConfig `cbor:"0,keyasint,omitempty" json:"disks,omitempty"`
}

type DisksListResults struct {
	call *rpc.Call
	data disksListResultsData
}

func (v *DisksListResults) SetDisks(disks []*DiskConfig) {
	x := slices.Clone(disks)
	v.data.Disks = &x
}

func (v *DisksListResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DisksListResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DisksListResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DisksListResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type disksDeleteArgsData struct {
	Id *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
}

type DisksDeleteArgs struct {
	call *rpc.Call
	data disksDeleteArgsData
}

func (v *DisksDeleteArgs) HasId() bool {
	return v.data.Id != nil
}

func (v *DisksDeleteArgs) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *DisksDeleteArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DisksDeleteArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DisksDeleteArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DisksDeleteArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type disksDeleteResultsData struct{}

type DisksDeleteResults struct {
	call *rpc.Call
	data disksDeleteResultsData
}

func (v *DisksDeleteResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DisksDeleteResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DisksDeleteResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DisksDeleteResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type DisksNew struct {
	*rpc.Call
	args    DisksNewArgs
	results DisksNewResults
}

func (t *DisksNew) Args() *DisksNewArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *DisksNew) Results() *DisksNewResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type DisksGetById struct {
	*rpc.Call
	args    DisksGetByIdArgs
	results DisksGetByIdResults
}

func (t *DisksGetById) Args() *DisksGetByIdArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *DisksGetById) Results() *DisksGetByIdResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type DisksGetByName struct {
	*rpc.Call
	args    DisksGetByNameArgs
	results DisksGetByNameResults
}

func (t *DisksGetByName) Args() *DisksGetByNameArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *DisksGetByName) Results() *DisksGetByNameResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type DisksList struct {
	*rpc.Call
	args    DisksListArgs
	results DisksListResults
}

func (t *DisksList) Args() *DisksListArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *DisksList) Results() *DisksListResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type DisksDelete struct {
	*rpc.Call
	args    DisksDeleteArgs
	results DisksDeleteResults
}

func (t *DisksDelete) Args() *DisksDeleteArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *DisksDelete) Results() *DisksDeleteResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type Disks interface {
	New(ctx context.Context, state *DisksNew) error
	GetById(ctx context.Context, state *DisksGetById) error
	GetByName(ctx context.Context, state *DisksGetByName) error
	List(ctx context.Context, state *DisksList) error
	Delete(ctx context.Context, state *DisksDelete) error
}

type reexportDisks struct {
	client *rpc.Client
}

func (_ reexportDisks) New(ctx context.Context, state *DisksNew) error {
	panic("not implemented")
}

func (_ reexportDisks) GetById(ctx context.Context, state *DisksGetById) error {
	panic("not implemented")
}

func (_ reexportDisks) GetByName(ctx context.Context, state *DisksGetByName) error {
	panic("not implemented")
}

func (_ reexportDisks) List(ctx context.Context, state *DisksList) error {
	panic("not implemented")
}

func (_ reexportDisks) Delete(ctx context.Context, state *DisksDelete) error {
	panic("not implemented")
}

func (t reexportDisks) CapabilityClient() *rpc.Client {
	return t.client
}

func AdaptDisks(t Disks) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "new",
			InterfaceName: "Disks",
			Index:         0,
			Handler: func(ctx context.Context, call *rpc.Call) error {
				return t.New(ctx, &DisksNew{Call: call})
			},
		},
		{
			Name:          "getById",
			InterfaceName: "Disks",
			Index:         0,
			Handler: func(ctx context.Context, call *rpc.Call) error {
				return t.GetById(ctx, &DisksGetById{Call: call})
			},
		},
		{
			Name:          "getByName",
			InterfaceName: "Disks",
			Index:         0,
			Handler: func(ctx context.Context, call *rpc.Call) error {
				return t.GetByName(ctx, &DisksGetByName{Call: call})
			},
		},
		{
			Name:          "list",
			InterfaceName: "Disks",
			Index:         0,
			Handler: func(ctx context.Context, call *rpc.Call) error {
				return t.List(ctx, &DisksList{Call: call})
			},
		},
		{
			Name:          "delete",
			InterfaceName: "Disks",
			Index:         0,
			Handler: func(ctx context.Context, call *rpc.Call) error {
				return t.Delete(ctx, &DisksDelete{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type DisksClient struct {
	*rpc.Client
}

func (c DisksClient) Export() Disks {
	return reexportDisks{client: c.Client}
}

type DisksClientNewResults struct {
	client *rpc.Client
	data   disksNewResultsData
}

func (v *DisksClientNewResults) HasId() bool {
	return v.data.Id != nil
}

func (v *DisksClientNewResults) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v DisksClient) New(ctx context.Context, name string, capacity int64) (*DisksClientNewResults, error) {
	args := DisksNewArgs{}
	args.data.Name = &name
	args.data.Capacity = &capacity

	var ret disksNewResultsData

	err := v.Client.Call(ctx, "new", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &DisksClientNewResults{client: v.Client, data: ret}, nil
}

type DisksClientGetByIdResults struct {
	client *rpc.Client
	data   disksGetByIdResultsData
}

func (v *DisksClientGetByIdResults) HasConfig() bool {
	return v.data.Config != nil
}

func (v *DisksClientGetByIdResults) Config() *DiskConfig {
	return v.data.Config
}

func (v DisksClient) GetById(ctx context.Context, id string) (*DisksClientGetByIdResults, error) {
	args := DisksGetByIdArgs{}
	args.data.Id = &id

	var ret disksGetByIdResultsData

	err := v.Client.Call(ctx, "getById", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &DisksClientGetByIdResults{client: v.Client, data: ret}, nil
}

type DisksClientGetByNameResults struct {
	client *rpc.Client
	data   disksGetByNameResultsData
}

func (v *DisksClientGetByNameResults) HasConfig() bool {
	return v.data.Config != nil
}

func (v *DisksClientGetByNameResults) Config() *DiskConfig {
	return v.data.Config
}

func (v DisksClient) GetByName(ctx context.Context, name string) (*DisksClientGetByNameResults, error) {
	args := DisksGetByNameArgs{}
	args.data.Name = &name

	var ret disksGetByNameResultsData

	err := v.Client.Call(ctx, "getByName", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &DisksClientGetByNameResults{client: v.Client, data: ret}, nil
}

type DisksClientListResults struct {
	client *rpc.Client
	data   disksListResultsData
}

func (v *DisksClientListResults) HasDisks() bool {
	return v.data.Disks != nil
}

func (v *DisksClientListResults) Disks() []*DiskConfig {
	if v.data.Disks == nil {
		return nil
	}
	return *v.data.Disks
}

func (v DisksClient) List(ctx context.Context) (*DisksClientListResults, error) {
	args := DisksListArgs{}

	var ret disksListResultsData

	err := v.Client.Call(ctx, "list", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &DisksClientListResults{client: v.Client, data: ret}, nil
}

type DisksClientDeleteResults struct {
	client *rpc.Client
	data   disksDeleteResultsData
}

func (v DisksClient) Delete(ctx context.Context, id string) (*DisksClientDeleteResults, error) {
	args := DisksDeleteArgs{}
	args.data.Id = &id

	var ret disksDeleteResultsData

	err := v.Client.Call(ctx, "delete", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &DisksClientDeleteResults{client: v.Client, data: ret}, nil
}

type addonsCreateInstanceArgsData struct {
	Name  *string `cbor:"0,keyasint,omitempty" json:"name,omitempty"`
	Addon *string `cbor:"1,keyasint,omitempty" json:"addon,omitempty"`
	Plan  *string `cbor:"2,keyasint,omitempty" json:"plan,omitempty"`
	App   *string `cbor:"3,keyasint,omitempty" json:"app,omitempty"`
}

type AddonsCreateInstanceArgs struct {
	call *rpc.Call
	data addonsCreateInstanceArgsData
}

func (v *AddonsCreateInstanceArgs) HasName() bool {
	return v.data.Name != nil
}

func (v *AddonsCreateInstanceArgs) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *AddonsCreateInstanceArgs) HasAddon() bool {
	return v.data.Addon != nil
}

func (v *AddonsCreateInstanceArgs) Addon() string {
	if v.data.Addon == nil {
		return ""
	}
	return *v.data.Addon
}

func (v *AddonsCreateInstanceArgs) HasPlan() bool {
	return v.data.Plan != nil
}

func (v *AddonsCreateInstanceArgs) Plan() string {
	if v.data.Plan == nil {
		return ""
	}
	return *v.data.Plan
}

func (v *AddonsCreateInstanceArgs) HasApp() bool {
	return v.data.App != nil
}

func (v *AddonsCreateInstanceArgs) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *AddonsCreateInstanceArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *AddonsCreateInstanceArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *AddonsCreateInstanceArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *AddonsCreateInstanceArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type addonsCreateInstanceResultsData struct {
	Id *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
}

type AddonsCreateInstanceResults struct {
	call *rpc.Call
	data addonsCreateInstanceResultsData
}

func (v *AddonsCreateInstanceResults) SetId(id string) {
	v.data.Id = &id
}

func (v *AddonsCreateInstanceResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *AddonsCreateInstanceResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *AddonsCreateInstanceResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *AddonsCreateInstanceResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type addonsListInstancesArgsData struct {
	App *string `cbor:"0,keyasint,omitempty" json:"app,omitempty"`
}

type AddonsListInstancesArgs struct {
	call *rpc.Call
	data addonsListInstancesArgsData
}

func (v *AddonsListInstancesArgs) HasApp() bool {
	return v.data.App != nil
}

func (v *AddonsListInstancesArgs) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *AddonsListInstancesArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *AddonsListInstancesArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *AddonsListInstancesArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *AddonsListInstancesArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type addonsListInstancesResultsData struct {
	Addons *[]*AddonInstance `cbor:"0,keyasint,omitempty" json:"addons,omitempty"`
}

type AddonsListInstancesResults struct {
	call *rpc.Call
	data addonsListInstancesResultsData
}

func (v *AddonsListInstancesResults) SetAddons(addons []*AddonInstance) {
	x := slices.Clone(addons)
	v.data.Addons = &x
}

func (v *AddonsListInstancesResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *AddonsListInstancesResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *AddonsListInstancesResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *AddonsListInstancesResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type addonsDeleteInstanceArgsData struct {
	App  *string `cbor:"0,keyasint,omitempty" json:"app,omitempty"`
	Name *string `cbor:"1,keyasint,omitempty" json:"name,omitempty"`
}

type AddonsDeleteInstanceArgs struct {
	call *rpc.Call
	data addonsDeleteInstanceArgsData
}

func (v *AddonsDeleteInstanceArgs) HasApp() bool {
	return v.data.App != nil
}

func (v *AddonsDeleteInstanceArgs) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *AddonsDeleteInstanceArgs) HasName() bool {
	return v.data.Name != nil
}

func (v *AddonsDeleteInstanceArgs) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *AddonsDeleteInstanceArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *AddonsDeleteInstanceArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *AddonsDeleteInstanceArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *AddonsDeleteInstanceArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type addonsDeleteInstanceResultsData struct{}

type AddonsDeleteInstanceResults struct {
	call *rpc.Call
	data addonsDeleteInstanceResultsData
}

func (v *AddonsDeleteInstanceResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *AddonsDeleteInstanceResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *AddonsDeleteInstanceResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *AddonsDeleteInstanceResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type AddonsCreateInstance struct {
	*rpc.Call
	args    AddonsCreateInstanceArgs
	results AddonsCreateInstanceResults
}

func (t *AddonsCreateInstance) Args() *AddonsCreateInstanceArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *AddonsCreateInstance) Results() *AddonsCreateInstanceResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type AddonsListInstances struct {
	*rpc.Call
	args    AddonsListInstancesArgs
	results AddonsListInstancesResults
}

func (t *AddonsListInstances) Args() *AddonsListInstancesArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *AddonsListInstances) Results() *AddonsListInstancesResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type AddonsDeleteInstance struct {
	*rpc.Call
	args    AddonsDeleteInstanceArgs
	results AddonsDeleteInstanceResults
}

func (t *AddonsDeleteInstance) Args() *AddonsDeleteInstanceArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *AddonsDeleteInstance) Results() *AddonsDeleteInstanceResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type Addons interface {
	CreateInstance(ctx context.Context, state *AddonsCreateInstance) error
	ListInstances(ctx context.Context, state *AddonsListInstances) error
	DeleteInstance(ctx context.Context, state *AddonsDeleteInstance) error
}

type reexportAddons struct {
	client *rpc.Client
}

func (_ reexportAddons) CreateInstance(ctx context.Context, state *AddonsCreateInstance) error {
	panic("not implemented")
}

func (_ reexportAddons) ListInstances(ctx context.Context, state *AddonsListInstances) error {
	panic("not implemented")
}

func (_ reexportAddons) DeleteInstance(ctx context.Context, state *AddonsDeleteInstance) error {
	panic("not implemented")
}

func (t reexportAddons) CapabilityClient() *rpc.Client {
	return t.client
}

func AdaptAddons(t Addons) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "createInstance",
			InterfaceName: "Addons",
			Index:         0,
			Handler: func(ctx context.Context, call *rpc.Call) error {
				return t.CreateInstance(ctx, &AddonsCreateInstance{Call: call})
			},
		},
		{
			Name:          "listInstances",
			InterfaceName: "Addons",
			Index:         0,
			Handler: func(ctx context.Context, call *rpc.Call) error {
				return t.ListInstances(ctx, &AddonsListInstances{Call: call})
			},
		},
		{
			Name:          "deleteInstance",
			InterfaceName: "Addons",
			Index:         0,
			Handler: func(ctx context.Context, call *rpc.Call) error {
				return t.DeleteInstance(ctx, &AddonsDeleteInstance{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type AddonsClient struct {
	*rpc.Client
}

func (c AddonsClient) Export() Addons {
	return reexportAddons{client: c.Client}
}

type AddonsClientCreateInstanceResults struct {
	client *rpc.Client
	data   addonsCreateInstanceResultsData
}

func (v *AddonsClientCreateInstanceResults) HasId() bool {
	return v.data.Id != nil
}

func (v *AddonsClientCreateInstanceResults) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v AddonsClient) CreateInstance(ctx context.Context, name string, addon string, plan string, app string) (*AddonsClientCreateInstanceResults, error) {
	args := AddonsCreateInstanceArgs{}
	args.data.Name = &name
	args.data.Addon = &addon
	args.data.Plan = &plan
	args.data.App = &app

	var ret addonsCreateInstanceResultsData

	err := v.Client.Call(ctx, "createInstance", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &AddonsClientCreateInstanceResults{client: v.Client, data: ret}, nil
}

type AddonsClientListInstancesResults struct {
	client *rpc.Client
	data   addonsListInstancesResultsData
}

func (v *AddonsClientListInstancesResults) HasAddons() bool {
	return v.data.Addons != nil
}

func (v *AddonsClientListInstancesResults) Addons() []*AddonInstance {
	if v.data.Addons == nil {
		return nil
	}
	return *v.data.Addons
}

func (v AddonsClient) ListInstances(ctx context.Context, app string) (*AddonsClientListInstancesResults, error) {
	args := AddonsListInstancesArgs{}
	args.data.App = &app

	var ret addonsListInstancesResultsData

	err := v.Client.Call(ctx, "listInstances", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &AddonsClientListInstancesResults{client: v.Client, data: ret}, nil
}

type AddonsClientDeleteInstanceResults struct {
	client *rpc.Client
	data   addonsDeleteInstanceResultsData
}

func (v AddonsClient) DeleteInstance(ctx context.Context, app string, name string) (*AddonsClientDeleteInstanceResults, error) {
	args := AddonsDeleteInstanceArgs{}
	args.data.App = &app
	args.data.Name = &name

	var ret addonsDeleteInstanceResultsData

	err := v.Client.Call(ctx, "deleteInstance", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &AddonsClientDeleteInstanceResults{client: v.Client, data: ret}, nil
}
