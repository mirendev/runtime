package entitysync

import (
	"context"
	"log/slog"
	"maps"
	"sync"
	"time"

	"miren.dev/runtime/metrics"
)

// StateMetrics publishes the exporter's mode as an operational gauge, so the
// central store can alert on a cluster whose sync sits in a state it should
// never be in. The motivating case is a session that is connected while
// cloud declines entity sync (a contract digest cloud doesn't know): the
// uplink reports healthy, nothing is exported, and until now the only place
// that said so was `miren debug cloud-sync`.
//
// This deliberately reports state, not freshness. A quiet cluster exports
// nothing for days, so a watermark that hasn't moved is not a fault; a mode
// of waiting/capability-not-selected always is.
type StateMetrics struct {
	Log         *slog.Logger
	Writer      metrics.PointWriter
	Diagnostics *Diagnostics

	mu sync.Mutex
	// last is the label set of the previous push, retired at zero when the
	// state changes. See Emit.
	last map[string]string
}

// State changes are rare, so the push only needs to be frequent enough that
// the series stays live for instant queries and an alert's pending window.
const defaultStateMetricsInterval = 30 * time.Second

// NewStateMetrics creates a StateMetrics collector. Writer may be nil for
// environments without metrics collection, in which case Monitor is a no-op.
func NewStateMetrics(log *slog.Logger, writer metrics.PointWriter, diagnostics *Diagnostics) *StateMetrics {
	return &StateMetrics{Log: log, Writer: writer, Diagnostics: diagnostics}
}

// Monitor pushes the current state immediately and then once per
// defaultStateMetricsInterval until ctx is cancelled.
func (m *StateMetrics) Monitor(ctx context.Context) {
	if m.Writer == nil || m.Diagnostics == nil {
		return
	}

	ticker := time.NewTicker(defaultStateMetricsInterval)
	defer ticker.Stop()

	for {
		if err := m.Emit(ctx); err != nil {
			m.Log.Error("failed to record entity sync state", "err", err)
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}

// Emit pushes one sample of miren_entity_sync_state. The current state's
// series reads 1, in the same idiom as miren_build_info: a rule matches on
// the labels and a value of 1.
//
// When the state changes, the previous state's series is written once at 0
// in the same batch. Pushed samples get no staleness marker (those come from
// a scrape loop), so without that the old series would keep reading 1 for
// the store's lookback window after it stopped being true, and an alert on
// the fault could hold or even fire after recovery. Only this process's own
// previous state is retired; a series left by a process that has since
// restarted still ages out on the lookback window.
//
// The reason label is the wait or retry reason. Every source of it is a
// fixed string (the exporter's setMode and retry call sites, SetDisabled,
// and "uplink-" plus an uplink state), which is what keeps the label bounded.
// Keep it that way: a reason built from an error message would mint a series
// per failure.
func (m *StateMetrics) Emit(ctx context.Context) error {
	if m == nil || m.Writer == nil || m.Diagnostics == nil {
		return nil
	}
	status := m.Diagnostics.SnapshotStatus()
	current := map[string]string{"mode": status.Mode, "reason": status.WaitReason}
	now := time.Now()

	m.mu.Lock()
	defer m.mu.Unlock()
	points := []metrics.MetricPoint{{Name: "miren_entity_sync_state", Labels: current, Value: 1, Timestamp: now}}
	if m.last != nil && !maps.Equal(m.last, current) {
		points = append(points, metrics.MetricPoint{Name: "miren_entity_sync_state", Labels: m.last, Value: 0, Timestamp: now})
	}
	if err := m.Writer.WritePoints(ctx, points); err != nil {
		// Keep the old state so the next push retries retiring it.
		return err
	}
	m.last = current
	return nil
}
