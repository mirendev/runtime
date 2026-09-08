package controller

import (
	"context"
	"maps"
	"sync/atomic"
	"time"

	"miren.dev/runtime/metrics"
)

type ManagerOption func(*ControllerManager)

func WithMetrics(writer *metrics.VictoriaMetricsWriter, labels map[string]string) ManagerOption {
	return func(manager *ControllerManager) {
		manager.metrics = writer
		manager.labels = maps.Clone(labels)
	}
}

type controllerCounters struct {
	dropped   atomic.Uint64
	coalesced atomic.Uint64
	retries   atomic.Uint64
	failures  atomic.Uint64
	writes    atomic.Uint64
	inFlight  atomic.Int64
}

func (c *ReconcileController) reportMetrics(ctx context.Context) {
	if c.metricWriter == nil {
		return
	}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		c.writeMetrics(ctx, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (c *ReconcileController) writeMetrics(ctx context.Context, now time.Time) {
	stats := c.queue.Stats()
	labels := maps.Clone(c.metricLabels)
	if labels == nil {
		labels = make(map[string]string)
	}
	labels["controller"] = c.name
	points := []metrics.MetricPoint{
		{Name: "reconcile_controller_queue_depth", Labels: labels, Value: float64(stats.depth), Timestamp: now},
		{Name: "reconcile_controller_queue_oldest_age_seconds", Labels: labels, Value: stats.oldestAge.Seconds(), Timestamp: now},
		{Name: "reconcile_controller_in_flight", Labels: labels, Value: float64(c.counters.inFlight.Load()), Timestamp: now},
		{Name: "reconcile_controller_dropped_total", Labels: labels, Value: float64(c.counters.dropped.Load()), Timestamp: now},
		{Name: "reconcile_controller_coalesced_total", Labels: labels, Value: float64(c.counters.coalesced.Load()), Timestamp: now},
		{Name: "reconcile_controller_retries_total", Labels: labels, Value: float64(c.counters.retries.Load()), Timestamp: now},
		{Name: "reconcile_controller_failures_total", Labels: labels, Value: float64(c.counters.failures.Load()), Timestamp: now},
		{Name: "reconcile_controller_writes_total", Labels: labels, Value: float64(c.counters.writes.Load()), Timestamp: now},
	}
	if err := c.metricWriter.WritePoints(ctx, points); err != nil {
		c.Log.Debug("failed to report controller metrics", "error", err)
	}
}
