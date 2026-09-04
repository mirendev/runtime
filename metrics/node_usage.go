package metrics

import (
	"context"
	"log/slog"
	"time"

	"miren.dev/runtime/pkg/sysstats"
)

// NodeUsage records what a whole host is doing, as opposed to what any single
// sandbox on it is doing.
//
// Two questions need this. First, "how much of this machine is left?" -- a
// workload burning 200% of a core is unremarkable on a 64-core box and an
// emergency on a 2-core one, and nothing in the per-sandbox series says which
// box it is. Second, "is the load even an app?" -- containerd, buildkit, the
// registry, the log pipeline and miren itself run outside every sandbox cgroup,
// so they are invisible in the per-sandbox series. Subtracting the sum of
// sandbox usage from the host total is what surfaces them, and that subtraction
// needs a host total to start from.
//
// Series are labeled by node so they join against the miren.node label the
// sandbox collectors now attach.
type NodeUsage struct {
	Log    *slog.Logger
	Writer *VictoriaMetricsWriter

	// NodeID is the node entity ID these series describe, and RunnerID is the
	// runner identifier for the same host. Both are emitted: the entity ID is
	// what joins to the node record, the runner ID is what an operator typed.
	NodeID   string
	RunnerID string

	// DataPath is the filesystem whose capacity is reported. Empty skips the
	// storage series rather than reporting the root filesystem, which would be
	// a different disk on most real deployments.
	DataPath string

	// prevBusy and prevTotal hold the previous tick's raw CPU counters. CPU
	// utilization is only meaningful as a delta between two samples, so the
	// first tick after startup emits no CPU series at all.
	prevBusy  uint64
	prevTotal uint64
	primed    bool
}

const defaultNodeUsageInterval = 10 * time.Second

// NewNodeUsage creates a NodeUsage collector. Writer may be nil for
// environments without metrics collection, in which case Monitor is a no-op.
func NewNodeUsage(log *slog.Logger, writer *VictoriaMetricsWriter, nodeID, runnerID, dataPath string) *NodeUsage {
	return &NodeUsage{
		Log:      log,
		Writer:   writer,
		NodeID:   nodeID,
		RunnerID: runnerID,
		DataPath: dataPath,
	}
}

// Monitor samples host stats every defaultNodeUsageInterval and pushes one
// batch of points per tick until ctx is cancelled. Mirrors the cadence and
// lifecycle of RuntimeMemory.Monitor.
func (n *NodeUsage) Monitor(ctx context.Context) {
	if n.Writer == nil {
		return
	}

	n.Log.Info("node resource metrics started",
		"node", n.NodeID, "runner", n.RunnerID, "interval", defaultNodeUsageInterval)

	ticker := time.NewTicker(defaultNodeUsageInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := n.collect(ctx); err != nil {
				n.Log.Warn("failed to record node resource usage", "err", err)
			}
		case <-ctx.Done():
			return
		}
	}
}

func (n *NodeUsage) collect(ctx context.Context) error {
	stats := sysstats.CollectSystemStats(n.DataPath)

	// A zero core count means the host's stats could not be read at all --
	// either /proc is unavailable or this is a platform with no collector
	// (darwin returns an empty struct). Writing the zeroed struct would publish
	// a host that appears to have no CPUs and no load, which reads as a healthy
	// idle machine rather than as missing data. Write nothing instead.
	if stats.CPUCoreCount == 0 {
		return nil
	}

	ts := time.Now()
	labels := map[string]string{"miren.node": n.NodeID}
	if n.RunnerID != "" {
		labels["miren.runner"] = n.RunnerID
	}

	points := make([]MetricPoint, 0, 9)
	emit := func(name string, value float64) {
		points = append(points, MetricPoint{Name: name, Labels: labels, Value: value, Timestamp: ts})
	}

	emit("node_cpu_cores_total", float64(stats.CPUCoreCount))

	if cores, ok := n.cpuCoresUsed(stats); ok {
		emit("node_cpu_cores_used", cores)
	}

	if stats.MemoryTotalBytes > 0 {
		emit("node_memory_total_bytes", float64(stats.MemoryTotalBytes))
		emit("node_memory_used_bytes", float64(stats.MemoryBytes))
	}

	if stats.StorageTotalBytes > 0 {
		emit("node_storage_total_bytes", float64(stats.StorageTotalBytes))
		emit("node_storage_used_bytes", float64(stats.StorageBytes))
	}

	emit("node_load1", stats.Load1)
	emit("node_load5", stats.Load5)
	emit("node_load15", stats.Load15)

	return n.Writer.WritePoints(ctx, points)
}

// cpuCoresUsed converts the jiffy counters into cores busy since the last
// sample, reporting ok=false when there is nothing to compare against yet.
//
// Working in cores rather than a percentage keeps this directly comparable with
// the per-sandbox CPU series, which is also in cores -- the subtraction that
// isolates Miren's own overhead needs both sides in the same unit.
func (n *NodeUsage) cpuCoresUsed(stats sysstats.ResourceUsage) (float64, bool) {
	busy, total := stats.CPUBusyJiffies, stats.CPUTotalJiffies
	if total == 0 || stats.CPUCoreCount == 0 {
		return 0, false
	}

	prevBusy, prevTotal, primed := n.prevBusy, n.prevTotal, n.primed
	n.prevBusy, n.prevTotal, n.primed = busy, total, true

	if !primed {
		return 0, false
	}

	// A counter that went backwards means the host rebooted between samples.
	// The delta is meaningless, so skip this tick and let the next one, which
	// compares two post-reboot samples, report the truth.
	if busy < prevBusy || total <= prevTotal {
		return 0, false
	}

	busyFraction := float64(busy-prevBusy) / float64(total-prevTotal)

	return busyFraction * float64(stats.CPUCoreCount), true
}
