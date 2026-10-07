package entitysync

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/pkg/uplink"
)

type recordingWriter struct {
	points []metrics.MetricPoint
}

func (w *recordingWriter) WritePoints(_ context.Context, points []metrics.MetricPoint) error {
	w.points = append(w.points, points...)
	return nil
}

func TestStateMetricsReportsDeclinedCapability(t *testing.T) {
	diagnostics := NewDiagnostics("sha256:test")
	// Connected, but cloud's welcome did not select entity sync: the 9/24
	// signature.
	diagnostics.ObserveUplink(uplink.Status{State: "connected", Session: &uplink.Session{ID: "session-1"}})

	writer := &recordingWriter{}
	require.NoError(t, NewStateMetrics(nil, writer, diagnostics).Emit(context.Background()))

	require.Len(t, writer.points, 1)
	point := writer.points[0]
	require.Equal(t, "miren_entity_sync_state", point.Name)
	require.Equal(t, float64(1), point.Value)
	require.Equal(t, map[string]string{"mode": "waiting", "reason": "capability-not-selected"}, point.Labels)
}

func TestStateMetricsReportsHealthyExport(t *testing.T) {
	diagnostics := NewDiagnostics("sha256:test")
	diagnostics.ObserveUplink(uplink.Status{State: "connected", Session: &uplink.Session{
		ID:           "session-1",
		Capabilities: []uplink.CapabilitySelection{{Name: uplink.CapabilityEntitySync, Version: Version1}},
	}})
	diagnostics.setMode("watching", "")

	writer := &recordingWriter{}
	require.NoError(t, NewStateMetrics(nil, writer, diagnostics).Emit(context.Background()))

	require.Len(t, writer.points, 1)
	require.Equal(t, map[string]string{"mode": "watching", "reason": ""}, writer.points[0].Labels)
}

func TestStateMetricsRetiresPreviousStateOnTransition(t *testing.T) {
	diagnostics := NewDiagnostics("sha256:test")
	diagnostics.ObserveUplink(uplink.Status{State: "connected", Session: &uplink.Session{ID: "session-1"}})
	writer := &recordingWriter{}
	collector := NewStateMetrics(nil, writer, diagnostics)
	declined := map[string]string{"mode": "waiting", "reason": "capability-not-selected"}
	watching := map[string]string{"mode": "watching", "reason": ""}

	require.NoError(t, collector.Emit(context.Background()))
	require.NoError(t, collector.Emit(context.Background()))
	require.Len(t, writer.points, 2, "an unchanged state writes only its own series")

	// Recovery: the declined series must be written at 0, or an instant
	// query keeps reading it as 1 until the store's lookback runs out.
	diagnostics.setMode("watching", "")
	writer.points = nil
	require.NoError(t, collector.Emit(context.Background()))
	require.Len(t, writer.points, 2)
	require.Equal(t, watching, writer.points[0].Labels)
	require.Equal(t, float64(1), writer.points[0].Value)
	require.Equal(t, declined, writer.points[1].Labels)
	require.Equal(t, float64(0), writer.points[1].Value)

	// Retired once; after that only the current state is written.
	writer.points = nil
	require.NoError(t, collector.Emit(context.Background()))
	require.Len(t, writer.points, 1)
	require.Equal(t, watching, writer.points[0].Labels)
}

func TestStateMetricsWithoutWriterIsNoop(t *testing.T) {
	require.NoError(t, NewStateMetrics(nil, nil, NewDiagnostics("sha256:test")).Emit(context.Background()))
}
