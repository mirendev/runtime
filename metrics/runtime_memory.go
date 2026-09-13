package metrics

import (
	"context"
	"log/slog"
	runtimemetrics "runtime/metrics"
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
	Writer PointWriter

	// Entity is the value of the "entity" label on every emitted series.
	Entity string
}

const defaultRuntimeMemoryInterval = 10 * time.Second

// runtime/metrics sample names we read. These are the STW-free source for the
// go_mem_* series below: unlike runtime.ReadMemStats (which stops the world to
// take a consistent snapshot), runtime/metrics.Read reads live counters. The
// runtime.MemStats fields the collector historically emitted don't all map to a
// single sample, so some series are derived — see collect() for the arithmetic,
// which follows the equivalences documented in the runtime/metrics package.
const (
	mHeapObjects  = "/memory/classes/heap/objects:bytes"
	mHeapUnused   = "/memory/classes/heap/unused:bytes"
	mHeapReleased = "/memory/classes/heap/released:bytes"
	mHeapFree     = "/memory/classes/heap/free:bytes"
	mHeapStacks   = "/memory/classes/heap/stacks:bytes"
	mTotal        = "/memory/classes/total:bytes"
	mGCObjects    = "/gc/heap/objects:objects"
	mGCGoal       = "/gc/heap/goal:bytes"
	mGCCycles     = "/gc/cycles/total:gc-cycles"
	mGoroutines   = "/sched/goroutines:goroutines"
)

// NewRuntimeMemory creates a RuntimeMemory collector. Writer may be nil for
// environments without metrics collection, in which case Monitor is a no-op.
func NewRuntimeMemory(log *slog.Logger, writer PointWriter) *RuntimeMemory {
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
	samples := []runtimemetrics.Sample{
		{Name: mHeapObjects}, {Name: mHeapUnused}, {Name: mHeapReleased},
		{Name: mHeapFree}, {Name: mHeapStacks}, {Name: mTotal},
		{Name: mGCObjects}, {Name: mGCGoal}, {Name: mGCCycles}, {Name: mGoroutines},
	}
	runtimemetrics.Read(samples)

	// Index the readable (KindUint64) samples by name. A sample the runtime
	// doesn't recognize comes back as KindBad; we simply skip any series that
	// depends on it rather than emit a bogus zero.
	vals := make(map[string]uint64, len(samples))
	for _, s := range samples {
		if s.Value.Kind() == runtimemetrics.KindUint64 {
			vals[s.Name] = s.Value.Uint64()
		}
	}

	ts := time.Now()
	labels := map[string]string{"entity": r.Entity}

	points := make([]MetricPoint, 0, 12)

	// emit appends one series whose value is the sum of the named samples, but
	// only if every one of them was present — a missing constituent skips the
	// series rather than emitting a bogus partial value.
	emit := func(name string, names ...string) {
		var total uint64
		for _, n := range names {
			v, ok := vals[n]
			if !ok {
				return
			}
			total += v
		}
		points = append(points, MetricPoint{Name: name, Labels: labels, Value: float64(total), Timestamp: ts})
	}

	// Emitted names are preserved from the previous runtime.MemStats-based
	// implementation so existing dashboards/PromQL keep working. The
	// runtime.MemStats -> runtime/metrics equivalences below are the ones
	// documented in the runtime/metrics package.
	emit("go_mem_heap_alloc_bytes", mHeapObjects)                                      // MemStats.HeapAlloc
	emit("go_mem_heap_inuse_bytes", mHeapObjects, mHeapUnused)                         // MemStats.HeapInuse
	emit("go_mem_heap_sys_bytes", mHeapObjects, mHeapUnused, mHeapReleased, mHeapFree) // MemStats.HeapSys
	emit("go_mem_heap_idle_bytes", mHeapReleased, mHeapFree)                           // MemStats.HeapIdle
	emit("go_mem_heap_released_bytes", mHeapReleased)                                  // MemStats.HeapReleased
	emit("go_mem_heap_objects", mGCObjects)                                            // MemStats.HeapObjects
	emit("go_mem_stack_inuse_bytes", mHeapStacks)                                      // MemStats.StackInuse
	emit("go_mem_next_gc_bytes", mGCGoal)                                              // MemStats.NextGC
	emit("go_gc_count_total", mGCCycles)                                               // MemStats.NumGC
	emit("go_goroutines", mGoroutines)                                                 // runtime.NumGoroutine()

	// MemStats.Sys = /memory/classes/total:bytes - /memory/classes/heap/released:bytes
	// (memory obtained from the OS that has not been released back to it). This
	// one is a difference, not a sum, so it can't go through emit.
	if total, ok := vals[mTotal]; ok {
		if released, ok := vals[mHeapReleased]; ok {
			points = append(points, MetricPoint{Name: "go_mem_sys_bytes", Labels: labels, Value: float64(total - released), Timestamp: ts})
		}
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
