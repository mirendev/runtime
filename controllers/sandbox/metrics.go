package sandbox

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/containerd/cgroups/v3/cgroup2"
	"github.com/containerd/cgroups/v3/cgroup2/stats"
	"miren.dev/runtime/api/metric/metric_v1alpha"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/pkg/asm/autoreg"
	"miren.dev/runtime/pkg/mapx"
	"miren.dev/runtime/pkg/rpc/standard"
	"miren.dev/runtime/pkg/units"
)

type Cgroups struct {
	cgroups map[string]*cgroup2.Manager

	attrs       map[string]string
	lastReading time.Time
	prev        map[string]*stats.Metrics
}

type Metrics struct {
	Log      *slog.Logger
	CPUUsage *metrics.CPUUsage
	MemUsage *metrics.MemoryUsage

	mu           sync.Mutex
	namedEntries map[string]*Cgroups
}

var _ = autoreg.Register[Metrics]()

func (m *Metrics) Add(name string, pathes map[string]string, attributes map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.namedEntries == nil {
		m.namedEntries = make(map[string]*Cgroups)
	}

	var cg Cgroups
	cg.attrs = attributes
	cg.cgroups = make(map[string]*cgroup2.Manager)
	cg.prev = make(map[string]*stats.Metrics)

	for k, v := range pathes {
		man, err := cgroup2.Load(v)
		if err != nil {
			return err
		}

		cg.cgroups[k] = man
	}

	m.namedEntries[name] = &cg

	return nil
}

func (m *Metrics) Remove(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.namedEntries == nil {
		return nil
	}

	delete(m.namedEntries, name)
	return nil
}

func (m *Metrics) Gather(name string) ([]*metric_v1alpha.ContainerSnapshot, error) {
	m.mu.Lock()
	cg, ok := m.namedEntries[name]
	m.mu.Unlock()

	if !ok {
		return nil, nil
	}

	var ret []*metric_v1alpha.ContainerSnapshot

	for name, cg := range mapx.StableOrder(cg.cgroups) {
		snapshot, err := cg.Stat()
		if err != nil {
			return nil, err
		}

		var cs metric_v1alpha.ContainerSnapshot
		cs.SetName(name)

		var ms metric_v1alpha.MetricSnapshot

		ms.SetMeasuredAt(standard.ToTimestamp(time.Now()))
		ms.SetTotalCpuTime(int64(snapshot.CPU.UsageUsec * 1000))
		ms.SetKernelCpuTime(int64(snapshot.CPU.SystemUsec * 1000))
		ms.SetMemoryUsage(int64(snapshot.Memory.Usage))
		ms.SetMemoryPeak(int64(snapshot.Memory.MaxUsage))
		ms.SetSwapUsage(int64(snapshot.Memory.SwapUsage))
		ms.SetSwapPeak(int64(snapshot.Memory.SwapMaxUsage))

		cs.SetMetrics(&ms)

		ret = append(ret, &cs)
	}

	return ret, nil
}

var _ metric_v1alpha.SandboxMetrics = (*Metrics)(nil)

func (m *Metrics) Snapshot(ctx context.Context, req *metric_v1alpha.SandboxMetricsSnapshot) error {
	containers, err := m.Gather(req.Args().Sandbox())
	if err != nil {
		return err
	}

	if containers != nil {
		res := req.Results()

		var ms metric_v1alpha.MetricSnapshot

		for _, cs := range containers {
			cms := cs.Metrics()
			if !ms.HasMeasuredAt() {
				ms.SetMeasuredAt(cms.MeasuredAt())
			}
			ms.SetTotalCpuTime(ms.TotalCpuTime() + cms.TotalCpuTime())
			ms.SetKernelCpuTime(ms.KernelCpuTime() + cms.KernelCpuTime())
			ms.SetMemoryUsage(ms.MemoryUsage() + cms.MemoryUsage())
			ms.SetMemoryPeak(ms.MemoryPeak() + cms.MemoryPeak())
			ms.SetSwapUsage(ms.SwapUsage() + cms.SwapUsage())
			ms.SetSwapPeak(ms.SwapPeak() + cms.SwapPeak())
		}

		res.SetMetrics(&ms)
		res.SetContainers(containers)
	}

	return nil
}

func (m *Metrics) writeStatsToClickhouse(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, cg := range m.namedEntries {
		if len(cg.prev) == 0 {
			cg.lastReading = time.Now()
			for k, mn := range cg.cgroups {
				cur, err := mn.Stat()
				if err != nil {
					m.Log.Error("failed to get stats", "err", err)
				} else {
					cg.prev[k] = cur
				}
			}
			continue
		}

		var (
			usage units.Microseconds
			mem   units.Bytes
		)

		for k, m := range cg.cgroups {
			cur, err := m.Stat()
			if err != nil {
				continue
			}

			prev := cg.prev[k]
			cg.prev[k] = cur

			usage += units.Microseconds(cur.CPU.UsageUsec - prev.CPU.UsageUsec)
			mem += units.Bytes(cur.Memory.Usage)
		}

		wallEnd := time.Now()

		err := m.CPUUsage.RecordUsage(ctx, name, cg.lastReading, wallEnd, usage, cg.attrs)
		if err != nil {
			m.Log.Error("failed to record CPU usage", "err", err)
		}
		err = m.MemUsage.RecordUsage(ctx, name, wallEnd, mem, cg.attrs)
		if err != nil {
			m.Log.Error("failed to record memory usage", "err", err)
		}

		m.Log.Debug("recorded container stats", "app", name, "usage", usage, "mem", mem, "dur", wallEnd.Sub(cg.lastReading))
		cg.lastReading = wallEnd
	}

	return nil
}

func (m *Metrics) Monitor(ctx context.Context) {
	m.Log.Debug("start sandbox resource monitoring")
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := m.writeStatsToClickhouse(ctx); err != nil {
				m.Log.Error("failed to write stats to Clickhouse", "err", err)
			}
		case <-ctx.Done():
			return
		}
	}
}
