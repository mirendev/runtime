package etcd

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"miren.dev/runtime/metrics"
)

// TestSetMetricsWriterKicksMaintenanceLoop verifies that attaching a metrics writer
// enqueues a single, non-blocking nudge for the maintenance loop. The loop consumes
// this to emit a health sample promptly instead of waiting for the next periodic tick
// (the writer is attached well after the loop's immediate startup check).
func TestSetMetricsWriterKicksMaintenanceLoop(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	e := NewEtcdComponent(log, nil, "test", t.TempDir())

	// A nil writer clears the sink but must not enqueue a kick — there is nothing to
	// emit to, so waking the loop would just log a status line into the void.
	e.SetMetricsWriter(nil)
	select {
	case <-e.metricsKick:
		t.Fatal("nil writer should not enqueue a maintenance kick")
	default:
	}

	// Attaching a real writer enqueues a kick, and repeated attaches coalesce onto the
	// single buffered slot rather than blocking (the loop will read the latest writer
	// anyway via the atomic pointer).
	w := metrics.NewVictoriaMetricsWriter(log, "localhost:8428", time.Second)
	e.SetMetricsWriter(w)
	e.SetMetricsWriter(w)
	e.SetMetricsWriter(w)

	select {
	case <-e.metricsKick:
	default:
		t.Fatal("attaching a metrics writer should enqueue a maintenance kick")
	}
	select {
	case <-e.metricsKick:
		t.Fatal("kicks should coalesce to a single pending signal")
	default:
	}
}
