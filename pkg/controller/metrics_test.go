package controller

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/metrics"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/slogfmt"
)

func TestReconcileControllerMetrics(t *testing.T) {
	type requestResult struct {
		body string
		err  error
	}
	resultCh := make(chan requestResult, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		resultCh <- requestResult{body: string(data), err: err}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	log := slog.New(slogfmt.NewTestHandler(t, &slog.HandlerOptions{Level: slog.LevelDebug}))
	writer := metrics.NewVictoriaMetricsWriter(log, server.URL, time.Second)
	controller := &ReconcileController{
		Log:          log,
		name:         "garden",
		queue:        newDirtyQueue(),
		metricWriter: writer,
		metricLabels: map[string]string{"role": "coordinator"},
	}
	defer controller.queue.Close()

	now := time.Now()
	controller.queue.now = func() time.Time { return now }
	controller.queue.Add(workSignal{
		id:       entity.Id("app/one"),
		priority: workRepair,
		present:  true,
		queuedAt: now.Add(-2 * time.Second),
	})
	controller.counters.dropped.Add(1)
	controller.counters.coalesced.Add(2)
	controller.counters.retries.Add(3)
	controller.counters.failures.Add(4)
	controller.counters.writes.Add(5)
	controller.counters.inFlight.Add(6)

	controller.writeMetrics(t.Context(), now)
	writer.Flush()
	var result requestResult
	select {
	case result = <-resultCh:
	case <-time.After(time.Second):
		require.FailNow(t, "timed out waiting for metrics request")
	}
	require.NoError(t, result.err)
	body := result.body

	for _, name := range []string{
		"reconcile_controller_queue_depth",
		"reconcile_controller_queue_oldest_age_seconds",
		"reconcile_controller_in_flight",
		"reconcile_controller_dropped_total",
		"reconcile_controller_coalesced_total",
		"reconcile_controller_retries_total",
		"reconcile_controller_failures_total",
		"reconcile_controller_writes_total",
	} {
		assert.Contains(t, body, name)
	}
	assert.Contains(t, body, `controller="garden"`)
	assert.Contains(t, body, `role="coordinator"`)
	metricLine := func(name string) string {
		for line := range strings.SplitSeq(body, "\n") {
			if strings.HasPrefix(line, name+"{") {
				return line
			}
		}
		return ""
	}
	assert.Contains(t, metricLine("reconcile_controller_queue_depth"), "} 1 ")
	assert.Contains(t, metricLine("reconcile_controller_queue_oldest_age_seconds"), "} 2 ")
	for name, value := range map[string]string{
		"reconcile_controller_in_flight":       "} 6 ",
		"reconcile_controller_dropped_total":   "} 1 ",
		"reconcile_controller_coalesced_total": "} 2 ",
		"reconcile_controller_retries_total":   "} 3 ",
		"reconcile_controller_failures_total":  "} 4 ",
		"reconcile_controller_writes_total":    "} 5 ",
	} {
		assert.Contains(t, metricLine(name), value, name)
	}
}
