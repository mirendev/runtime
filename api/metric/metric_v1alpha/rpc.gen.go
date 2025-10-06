package metric_v1alpha

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/fxamacker/cbor/v2"
	rpc "miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/standard"
)

type metricSnapshotData struct {
	MeasuredAt    *standard.Timestamp `cbor:"0,keyasint,omitempty" json:"measured_at,omitempty"`
	TotalCpuTime  *int64              `cbor:"1,keyasint,omitempty" json:"total_cpu_time,omitempty"`
	KernelCpuTime *int64              `cbor:"2,keyasint,omitempty" json:"kernel_cpu_time,omitempty"`
	MemoryUsage   *int64              `cbor:"3,keyasint,omitempty" json:"memory_usage,omitempty"`
	MemoryPeak    *int64              `cbor:"4,keyasint,omitempty" json:"memory_peak,omitempty"`
	SwapUsage     *int64              `cbor:"5,keyasint,omitempty" json:"swap_usage,omitempty"`
	SwapPeak      *int64              `cbor:"6,keyasint,omitempty" json:"swap_peak,omitempty"`
}

type MetricSnapshot struct {
	data metricSnapshotData
}

func (v *MetricSnapshot) HasMeasuredAt() bool {
	return v.data.MeasuredAt != nil
}

func (v *MetricSnapshot) MeasuredAt() *standard.Timestamp {
	return v.data.MeasuredAt
}

func (v *MetricSnapshot) SetMeasuredAt(measured_at *standard.Timestamp) {
	v.data.MeasuredAt = measured_at
}

func (v *MetricSnapshot) HasTotalCpuTime() bool {
	return v.data.TotalCpuTime != nil
}

func (v *MetricSnapshot) TotalCpuTime() int64 {
	if v.data.TotalCpuTime == nil {
		return 0
	}
	return *v.data.TotalCpuTime
}

func (v *MetricSnapshot) SetTotalCpuTime(total_cpu_time int64) {
	v.data.TotalCpuTime = &total_cpu_time
}

func (v *MetricSnapshot) HasKernelCpuTime() bool {
	return v.data.KernelCpuTime != nil
}

func (v *MetricSnapshot) KernelCpuTime() int64 {
	if v.data.KernelCpuTime == nil {
		return 0
	}
	return *v.data.KernelCpuTime
}

func (v *MetricSnapshot) SetKernelCpuTime(kernel_cpu_time int64) {
	v.data.KernelCpuTime = &kernel_cpu_time
}

func (v *MetricSnapshot) HasMemoryUsage() bool {
	return v.data.MemoryUsage != nil
}

func (v *MetricSnapshot) MemoryUsage() int64 {
	if v.data.MemoryUsage == nil {
		return 0
	}
	return *v.data.MemoryUsage
}

func (v *MetricSnapshot) SetMemoryUsage(memory_usage int64) {
	v.data.MemoryUsage = &memory_usage
}

func (v *MetricSnapshot) HasMemoryPeak() bool {
	return v.data.MemoryPeak != nil
}

func (v *MetricSnapshot) MemoryPeak() int64 {
	if v.data.MemoryPeak == nil {
		return 0
	}
	return *v.data.MemoryPeak
}

func (v *MetricSnapshot) SetMemoryPeak(memory_peak int64) {
	v.data.MemoryPeak = &memory_peak
}

func (v *MetricSnapshot) HasSwapUsage() bool {
	return v.data.SwapUsage != nil
}

func (v *MetricSnapshot) SwapUsage() int64 {
	if v.data.SwapUsage == nil {
		return 0
	}
	return *v.data.SwapUsage
}

func (v *MetricSnapshot) SetSwapUsage(swap_usage int64) {
	v.data.SwapUsage = &swap_usage
}

func (v *MetricSnapshot) HasSwapPeak() bool {
	return v.data.SwapPeak != nil
}

func (v *MetricSnapshot) SwapPeak() int64 {
	if v.data.SwapPeak == nil {
		return 0
	}
	return *v.data.SwapPeak
}

func (v *MetricSnapshot) SetSwapPeak(swap_peak int64) {
	v.data.SwapPeak = &swap_peak
}

func (v *MetricSnapshot) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *MetricSnapshot) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *MetricSnapshot) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *MetricSnapshot) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type containerSnapshotData struct {
	Name    *string         `cbor:"0,keyasint,omitempty" json:"name,omitempty"`
	Metrics *MetricSnapshot `cbor:"1,keyasint,omitempty" json:"metrics,omitempty"`
}

type ContainerSnapshot struct {
	data containerSnapshotData
}

func (v *ContainerSnapshot) HasName() bool {
	return v.data.Name != nil
}

func (v *ContainerSnapshot) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *ContainerSnapshot) SetName(name string) {
	v.data.Name = &name
}

func (v *ContainerSnapshot) HasMetrics() bool {
	return v.data.Metrics != nil
}

func (v *ContainerSnapshot) Metrics() *MetricSnapshot {
	return v.data.Metrics
}

func (v *ContainerSnapshot) SetMetrics(metrics *MetricSnapshot) {
	v.data.Metrics = metrics
}

func (v *ContainerSnapshot) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ContainerSnapshot) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ContainerSnapshot) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ContainerSnapshot) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type sandboxMetricsSnapshotArgsData struct {
	Sandbox *string `cbor:"0,keyasint,omitempty" json:"sandbox,omitempty"`
}

type SandboxMetricsSnapshotArgs struct {
	call rpc.Call
	data sandboxMetricsSnapshotArgsData
}

func (v *SandboxMetricsSnapshotArgs) HasSandbox() bool {
	return v.data.Sandbox != nil
}

func (v *SandboxMetricsSnapshotArgs) Sandbox() string {
	if v.data.Sandbox == nil {
		return ""
	}
	return *v.data.Sandbox
}

func (v *SandboxMetricsSnapshotArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SandboxMetricsSnapshotArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SandboxMetricsSnapshotArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SandboxMetricsSnapshotArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type sandboxMetricsSnapshotResultsData struct {
	Metrics    *MetricSnapshot       `cbor:"0,keyasint,omitempty" json:"metrics,omitempty"`
	Containers *[]*ContainerSnapshot `cbor:"1,keyasint,omitempty" json:"containers,omitempty"`
}

type SandboxMetricsSnapshotResults struct {
	call rpc.Call
	data sandboxMetricsSnapshotResultsData
}

func (v *SandboxMetricsSnapshotResults) SetMetrics(metrics *MetricSnapshot) {
	v.data.Metrics = metrics
}

func (v *SandboxMetricsSnapshotResults) SetContainers(containers []*ContainerSnapshot) {
	x := slices.Clone(containers)
	v.data.Containers = &x
}

func (v *SandboxMetricsSnapshotResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SandboxMetricsSnapshotResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SandboxMetricsSnapshotResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SandboxMetricsSnapshotResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type SandboxMetricsSnapshot struct {
	rpc.Call
	args    SandboxMetricsSnapshotArgs
	results SandboxMetricsSnapshotResults
}

func (t *SandboxMetricsSnapshot) Args() *SandboxMetricsSnapshotArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *SandboxMetricsSnapshot) Results() *SandboxMetricsSnapshotResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type SandboxMetrics interface {
	Snapshot(ctx context.Context, state *SandboxMetricsSnapshot) error
}

type reexportSandboxMetrics struct {
	client rpc.Client
}

func (reexportSandboxMetrics) Snapshot(ctx context.Context, state *SandboxMetricsSnapshot) error {
	panic("not implemented")
}

func (t reexportSandboxMetrics) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptSandboxMetrics(t SandboxMetrics) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "snapshot",
			InterfaceName: "SandboxMetrics",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Snapshot(ctx, &SandboxMetricsSnapshot{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type SandboxMetricsClient struct {
	rpc.Client
}

func NewSandboxMetricsClient(client rpc.Client) *SandboxMetricsClient {
	return &SandboxMetricsClient{Client: client}
}

func (c SandboxMetricsClient) Export() SandboxMetrics {
	return reexportSandboxMetrics{client: c.Client}
}

type SandboxMetricsClientSnapshotResults struct {
	client rpc.Client
	data   sandboxMetricsSnapshotResultsData
}

func (v *SandboxMetricsClientSnapshotResults) HasMetrics() bool {
	return v.data.Metrics != nil
}

func (v *SandboxMetricsClientSnapshotResults) Metrics() *MetricSnapshot {
	return v.data.Metrics
}

func (v *SandboxMetricsClientSnapshotResults) HasContainers() bool {
	return v.data.Containers != nil
}

func (v *SandboxMetricsClientSnapshotResults) Containers() []*ContainerSnapshot {
	if v.data.Containers == nil {
		return nil
	}
	return *v.data.Containers
}

func (v SandboxMetricsClient) Snapshot(ctx context.Context, sandbox string) (*SandboxMetricsClientSnapshotResults, error) {
	args := SandboxMetricsSnapshotArgs{}
	args.data.Sandbox = &sandbox

	var ret sandboxMetricsSnapshotResultsData

	err := v.Call(ctx, "snapshot", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &SandboxMetricsClientSnapshotResults{client: v.Client, data: ret}, nil
}
