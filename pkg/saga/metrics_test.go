package saga

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/metrics"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

type recordingWriter struct {
	batches [][]metrics.MetricPoint
	err     error
}

func (w *recordingWriter) WritePoints(_ context.Context, points []metrics.MetricPoint) error {
	if w.err != nil {
		return w.err
	}
	w.batches = append(w.batches, points)
	return nil
}

// last renders the most recent batch as name{k=v,...} => value, sorted, so a
// test can compare a whole push at once.
func (w *recordingWriter) last(t *testing.T) map[string]float64 {
	t.Helper()
	require.NotEmpty(t, w.batches, "nothing was pushed")
	out := map[string]float64{}
	for _, p := range w.batches[len(w.batches)-1] {
		keys := make([]string, 0, len(p.Labels))
		for k := range p.Labels {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, k+"="+p.Labels[k])
		}
		out[fmt.Sprintf("%s{%s}", p.Name, strings.Join(parts, ","))] = p.Value
	}
	return out
}

func TestCountsMetrics_EmitsEverySeriesForACountedDefinition(t *testing.T) {
	counts := &Counts{}
	counts.Add("create-sandbox", EventStarted, 3)
	counts.Add("create-sandbox", EventCompleted, 2)
	counts.Add("create-sandbox", EventStrandedForced, 1)

	w := &recordingWriter{}
	m := &CountsMetrics{Log: testutils.TestLogger(t), Writer: w, Counts: counts, Entity: metrics.EntityRunner}
	require.NoError(t, m.Emit(context.Background(), time.Now()))

	labels := "definition=create-sandbox,entity=miren/runner"
	assert.Equal(t, map[string]float64{
		"saga_executions_started_total{" + labels + "}":                      3,
		"saga_executions_finished_total{" + labels + ",outcome=completed}":   2,
		"saga_executions_finished_total{" + labels + ",outcome=rolled_back}": 0,
		"saga_compensation_failures_total{" + labels + "}":                   0,
		"saga_recoveries_total{" + labels + ",outcome=recovered}":            0,
		"saga_recoveries_total{" + labels + ",outcome=failed}":               0,
		"saga_stranded_forced_total{" + labels + "}":                         1,
	}, w.last(t))
}

// TestCountsMetrics_FirstPushCarriesAZeroBaseline pins the zero each
// definition's series get on their first push. Recovery runs at boot, before
// anything else is counted, so a failed recovery is often the first sample its
// series ever gets, and increase() needs something before it to measure from.
func TestCountsMetrics_FirstPushCarriesAZeroBaseline(t *testing.T) {
	counts := &Counts{}
	counts.Add("create-sandbox", EventRecoveryFailed, 1)
	since := counts.Snapshot()["create-sandbox"].Since
	now := since.Add(5 * time.Second)

	w := &recordingWriter{err: errors.New("store unavailable")}
	m := &CountsMetrics{Log: testutils.TestLogger(t), Writer: w, Counts: counts, Entity: metrics.EntityControl}
	require.Error(t, m.Emit(context.Background(), now))

	// The failed push did not count as having sent the baseline.
	w.err = nil
	require.NoError(t, m.Emit(context.Background(), now))

	var failed []metrics.MetricPoint
	for _, p := range w.batches[0] {
		if p.Name == "saga_recoveries_total" && p.Labels["outcome"] == "failed" {
			failed = append(failed, p)
		}
	}
	require.Len(t, failed, 2, "a zero baseline and the current value")
	assert.Equal(t, float64(0), failed[0].Value)
	assert.True(t, since.Equal(failed[0].Timestamp), "the baseline is stamped when the definition was first counted")
	assert.Equal(t, float64(1), failed[1].Value)
	assert.True(t, now.Equal(failed[1].Timestamp))
	assert.Len(t, w.batches[0], 2*NumEvents, "every series gets a baseline")

	require.NoError(t, m.Emit(context.Background(), now.Add(10*time.Second)))
	assert.Len(t, w.batches[1], NumEvents, "the baseline goes out once")

	// A definition counted later gets its own baseline on its own first push.
	counts.Add("build-from-tar", EventStarted, 1)
	require.NoError(t, m.Emit(context.Background(), now.Add(20*time.Second)))
	assert.Len(t, w.batches[2], 3*NumEvents)
}

func TestCountsMetrics_NothingCountedPushesNothing(t *testing.T) {
	w := &recordingWriter{}
	m := &CountsMetrics{Log: testutils.TestLogger(t), Writer: w, Counts: &Counts{}, Entity: metrics.EntityControl}
	require.NoError(t, m.Emit(context.Background(), time.Now()))
	assert.Empty(t, w.batches)
}

func saveInFlight(t *testing.T, storage Storage, id, definition string, status Status, created time.Time) {
	t.Helper()
	exec := &Execution{
		ID:              id,
		DefinitionName:  definition,
		Status:          StatusPending,
		InitialInputs:   map[string]any{},
		ExecutedActions: map[string]*ActionResult{},
		ExecutionOrder:  []string{},
		CreatedAt:       created,
		UpdatedAt:       created,
	}
	require.NoError(t, storage.Save(context.Background(), exec))
	if status != StatusPending {
		exec.Status = status
		// Touched just now, as a retrying undo would be. The age must still
		// come from when it started.
		exec.UpdatedAt = time.Now()
		require.NoError(t, storage.Save(context.Background(), exec))
	}
}

func TestInFlightMetrics_CountsAndOldestAgeByDefinitionAndStatus(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	storage := NewMemoryStorage()
	saveInFlight(t, storage, "a", "create-sandbox", StatusRunning, now.Add(-2*time.Minute))
	saveInFlight(t, storage, "b", "create-sandbox", StatusRunning, now.Add(-10*time.Minute))
	saveInFlight(t, storage, "c", "create-sandbox", StatusUndoing, now.Add(-3*time.Hour))
	saveInFlight(t, storage, "d", "build", StatusPending, now.Add(-30*time.Second))
	saveInFlight(t, storage, "done", "build", StatusCompleted, now.Add(-time.Hour))

	w := &recordingWriter{}
	m := NewInFlightMetrics(testutils.TestLogger(t), w, storage)
	require.NoError(t, m.Walk(context.Background(), now))
	require.NoError(t, m.Emit(context.Background(), now))

	assert.Equal(t, map[string]float64{
		"saga_incomplete_executions{definition=create-sandbox,status=running}":         2,
		"saga_incomplete_oldest_age_seconds{definition=create-sandbox,status=running}": 600,
		"saga_incomplete_executions{definition=create-sandbox,status=undoing}":         1,
		"saga_incomplete_oldest_age_seconds{definition=create-sandbox,status=undoing}": 3 * 3600,
		"saga_incomplete_executions{definition=build,status=pending}":                  1,
		"saga_incomplete_oldest_age_seconds{definition=build,status=pending}":          30,
	}, w.last(t))

	// Ages are computed at push time from the walk's start times, so a push
	// between walks still reports a growing age.
	require.NoError(t, m.Emit(context.Background(), now.Add(time.Minute)))
	assert.Equal(t, float64(3*3600+60),
		w.last(t)["saga_incomplete_oldest_age_seconds{definition=create-sandbox,status=undoing}"])
}

// TestInFlightMetrics_RetiresFinishedGroupsAtZero pins that a group whose
// executions have all finished is written at zero once. Pushed samples get no
// staleness marker, so without it the last nonzero value would hold for the
// store's lookback window and an age alert could keep firing after recovery.
func TestInFlightMetrics_RetiresFinishedGroupsAtZero(t *testing.T) {
	now := time.Now()
	storage := NewMemoryStorage()
	saveInFlight(t, storage, "stuck", "create-sandbox", StatusUndoing, now.Add(-3*time.Hour))

	w := &recordingWriter{}
	m := NewInFlightMetrics(testutils.TestLogger(t), w, storage)
	require.NoError(t, m.Walk(context.Background(), now))
	require.NoError(t, m.Emit(context.Background(), now))

	exec, err := storage.Get(context.Background(), "stuck")
	require.NoError(t, err)
	exec.Status = StatusFailed
	require.NoError(t, storage.Save(context.Background(), exec))

	require.NoError(t, m.Walk(context.Background(), now))
	require.NoError(t, m.Emit(context.Background(), now))
	assert.Equal(t, map[string]float64{
		"saga_incomplete_executions{definition=create-sandbox,status=undoing}":         0,
		"saga_incomplete_oldest_age_seconds{definition=create-sandbox,status=undoing}": 0,
	}, w.last(t))

	batches := len(w.batches)
	require.NoError(t, m.Emit(context.Background(), now))
	assert.Len(t, w.batches, batches, "a retired group is written once, then nothing is left to push")
}

func TestInFlightMetrics_FailedPushRetriesTheZeros(t *testing.T) {
	now := time.Now()
	storage := NewMemoryStorage()
	saveInFlight(t, storage, "x", "build", StatusRunning, now.Add(-time.Minute))

	w := &recordingWriter{}
	m := NewInFlightMetrics(testutils.TestLogger(t), w, storage)
	require.NoError(t, m.Walk(context.Background(), now))
	require.NoError(t, m.Emit(context.Background(), now))

	exec, err := storage.Get(context.Background(), "x")
	require.NoError(t, err)
	exec.Status = StatusCompleted
	require.NoError(t, storage.Save(context.Background(), exec))
	require.NoError(t, m.Walk(context.Background(), now))

	w.err = errors.New("store unavailable")
	require.Error(t, m.Emit(context.Background(), now))

	w.err = nil
	require.NoError(t, m.Emit(context.Background(), now))
	assert.Equal(t, float64(0), w.last(t)["saga_incomplete_executions{definition=build,status=running}"])
}

type failingSummaryStorage struct{}

func (failingSummaryStorage) ListIncompleteSummaryPage(context.Context, IncompleteSummaryQuery) (*IncompleteSummaryPage, error) {
	return nil, errors.New("store unavailable")
}

func TestInFlightMetrics_FailedWalkKeepsThePreviousResult(t *testing.T) {
	now := time.Now()
	storage := NewMemoryStorage()
	saveInFlight(t, storage, "x", "build", StatusRunning, now.Add(-time.Minute))

	w := &recordingWriter{}
	m := NewInFlightMetrics(testutils.TestLogger(t), w, storage)
	require.NoError(t, m.Walk(context.Background(), now))

	m.Storage = failingSummaryStorage{}
	require.Error(t, m.Walk(context.Background(), now))
	require.NoError(t, m.Emit(context.Background(), now))
	assert.Equal(t, float64(1), w.last(t)["saga_incomplete_executions{definition=build,status=running}"],
		"the store not answering is not every execution finishing")

	// But only for so long. Past that, an old picture pushed with a fresh
	// timestamp would read as current, so nothing is pushed and the series
	// go stale.
	batches := len(w.batches)
	require.NoError(t, m.Emit(context.Background(), now.Add(inFlightMaxResultAge+time.Second)))
	assert.Len(t, w.batches, batches, "a result past its age limit must not be pushed")
}

// TestInFlightMetrics_WalkRestsAfterEachAttempt pins that the next walk is
// scheduled from when the last one ended, success or failure, so a slow or
// failing store is never walked back to back.
func TestInFlightMetrics_WalkRestsAfterEachAttempt(t *testing.T) {
	m := NewInFlightMetrics(testutils.TestLogger(t), &recordingWriter{}, NewMemoryStorage())
	assert.True(t, m.walkDue(time.Now()), "the first walk is due immediately")

	require.NoError(t, m.Walk(context.Background(), time.Now()))
	assert.False(t, m.walkDue(time.Now()))
	assert.True(t, m.walkDue(time.Now().Add(defaultInFlightWalkInterval+time.Second)))

	m.walkFailed(time.Now())
	assert.False(t, m.walkDue(time.Now()), "a failed walk rests as long as a successful one")
}

// TestInFlightMetrics_CountsEachExecutionOnce pins the dedup across status
// indexes. A stale pending index entry for an execution that has moved on to
// running decodes with its real status, so without dedup it is counted as two
// running executions.
func TestInFlightMetrics_CountsEachExecutionOnce(t *testing.T) {
	runIndexBackedConformance(t, func(t *testing.T, storage Storage, store *entity.MockStore) {
		now := time.Now()
		saveInFlight(t, storage, "moved-on", "create-sandbox", StatusRunning, now.Add(-time.Minute))
		seedStaleStatusIndex(t, store, "moved-on", StatusPending)

		w := &recordingWriter{}
		m := NewInFlightMetrics(testutils.TestLogger(t), w, storage)
		require.NoError(t, m.Walk(context.Background(), now))
		require.NoError(t, m.Emit(context.Background(), now))

		assert.Equal(t, float64(1), w.last(t)["saga_incomplete_executions{definition=create-sandbox,status=running}"])
	})
}
