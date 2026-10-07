package saga

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// eventCounts reads one definition's counts by event name, so a failed
// assertion shows which event was off rather than an index.
func eventCounts(c *Counts, definition string) map[string]uint64 {
	snap := c.Snapshot()[definition]
	out := map[string]uint64{}
	for ev := range NumEvents {
		if snap.Events[ev] > 0 {
			out[Event(ev).String()] = snap.Events[ev]
		}
	}
	return out
}

func defineCalc(t *testing.T, registry *Registry, name string, ctrl *testController) {
	t.Helper()
	require.NoError(t, Define(name).
		Using(ctrl).
		Action("add", AddNumbers).Undo(UndoAddNumbers).
		Action("multiply", Multiply).Undo(UndoMultiply).
		RegisterTo(registry))
}

func runCalc(executor *Executor, name, id string) error {
	return executor.Start(name).
		Input("a", 2).
		Input("b", 3).
		Input("factor", 4).
		WithID(id).
		Execute(context.Background())
}

func TestCounts_ExecutionOutcomesByDefinition(t *testing.T) {
	registry := NewRegistry()
	defineCalc(t, registry, "counts-ok", &testController{})
	defineCalc(t, registry, "counts-rollback", &testController{failMultiply: true})

	counts := &Counts{}
	executor := NewExecutor(NewMemoryStorage(), WithRegistry(registry), WithCounts(counts))

	require.NoError(t, runCalc(executor, "counts-ok", "ok-1"))
	require.NoError(t, runCalc(executor, "counts-ok", "ok-2"))
	require.Error(t, runCalc(executor, "counts-rollback", "rollback-1"))

	assert.Equal(t, map[string]uint64{"started": 2, "completed": 2}, eventCounts(counts, "counts-ok"))
	assert.Equal(t, map[string]uint64{"started": 1, "rolled_back": 1}, eventCounts(counts, "counts-rollback"))

	// Continuing a named execution that already finished is not a new start.
	require.NoError(t, runCalc(executor, "counts-ok", "ok-1"))
	assert.Equal(t, map[string]uint64{"started": 2, "completed": 2}, eventCounts(counts, "counts-ok"))
}

// TestCounts_CompensationAndRecovery follows one execution whose undo keeps
// failing: every undo pass that leaves work behind counts, a recovery that
// leaves it in flight is a failed recovery, and the recovery that finally
// rolls it back is a successful one even though resume reports the rollback
// as an error.
func TestCounts_CompensationAndRecovery(t *testing.T) {
	registry := NewRegistry()
	ctrl := &testController{failMultiply: true, failUndoAdd: true}
	defineCalc(t, registry, "counts-stuck", ctrl)

	counts := &Counts{}
	storage := NewMemoryStorage()
	executor := NewExecutor(storage, WithRegistry(registry), WithCounts(counts))

	require.Error(t, runCalc(executor, "counts-stuck", "stuck-1"))
	assert.Equal(t, map[string]uint64{"started": 1, "compensation_failed": 1},
		eventCounts(counts, "counts-stuck"))

	require.Error(t, executor.Recover(context.Background()))
	assert.Equal(t, map[string]uint64{"started": 1, "compensation_failed": 2, "recovery_failed": 1},
		eventCounts(counts, "counts-stuck"))

	ctrl.failUndoAdd = false
	require.Error(t, executor.Recover(context.Background()),
		"a recovered rollback still reports the saga's failure")
	assert.Equal(t, map[string]uint64{
		"started": 1, "compensation_failed": 2, "recovery_failed": 1,
		"rolled_back": 1, "recovered": 1,
	}, eventCounts(counts, "counts-stuck"))

	exec, err := storage.Get(context.Background(), "stuck-1")
	require.NoError(t, err)
	assert.Equal(t, StatusFailed, exec.Status)
}

func TestCounts_RecoveryResumesToCompletion(t *testing.T) {
	registry := NewRegistry()
	defineCalc(t, registry, "counts-resume", &testController{})

	storage := NewMemoryStorage()
	now := time.Now()
	require.NoError(t, storage.Save(context.Background(), &Execution{
		ID:                "crashed",
		DefinitionName:    "counts-resume",
		DefinitionVersion: 1,
		InitialInputs:     map[string]any{"a": float64(2), "b": float64(3), "factor": float64(4)},
		Status:            StatusRunning,
		ExecutedActions: map[string]*ActionResult{
			"add": {Output: []byte(`{"Sum":5}`), ExecutedAt: now},
		},
		ExecutionOrder: []string{"add"},
		CreatedAt:      now,
		UpdatedAt:      now,
	}))

	counts := &Counts{}
	executor := NewExecutor(storage, WithRegistry(registry), WithCounts(counts))
	require.NoError(t, executor.Recover(context.Background()))

	assert.Equal(t, map[string]uint64{"completed": 1, "recovered": 1}, eventCounts(counts, "counts-resume"),
		"a recovered execution was started by a previous process, so it is not counted as a start here")
}

// TestCounts_UnpersistedFinishIsNotCounted pins that an outcome counts only
// once the store has it. The executor sets a terminal status before saving it,
// so a failed save leaves an execution that looks finished in memory and is
// still in flight in the store, which is what recovery will find next time.
func TestCounts_UnpersistedFinishIsNotCounted(t *testing.T) {
	t.Run("rollback", func(t *testing.T) {
		registry := NewRegistry()
		defineCalc(t, registry, "counts-unsaved-rollback", &testController{failMultiply: true})

		// Saves: initial, running, add's result, the failure, undoing, add's
		// undo. The seventh, the terminal failed state, is the one that fails.
		counts := &Counts{}
		storage := newFailingStorage(6)
		executor := NewExecutor(storage, WithRegistry(registry), WithCounts(counts))

		require.Error(t, runCalc(executor, "counts-unsaved-rollback", "unsaved-rollback"))
		assert.Equal(t, map[string]uint64{"started": 1}, eventCounts(counts, "counts-unsaved-rollback"))

		exec, err := storage.Get(context.Background(), "unsaved-rollback")
		require.NoError(t, err)
		assert.Equal(t, StatusUndoing, exec.Status)
	})

	t.Run("recovery", func(t *testing.T) {
		registry := NewRegistry()
		defineCalc(t, registry, "counts-unsaved-recovery", &testController{})

		// Saves: the seeded crash, running, multiply's result. The fourth, the
		// completed state, is the one that fails.
		storage := newFailingStorage(3)
		now := time.Now()
		require.NoError(t, storage.Save(context.Background(), &Execution{
			ID:                "unsaved-recovery",
			DefinitionName:    "counts-unsaved-recovery",
			DefinitionVersion: 1,
			InitialInputs:     map[string]any{"a": float64(2), "b": float64(3), "factor": float64(4)},
			Status:            StatusRunning,
			ExecutedActions: map[string]*ActionResult{
				"add": {Output: []byte(`{"Sum":5}`), ExecutedAt: now},
			},
			ExecutionOrder: []string{"add"},
			CreatedAt:      now,
			UpdatedAt:      now,
		}))

		counts := &Counts{}
		executor := NewExecutor(storage, WithRegistry(registry), WithCounts(counts))
		require.Error(t, executor.Recover(context.Background()))

		assert.Equal(t, map[string]uint64{"recovery_failed": 1}, eventCounts(counts, "counts-unsaved-recovery"))
	})
}

type undoCanceller struct{ cancel context.CancelFunc }

// cancellingUndoAdd stands in for an undo that is running when shutdown
// cancels the context, and fails because of it.
func cancellingUndoAdd(ctx context.Context, in AddNumbersIn, out AddNumbersOut) error {
	Get[*undoCanceller](ctx).cancel()
	return ctx.Err()
}

// TestCounts_ShutdownDuringLastUndoIsNotACompensationFailure pins that an undo
// cut short by shutdown counts as nothing, even when it is the last one and no
// later iteration of the undo loop sees the cancellation.
func TestCounts_ShutdownDuringLastUndoIsNotACompensationFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	registry := NewRegistry()
	require.NoError(t, Define("counts-shutdown-undo").
		Using(&testController{failMultiply: true}, &undoCanceller{cancel: cancel}).
		Action("add", AddNumbers).Undo(cancellingUndoAdd).
		Action("multiply", Multiply).Undo(UndoMultiply).
		RegisterTo(registry))

	counts := &Counts{}
	storage := NewMemoryStorage()
	executor := NewExecutor(storage, WithRegistry(registry), WithCounts(counts))

	err := executor.Start("counts-shutdown-undo").
		Input("a", 2).
		Input("b", 3).
		Input("factor", 4).
		WithID("shutdown-undo").
		Execute(ctx)
	require.ErrorIs(t, err, context.Canceled)

	assert.Equal(t, map[string]uint64{"started": 1}, eventCounts(counts, "counts-shutdown-undo"))

	exec, err := storage.Get(context.Background(), "shutdown-undo")
	require.NoError(t, err)
	assert.Equal(t, StatusUndoing, exec.Status, "left for recovery to resume")
}

// cancellingButSucceedingUndoAdd finishes its undo even though shutdown
// arrives while it runs.
func cancellingButSucceedingUndoAdd(ctx context.Context, in AddNumbersIn, out AddNumbersOut) error {
	Get[*undoCanceller](ctx).cancel()
	return nil
}

// TestCounts_ShutdownAfterEveryUndoSucceededStillRollsBack pins the other side
// of the shutdown check: when every undo succeeded, a cancellation arriving at
// the end changes nothing, and the rollback is recorded as it always was.
func TestCounts_ShutdownAfterEveryUndoSucceededStillRollsBack(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	registry := NewRegistry()
	require.NoError(t, Define("counts-shutdown-after-undo").
		Using(&testController{failMultiply: true}, &undoCanceller{cancel: cancel}).
		Action("add", AddNumbers).Undo(cancellingButSucceedingUndoAdd).
		Action("multiply", Multiply).Undo(UndoMultiply).
		RegisterTo(registry))

	counts := &Counts{}
	storage := NewMemoryStorage()
	executor := NewExecutor(storage, WithRegistry(registry), WithCounts(counts))

	err := executor.Start("counts-shutdown-after-undo").
		Input("a", 2).
		Input("b", 3).
		Input("factor", 4).
		WithID("shutdown-after-undo").
		Execute(ctx)
	require.Error(t, err)
	assert.NotErrorIs(t, err, context.Canceled, "a completed rollback reports the saga's failure, not an interruption")

	assert.Equal(t, map[string]uint64{"started": 1, "rolled_back": 1}, eventCounts(counts, "counts-shutdown-after-undo"))

	exec, err := storage.Get(context.Background(), "shutdown-after-undo")
	require.NoError(t, err)
	assert.Equal(t, StatusFailed, exec.Status)
}

func TestCounts_NilIsANoOp(t *testing.T) {
	var counts *Counts
	assert.NotPanics(t, func() { counts.Add("anything", EventStarted, 1) })
}
