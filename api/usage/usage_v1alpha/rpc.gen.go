package usage_v1alpha

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/fxamacker/cbor/v2"
	rpc "miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/standard"
)

type usageWindowData struct {
	Start       *standard.Timestamp `cbor:"0,keyasint,omitempty" json:"start,omitempty"`
	End         *standard.Timestamp `cbor:"1,keyasint,omitempty" json:"end,omitempty"`
	Aggregate   *string             `cbor:"2,keyasint,omitempty" json:"aggregate,omitempty"`
	StepSeconds *int64              `cbor:"3,keyasint,omitempty" json:"step_seconds,omitempty"`
}

type UsageWindow struct {
	data usageWindowData
}

func (v *UsageWindow) HasStart() bool {
	return v.data.Start != nil
}

func (v *UsageWindow) Start() *standard.Timestamp {
	return v.data.Start
}

func (v *UsageWindow) SetStart(start *standard.Timestamp) {
	v.data.Start = start
}

func (v *UsageWindow) HasEnd() bool {
	return v.data.End != nil
}

func (v *UsageWindow) End() *standard.Timestamp {
	return v.data.End
}

func (v *UsageWindow) SetEnd(end *standard.Timestamp) {
	v.data.End = end
}

func (v *UsageWindow) HasAggregate() bool {
	return v.data.Aggregate != nil
}

func (v *UsageWindow) Aggregate() string {
	if v.data.Aggregate == nil {
		return ""
	}
	return *v.data.Aggregate
}

func (v *UsageWindow) SetAggregate(aggregate string) {
	v.data.Aggregate = &aggregate
}

func (v *UsageWindow) HasStepSeconds() bool {
	return v.data.StepSeconds != nil
}

func (v *UsageWindow) StepSeconds() int64 {
	if v.data.StepSeconds == nil {
		return 0
	}
	return *v.data.StepSeconds
}

func (v *UsageWindow) SetStepSeconds(step_seconds int64) {
	v.data.StepSeconds = &step_seconds
}

func (v *UsageWindow) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *UsageWindow) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *UsageWindow) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *UsageWindow) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type sandboxRefData struct {
	Sandbox        *string             `cbor:"0,keyasint,omitempty" json:"sandbox,omitempty"`
	SandboxShortId *string             `cbor:"1,keyasint,omitempty" json:"sandbox_short_id,omitempty"`
	App            *string             `cbor:"2,keyasint,omitempty" json:"app,omitempty"`
	AppId          *string             `cbor:"3,keyasint,omitempty" json:"app_id,omitempty"`
	Service        *string             `cbor:"4,keyasint,omitempty" json:"service,omitempty"`
	Pool           *string             `cbor:"5,keyasint,omitempty" json:"pool,omitempty"`
	Version        *string             `cbor:"6,keyasint,omitempty" json:"version,omitempty"`
	VersionShortId *string             `cbor:"7,keyasint,omitempty" json:"version_short_id,omitempty"`
	Node           *string             `cbor:"8,keyasint,omitempty" json:"node,omitempty"`
	NodeName       *string             `cbor:"9,keyasint,omitempty" json:"node_name,omitempty"`
	RunnerId       *string             `cbor:"10,keyasint,omitempty" json:"runner_id,omitempty"`
	Kind           *string             `cbor:"11,keyasint,omitempty" json:"kind,omitempty"`
	Status         *string             `cbor:"12,keyasint,omitempty" json:"status,omitempty"`
	StartedAt      *standard.Timestamp `cbor:"13,keyasint,omitempty" json:"started_at,omitempty"`
}

type SandboxRef struct {
	data sandboxRefData
}

func (v *SandboxRef) HasSandbox() bool {
	return v.data.Sandbox != nil
}

func (v *SandboxRef) Sandbox() string {
	if v.data.Sandbox == nil {
		return ""
	}
	return *v.data.Sandbox
}

func (v *SandboxRef) SetSandbox(sandbox string) {
	v.data.Sandbox = &sandbox
}

func (v *SandboxRef) HasSandboxShortId() bool {
	return v.data.SandboxShortId != nil
}

func (v *SandboxRef) SandboxShortId() string {
	if v.data.SandboxShortId == nil {
		return ""
	}
	return *v.data.SandboxShortId
}

func (v *SandboxRef) SetSandboxShortId(sandbox_short_id string) {
	v.data.SandboxShortId = &sandbox_short_id
}

func (v *SandboxRef) HasApp() bool {
	return v.data.App != nil
}

func (v *SandboxRef) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *SandboxRef) SetApp(app string) {
	v.data.App = &app
}

func (v *SandboxRef) HasAppId() bool {
	return v.data.AppId != nil
}

func (v *SandboxRef) AppId() string {
	if v.data.AppId == nil {
		return ""
	}
	return *v.data.AppId
}

func (v *SandboxRef) SetAppId(app_id string) {
	v.data.AppId = &app_id
}

func (v *SandboxRef) HasService() bool {
	return v.data.Service != nil
}

func (v *SandboxRef) Service() string {
	if v.data.Service == nil {
		return ""
	}
	return *v.data.Service
}

func (v *SandboxRef) SetService(service string) {
	v.data.Service = &service
}

func (v *SandboxRef) HasPool() bool {
	return v.data.Pool != nil
}

func (v *SandboxRef) Pool() string {
	if v.data.Pool == nil {
		return ""
	}
	return *v.data.Pool
}

func (v *SandboxRef) SetPool(pool string) {
	v.data.Pool = &pool
}

func (v *SandboxRef) HasVersion() bool {
	return v.data.Version != nil
}

func (v *SandboxRef) Version() string {
	if v.data.Version == nil {
		return ""
	}
	return *v.data.Version
}

func (v *SandboxRef) SetVersion(version string) {
	v.data.Version = &version
}

func (v *SandboxRef) HasVersionShortId() bool {
	return v.data.VersionShortId != nil
}

func (v *SandboxRef) VersionShortId() string {
	if v.data.VersionShortId == nil {
		return ""
	}
	return *v.data.VersionShortId
}

func (v *SandboxRef) SetVersionShortId(version_short_id string) {
	v.data.VersionShortId = &version_short_id
}

func (v *SandboxRef) HasNode() bool {
	return v.data.Node != nil
}

func (v *SandboxRef) Node() string {
	if v.data.Node == nil {
		return ""
	}
	return *v.data.Node
}

func (v *SandboxRef) SetNode(node string) {
	v.data.Node = &node
}

func (v *SandboxRef) HasNodeName() bool {
	return v.data.NodeName != nil
}

func (v *SandboxRef) NodeName() string {
	if v.data.NodeName == nil {
		return ""
	}
	return *v.data.NodeName
}

func (v *SandboxRef) SetNodeName(node_name string) {
	v.data.NodeName = &node_name
}

func (v *SandboxRef) HasRunnerId() bool {
	return v.data.RunnerId != nil
}

func (v *SandboxRef) RunnerId() string {
	if v.data.RunnerId == nil {
		return ""
	}
	return *v.data.RunnerId
}

func (v *SandboxRef) SetRunnerId(runner_id string) {
	v.data.RunnerId = &runner_id
}

func (v *SandboxRef) HasKind() bool {
	return v.data.Kind != nil
}

func (v *SandboxRef) Kind() string {
	if v.data.Kind == nil {
		return ""
	}
	return *v.data.Kind
}

func (v *SandboxRef) SetKind(kind string) {
	v.data.Kind = &kind
}

func (v *SandboxRef) HasStatus() bool {
	return v.data.Status != nil
}

func (v *SandboxRef) Status() string {
	if v.data.Status == nil {
		return ""
	}
	return *v.data.Status
}

func (v *SandboxRef) SetStatus(status string) {
	v.data.Status = &status
}

func (v *SandboxRef) HasStartedAt() bool {
	return v.data.StartedAt != nil
}

func (v *SandboxRef) StartedAt() *standard.Timestamp {
	return v.data.StartedAt
}

func (v *SandboxRef) SetStartedAt(started_at *standard.Timestamp) {
	v.data.StartedAt = started_at
}

func (v *SandboxRef) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SandboxRef) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SandboxRef) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SandboxRef) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type cpuUsageData struct {
	Cores         *float64 `cbor:"0,keyasint,omitempty" json:"cores,omitempty"`
	PercentOfNode *float64 `cbor:"1,keyasint,omitempty" json:"percent_of_node,omitempty"`
}

type CpuUsage struct {
	data cpuUsageData
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

func (v *CpuUsage) HasPercentOfNode() bool {
	return v.data.PercentOfNode != nil
}

func (v *CpuUsage) PercentOfNode() float64 {
	if v.data.PercentOfNode == nil {
		return 0
	}
	return *v.data.PercentOfNode
}

func (v *CpuUsage) SetPercentOfNode(percent_of_node float64) {
	v.data.PercentOfNode = &percent_of_node
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
	Bytes         *int64   `cbor:"0,keyasint,omitempty" json:"bytes,omitempty"`
	PercentOfNode *float64 `cbor:"1,keyasint,omitempty" json:"percent_of_node,omitempty"`
}

type MemoryUsage struct {
	data memoryUsageData
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

func (v *MemoryUsage) HasPercentOfNode() bool {
	return v.data.PercentOfNode != nil
}

func (v *MemoryUsage) PercentOfNode() float64 {
	if v.data.PercentOfNode == nil {
		return 0
	}
	return *v.data.PercentOfNode
}

func (v *MemoryUsage) SetPercentOfNode(percent_of_node float64) {
	v.data.PercentOfNode = &percent_of_node
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

type sandboxUsageData struct {
	Ref        *SandboxRef         `cbor:"0,keyasint,omitempty" json:"ref,omitempty"`
	Cpu        *CpuUsage           `cbor:"1,keyasint,omitempty" json:"cpu,omitempty"`
	Memory     *MemoryUsage        `cbor:"2,keyasint,omitempty" json:"memory,omitempty"`
	MeasuredAt *standard.Timestamp `cbor:"3,keyasint,omitempty" json:"measured_at,omitempty"`
	Stale      *bool               `cbor:"4,keyasint,omitempty" json:"stale,omitempty"`
}

type SandboxUsage struct {
	data sandboxUsageData
}

func (v *SandboxUsage) HasRef() bool {
	return v.data.Ref != nil
}

func (v *SandboxUsage) Ref() *SandboxRef {
	return v.data.Ref
}

func (v *SandboxUsage) SetRef(ref *SandboxRef) {
	v.data.Ref = ref
}

func (v *SandboxUsage) HasCpu() bool {
	return v.data.Cpu != nil
}

func (v *SandboxUsage) Cpu() *CpuUsage {
	return v.data.Cpu
}

func (v *SandboxUsage) SetCpu(cpu *CpuUsage) {
	v.data.Cpu = cpu
}

func (v *SandboxUsage) HasMemory() bool {
	return v.data.Memory != nil
}

func (v *SandboxUsage) Memory() *MemoryUsage {
	return v.data.Memory
}

func (v *SandboxUsage) SetMemory(memory *MemoryUsage) {
	v.data.Memory = memory
}

func (v *SandboxUsage) HasMeasuredAt() bool {
	return v.data.MeasuredAt != nil
}

func (v *SandboxUsage) MeasuredAt() *standard.Timestamp {
	return v.data.MeasuredAt
}

func (v *SandboxUsage) SetMeasuredAt(measured_at *standard.Timestamp) {
	v.data.MeasuredAt = measured_at
}

func (v *SandboxUsage) HasStale() bool {
	return v.data.Stale != nil
}

func (v *SandboxUsage) Stale() bool {
	if v.data.Stale == nil {
		return false
	}
	return *v.data.Stale
}

func (v *SandboxUsage) SetStale(stale bool) {
	v.data.Stale = &stale
}

func (v *SandboxUsage) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SandboxUsage) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SandboxUsage) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SandboxUsage) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type resourceTotalsData struct {
	CpuCores      *float64 `cbor:"0,keyasint,omitempty" json:"cpu_cores,omitempty"`
	CpuPercent    *float64 `cbor:"1,keyasint,omitempty" json:"cpu_percent,omitempty"`
	MemoryBytes   *int64   `cbor:"2,keyasint,omitempty" json:"memory_bytes,omitempty"`
	MemoryPercent *float64 `cbor:"3,keyasint,omitempty" json:"memory_percent,omitempty"`
}

type ResourceTotals struct {
	data resourceTotalsData
}

func (v *ResourceTotals) HasCpuCores() bool {
	return v.data.CpuCores != nil
}

func (v *ResourceTotals) CpuCores() float64 {
	if v.data.CpuCores == nil {
		return 0
	}
	return *v.data.CpuCores
}

func (v *ResourceTotals) SetCpuCores(cpu_cores float64) {
	v.data.CpuCores = &cpu_cores
}

func (v *ResourceTotals) HasCpuPercent() bool {
	return v.data.CpuPercent != nil
}

func (v *ResourceTotals) CpuPercent() float64 {
	if v.data.CpuPercent == nil {
		return 0
	}
	return *v.data.CpuPercent
}

func (v *ResourceTotals) SetCpuPercent(cpu_percent float64) {
	v.data.CpuPercent = &cpu_percent
}

func (v *ResourceTotals) HasMemoryBytes() bool {
	return v.data.MemoryBytes != nil
}

func (v *ResourceTotals) MemoryBytes() int64 {
	if v.data.MemoryBytes == nil {
		return 0
	}
	return *v.data.MemoryBytes
}

func (v *ResourceTotals) SetMemoryBytes(memory_bytes int64) {
	v.data.MemoryBytes = &memory_bytes
}

func (v *ResourceTotals) HasMemoryPercent() bool {
	return v.data.MemoryPercent != nil
}

func (v *ResourceTotals) MemoryPercent() float64 {
	if v.data.MemoryPercent == nil {
		return 0
	}
	return *v.data.MemoryPercent
}

func (v *ResourceTotals) SetMemoryPercent(memory_percent float64) {
	v.data.MemoryPercent = &memory_percent
}

func (v *ResourceTotals) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ResourceTotals) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ResourceTotals) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ResourceTotals) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type nodeCapacityData struct {
	CpuCores     *float64 `cbor:"0,keyasint,omitempty" json:"cpu_cores,omitempty"`
	MemoryBytes  *int64   `cbor:"1,keyasint,omitempty" json:"memory_bytes,omitempty"`
	StorageBytes *int64   `cbor:"2,keyasint,omitempty" json:"storage_bytes,omitempty"`
}

type NodeCapacity struct {
	data nodeCapacityData
}

func (v *NodeCapacity) HasCpuCores() bool {
	return v.data.CpuCores != nil
}

func (v *NodeCapacity) CpuCores() float64 {
	if v.data.CpuCores == nil {
		return 0
	}
	return *v.data.CpuCores
}

func (v *NodeCapacity) SetCpuCores(cpu_cores float64) {
	v.data.CpuCores = &cpu_cores
}

func (v *NodeCapacity) HasMemoryBytes() bool {
	return v.data.MemoryBytes != nil
}

func (v *NodeCapacity) MemoryBytes() int64 {
	if v.data.MemoryBytes == nil {
		return 0
	}
	return *v.data.MemoryBytes
}

func (v *NodeCapacity) SetMemoryBytes(memory_bytes int64) {
	v.data.MemoryBytes = &memory_bytes
}

func (v *NodeCapacity) HasStorageBytes() bool {
	return v.data.StorageBytes != nil
}

func (v *NodeCapacity) StorageBytes() int64 {
	if v.data.StorageBytes == nil {
		return 0
	}
	return *v.data.StorageBytes
}

func (v *NodeCapacity) SetStorageBytes(storage_bytes int64) {
	v.data.StorageBytes = &storage_bytes
}

func (v *NodeCapacity) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *NodeCapacity) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *NodeCapacity) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *NodeCapacity) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type nodeUsageData struct {
	Node                *string             `cbor:"0,keyasint,omitempty" json:"node,omitempty"`
	NodeName            *string             `cbor:"1,keyasint,omitempty" json:"node_name,omitempty"`
	RunnerId            *string             `cbor:"2,keyasint,omitempty" json:"runner_id,omitempty"`
	Role                *string             `cbor:"3,keyasint,omitempty" json:"role,omitempty"`
	Status              *string             `cbor:"4,keyasint,omitempty" json:"status,omitempty"`
	Scheduling          *string             `cbor:"5,keyasint,omitempty" json:"scheduling,omitempty"`
	Capacity            *NodeCapacity       `cbor:"6,keyasint,omitempty" json:"capacity,omitempty"`
	Total               *ResourceTotals     `cbor:"7,keyasint,omitempty" json:"total,omitempty"`
	Sandboxes           *ResourceTotals     `cbor:"8,keyasint,omitempty" json:"sandboxes,omitempty"`
	System              *ResourceTotals     `cbor:"9,keyasint,omitempty" json:"system,omitempty"`
	Load1               *float64            `cbor:"10,keyasint,omitempty" json:"load1,omitempty"`
	Load5               *float64            `cbor:"11,keyasint,omitempty" json:"load5,omitempty"`
	Load15              *float64            `cbor:"12,keyasint,omitempty" json:"load15,omitempty"`
	StorageUsedBytes    *int64              `cbor:"13,keyasint,omitempty" json:"storage_used_bytes,omitempty"`
	SandboxCount        *int64              `cbor:"14,keyasint,omitempty" json:"sandbox_count,omitempty"`
	RunningSandboxCount *int64              `cbor:"15,keyasint,omitempty" json:"running_sandbox_count,omitempty"`
	MeasuredAt          *standard.Timestamp `cbor:"16,keyasint,omitempty" json:"measured_at,omitempty"`
	Stale               *bool               `cbor:"17,keyasint,omitempty" json:"stale,omitempty"`
}

type NodeUsage struct {
	data nodeUsageData
}

func (v *NodeUsage) HasNode() bool {
	return v.data.Node != nil
}

func (v *NodeUsage) Node() string {
	if v.data.Node == nil {
		return ""
	}
	return *v.data.Node
}

func (v *NodeUsage) SetNode(node string) {
	v.data.Node = &node
}

func (v *NodeUsage) HasNodeName() bool {
	return v.data.NodeName != nil
}

func (v *NodeUsage) NodeName() string {
	if v.data.NodeName == nil {
		return ""
	}
	return *v.data.NodeName
}

func (v *NodeUsage) SetNodeName(node_name string) {
	v.data.NodeName = &node_name
}

func (v *NodeUsage) HasRunnerId() bool {
	return v.data.RunnerId != nil
}

func (v *NodeUsage) RunnerId() string {
	if v.data.RunnerId == nil {
		return ""
	}
	return *v.data.RunnerId
}

func (v *NodeUsage) SetRunnerId(runner_id string) {
	v.data.RunnerId = &runner_id
}

func (v *NodeUsage) HasRole() bool {
	return v.data.Role != nil
}

func (v *NodeUsage) Role() string {
	if v.data.Role == nil {
		return ""
	}
	return *v.data.Role
}

func (v *NodeUsage) SetRole(role string) {
	v.data.Role = &role
}

func (v *NodeUsage) HasStatus() bool {
	return v.data.Status != nil
}

func (v *NodeUsage) Status() string {
	if v.data.Status == nil {
		return ""
	}
	return *v.data.Status
}

func (v *NodeUsage) SetStatus(status string) {
	v.data.Status = &status
}

func (v *NodeUsage) HasScheduling() bool {
	return v.data.Scheduling != nil
}

func (v *NodeUsage) Scheduling() string {
	if v.data.Scheduling == nil {
		return ""
	}
	return *v.data.Scheduling
}

func (v *NodeUsage) SetScheduling(scheduling string) {
	v.data.Scheduling = &scheduling
}

func (v *NodeUsage) HasCapacity() bool {
	return v.data.Capacity != nil
}

func (v *NodeUsage) Capacity() *NodeCapacity {
	return v.data.Capacity
}

func (v *NodeUsage) SetCapacity(capacity *NodeCapacity) {
	v.data.Capacity = capacity
}

func (v *NodeUsage) HasTotal() bool {
	return v.data.Total != nil
}

func (v *NodeUsage) Total() *ResourceTotals {
	return v.data.Total
}

func (v *NodeUsage) SetTotal(total *ResourceTotals) {
	v.data.Total = total
}

func (v *NodeUsage) HasSandboxes() bool {
	return v.data.Sandboxes != nil
}

func (v *NodeUsage) Sandboxes() *ResourceTotals {
	return v.data.Sandboxes
}

func (v *NodeUsage) SetSandboxes(sandboxes *ResourceTotals) {
	v.data.Sandboxes = sandboxes
}

func (v *NodeUsage) HasSystem() bool {
	return v.data.System != nil
}

func (v *NodeUsage) System() *ResourceTotals {
	return v.data.System
}

func (v *NodeUsage) SetSystem(system *ResourceTotals) {
	v.data.System = system
}

func (v *NodeUsage) HasLoad1() bool {
	return v.data.Load1 != nil
}

func (v *NodeUsage) Load1() float64 {
	if v.data.Load1 == nil {
		return 0
	}
	return *v.data.Load1
}

func (v *NodeUsage) SetLoad1(load1 float64) {
	v.data.Load1 = &load1
}

func (v *NodeUsage) HasLoad5() bool {
	return v.data.Load5 != nil
}

func (v *NodeUsage) Load5() float64 {
	if v.data.Load5 == nil {
		return 0
	}
	return *v.data.Load5
}

func (v *NodeUsage) SetLoad5(load5 float64) {
	v.data.Load5 = &load5
}

func (v *NodeUsage) HasLoad15() bool {
	return v.data.Load15 != nil
}

func (v *NodeUsage) Load15() float64 {
	if v.data.Load15 == nil {
		return 0
	}
	return *v.data.Load15
}

func (v *NodeUsage) SetLoad15(load15 float64) {
	v.data.Load15 = &load15
}

func (v *NodeUsage) HasStorageUsedBytes() bool {
	return v.data.StorageUsedBytes != nil
}

func (v *NodeUsage) StorageUsedBytes() int64 {
	if v.data.StorageUsedBytes == nil {
		return 0
	}
	return *v.data.StorageUsedBytes
}

func (v *NodeUsage) SetStorageUsedBytes(storage_used_bytes int64) {
	v.data.StorageUsedBytes = &storage_used_bytes
}

func (v *NodeUsage) HasSandboxCount() bool {
	return v.data.SandboxCount != nil
}

func (v *NodeUsage) SandboxCount() int64 {
	if v.data.SandboxCount == nil {
		return 0
	}
	return *v.data.SandboxCount
}

func (v *NodeUsage) SetSandboxCount(sandbox_count int64) {
	v.data.SandboxCount = &sandbox_count
}

func (v *NodeUsage) HasRunningSandboxCount() bool {
	return v.data.RunningSandboxCount != nil
}

func (v *NodeUsage) RunningSandboxCount() int64 {
	if v.data.RunningSandboxCount == nil {
		return 0
	}
	return *v.data.RunningSandboxCount
}

func (v *NodeUsage) SetRunningSandboxCount(running_sandbox_count int64) {
	v.data.RunningSandboxCount = &running_sandbox_count
}

func (v *NodeUsage) HasMeasuredAt() bool {
	return v.data.MeasuredAt != nil
}

func (v *NodeUsage) MeasuredAt() *standard.Timestamp {
	return v.data.MeasuredAt
}

func (v *NodeUsage) SetMeasuredAt(measured_at *standard.Timestamp) {
	v.data.MeasuredAt = measured_at
}

func (v *NodeUsage) HasStale() bool {
	return v.data.Stale != nil
}

func (v *NodeUsage) Stale() bool {
	if v.data.Stale == nil {
		return false
	}
	return *v.data.Stale
}

func (v *NodeUsage) SetStale(stale bool) {
	v.data.Stale = &stale
}

func (v *NodeUsage) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *NodeUsage) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *NodeUsage) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *NodeUsage) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type appUsageData struct {
	App          *string         `cbor:"0,keyasint,omitempty" json:"app,omitempty"`
	AppId        *string         `cbor:"1,keyasint,omitempty" json:"app_id,omitempty"`
	Total        *ResourceTotals `cbor:"2,keyasint,omitempty" json:"total,omitempty"`
	Services     *ResourceTotals `cbor:"3,keyasint,omitempty" json:"services,omitempty"`
	Addons       *ResourceTotals `cbor:"4,keyasint,omitempty" json:"addons,omitempty"`
	SandboxCount *int64          `cbor:"5,keyasint,omitempty" json:"sandbox_count,omitempty"`
	ServiceCount *int64          `cbor:"6,keyasint,omitempty" json:"service_count,omitempty"`
	AddonCount   *int64          `cbor:"7,keyasint,omitempty" json:"addon_count,omitempty"`
	Stale        *bool           `cbor:"8,keyasint,omitempty" json:"stale,omitempty"`
}

type AppUsage struct {
	data appUsageData
}

func (v *AppUsage) HasApp() bool {
	return v.data.App != nil
}

func (v *AppUsage) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *AppUsage) SetApp(app string) {
	v.data.App = &app
}

func (v *AppUsage) HasAppId() bool {
	return v.data.AppId != nil
}

func (v *AppUsage) AppId() string {
	if v.data.AppId == nil {
		return ""
	}
	return *v.data.AppId
}

func (v *AppUsage) SetAppId(app_id string) {
	v.data.AppId = &app_id
}

func (v *AppUsage) HasTotal() bool {
	return v.data.Total != nil
}

func (v *AppUsage) Total() *ResourceTotals {
	return v.data.Total
}

func (v *AppUsage) SetTotal(total *ResourceTotals) {
	v.data.Total = total
}

func (v *AppUsage) HasServices() bool {
	return v.data.Services != nil
}

func (v *AppUsage) Services() *ResourceTotals {
	return v.data.Services
}

func (v *AppUsage) SetServices(services *ResourceTotals) {
	v.data.Services = services
}

func (v *AppUsage) HasAddons() bool {
	return v.data.Addons != nil
}

func (v *AppUsage) Addons() *ResourceTotals {
	return v.data.Addons
}

func (v *AppUsage) SetAddons(addons *ResourceTotals) {
	v.data.Addons = addons
}

func (v *AppUsage) HasSandboxCount() bool {
	return v.data.SandboxCount != nil
}

func (v *AppUsage) SandboxCount() int64 {
	if v.data.SandboxCount == nil {
		return 0
	}
	return *v.data.SandboxCount
}

func (v *AppUsage) SetSandboxCount(sandbox_count int64) {
	v.data.SandboxCount = &sandbox_count
}

func (v *AppUsage) HasServiceCount() bool {
	return v.data.ServiceCount != nil
}

func (v *AppUsage) ServiceCount() int64 {
	if v.data.ServiceCount == nil {
		return 0
	}
	return *v.data.ServiceCount
}

func (v *AppUsage) SetServiceCount(service_count int64) {
	v.data.ServiceCount = &service_count
}

func (v *AppUsage) HasAddonCount() bool {
	return v.data.AddonCount != nil
}

func (v *AppUsage) AddonCount() int64 {
	if v.data.AddonCount == nil {
		return 0
	}
	return *v.data.AddonCount
}

func (v *AppUsage) SetAddonCount(addon_count int64) {
	v.data.AddonCount = &addon_count
}

func (v *AppUsage) HasStale() bool {
	return v.data.Stale != nil
}

func (v *AppUsage) Stale() bool {
	if v.data.Stale == nil {
		return false
	}
	return *v.data.Stale
}

func (v *AppUsage) SetStale(stale bool) {
	v.data.Stale = &stale
}

func (v *AppUsage) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *AppUsage) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *AppUsage) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *AppUsage) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type selectorData struct {
	App           *string `cbor:"0,keyasint,omitempty" json:"app,omitempty"`
	Service       *string `cbor:"1,keyasint,omitempty" json:"service,omitempty"`
	Node          *string `cbor:"2,keyasint,omitempty" json:"node,omitempty"`
	Kind          *string `cbor:"3,keyasint,omitempty" json:"kind,omitempty"`
	Status        *string `cbor:"4,keyasint,omitempty" json:"status,omitempty"`
	IncludeSystem *bool   `cbor:"5,keyasint,omitempty" json:"include_system,omitempty"`
	IncludeAddons *bool   `cbor:"6,keyasint,omitempty" json:"include_addons,omitempty"`
}

type Selector struct {
	data selectorData
}

func (v *Selector) HasApp() bool {
	return v.data.App != nil
}

func (v *Selector) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *Selector) SetApp(app string) {
	v.data.App = &app
}

func (v *Selector) HasService() bool {
	return v.data.Service != nil
}

func (v *Selector) Service() string {
	if v.data.Service == nil {
		return ""
	}
	return *v.data.Service
}

func (v *Selector) SetService(service string) {
	v.data.Service = &service
}

func (v *Selector) HasNode() bool {
	return v.data.Node != nil
}

func (v *Selector) Node() string {
	if v.data.Node == nil {
		return ""
	}
	return *v.data.Node
}

func (v *Selector) SetNode(node string) {
	v.data.Node = &node
}

func (v *Selector) HasKind() bool {
	return v.data.Kind != nil
}

func (v *Selector) Kind() string {
	if v.data.Kind == nil {
		return ""
	}
	return *v.data.Kind
}

func (v *Selector) SetKind(kind string) {
	v.data.Kind = &kind
}

func (v *Selector) HasStatus() bool {
	return v.data.Status != nil
}

func (v *Selector) Status() string {
	if v.data.Status == nil {
		return ""
	}
	return *v.data.Status
}

func (v *Selector) SetStatus(status string) {
	v.data.Status = &status
}

func (v *Selector) HasIncludeSystem() bool {
	return v.data.IncludeSystem != nil
}

func (v *Selector) IncludeSystem() bool {
	if v.data.IncludeSystem == nil {
		return false
	}
	return *v.data.IncludeSystem
}

func (v *Selector) SetIncludeSystem(include_system bool) {
	v.data.IncludeSystem = &include_system
}

func (v *Selector) HasIncludeAddons() bool {
	return v.data.IncludeAddons != nil
}

func (v *Selector) IncludeAddons() bool {
	if v.data.IncludeAddons == nil {
		return false
	}
	return *v.data.IncludeAddons
}

func (v *Selector) SetIncludeAddons(include_addons bool) {
	v.data.IncludeAddons = &include_addons
}

func (v *Selector) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *Selector) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *Selector) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *Selector) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type windowData struct {
	Start     *standard.Timestamp `cbor:"0,keyasint,omitempty" json:"start,omitempty"`
	End       *standard.Timestamp `cbor:"1,keyasint,omitempty" json:"end,omitempty"`
	Aggregate *string             `cbor:"2,keyasint,omitempty" json:"aggregate,omitempty"`
}

type Window struct {
	data windowData
}

func (v *Window) HasStart() bool {
	return v.data.Start != nil
}

func (v *Window) Start() *standard.Timestamp {
	return v.data.Start
}

func (v *Window) SetStart(start *standard.Timestamp) {
	v.data.Start = start
}

func (v *Window) HasEnd() bool {
	return v.data.End != nil
}

func (v *Window) End() *standard.Timestamp {
	return v.data.End
}

func (v *Window) SetEnd(end *standard.Timestamp) {
	v.data.End = end
}

func (v *Window) HasAggregate() bool {
	return v.data.Aggregate != nil
}

func (v *Window) Aggregate() string {
	if v.data.Aggregate == nil {
		return ""
	}
	return *v.data.Aggregate
}

func (v *Window) SetAggregate(aggregate string) {
	v.data.Aggregate = &aggregate
}

func (v *Window) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *Window) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *Window) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *Window) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type orderingData struct {
	Sort  *string `cbor:"0,keyasint,omitempty" json:"sort,omitempty"`
	Order *string `cbor:"1,keyasint,omitempty" json:"order,omitempty"`
	Limit *int32  `cbor:"2,keyasint,omitempty" json:"limit,omitempty"`
}

type Ordering struct {
	data orderingData
}

func (v *Ordering) HasSort() bool {
	return v.data.Sort != nil
}

func (v *Ordering) Sort() string {
	if v.data.Sort == nil {
		return ""
	}
	return *v.data.Sort
}

func (v *Ordering) SetSort(sort string) {
	v.data.Sort = &sort
}

func (v *Ordering) HasOrder() bool {
	return v.data.Order != nil
}

func (v *Ordering) Order() string {
	if v.data.Order == nil {
		return ""
	}
	return *v.data.Order
}

func (v *Ordering) SetOrder(order string) {
	v.data.Order = &order
}

func (v *Ordering) HasLimit() bool {
	return v.data.Limit != nil
}

func (v *Ordering) Limit() int32 {
	if v.data.Limit == nil {
		return 0
	}
	return *v.data.Limit
}

func (v *Ordering) SetLimit(limit int32) {
	v.data.Limit = &limit
}

func (v *Ordering) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *Ordering) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *Ordering) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *Ordering) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type detailOptionsData struct {
	IncludeSeries     *bool  `cbor:"0,keyasint,omitempty" json:"include_series,omitempty"`
	IncludeContainers *bool  `cbor:"1,keyasint,omitempty" json:"include_containers,omitempty"`
	StepSeconds       *int64 `cbor:"2,keyasint,omitempty" json:"step_seconds,omitempty"`
}

type DetailOptions struct {
	data detailOptionsData
}

func (v *DetailOptions) HasIncludeSeries() bool {
	return v.data.IncludeSeries != nil
}

func (v *DetailOptions) IncludeSeries() bool {
	if v.data.IncludeSeries == nil {
		return false
	}
	return *v.data.IncludeSeries
}

func (v *DetailOptions) SetIncludeSeries(include_series bool) {
	v.data.IncludeSeries = &include_series
}

func (v *DetailOptions) HasIncludeContainers() bool {
	return v.data.IncludeContainers != nil
}

func (v *DetailOptions) IncludeContainers() bool {
	if v.data.IncludeContainers == nil {
		return false
	}
	return *v.data.IncludeContainers
}

func (v *DetailOptions) SetIncludeContainers(include_containers bool) {
	v.data.IncludeContainers = &include_containers
}

func (v *DetailOptions) HasStepSeconds() bool {
	return v.data.StepSeconds != nil
}

func (v *DetailOptions) StepSeconds() int64 {
	if v.data.StepSeconds == nil {
		return 0
	}
	return *v.data.StepSeconds
}

func (v *DetailOptions) SetStepSeconds(step_seconds int64) {
	v.data.StepSeconds = &step_seconds
}

func (v *DetailOptions) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DetailOptions) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DetailOptions) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DetailOptions) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type containerUsageData struct {
	Name   *string      `cbor:"0,keyasint,omitempty" json:"name,omitempty"`
	Cpu    *CpuUsage    `cbor:"1,keyasint,omitempty" json:"cpu,omitempty"`
	Memory *MemoryUsage `cbor:"2,keyasint,omitempty" json:"memory,omitempty"`
}

type ContainerUsage struct {
	data containerUsageData
}

func (v *ContainerUsage) HasName() bool {
	return v.data.Name != nil
}

func (v *ContainerUsage) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *ContainerUsage) SetName(name string) {
	v.data.Name = &name
}

func (v *ContainerUsage) HasCpu() bool {
	return v.data.Cpu != nil
}

func (v *ContainerUsage) Cpu() *CpuUsage {
	return v.data.Cpu
}

func (v *ContainerUsage) SetCpu(cpu *CpuUsage) {
	v.data.Cpu = cpu
}

func (v *ContainerUsage) HasMemory() bool {
	return v.data.Memory != nil
}

func (v *ContainerUsage) Memory() *MemoryUsage {
	return v.data.Memory
}

func (v *ContainerUsage) SetMemory(memory *MemoryUsage) {
	v.data.Memory = memory
}

func (v *ContainerUsage) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ContainerUsage) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ContainerUsage) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ContainerUsage) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type usagePointData struct {
	At    *standard.Timestamp `cbor:"0,keyasint,omitempty" json:"at,omitempty"`
	Value *float64            `cbor:"1,keyasint,omitempty" json:"value,omitempty"`
}

type UsagePoint struct {
	data usagePointData
}

func (v *UsagePoint) HasAt() bool {
	return v.data.At != nil
}

func (v *UsagePoint) At() *standard.Timestamp {
	return v.data.At
}

func (v *UsagePoint) SetAt(at *standard.Timestamp) {
	v.data.At = at
}

func (v *UsagePoint) HasValue() bool {
	return v.data.Value != nil
}

func (v *UsagePoint) Value() float64 {
	if v.data.Value == nil {
		return 0
	}
	return *v.data.Value
}

func (v *UsagePoint) SetValue(value float64) {
	v.data.Value = &value
}

func (v *UsagePoint) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *UsagePoint) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *UsagePoint) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *UsagePoint) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type usageSeriesData struct {
	Metric *string        `cbor:"0,keyasint,omitempty" json:"metric,omitempty"`
	Points *[]*UsagePoint `cbor:"1,keyasint,omitempty" json:"points,omitempty"`
}

type UsageSeries struct {
	data usageSeriesData
}

func (v *UsageSeries) HasMetric() bool {
	return v.data.Metric != nil
}

func (v *UsageSeries) Metric() string {
	if v.data.Metric == nil {
		return ""
	}
	return *v.data.Metric
}

func (v *UsageSeries) SetMetric(metric string) {
	v.data.Metric = &metric
}

func (v *UsageSeries) HasPoints() bool {
	return v.data.Points != nil
}

func (v *UsageSeries) Points() []*UsagePoint {
	if v.data.Points == nil {
		return nil
	}
	return *v.data.Points
}

func (v *UsageSeries) SetPoints(points []*UsagePoint) {
	x := slices.Clone(points)
	v.data.Points = &x
}

func (v *UsageSeries) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *UsageSeries) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *UsageSeries) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *UsageSeries) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type sandboxExitData struct {
	Code      *int32              `cbor:"0,keyasint,omitempty" json:"code,omitempty"`
	At        *standard.Timestamp `cbor:"1,keyasint,omitempty" json:"at,omitempty"`
	Container *string             `cbor:"2,keyasint,omitempty" json:"container,omitempty"`
}

type SandboxExit struct {
	data sandboxExitData
}

func (v *SandboxExit) HasCode() bool {
	return v.data.Code != nil
}

func (v *SandboxExit) Code() int32 {
	if v.data.Code == nil {
		return 0
	}
	return *v.data.Code
}

func (v *SandboxExit) SetCode(code int32) {
	v.data.Code = &code
}

func (v *SandboxExit) HasAt() bool {
	return v.data.At != nil
}

func (v *SandboxExit) At() *standard.Timestamp {
	return v.data.At
}

func (v *SandboxExit) SetAt(at *standard.Timestamp) {
	v.data.At = at
}

func (v *SandboxExit) HasContainer() bool {
	return v.data.Container != nil
}

func (v *SandboxExit) Container() string {
	if v.data.Container == nil {
		return ""
	}
	return *v.data.Container
}

func (v *SandboxExit) SetContainer(container string) {
	v.data.Container = &container
}

func (v *SandboxExit) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SandboxExit) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SandboxExit) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SandboxExit) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type crashLoopStateData struct {
	ConsecutiveCrashes *int32              `cbor:"0,keyasint,omitempty" json:"consecutive_crashes,omitempty"`
	LastCrashAt        *standard.Timestamp `cbor:"1,keyasint,omitempty" json:"last_crash_at,omitempty"`
	CooldownUntil      *standard.Timestamp `cbor:"2,keyasint,omitempty" json:"cooldown_until,omitempty"`
}

type CrashLoopState struct {
	data crashLoopStateData
}

func (v *CrashLoopState) HasConsecutiveCrashes() bool {
	return v.data.ConsecutiveCrashes != nil
}

func (v *CrashLoopState) ConsecutiveCrashes() int32 {
	if v.data.ConsecutiveCrashes == nil {
		return 0
	}
	return *v.data.ConsecutiveCrashes
}

func (v *CrashLoopState) SetConsecutiveCrashes(consecutive_crashes int32) {
	v.data.ConsecutiveCrashes = &consecutive_crashes
}

func (v *CrashLoopState) HasLastCrashAt() bool {
	return v.data.LastCrashAt != nil
}

func (v *CrashLoopState) LastCrashAt() *standard.Timestamp {
	return v.data.LastCrashAt
}

func (v *CrashLoopState) SetLastCrashAt(last_crash_at *standard.Timestamp) {
	v.data.LastCrashAt = last_crash_at
}

func (v *CrashLoopState) HasCooldownUntil() bool {
	return v.data.CooldownUntil != nil
}

func (v *CrashLoopState) CooldownUntil() *standard.Timestamp {
	return v.data.CooldownUntil
}

func (v *CrashLoopState) SetCooldownUntil(cooldown_until *standard.Timestamp) {
	v.data.CooldownUntil = cooldown_until
}

func (v *CrashLoopState) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CrashLoopState) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CrashLoopState) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CrashLoopState) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type resourceUsageListSandboxesArgsData struct {
	Selector *Selector `cbor:"0,keyasint,omitempty" json:"selector,omitempty"`
	Window   *Window   `cbor:"1,keyasint,omitempty" json:"window,omitempty"`
	Ordering *Ordering `cbor:"2,keyasint,omitempty" json:"ordering,omitempty"`
}

type ResourceUsageListSandboxesArgs struct {
	call rpc.Call
	data resourceUsageListSandboxesArgsData
}

func (v *ResourceUsageListSandboxesArgs) HasSelector() bool {
	return v.data.Selector != nil
}

func (v *ResourceUsageListSandboxesArgs) Selector() *Selector {
	return v.data.Selector
}

func (v *ResourceUsageListSandboxesArgs) HasWindow() bool {
	return v.data.Window != nil
}

func (v *ResourceUsageListSandboxesArgs) Window() *Window {
	return v.data.Window
}

func (v *ResourceUsageListSandboxesArgs) HasOrdering() bool {
	return v.data.Ordering != nil
}

func (v *ResourceUsageListSandboxesArgs) Ordering() *Ordering {
	return v.data.Ordering
}

func (v *ResourceUsageListSandboxesArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ResourceUsageListSandboxesArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ResourceUsageListSandboxesArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ResourceUsageListSandboxesArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type resourceUsageListSandboxesResultsData struct {
	Sandboxes   *[]*SandboxUsage    `cbor:"0,keyasint,omitempty" json:"sandboxes,omitempty"`
	Window      *UsageWindow        `cbor:"1,keyasint,omitempty" json:"window,omitempty"`
	Cluster     *ResourceTotals     `cbor:"2,keyasint,omitempty" json:"cluster,omitempty"`
	TotalCount  *int32              `cbor:"3,keyasint,omitempty" json:"total_count,omitempty"`
	CollectedAt *standard.Timestamp `cbor:"4,keyasint,omitempty" json:"collected_at,omitempty"`
	Warnings    *[]string           `cbor:"5,keyasint,omitempty" json:"warnings,omitempty"`
}

type ResourceUsageListSandboxesResults struct {
	call rpc.Call
	data resourceUsageListSandboxesResultsData
}

func (v *ResourceUsageListSandboxesResults) SetSandboxes(sandboxes []*SandboxUsage) {
	x := slices.Clone(sandboxes)
	v.data.Sandboxes = &x
}

func (v *ResourceUsageListSandboxesResults) SetWindow(window *UsageWindow) {
	v.data.Window = window
}

func (v *ResourceUsageListSandboxesResults) SetCluster(cluster *ResourceTotals) {
	v.data.Cluster = cluster
}

func (v *ResourceUsageListSandboxesResults) SetTotalCount(total_count int32) {
	v.data.TotalCount = &total_count
}

func (v *ResourceUsageListSandboxesResults) SetCollectedAt(collected_at *standard.Timestamp) {
	v.data.CollectedAt = collected_at
}

func (v *ResourceUsageListSandboxesResults) SetWarnings(warnings []string) {
	x := slices.Clone(warnings)
	v.data.Warnings = &x
}

func (v *ResourceUsageListSandboxesResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ResourceUsageListSandboxesResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ResourceUsageListSandboxesResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ResourceUsageListSandboxesResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type resourceUsageGetSandboxArgsData struct {
	Sandbox *string        `cbor:"0,keyasint,omitempty" json:"sandbox,omitempty"`
	Window  *Window        `cbor:"1,keyasint,omitempty" json:"window,omitempty"`
	Options *DetailOptions `cbor:"2,keyasint,omitempty" json:"options,omitempty"`
}

type ResourceUsageGetSandboxArgs struct {
	call rpc.Call
	data resourceUsageGetSandboxArgsData
}

func (v *ResourceUsageGetSandboxArgs) HasSandbox() bool {
	return v.data.Sandbox != nil
}

func (v *ResourceUsageGetSandboxArgs) Sandbox() string {
	if v.data.Sandbox == nil {
		return ""
	}
	return *v.data.Sandbox
}

func (v *ResourceUsageGetSandboxArgs) HasWindow() bool {
	return v.data.Window != nil
}

func (v *ResourceUsageGetSandboxArgs) Window() *Window {
	return v.data.Window
}

func (v *ResourceUsageGetSandboxArgs) HasOptions() bool {
	return v.data.Options != nil
}

func (v *ResourceUsageGetSandboxArgs) Options() *DetailOptions {
	return v.data.Options
}

func (v *ResourceUsageGetSandboxArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ResourceUsageGetSandboxArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ResourceUsageGetSandboxArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ResourceUsageGetSandboxArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type resourceUsageGetSandboxResultsData struct {
	Usage         *SandboxUsage      `cbor:"0,keyasint,omitempty" json:"usage,omitempty"`
	Containers    *[]*ContainerUsage `cbor:"1,keyasint,omitempty" json:"containers,omitempty"`
	Series        *[]*UsageSeries    `cbor:"2,keyasint,omitempty" json:"series,omitempty"`
	Exit          *SandboxExit       `cbor:"3,keyasint,omitempty" json:"exit,omitempty"`
	CrashLoop     *CrashLoopState    `cbor:"4,keyasint,omitempty" json:"crash_loop,omitempty"`
	Image         *string            `cbor:"5,keyasint,omitempty" json:"image,omitempty"`
	RestartPolicy *string            `cbor:"6,keyasint,omitempty" json:"restart_policy,omitempty"`
	Window        *UsageWindow       `cbor:"7,keyasint,omitempty" json:"window,omitempty"`
	Warnings      *[]string          `cbor:"8,keyasint,omitempty" json:"warnings,omitempty"`
}

type ResourceUsageGetSandboxResults struct {
	call rpc.Call
	data resourceUsageGetSandboxResultsData
}

func (v *ResourceUsageGetSandboxResults) SetUsage(usage *SandboxUsage) {
	v.data.Usage = usage
}

func (v *ResourceUsageGetSandboxResults) SetContainers(containers []*ContainerUsage) {
	x := slices.Clone(containers)
	v.data.Containers = &x
}

func (v *ResourceUsageGetSandboxResults) SetSeries(series []*UsageSeries) {
	x := slices.Clone(series)
	v.data.Series = &x
}

func (v *ResourceUsageGetSandboxResults) SetExit(exit *SandboxExit) {
	v.data.Exit = exit
}

func (v *ResourceUsageGetSandboxResults) SetCrashLoop(crash_loop *CrashLoopState) {
	v.data.CrashLoop = crash_loop
}

func (v *ResourceUsageGetSandboxResults) SetImage(image string) {
	v.data.Image = &image
}

func (v *ResourceUsageGetSandboxResults) SetRestartPolicy(restart_policy string) {
	v.data.RestartPolicy = &restart_policy
}

func (v *ResourceUsageGetSandboxResults) SetWindow(window *UsageWindow) {
	v.data.Window = window
}

func (v *ResourceUsageGetSandboxResults) SetWarnings(warnings []string) {
	x := slices.Clone(warnings)
	v.data.Warnings = &x
}

func (v *ResourceUsageGetSandboxResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ResourceUsageGetSandboxResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ResourceUsageGetSandboxResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ResourceUsageGetSandboxResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type resourceUsageListNodesArgsData struct {
	Selector *Selector `cbor:"0,keyasint,omitempty" json:"selector,omitempty"`
	Window   *Window   `cbor:"1,keyasint,omitempty" json:"window,omitempty"`
	Ordering *Ordering `cbor:"2,keyasint,omitempty" json:"ordering,omitempty"`
}

type ResourceUsageListNodesArgs struct {
	call rpc.Call
	data resourceUsageListNodesArgsData
}

func (v *ResourceUsageListNodesArgs) HasSelector() bool {
	return v.data.Selector != nil
}

func (v *ResourceUsageListNodesArgs) Selector() *Selector {
	return v.data.Selector
}

func (v *ResourceUsageListNodesArgs) HasWindow() bool {
	return v.data.Window != nil
}

func (v *ResourceUsageListNodesArgs) Window() *Window {
	return v.data.Window
}

func (v *ResourceUsageListNodesArgs) HasOrdering() bool {
	return v.data.Ordering != nil
}

func (v *ResourceUsageListNodesArgs) Ordering() *Ordering {
	return v.data.Ordering
}

func (v *ResourceUsageListNodesArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ResourceUsageListNodesArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ResourceUsageListNodesArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ResourceUsageListNodesArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type resourceUsageListNodesResultsData struct {
	Nodes       *[]*NodeUsage       `cbor:"0,keyasint,omitempty" json:"nodes,omitempty"`
	Cluster     *ResourceTotals     `cbor:"1,keyasint,omitempty" json:"cluster,omitempty"`
	Capacity    *NodeCapacity       `cbor:"2,keyasint,omitempty" json:"capacity,omitempty"`
	Window      *UsageWindow        `cbor:"3,keyasint,omitempty" json:"window,omitempty"`
	CollectedAt *standard.Timestamp `cbor:"4,keyasint,omitempty" json:"collected_at,omitempty"`
	Warnings    *[]string           `cbor:"5,keyasint,omitempty" json:"warnings,omitempty"`
}

type ResourceUsageListNodesResults struct {
	call rpc.Call
	data resourceUsageListNodesResultsData
}

func (v *ResourceUsageListNodesResults) SetNodes(nodes []*NodeUsage) {
	x := slices.Clone(nodes)
	v.data.Nodes = &x
}

func (v *ResourceUsageListNodesResults) SetCluster(cluster *ResourceTotals) {
	v.data.Cluster = cluster
}

func (v *ResourceUsageListNodesResults) SetCapacity(capacity *NodeCapacity) {
	v.data.Capacity = capacity
}

func (v *ResourceUsageListNodesResults) SetWindow(window *UsageWindow) {
	v.data.Window = window
}

func (v *ResourceUsageListNodesResults) SetCollectedAt(collected_at *standard.Timestamp) {
	v.data.CollectedAt = collected_at
}

func (v *ResourceUsageListNodesResults) SetWarnings(warnings []string) {
	x := slices.Clone(warnings)
	v.data.Warnings = &x
}

func (v *ResourceUsageListNodesResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ResourceUsageListNodesResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ResourceUsageListNodesResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ResourceUsageListNodesResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type resourceUsageListAppsArgsData struct {
	Selector *Selector `cbor:"0,keyasint,omitempty" json:"selector,omitempty"`
	Window   *Window   `cbor:"1,keyasint,omitempty" json:"window,omitempty"`
	Ordering *Ordering `cbor:"2,keyasint,omitempty" json:"ordering,omitempty"`
}

type ResourceUsageListAppsArgs struct {
	call rpc.Call
	data resourceUsageListAppsArgsData
}

func (v *ResourceUsageListAppsArgs) HasSelector() bool {
	return v.data.Selector != nil
}

func (v *ResourceUsageListAppsArgs) Selector() *Selector {
	return v.data.Selector
}

func (v *ResourceUsageListAppsArgs) HasWindow() bool {
	return v.data.Window != nil
}

func (v *ResourceUsageListAppsArgs) Window() *Window {
	return v.data.Window
}

func (v *ResourceUsageListAppsArgs) HasOrdering() bool {
	return v.data.Ordering != nil
}

func (v *ResourceUsageListAppsArgs) Ordering() *Ordering {
	return v.data.Ordering
}

func (v *ResourceUsageListAppsArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ResourceUsageListAppsArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ResourceUsageListAppsArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ResourceUsageListAppsArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type resourceUsageListAppsResultsData struct {
	Apps        *[]*AppUsage        `cbor:"0,keyasint,omitempty" json:"apps,omitempty"`
	Cluster     *ResourceTotals     `cbor:"1,keyasint,omitempty" json:"cluster,omitempty"`
	TotalCount  *int32              `cbor:"2,keyasint,omitempty" json:"total_count,omitempty"`
	Window      *UsageWindow        `cbor:"3,keyasint,omitempty" json:"window,omitempty"`
	CollectedAt *standard.Timestamp `cbor:"4,keyasint,omitempty" json:"collected_at,omitempty"`
	Warnings    *[]string           `cbor:"5,keyasint,omitempty" json:"warnings,omitempty"`
}

type ResourceUsageListAppsResults struct {
	call rpc.Call
	data resourceUsageListAppsResultsData
}

func (v *ResourceUsageListAppsResults) SetApps(apps []*AppUsage) {
	x := slices.Clone(apps)
	v.data.Apps = &x
}

func (v *ResourceUsageListAppsResults) SetCluster(cluster *ResourceTotals) {
	v.data.Cluster = cluster
}

func (v *ResourceUsageListAppsResults) SetTotalCount(total_count int32) {
	v.data.TotalCount = &total_count
}

func (v *ResourceUsageListAppsResults) SetWindow(window *UsageWindow) {
	v.data.Window = window
}

func (v *ResourceUsageListAppsResults) SetCollectedAt(collected_at *standard.Timestamp) {
	v.data.CollectedAt = collected_at
}

func (v *ResourceUsageListAppsResults) SetWarnings(warnings []string) {
	x := slices.Clone(warnings)
	v.data.Warnings = &x
}

func (v *ResourceUsageListAppsResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ResourceUsageListAppsResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ResourceUsageListAppsResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ResourceUsageListAppsResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type resourceUsageHttpListSandboxesArgsData struct {
	App           *string `cbor:"0,keyasint,omitempty" json:"app,omitempty"`
	Service       *string `cbor:"1,keyasint,omitempty" json:"service,omitempty"`
	Node          *string `cbor:"2,keyasint,omitempty" json:"node,omitempty"`
	Kind          *string `cbor:"3,keyasint,omitempty" json:"kind,omitempty"`
	Status        *string `cbor:"4,keyasint,omitempty" json:"status,omitempty"`
	IncludeSystem *bool   `cbor:"5,keyasint,omitempty" json:"include_system,omitempty"`
	Since         *string `cbor:"6,keyasint,omitempty" json:"since,omitempty"`
	Until         *string `cbor:"7,keyasint,omitempty" json:"until,omitempty"`
	Aggregate     *string `cbor:"8,keyasint,omitempty" json:"aggregate,omitempty"`
	Sort          *string `cbor:"9,keyasint,omitempty" json:"sort,omitempty"`
	Order         *string `cbor:"10,keyasint,omitempty" json:"order,omitempty"`
	Limit         *int32  `cbor:"11,keyasint,omitempty" json:"limit,omitempty"`
}

type ResourceUsageHttpListSandboxesArgs struct {
	call rpc.Call
	data resourceUsageHttpListSandboxesArgsData
}

func (v *ResourceUsageHttpListSandboxesArgs) HasApp() bool {
	return v.data.App != nil
}

func (v *ResourceUsageHttpListSandboxesArgs) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *ResourceUsageHttpListSandboxesArgs) HasService() bool {
	return v.data.Service != nil
}

func (v *ResourceUsageHttpListSandboxesArgs) Service() string {
	if v.data.Service == nil {
		return ""
	}
	return *v.data.Service
}

func (v *ResourceUsageHttpListSandboxesArgs) HasNode() bool {
	return v.data.Node != nil
}

func (v *ResourceUsageHttpListSandboxesArgs) Node() string {
	if v.data.Node == nil {
		return ""
	}
	return *v.data.Node
}

func (v *ResourceUsageHttpListSandboxesArgs) HasKind() bool {
	return v.data.Kind != nil
}

func (v *ResourceUsageHttpListSandboxesArgs) Kind() string {
	if v.data.Kind == nil {
		return ""
	}
	return *v.data.Kind
}

func (v *ResourceUsageHttpListSandboxesArgs) HasStatus() bool {
	return v.data.Status != nil
}

func (v *ResourceUsageHttpListSandboxesArgs) Status() string {
	if v.data.Status == nil {
		return ""
	}
	return *v.data.Status
}

func (v *ResourceUsageHttpListSandboxesArgs) HasIncludeSystem() bool {
	return v.data.IncludeSystem != nil
}

func (v *ResourceUsageHttpListSandboxesArgs) IncludeSystem() bool {
	if v.data.IncludeSystem == nil {
		return false
	}
	return *v.data.IncludeSystem
}

func (v *ResourceUsageHttpListSandboxesArgs) HasSince() bool {
	return v.data.Since != nil
}

func (v *ResourceUsageHttpListSandboxesArgs) Since() string {
	if v.data.Since == nil {
		return ""
	}
	return *v.data.Since
}

func (v *ResourceUsageHttpListSandboxesArgs) HasUntil() bool {
	return v.data.Until != nil
}

func (v *ResourceUsageHttpListSandboxesArgs) Until() string {
	if v.data.Until == nil {
		return ""
	}
	return *v.data.Until
}

func (v *ResourceUsageHttpListSandboxesArgs) HasAggregate() bool {
	return v.data.Aggregate != nil
}

func (v *ResourceUsageHttpListSandboxesArgs) Aggregate() string {
	if v.data.Aggregate == nil {
		return ""
	}
	return *v.data.Aggregate
}

func (v *ResourceUsageHttpListSandboxesArgs) HasSort() bool {
	return v.data.Sort != nil
}

func (v *ResourceUsageHttpListSandboxesArgs) Sort() string {
	if v.data.Sort == nil {
		return ""
	}
	return *v.data.Sort
}

func (v *ResourceUsageHttpListSandboxesArgs) HasOrder() bool {
	return v.data.Order != nil
}

func (v *ResourceUsageHttpListSandboxesArgs) Order() string {
	if v.data.Order == nil {
		return ""
	}
	return *v.data.Order
}

func (v *ResourceUsageHttpListSandboxesArgs) HasLimit() bool {
	return v.data.Limit != nil
}

func (v *ResourceUsageHttpListSandboxesArgs) Limit() int32 {
	if v.data.Limit == nil {
		return 0
	}
	return *v.data.Limit
}

func (v *ResourceUsageHttpListSandboxesArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ResourceUsageHttpListSandboxesArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ResourceUsageHttpListSandboxesArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ResourceUsageHttpListSandboxesArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type resourceUsageHttpListSandboxesResultsData struct {
	Sandboxes   *[]*SandboxUsage    `cbor:"0,keyasint,omitempty" json:"sandboxes,omitempty"`
	Window      *UsageWindow        `cbor:"1,keyasint,omitempty" json:"window,omitempty"`
	Cluster     *ResourceTotals     `cbor:"2,keyasint,omitempty" json:"cluster,omitempty"`
	TotalCount  *int32              `cbor:"3,keyasint,omitempty" json:"total_count,omitempty"`
	CollectedAt *standard.Timestamp `cbor:"4,keyasint,omitempty" json:"collected_at,omitempty"`
	Warnings    *[]string           `cbor:"5,keyasint,omitempty" json:"warnings,omitempty"`
}

type ResourceUsageHttpListSandboxesResults struct {
	call rpc.Call
	data resourceUsageHttpListSandboxesResultsData
}

func (v *ResourceUsageHttpListSandboxesResults) SetSandboxes(sandboxes []*SandboxUsage) {
	x := slices.Clone(sandboxes)
	v.data.Sandboxes = &x
}

func (v *ResourceUsageHttpListSandboxesResults) SetWindow(window *UsageWindow) {
	v.data.Window = window
}

func (v *ResourceUsageHttpListSandboxesResults) SetCluster(cluster *ResourceTotals) {
	v.data.Cluster = cluster
}

func (v *ResourceUsageHttpListSandboxesResults) SetTotalCount(total_count int32) {
	v.data.TotalCount = &total_count
}

func (v *ResourceUsageHttpListSandboxesResults) SetCollectedAt(collected_at *standard.Timestamp) {
	v.data.CollectedAt = collected_at
}

func (v *ResourceUsageHttpListSandboxesResults) SetWarnings(warnings []string) {
	x := slices.Clone(warnings)
	v.data.Warnings = &x
}

func (v *ResourceUsageHttpListSandboxesResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ResourceUsageHttpListSandboxesResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ResourceUsageHttpListSandboxesResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ResourceUsageHttpListSandboxesResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type resourceUsageHttpGetSandboxArgsData struct {
	Sandbox    *string `cbor:"0,keyasint,omitempty" json:"sandbox,omitempty"`
	Since      *string `cbor:"1,keyasint,omitempty" json:"since,omitempty"`
	Until      *string `cbor:"2,keyasint,omitempty" json:"until,omitempty"`
	Aggregate  *string `cbor:"3,keyasint,omitempty" json:"aggregate,omitempty"`
	Series     *bool   `cbor:"4,keyasint,omitempty" json:"series,omitempty"`
	Containers *bool   `cbor:"5,keyasint,omitempty" json:"containers,omitempty"`
	Step       *string `cbor:"6,keyasint,omitempty" json:"step,omitempty"`
}

type ResourceUsageHttpGetSandboxArgs struct {
	call rpc.Call
	data resourceUsageHttpGetSandboxArgsData
}

func (v *ResourceUsageHttpGetSandboxArgs) HasSandbox() bool {
	return v.data.Sandbox != nil
}

func (v *ResourceUsageHttpGetSandboxArgs) Sandbox() string {
	if v.data.Sandbox == nil {
		return ""
	}
	return *v.data.Sandbox
}

func (v *ResourceUsageHttpGetSandboxArgs) HasSince() bool {
	return v.data.Since != nil
}

func (v *ResourceUsageHttpGetSandboxArgs) Since() string {
	if v.data.Since == nil {
		return ""
	}
	return *v.data.Since
}

func (v *ResourceUsageHttpGetSandboxArgs) HasUntil() bool {
	return v.data.Until != nil
}

func (v *ResourceUsageHttpGetSandboxArgs) Until() string {
	if v.data.Until == nil {
		return ""
	}
	return *v.data.Until
}

func (v *ResourceUsageHttpGetSandboxArgs) HasAggregate() bool {
	return v.data.Aggregate != nil
}

func (v *ResourceUsageHttpGetSandboxArgs) Aggregate() string {
	if v.data.Aggregate == nil {
		return ""
	}
	return *v.data.Aggregate
}

func (v *ResourceUsageHttpGetSandboxArgs) HasSeries() bool {
	return v.data.Series != nil
}

func (v *ResourceUsageHttpGetSandboxArgs) Series() bool {
	if v.data.Series == nil {
		return false
	}
	return *v.data.Series
}

func (v *ResourceUsageHttpGetSandboxArgs) HasContainers() bool {
	return v.data.Containers != nil
}

func (v *ResourceUsageHttpGetSandboxArgs) Containers() bool {
	if v.data.Containers == nil {
		return false
	}
	return *v.data.Containers
}

func (v *ResourceUsageHttpGetSandboxArgs) HasStep() bool {
	return v.data.Step != nil
}

func (v *ResourceUsageHttpGetSandboxArgs) Step() string {
	if v.data.Step == nil {
		return ""
	}
	return *v.data.Step
}

func (v *ResourceUsageHttpGetSandboxArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ResourceUsageHttpGetSandboxArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ResourceUsageHttpGetSandboxArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ResourceUsageHttpGetSandboxArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type resourceUsageHttpGetSandboxResultsData struct {
	Usage         *SandboxUsage      `cbor:"0,keyasint,omitempty" json:"usage,omitempty"`
	Containers    *[]*ContainerUsage `cbor:"1,keyasint,omitempty" json:"containers,omitempty"`
	Series        *[]*UsageSeries    `cbor:"2,keyasint,omitempty" json:"series,omitempty"`
	Exit          *SandboxExit       `cbor:"3,keyasint,omitempty" json:"exit,omitempty"`
	CrashLoop     *CrashLoopState    `cbor:"4,keyasint,omitempty" json:"crash_loop,omitempty"`
	Image         *string            `cbor:"5,keyasint,omitempty" json:"image,omitempty"`
	RestartPolicy *string            `cbor:"6,keyasint,omitempty" json:"restart_policy,omitempty"`
	Window        *UsageWindow       `cbor:"7,keyasint,omitempty" json:"window,omitempty"`
	Warnings      *[]string          `cbor:"8,keyasint,omitempty" json:"warnings,omitempty"`
}

type ResourceUsageHttpGetSandboxResults struct {
	call rpc.Call
	data resourceUsageHttpGetSandboxResultsData
}

func (v *ResourceUsageHttpGetSandboxResults) SetUsage(usage *SandboxUsage) {
	v.data.Usage = usage
}

func (v *ResourceUsageHttpGetSandboxResults) SetContainers(containers []*ContainerUsage) {
	x := slices.Clone(containers)
	v.data.Containers = &x
}

func (v *ResourceUsageHttpGetSandboxResults) SetSeries(series []*UsageSeries) {
	x := slices.Clone(series)
	v.data.Series = &x
}

func (v *ResourceUsageHttpGetSandboxResults) SetExit(exit *SandboxExit) {
	v.data.Exit = exit
}

func (v *ResourceUsageHttpGetSandboxResults) SetCrashLoop(crash_loop *CrashLoopState) {
	v.data.CrashLoop = crash_loop
}

func (v *ResourceUsageHttpGetSandboxResults) SetImage(image string) {
	v.data.Image = &image
}

func (v *ResourceUsageHttpGetSandboxResults) SetRestartPolicy(restart_policy string) {
	v.data.RestartPolicy = &restart_policy
}

func (v *ResourceUsageHttpGetSandboxResults) SetWindow(window *UsageWindow) {
	v.data.Window = window
}

func (v *ResourceUsageHttpGetSandboxResults) SetWarnings(warnings []string) {
	x := slices.Clone(warnings)
	v.data.Warnings = &x
}

func (v *ResourceUsageHttpGetSandboxResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ResourceUsageHttpGetSandboxResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ResourceUsageHttpGetSandboxResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ResourceUsageHttpGetSandboxResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type resourceUsageHttpListNodesArgsData struct {
	Since     *string `cbor:"0,keyasint,omitempty" json:"since,omitempty"`
	Until     *string `cbor:"1,keyasint,omitempty" json:"until,omitempty"`
	Aggregate *string `cbor:"2,keyasint,omitempty" json:"aggregate,omitempty"`
	Sort      *string `cbor:"3,keyasint,omitempty" json:"sort,omitempty"`
	Order     *string `cbor:"4,keyasint,omitempty" json:"order,omitempty"`
	Limit     *int32  `cbor:"5,keyasint,omitempty" json:"limit,omitempty"`
}

type ResourceUsageHttpListNodesArgs struct {
	call rpc.Call
	data resourceUsageHttpListNodesArgsData
}

func (v *ResourceUsageHttpListNodesArgs) HasSince() bool {
	return v.data.Since != nil
}

func (v *ResourceUsageHttpListNodesArgs) Since() string {
	if v.data.Since == nil {
		return ""
	}
	return *v.data.Since
}

func (v *ResourceUsageHttpListNodesArgs) HasUntil() bool {
	return v.data.Until != nil
}

func (v *ResourceUsageHttpListNodesArgs) Until() string {
	if v.data.Until == nil {
		return ""
	}
	return *v.data.Until
}

func (v *ResourceUsageHttpListNodesArgs) HasAggregate() bool {
	return v.data.Aggregate != nil
}

func (v *ResourceUsageHttpListNodesArgs) Aggregate() string {
	if v.data.Aggregate == nil {
		return ""
	}
	return *v.data.Aggregate
}

func (v *ResourceUsageHttpListNodesArgs) HasSort() bool {
	return v.data.Sort != nil
}

func (v *ResourceUsageHttpListNodesArgs) Sort() string {
	if v.data.Sort == nil {
		return ""
	}
	return *v.data.Sort
}

func (v *ResourceUsageHttpListNodesArgs) HasOrder() bool {
	return v.data.Order != nil
}

func (v *ResourceUsageHttpListNodesArgs) Order() string {
	if v.data.Order == nil {
		return ""
	}
	return *v.data.Order
}

func (v *ResourceUsageHttpListNodesArgs) HasLimit() bool {
	return v.data.Limit != nil
}

func (v *ResourceUsageHttpListNodesArgs) Limit() int32 {
	if v.data.Limit == nil {
		return 0
	}
	return *v.data.Limit
}

func (v *ResourceUsageHttpListNodesArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ResourceUsageHttpListNodesArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ResourceUsageHttpListNodesArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ResourceUsageHttpListNodesArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type resourceUsageHttpListNodesResultsData struct {
	Nodes       *[]*NodeUsage       `cbor:"0,keyasint,omitempty" json:"nodes,omitempty"`
	Cluster     *ResourceTotals     `cbor:"1,keyasint,omitempty" json:"cluster,omitempty"`
	Capacity    *NodeCapacity       `cbor:"2,keyasint,omitempty" json:"capacity,omitempty"`
	Window      *UsageWindow        `cbor:"3,keyasint,omitempty" json:"window,omitempty"`
	CollectedAt *standard.Timestamp `cbor:"4,keyasint,omitempty" json:"collected_at,omitempty"`
	Warnings    *[]string           `cbor:"5,keyasint,omitempty" json:"warnings,omitempty"`
}

type ResourceUsageHttpListNodesResults struct {
	call rpc.Call
	data resourceUsageHttpListNodesResultsData
}

func (v *ResourceUsageHttpListNodesResults) SetNodes(nodes []*NodeUsage) {
	x := slices.Clone(nodes)
	v.data.Nodes = &x
}

func (v *ResourceUsageHttpListNodesResults) SetCluster(cluster *ResourceTotals) {
	v.data.Cluster = cluster
}

func (v *ResourceUsageHttpListNodesResults) SetCapacity(capacity *NodeCapacity) {
	v.data.Capacity = capacity
}

func (v *ResourceUsageHttpListNodesResults) SetWindow(window *UsageWindow) {
	v.data.Window = window
}

func (v *ResourceUsageHttpListNodesResults) SetCollectedAt(collected_at *standard.Timestamp) {
	v.data.CollectedAt = collected_at
}

func (v *ResourceUsageHttpListNodesResults) SetWarnings(warnings []string) {
	x := slices.Clone(warnings)
	v.data.Warnings = &x
}

func (v *ResourceUsageHttpListNodesResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ResourceUsageHttpListNodesResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ResourceUsageHttpListNodesResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ResourceUsageHttpListNodesResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type resourceUsageHttpGetNodeArgsData struct {
	Node      *string `cbor:"0,keyasint,omitempty" json:"node,omitempty"`
	Since     *string `cbor:"1,keyasint,omitempty" json:"since,omitempty"`
	Until     *string `cbor:"2,keyasint,omitempty" json:"until,omitempty"`
	Aggregate *string `cbor:"3,keyasint,omitempty" json:"aggregate,omitempty"`
}

type ResourceUsageHttpGetNodeArgs struct {
	call rpc.Call
	data resourceUsageHttpGetNodeArgsData
}

func (v *ResourceUsageHttpGetNodeArgs) HasNode() bool {
	return v.data.Node != nil
}

func (v *ResourceUsageHttpGetNodeArgs) Node() string {
	if v.data.Node == nil {
		return ""
	}
	return *v.data.Node
}

func (v *ResourceUsageHttpGetNodeArgs) HasSince() bool {
	return v.data.Since != nil
}

func (v *ResourceUsageHttpGetNodeArgs) Since() string {
	if v.data.Since == nil {
		return ""
	}
	return *v.data.Since
}

func (v *ResourceUsageHttpGetNodeArgs) HasUntil() bool {
	return v.data.Until != nil
}

func (v *ResourceUsageHttpGetNodeArgs) Until() string {
	if v.data.Until == nil {
		return ""
	}
	return *v.data.Until
}

func (v *ResourceUsageHttpGetNodeArgs) HasAggregate() bool {
	return v.data.Aggregate != nil
}

func (v *ResourceUsageHttpGetNodeArgs) Aggregate() string {
	if v.data.Aggregate == nil {
		return ""
	}
	return *v.data.Aggregate
}

func (v *ResourceUsageHttpGetNodeArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ResourceUsageHttpGetNodeArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ResourceUsageHttpGetNodeArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ResourceUsageHttpGetNodeArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type resourceUsageHttpGetNodeResultsData struct {
	Usage    *NodeUsage   `cbor:"0,keyasint,omitempty" json:"usage,omitempty"`
	Window   *UsageWindow `cbor:"1,keyasint,omitempty" json:"window,omitempty"`
	Warnings *[]string    `cbor:"2,keyasint,omitempty" json:"warnings,omitempty"`
}

type ResourceUsageHttpGetNodeResults struct {
	call rpc.Call
	data resourceUsageHttpGetNodeResultsData
}

func (v *ResourceUsageHttpGetNodeResults) SetUsage(usage *NodeUsage) {
	v.data.Usage = usage
}

func (v *ResourceUsageHttpGetNodeResults) SetWindow(window *UsageWindow) {
	v.data.Window = window
}

func (v *ResourceUsageHttpGetNodeResults) SetWarnings(warnings []string) {
	x := slices.Clone(warnings)
	v.data.Warnings = &x
}

func (v *ResourceUsageHttpGetNodeResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ResourceUsageHttpGetNodeResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ResourceUsageHttpGetNodeResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ResourceUsageHttpGetNodeResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type resourceUsageHttpListAppsArgsData struct {
	Since     *string `cbor:"0,keyasint,omitempty" json:"since,omitempty"`
	Until     *string `cbor:"1,keyasint,omitempty" json:"until,omitempty"`
	Aggregate *string `cbor:"2,keyasint,omitempty" json:"aggregate,omitempty"`
	Sort      *string `cbor:"3,keyasint,omitempty" json:"sort,omitempty"`
	Order     *string `cbor:"4,keyasint,omitempty" json:"order,omitempty"`
	Limit     *int32  `cbor:"5,keyasint,omitempty" json:"limit,omitempty"`
	Addons    *bool   `cbor:"6,keyasint,omitempty" json:"addons,omitempty"`
	Node      *string `cbor:"7,keyasint,omitempty" json:"node,omitempty"`
}

type ResourceUsageHttpListAppsArgs struct {
	call rpc.Call
	data resourceUsageHttpListAppsArgsData
}

func (v *ResourceUsageHttpListAppsArgs) HasSince() bool {
	return v.data.Since != nil
}

func (v *ResourceUsageHttpListAppsArgs) Since() string {
	if v.data.Since == nil {
		return ""
	}
	return *v.data.Since
}

func (v *ResourceUsageHttpListAppsArgs) HasUntil() bool {
	return v.data.Until != nil
}

func (v *ResourceUsageHttpListAppsArgs) Until() string {
	if v.data.Until == nil {
		return ""
	}
	return *v.data.Until
}

func (v *ResourceUsageHttpListAppsArgs) HasAggregate() bool {
	return v.data.Aggregate != nil
}

func (v *ResourceUsageHttpListAppsArgs) Aggregate() string {
	if v.data.Aggregate == nil {
		return ""
	}
	return *v.data.Aggregate
}

func (v *ResourceUsageHttpListAppsArgs) HasSort() bool {
	return v.data.Sort != nil
}

func (v *ResourceUsageHttpListAppsArgs) Sort() string {
	if v.data.Sort == nil {
		return ""
	}
	return *v.data.Sort
}

func (v *ResourceUsageHttpListAppsArgs) HasOrder() bool {
	return v.data.Order != nil
}

func (v *ResourceUsageHttpListAppsArgs) Order() string {
	if v.data.Order == nil {
		return ""
	}
	return *v.data.Order
}

func (v *ResourceUsageHttpListAppsArgs) HasLimit() bool {
	return v.data.Limit != nil
}

func (v *ResourceUsageHttpListAppsArgs) Limit() int32 {
	if v.data.Limit == nil {
		return 0
	}
	return *v.data.Limit
}

func (v *ResourceUsageHttpListAppsArgs) HasAddons() bool {
	return v.data.Addons != nil
}

func (v *ResourceUsageHttpListAppsArgs) Addons() bool {
	if v.data.Addons == nil {
		return false
	}
	return *v.data.Addons
}

func (v *ResourceUsageHttpListAppsArgs) HasNode() bool {
	return v.data.Node != nil
}

func (v *ResourceUsageHttpListAppsArgs) Node() string {
	if v.data.Node == nil {
		return ""
	}
	return *v.data.Node
}

func (v *ResourceUsageHttpListAppsArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ResourceUsageHttpListAppsArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ResourceUsageHttpListAppsArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ResourceUsageHttpListAppsArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type resourceUsageHttpListAppsResultsData struct {
	Apps        *[]*AppUsage        `cbor:"0,keyasint,omitempty" json:"apps,omitempty"`
	Cluster     *ResourceTotals     `cbor:"1,keyasint,omitempty" json:"cluster,omitempty"`
	TotalCount  *int32              `cbor:"2,keyasint,omitempty" json:"total_count,omitempty"`
	Window      *UsageWindow        `cbor:"3,keyasint,omitempty" json:"window,omitempty"`
	CollectedAt *standard.Timestamp `cbor:"4,keyasint,omitempty" json:"collected_at,omitempty"`
	Warnings    *[]string           `cbor:"5,keyasint,omitempty" json:"warnings,omitempty"`
}

type ResourceUsageHttpListAppsResults struct {
	call rpc.Call
	data resourceUsageHttpListAppsResultsData
}

func (v *ResourceUsageHttpListAppsResults) SetApps(apps []*AppUsage) {
	x := slices.Clone(apps)
	v.data.Apps = &x
}

func (v *ResourceUsageHttpListAppsResults) SetCluster(cluster *ResourceTotals) {
	v.data.Cluster = cluster
}

func (v *ResourceUsageHttpListAppsResults) SetTotalCount(total_count int32) {
	v.data.TotalCount = &total_count
}

func (v *ResourceUsageHttpListAppsResults) SetWindow(window *UsageWindow) {
	v.data.Window = window
}

func (v *ResourceUsageHttpListAppsResults) SetCollectedAt(collected_at *standard.Timestamp) {
	v.data.CollectedAt = collected_at
}

func (v *ResourceUsageHttpListAppsResults) SetWarnings(warnings []string) {
	x := slices.Clone(warnings)
	v.data.Warnings = &x
}

func (v *ResourceUsageHttpListAppsResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ResourceUsageHttpListAppsResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ResourceUsageHttpListAppsResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ResourceUsageHttpListAppsResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type resourceUsageHttpGetAppArgsData struct {
	App       *string `cbor:"0,keyasint,omitempty" json:"app,omitempty"`
	Since     *string `cbor:"1,keyasint,omitempty" json:"since,omitempty"`
	Until     *string `cbor:"2,keyasint,omitempty" json:"until,omitempty"`
	Aggregate *string `cbor:"3,keyasint,omitempty" json:"aggregate,omitempty"`
	Addons    *bool   `cbor:"4,keyasint,omitempty" json:"addons,omitempty"`
}

type ResourceUsageHttpGetAppArgs struct {
	call rpc.Call
	data resourceUsageHttpGetAppArgsData
}

func (v *ResourceUsageHttpGetAppArgs) HasApp() bool {
	return v.data.App != nil
}

func (v *ResourceUsageHttpGetAppArgs) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *ResourceUsageHttpGetAppArgs) HasSince() bool {
	return v.data.Since != nil
}

func (v *ResourceUsageHttpGetAppArgs) Since() string {
	if v.data.Since == nil {
		return ""
	}
	return *v.data.Since
}

func (v *ResourceUsageHttpGetAppArgs) HasUntil() bool {
	return v.data.Until != nil
}

func (v *ResourceUsageHttpGetAppArgs) Until() string {
	if v.data.Until == nil {
		return ""
	}
	return *v.data.Until
}

func (v *ResourceUsageHttpGetAppArgs) HasAggregate() bool {
	return v.data.Aggregate != nil
}

func (v *ResourceUsageHttpGetAppArgs) Aggregate() string {
	if v.data.Aggregate == nil {
		return ""
	}
	return *v.data.Aggregate
}

func (v *ResourceUsageHttpGetAppArgs) HasAddons() bool {
	return v.data.Addons != nil
}

func (v *ResourceUsageHttpGetAppArgs) Addons() bool {
	if v.data.Addons == nil {
		return false
	}
	return *v.data.Addons
}

func (v *ResourceUsageHttpGetAppArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ResourceUsageHttpGetAppArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ResourceUsageHttpGetAppArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ResourceUsageHttpGetAppArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type resourceUsageHttpGetAppResultsData struct {
	Usage    *AppUsage    `cbor:"0,keyasint,omitempty" json:"usage,omitempty"`
	Window   *UsageWindow `cbor:"1,keyasint,omitempty" json:"window,omitempty"`
	Warnings *[]string    `cbor:"2,keyasint,omitempty" json:"warnings,omitempty"`
}

type ResourceUsageHttpGetAppResults struct {
	call rpc.Call
	data resourceUsageHttpGetAppResultsData
}

func (v *ResourceUsageHttpGetAppResults) SetUsage(usage *AppUsage) {
	v.data.Usage = usage
}

func (v *ResourceUsageHttpGetAppResults) SetWindow(window *UsageWindow) {
	v.data.Window = window
}

func (v *ResourceUsageHttpGetAppResults) SetWarnings(warnings []string) {
	x := slices.Clone(warnings)
	v.data.Warnings = &x
}

func (v *ResourceUsageHttpGetAppResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ResourceUsageHttpGetAppResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ResourceUsageHttpGetAppResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ResourceUsageHttpGetAppResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type ResourceUsageListSandboxes struct {
	rpc.Call
	args    ResourceUsageListSandboxesArgs
	results ResourceUsageListSandboxesResults
}

func (t *ResourceUsageListSandboxes) Args() *ResourceUsageListSandboxesArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *ResourceUsageListSandboxes) Results() *ResourceUsageListSandboxesResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type ResourceUsageGetSandbox struct {
	rpc.Call
	args    ResourceUsageGetSandboxArgs
	results ResourceUsageGetSandboxResults
}

func (t *ResourceUsageGetSandbox) Args() *ResourceUsageGetSandboxArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *ResourceUsageGetSandbox) Results() *ResourceUsageGetSandboxResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type ResourceUsageListNodes struct {
	rpc.Call
	args    ResourceUsageListNodesArgs
	results ResourceUsageListNodesResults
}

func (t *ResourceUsageListNodes) Args() *ResourceUsageListNodesArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *ResourceUsageListNodes) Results() *ResourceUsageListNodesResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type ResourceUsageListApps struct {
	rpc.Call
	args    ResourceUsageListAppsArgs
	results ResourceUsageListAppsResults
}

func (t *ResourceUsageListApps) Args() *ResourceUsageListAppsArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *ResourceUsageListApps) Results() *ResourceUsageListAppsResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type ResourceUsageHttpListSandboxes struct {
	rpc.Call
	args    ResourceUsageHttpListSandboxesArgs
	results ResourceUsageHttpListSandboxesResults
}

func (t *ResourceUsageHttpListSandboxes) Args() *ResourceUsageHttpListSandboxesArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *ResourceUsageHttpListSandboxes) Results() *ResourceUsageHttpListSandboxesResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type ResourceUsageHttpGetSandbox struct {
	rpc.Call
	args    ResourceUsageHttpGetSandboxArgs
	results ResourceUsageHttpGetSandboxResults
}

func (t *ResourceUsageHttpGetSandbox) Args() *ResourceUsageHttpGetSandboxArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *ResourceUsageHttpGetSandbox) Results() *ResourceUsageHttpGetSandboxResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type ResourceUsageHttpListNodes struct {
	rpc.Call
	args    ResourceUsageHttpListNodesArgs
	results ResourceUsageHttpListNodesResults
}

func (t *ResourceUsageHttpListNodes) Args() *ResourceUsageHttpListNodesArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *ResourceUsageHttpListNodes) Results() *ResourceUsageHttpListNodesResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type ResourceUsageHttpGetNode struct {
	rpc.Call
	args    ResourceUsageHttpGetNodeArgs
	results ResourceUsageHttpGetNodeResults
}

func (t *ResourceUsageHttpGetNode) Args() *ResourceUsageHttpGetNodeArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *ResourceUsageHttpGetNode) Results() *ResourceUsageHttpGetNodeResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type ResourceUsageHttpListApps struct {
	rpc.Call
	args    ResourceUsageHttpListAppsArgs
	results ResourceUsageHttpListAppsResults
}

func (t *ResourceUsageHttpListApps) Args() *ResourceUsageHttpListAppsArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *ResourceUsageHttpListApps) Results() *ResourceUsageHttpListAppsResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type ResourceUsageHttpGetApp struct {
	rpc.Call
	args    ResourceUsageHttpGetAppArgs
	results ResourceUsageHttpGetAppResults
}

func (t *ResourceUsageHttpGetApp) Args() *ResourceUsageHttpGetAppArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *ResourceUsageHttpGetApp) Results() *ResourceUsageHttpGetAppResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type ResourceUsage interface {
	ListSandboxes(ctx context.Context, state *ResourceUsageListSandboxes) error
	GetSandbox(ctx context.Context, state *ResourceUsageGetSandbox) error
	ListNodes(ctx context.Context, state *ResourceUsageListNodes) error
	ListApps(ctx context.Context, state *ResourceUsageListApps) error
	HttpListSandboxes(ctx context.Context, state *ResourceUsageHttpListSandboxes) error
	HttpGetSandbox(ctx context.Context, state *ResourceUsageHttpGetSandbox) error
	HttpListNodes(ctx context.Context, state *ResourceUsageHttpListNodes) error
	HttpGetNode(ctx context.Context, state *ResourceUsageHttpGetNode) error
	HttpListApps(ctx context.Context, state *ResourceUsageHttpListApps) error
	HttpGetApp(ctx context.Context, state *ResourceUsageHttpGetApp) error
}

type reexportResourceUsage struct {
	client rpc.Client
}

func (reexportResourceUsage) ListSandboxes(ctx context.Context, state *ResourceUsageListSandboxes) error {
	panic("not implemented")
}

func (reexportResourceUsage) GetSandbox(ctx context.Context, state *ResourceUsageGetSandbox) error {
	panic("not implemented")
}

func (reexportResourceUsage) ListNodes(ctx context.Context, state *ResourceUsageListNodes) error {
	panic("not implemented")
}

func (reexportResourceUsage) ListApps(ctx context.Context, state *ResourceUsageListApps) error {
	panic("not implemented")
}

func (reexportResourceUsage) HttpListSandboxes(ctx context.Context, state *ResourceUsageHttpListSandboxes) error {
	panic("not implemented")
}

func (reexportResourceUsage) HttpGetSandbox(ctx context.Context, state *ResourceUsageHttpGetSandbox) error {
	panic("not implemented")
}

func (reexportResourceUsage) HttpListNodes(ctx context.Context, state *ResourceUsageHttpListNodes) error {
	panic("not implemented")
}

func (reexportResourceUsage) HttpGetNode(ctx context.Context, state *ResourceUsageHttpGetNode) error {
	panic("not implemented")
}

func (reexportResourceUsage) HttpListApps(ctx context.Context, state *ResourceUsageHttpListApps) error {
	panic("not implemented")
}

func (reexportResourceUsage) HttpGetApp(ctx context.Context, state *ResourceUsageHttpGetApp) error {
	panic("not implemented")
}

func (t reexportResourceUsage) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptResourceUsage(t ResourceUsage) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "listSandboxes",
			InterfaceName: "ResourceUsage",
			Index:         0,
			Public:        false,
			Params:        []string{"selector", "window", "ordering"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.ListSandboxes(ctx, &ResourceUsageListSandboxes{Call: call})
			},
		},
		{
			Name:          "getSandbox",
			InterfaceName: "ResourceUsage",
			Index:         0,
			Public:        false,
			Params:        []string{"sandbox", "window", "options"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.GetSandbox(ctx, &ResourceUsageGetSandbox{Call: call})
			},
		},
		{
			Name:          "listNodes",
			InterfaceName: "ResourceUsage",
			Index:         0,
			Public:        false,
			Params:        []string{"selector", "window", "ordering"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.ListNodes(ctx, &ResourceUsageListNodes{Call: call})
			},
		},
		{
			Name:          "listApps",
			InterfaceName: "ResourceUsage",
			Index:         0,
			Public:        false,
			Params:        []string{"selector", "window", "ordering"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.ListApps(ctx, &ResourceUsageListApps{Call: call})
			},
		},
		{
			Name:          "httpListSandboxes",
			InterfaceName: "ResourceUsage",
			Index:         0,
			Public:        false,
			RestOnly:      true,
			Params:        []string{"app", "service", "node", "kind", "status", "include_system", "since", "until", "aggregate", "sort", "order", "limit"},
			HTTP: &rpc.HTTPBinding{
				Verb:       "GET",
				Path:       "/api/v1/usage/sandboxes",
				Body:       "",
				PathParams: []string{},
				Query: []rpc.HTTPParam{
					{Name: "app", Kind: "string"},
					{Name: "service", Kind: "string"},
					{Name: "node", Kind: "string"},
					{Name: "kind", Kind: "string"},
					{Name: "status", Kind: "string"},
					{Name: "include_system", Kind: "bool"},
					{Name: "since", Kind: "string"},
					{Name: "until", Kind: "string"},
					{Name: "aggregate", Kind: "string"},
					{Name: "sort", Kind: "string"},
					{Name: "order", Kind: "string"},
					{Name: "limit", Kind: "int"},
				},
			},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.HttpListSandboxes(ctx, &ResourceUsageHttpListSandboxes{Call: call})
			},
		},
		{
			Name:          "httpGetSandbox",
			InterfaceName: "ResourceUsage",
			Index:         0,
			Public:        false,
			RestOnly:      true,
			Params:        []string{"sandbox", "since", "until", "aggregate", "series", "containers", "step"},
			HTTP: &rpc.HTTPBinding{
				Verb:       "GET",
				Path:       "/api/v1/usage/sandboxes/{sandbox}",
				Body:       "",
				PathParams: []string{"sandbox"},
				Query: []rpc.HTTPParam{
					{Name: "since", Kind: "string"},
					{Name: "until", Kind: "string"},
					{Name: "aggregate", Kind: "string"},
					{Name: "series", Kind: "bool"},
					{Name: "containers", Kind: "bool"},
					{Name: "step", Kind: "string"},
				},
			},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.HttpGetSandbox(ctx, &ResourceUsageHttpGetSandbox{Call: call})
			},
		},
		{
			Name:          "httpListNodes",
			InterfaceName: "ResourceUsage",
			Index:         0,
			Public:        false,
			RestOnly:      true,
			Params:        []string{"since", "until", "aggregate", "sort", "order", "limit"},
			HTTP: &rpc.HTTPBinding{
				Verb:       "GET",
				Path:       "/api/v1/usage/nodes",
				Body:       "",
				PathParams: []string{},
				Query: []rpc.HTTPParam{
					{Name: "since", Kind: "string"},
					{Name: "until", Kind: "string"},
					{Name: "aggregate", Kind: "string"},
					{Name: "sort", Kind: "string"},
					{Name: "order", Kind: "string"},
					{Name: "limit", Kind: "int"},
				},
			},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.HttpListNodes(ctx, &ResourceUsageHttpListNodes{Call: call})
			},
		},
		{
			Name:          "httpGetNode",
			InterfaceName: "ResourceUsage",
			Index:         0,
			Public:        false,
			RestOnly:      true,
			Params:        []string{"node", "since", "until", "aggregate"},
			HTTP: &rpc.HTTPBinding{
				Verb:       "GET",
				Path:       "/api/v1/usage/nodes/{node}",
				Body:       "",
				PathParams: []string{"node"},
				Query: []rpc.HTTPParam{
					{Name: "since", Kind: "string"},
					{Name: "until", Kind: "string"},
					{Name: "aggregate", Kind: "string"},
				},
			},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.HttpGetNode(ctx, &ResourceUsageHttpGetNode{Call: call})
			},
		},
		{
			Name:          "httpListApps",
			InterfaceName: "ResourceUsage",
			Index:         0,
			Public:        false,
			RestOnly:      true,
			Params:        []string{"since", "until", "aggregate", "sort", "order", "limit", "addons", "node"},
			HTTP: &rpc.HTTPBinding{
				Verb:       "GET",
				Path:       "/api/v1/usage/apps",
				Body:       "",
				PathParams: []string{},
				Query: []rpc.HTTPParam{
					{Name: "since", Kind: "string"},
					{Name: "until", Kind: "string"},
					{Name: "aggregate", Kind: "string"},
					{Name: "sort", Kind: "string"},
					{Name: "order", Kind: "string"},
					{Name: "limit", Kind: "int"},
					{Name: "addons", Kind: "bool"},
					{Name: "node", Kind: "string"},
				},
			},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.HttpListApps(ctx, &ResourceUsageHttpListApps{Call: call})
			},
		},
		{
			Name:          "httpGetApp",
			InterfaceName: "ResourceUsage",
			Index:         0,
			Public:        false,
			RestOnly:      true,
			Params:        []string{"app", "since", "until", "aggregate", "addons"},
			HTTP: &rpc.HTTPBinding{
				Verb:       "GET",
				Path:       "/api/v1/usage/apps/{app}",
				Body:       "",
				PathParams: []string{"app"},
				Query: []rpc.HTTPParam{
					{Name: "since", Kind: "string"},
					{Name: "until", Kind: "string"},
					{Name: "aggregate", Kind: "string"},
					{Name: "addons", Kind: "bool"},
				},
			},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.HttpGetApp(ctx, &ResourceUsageHttpGetApp{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type ResourceUsageClient struct {
	rpc.Client
}

func NewResourceUsageClient(client rpc.Client) *ResourceUsageClient {
	return &ResourceUsageClient{Client: client}
}

func (c ResourceUsageClient) Export() ResourceUsage {
	return reexportResourceUsage{client: c.Client}
}

type ResourceUsageClientListSandboxesResults struct {
	client rpc.Client
	data   resourceUsageListSandboxesResultsData
}

func (v *ResourceUsageClientListSandboxesResults) HasSandboxes() bool {
	return v.data.Sandboxes != nil
}

func (v *ResourceUsageClientListSandboxesResults) Sandboxes() []*SandboxUsage {
	if v.data.Sandboxes == nil {
		return nil
	}
	return *v.data.Sandboxes
}

func (v *ResourceUsageClientListSandboxesResults) HasWindow() bool {
	return v.data.Window != nil
}

func (v *ResourceUsageClientListSandboxesResults) Window() *UsageWindow {
	return v.data.Window
}

func (v *ResourceUsageClientListSandboxesResults) HasCluster() bool {
	return v.data.Cluster != nil
}

func (v *ResourceUsageClientListSandboxesResults) Cluster() *ResourceTotals {
	return v.data.Cluster
}

func (v *ResourceUsageClientListSandboxesResults) HasTotalCount() bool {
	return v.data.TotalCount != nil
}

func (v *ResourceUsageClientListSandboxesResults) TotalCount() int32 {
	if v.data.TotalCount == nil {
		return 0
	}
	return *v.data.TotalCount
}

func (v *ResourceUsageClientListSandboxesResults) HasCollectedAt() bool {
	return v.data.CollectedAt != nil
}

func (v *ResourceUsageClientListSandboxesResults) CollectedAt() *standard.Timestamp {
	return v.data.CollectedAt
}

func (v *ResourceUsageClientListSandboxesResults) HasWarnings() bool {
	return v.data.Warnings != nil
}

func (v *ResourceUsageClientListSandboxesResults) Warnings() []string {
	if v.data.Warnings == nil {
		return nil
	}
	return *v.data.Warnings
}

func (v ResourceUsageClient) ListSandboxes(ctx context.Context, selector *Selector, window *Window, ordering *Ordering) (*ResourceUsageClientListSandboxesResults, error) {
	args := ResourceUsageListSandboxesArgs{}
	args.data.Selector = selector
	args.data.Window = window
	args.data.Ordering = ordering

	var ret resourceUsageListSandboxesResultsData

	err := v.Call(ctx, "listSandboxes", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &ResourceUsageClientListSandboxesResults{client: v.Client, data: ret}, nil
}

type ResourceUsageClientGetSandboxResults struct {
	client rpc.Client
	data   resourceUsageGetSandboxResultsData
}

func (v *ResourceUsageClientGetSandboxResults) HasUsage() bool {
	return v.data.Usage != nil
}

func (v *ResourceUsageClientGetSandboxResults) Usage() *SandboxUsage {
	return v.data.Usage
}

func (v *ResourceUsageClientGetSandboxResults) HasContainers() bool {
	return v.data.Containers != nil
}

func (v *ResourceUsageClientGetSandboxResults) Containers() []*ContainerUsage {
	if v.data.Containers == nil {
		return nil
	}
	return *v.data.Containers
}

func (v *ResourceUsageClientGetSandboxResults) HasSeries() bool {
	return v.data.Series != nil
}

func (v *ResourceUsageClientGetSandboxResults) Series() []*UsageSeries {
	if v.data.Series == nil {
		return nil
	}
	return *v.data.Series
}

func (v *ResourceUsageClientGetSandboxResults) HasExit() bool {
	return v.data.Exit != nil
}

func (v *ResourceUsageClientGetSandboxResults) Exit() *SandboxExit {
	return v.data.Exit
}

func (v *ResourceUsageClientGetSandboxResults) HasCrashLoop() bool {
	return v.data.CrashLoop != nil
}

func (v *ResourceUsageClientGetSandboxResults) CrashLoop() *CrashLoopState {
	return v.data.CrashLoop
}

func (v *ResourceUsageClientGetSandboxResults) HasImage() bool {
	return v.data.Image != nil
}

func (v *ResourceUsageClientGetSandboxResults) Image() string {
	if v.data.Image == nil {
		return ""
	}
	return *v.data.Image
}

func (v *ResourceUsageClientGetSandboxResults) HasRestartPolicy() bool {
	return v.data.RestartPolicy != nil
}

func (v *ResourceUsageClientGetSandboxResults) RestartPolicy() string {
	if v.data.RestartPolicy == nil {
		return ""
	}
	return *v.data.RestartPolicy
}

func (v *ResourceUsageClientGetSandboxResults) HasWindow() bool {
	return v.data.Window != nil
}

func (v *ResourceUsageClientGetSandboxResults) Window() *UsageWindow {
	return v.data.Window
}

func (v *ResourceUsageClientGetSandboxResults) HasWarnings() bool {
	return v.data.Warnings != nil
}

func (v *ResourceUsageClientGetSandboxResults) Warnings() []string {
	if v.data.Warnings == nil {
		return nil
	}
	return *v.data.Warnings
}

func (v ResourceUsageClient) GetSandbox(ctx context.Context, sandbox string, window *Window, options *DetailOptions) (*ResourceUsageClientGetSandboxResults, error) {
	args := ResourceUsageGetSandboxArgs{}
	args.data.Sandbox = &sandbox
	args.data.Window = window
	args.data.Options = options

	var ret resourceUsageGetSandboxResultsData

	err := v.Call(ctx, "getSandbox", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &ResourceUsageClientGetSandboxResults{client: v.Client, data: ret}, nil
}

type ResourceUsageClientListNodesResults struct {
	client rpc.Client
	data   resourceUsageListNodesResultsData
}

func (v *ResourceUsageClientListNodesResults) HasNodes() bool {
	return v.data.Nodes != nil
}

func (v *ResourceUsageClientListNodesResults) Nodes() []*NodeUsage {
	if v.data.Nodes == nil {
		return nil
	}
	return *v.data.Nodes
}

func (v *ResourceUsageClientListNodesResults) HasCluster() bool {
	return v.data.Cluster != nil
}

func (v *ResourceUsageClientListNodesResults) Cluster() *ResourceTotals {
	return v.data.Cluster
}

func (v *ResourceUsageClientListNodesResults) HasCapacity() bool {
	return v.data.Capacity != nil
}

func (v *ResourceUsageClientListNodesResults) Capacity() *NodeCapacity {
	return v.data.Capacity
}

func (v *ResourceUsageClientListNodesResults) HasWindow() bool {
	return v.data.Window != nil
}

func (v *ResourceUsageClientListNodesResults) Window() *UsageWindow {
	return v.data.Window
}

func (v *ResourceUsageClientListNodesResults) HasCollectedAt() bool {
	return v.data.CollectedAt != nil
}

func (v *ResourceUsageClientListNodesResults) CollectedAt() *standard.Timestamp {
	return v.data.CollectedAt
}

func (v *ResourceUsageClientListNodesResults) HasWarnings() bool {
	return v.data.Warnings != nil
}

func (v *ResourceUsageClientListNodesResults) Warnings() []string {
	if v.data.Warnings == nil {
		return nil
	}
	return *v.data.Warnings
}

func (v ResourceUsageClient) ListNodes(ctx context.Context, selector *Selector, window *Window, ordering *Ordering) (*ResourceUsageClientListNodesResults, error) {
	args := ResourceUsageListNodesArgs{}
	args.data.Selector = selector
	args.data.Window = window
	args.data.Ordering = ordering

	var ret resourceUsageListNodesResultsData

	err := v.Call(ctx, "listNodes", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &ResourceUsageClientListNodesResults{client: v.Client, data: ret}, nil
}

type ResourceUsageClientListAppsResults struct {
	client rpc.Client
	data   resourceUsageListAppsResultsData
}

func (v *ResourceUsageClientListAppsResults) HasApps() bool {
	return v.data.Apps != nil
}

func (v *ResourceUsageClientListAppsResults) Apps() []*AppUsage {
	if v.data.Apps == nil {
		return nil
	}
	return *v.data.Apps
}

func (v *ResourceUsageClientListAppsResults) HasCluster() bool {
	return v.data.Cluster != nil
}

func (v *ResourceUsageClientListAppsResults) Cluster() *ResourceTotals {
	return v.data.Cluster
}

func (v *ResourceUsageClientListAppsResults) HasTotalCount() bool {
	return v.data.TotalCount != nil
}

func (v *ResourceUsageClientListAppsResults) TotalCount() int32 {
	if v.data.TotalCount == nil {
		return 0
	}
	return *v.data.TotalCount
}

func (v *ResourceUsageClientListAppsResults) HasWindow() bool {
	return v.data.Window != nil
}

func (v *ResourceUsageClientListAppsResults) Window() *UsageWindow {
	return v.data.Window
}

func (v *ResourceUsageClientListAppsResults) HasCollectedAt() bool {
	return v.data.CollectedAt != nil
}

func (v *ResourceUsageClientListAppsResults) CollectedAt() *standard.Timestamp {
	return v.data.CollectedAt
}

func (v *ResourceUsageClientListAppsResults) HasWarnings() bool {
	return v.data.Warnings != nil
}

func (v *ResourceUsageClientListAppsResults) Warnings() []string {
	if v.data.Warnings == nil {
		return nil
	}
	return *v.data.Warnings
}

func (v ResourceUsageClient) ListApps(ctx context.Context, selector *Selector, window *Window, ordering *Ordering) (*ResourceUsageClientListAppsResults, error) {
	args := ResourceUsageListAppsArgs{}
	args.data.Selector = selector
	args.data.Window = window
	args.data.Ordering = ordering

	var ret resourceUsageListAppsResultsData

	err := v.Call(ctx, "listApps", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &ResourceUsageClientListAppsResults{client: v.Client, data: ret}, nil
}
