//go:build linux

package server

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/metrics"
)

// recordingSink is a PointWriter that keeps every batch it is handed.
type recordingSink struct {
	batches [][]metrics.MetricPoint
}

func (s *recordingSink) WritePoints(_ context.Context, points []metrics.MetricPoint) error {
	s.batches = append(s.batches, points)
	return nil
}

func TestAttachShippingReemitsProcessIdentity(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	embedded := &recordingSink{}
	operational := metrics.NewFanout(embedded)
	processInfo := metrics.NewProcessInfo(log, operational)
	observability := observabilityBootOutput{
		operationalMetrics: operational,
		processInfo:        processInfo,
	}

	// The process pushed its identity before the shipping sink existed; only
	// the embedded store has it.
	require.NoError(t, processInfo.Emit(t.Context()))
	require.Len(t, embedded.batches, 1)

	shipping := &recordingSink{}
	b := &appMetricsBoot{}
	b.attachShipping(t.Context(), log, observability, shipping, map[string]string{"miren_cluster": "c1"})

	require.Len(t, shipping.batches, 1, "shipping sink must receive the re-emit that follows Attach")
	names := map[string]map[string]string{}
	for _, p := range shipping.batches[0] {
		names[p.Name] = p.Labels
	}
	assert.Contains(t, names, "process_start_time_seconds")
	assert.Contains(t, names, "miren_build_info")
	for name, labels := range names {
		assert.Equal(t, "c1", labels["miren_cluster"], "%s missing cluster identity", name)
		assert.Equal(t, "miren/control", labels["entity"], "%s missing entity", name)
	}
	assert.Len(t, embedded.batches, 2, "the embedded store sees the re-emit too")
}

func TestManagedMetricsClusterLabel(t *testing.T) {
	assert.Equal(t, "cluster-123", managedMetricsClusterLabel("cluster-123", "friendly-name"))
	assert.Equal(t, "friendly-name", managedMetricsClusterLabel("", "friendly-name"))
	assert.Equal(t, "local", managedMetricsClusterLabel("", ""))
}
