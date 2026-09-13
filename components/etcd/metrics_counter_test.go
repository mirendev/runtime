package etcd

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/metrics"
)

type recordingSink struct {
	batches [][]metrics.MetricPoint
}

func (s *recordingSink) WritePoints(_ context.Context, points []metrics.MetricPoint) error {
	s.batches = append(s.batches, points)
	return nil
}

// TestEmitMetricsIncludesRecoveryCounter checks the counter rides along with
// every gauge batch, so a sink attached after a recovery still sees it even
// though the alarm gauge already reads healthy.
func TestEmitMetricsIncludesRecoveryCounter(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	e := NewEtcdComponent(log, nil, "test", t.TempDir())
	e.noSpaceRecoveries.Add(1)
	sink := &recordingSink{}
	e.SetMetricsWriter(sink)

	e.emitMetrics(context.Background(), 100, 50, 2.0, false)

	require.Len(t, sink.batches, 1)
	values := make(map[string]float64)
	for _, point := range sink.batches[0] {
		values[point.Name] = point.Value
	}
	assert.Equal(t, 1.0, values["etcd_nospace_recovery_total"])
	assert.Equal(t, 0.0, values["etcd_nospace_alarm"])
	assert.Equal(t, 100.0, values["etcd_db_size_bytes"])
}
