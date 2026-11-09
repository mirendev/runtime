package diskio

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"miren.dev/runtime/metrics"
)

// diskMetrics publishes this process's disk counters. Each increment pushes
// the counter's running total, so increase() sees every event and a restart
// starts a new series from zero. A nil writer, or a nil *diskMetrics, records
// nothing.
type diskMetrics struct {
	writer metrics.PointWriter
	mu     sync.Mutex
	counts map[string]float64
}

func newDiskMetrics(w metrics.PointWriter) *diskMetrics {
	if w == nil {
		return nil
	}
	return &diskMetrics{writer: w, counts: map[string]float64{}}
}

func (m *diskMetrics) inc(ctx context.Context, name string, labels map[string]string) {
	if m == nil {
		return
	}
	keys := make([]string, 0, len(labels))
	for k, v := range labels {
		keys = append(keys, k+"="+v)
	}
	sort.Strings(keys)
	key := name + "{" + strings.Join(keys, ",") + "}"
	m.mu.Lock()
	m.counts[key]++
	value := m.counts[key]
	m.mu.Unlock()
	// Delivery is best-effort: a lost push is repaired by the next one.
	_ = m.writer.WritePoints(context.WithoutCancel(ctx), []metrics.MetricPoint{{Name: name, Labels: labels, Value: value, Timestamp: time.Now()}})
}
