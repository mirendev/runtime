package metrics

import (
	"context"
	"log/slog"
	"runtime"
	"time"
)

// RuntimeMemory collects Go runtime memory stats for the miren control process
// itself and pushes them to VictoriaMetrics in the same push-only style as the
// per-sandbox collectors (MemoryUsage/CPUUsage). The control process is not
// covered by any per-sandbox cgroup, so today there is no visibility into the
// coordinator's own heap/RSS growth — these series are that visibility.
//
// All series carry entity="miren/control" so they sit alongside the per-app
// memory_usage_bytes{entity="app/..."} series and can be compared directly.
// The key diagnostic is the gap between process_resident_memory_bytes (the
// whole control process, including off-heap: cgo, mmap'd bbolt/etcd, the
// buildkit content store) and go_mem_heap_inuse_bytes (Go-managed heap only):
//   - both balloon together  -> Go heap; a pprof heap profile names the site.
//   - RSS balloons, heap flat -> off-heap; pprof can't see it, look at bbolt/buildkit.
type RuntimeMemory struct {
	Log    *slog.Logger
	Writer *VictoriaMetricsWriter

	// Entity is the value of the "entity" label on every emitted series.
	Entity string
}

const defaultRuntimeMemoryInterval = 10 * time.Second

// NewRuntimeMemory creates a RuntimeMemory collector. Writer may be nil for
// environments without metrics collection, in which case Monitor is a no-op.
func NewRuntimeMemory(log *slog.Logger, writer *VictoriaMetricsWriter) *RuntimeMemory {
	return &RuntimeMemory{
		Log:    log,
		Writer: writer,
		Entity: "miren/control",
	}
}

// Monitor samples runtime memory every defaultRuntimeMemoryInterval and pushes
// one batch of points per tick until ctx is cancelled. Mirrors the cadence and
// lifecycle of (*sandbox.Metrics).Monitor.
func (r *RuntimeMemory) Monitor(ctx context.Context) {
	if r.Writer == nil {
		return
	}

	r.Log.Info("control-process runtime memory metrics started",
		"entity", r.Entity, "interval", defaultRuntimeMemoryInterval)

	ticker := time.NewTicker(defaultRuntimeMemoryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := r.collect(ctx); err != nil {
				r.Log.Error("failed to record control-process runtime memory", "err", err)
			}
		case <-ctx.Done():
			return
		}
	}
}

func (r *RuntimeMemory) collect(ctx context.Context) error {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	ts := time.Now()
	labels := map[string]string{"entity": r.Entity}

	points := []MetricPoint{
		{Name: "go_mem_heap_alloc_bytes", Labels: labels, Value: float64(ms.HeapAlloc), Timestamp: ts},
		{Name: "go_mem_heap_inuse_bytes", Labels: labels, Value: float64(ms.HeapInuse), Timestamp: ts},
		{Name: "go_mem_heap_sys_bytes", Labels: labels, Value: float64(ms.HeapSys), Timestamp: ts},
		{Name: "go_mem_heap_idle_bytes", Labels: labels, Value: float64(ms.HeapIdle), Timestamp: ts},
		{Name: "go_mem_heap_released_bytes", Labels: labels, Value: float64(ms.HeapReleased), Timestamp: ts},
		{Name: "go_mem_heap_objects", Labels: labels, Value: float64(ms.HeapObjects), Timestamp: ts},
		{Name: "go_mem_stack_inuse_bytes", Labels: labels, Value: float64(ms.StackInuse), Timestamp: ts},
		{Name: "go_mem_sys_bytes", Labels: labels, Value: float64(ms.Sys), Timestamp: ts},
		{Name: "go_mem_next_gc_bytes", Labels: labels, Value: float64(ms.NextGC), Timestamp: ts},
		{Name: "go_gc_count_total", Labels: labels, Value: float64(ms.NumGC), Timestamp: ts},
		{Name: "go_goroutines", Labels: labels, Value: float64(runtime.NumGoroutine()), Timestamp: ts},
	}

	// Process RSS includes off-heap memory the Go runtime stats can't see
	// (mmap'd bbolt/etcd, buildkit content store, cgo). The gap against
	// go_mem_heap_inuse_bytes is what tells us which kind of memory balloons.
	if rss, ok := processResidentBytes(); ok {
		points = append(points, MetricPoint{
			Name:      "process_resident_memory_bytes",
			Labels:    labels,
			Value:     float64(rss),
			Timestamp: ts,
		})
	}

	return r.Writer.WritePoints(ctx, points)
}
