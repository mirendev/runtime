package saga

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/entity/testutils"
)

// saveAged persists an execution whose last state change was `age` ago.
func saveAged(t *testing.T, storage Storage, id string, status Status, age time.Duration) {
	t.Helper()

	finished := time.Now().Add(-age)
	err := storage.Save(context.Background(), &Execution{
		ID:             id,
		DefinitionName: "create-sandbox",
		Status:         status,
		InitialInputs:  map[string]any{"app": "demo"},
		ExecutedActions: map[string]*ActionResult{
			"allocate-ip": {Output: []byte(`{"ip":"10.0.0.5"}`), ExecutedAt: finished},
		},
		ExecutionOrder: []string{"allocate-ip"},
		CreatedAt:      finished,
		UpdatedAt:      finished,
	})
	require.NoError(t, err)
}

func executionExists(t *testing.T, storage Storage, id string) bool {
	t.Helper()

	_, err := storage.Get(context.Background(), id)
	return err == nil
}

func weekRetention() RetentionConfig {
	return RetentionConfig{Retention: 7 * 24 * time.Hour, MaxDeletes: 1000}
}

// TestRunRetention_DeletesExpiredTerminalExecutions is the core of MIR-1526: an
// execution that finished longer ago than the window goes away, one that
// finished recently does not, and success and failure are treated alike.
func TestRunRetention_DeletesExpiredTerminalExecutions(t *testing.T) {
	for _, backend := range allStorageBackends() {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			storage := backend.make(t)

			saveAged(t, storage, "old-completed", StatusCompleted, 30*24*time.Hour)
			saveAged(t, storage, "old-failed", StatusFailed, 8*24*time.Hour)
			saveAged(t, storage, "fresh-completed", StatusCompleted, 1*time.Hour)
			saveAged(t, storage, "fresh-failed", StatusFailed, 6*24*time.Hour)

			result, err := RunRetention(ctx, storage, weekRetention(), testutils.TestLogger(t))
			require.NoError(t, err)

			assert.Equal(t, 2, result.Deleted)
			assert.Equal(t, 4, result.Scanned)
			assert.Zero(t, result.Failed)

			assert.False(t, executionExists(t, storage, "old-completed"),
				"a completed saga past retention must be deleted")
			assert.False(t, executionExists(t, storage, "old-failed"),
				"a failed saga past retention must be deleted on the same rule as a completed one")
			assert.True(t, executionExists(t, storage, "fresh-completed"),
				"a saga inside the retention window must survive")
			assert.True(t, executionExists(t, storage, "fresh-failed"),
				"a failure inside the retention window is still debuggable")
		})
	}
}

// TestRunRetention_NeverDeletesIncompleteExecutions is the safety property that
// matters most. An execution still in flight is the one thing recovery needs,
// and an undoing saga can legitimately sit unfinished for a long time while its
// undos keep failing and retrying. Age must never be enough to collect one.
func TestRunRetention_NeverDeletesIncompleteExecutions(t *testing.T) {
	for _, backend := range allStorageBackends() {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			storage := backend.make(t)

			ids := []string{"ancient-pending", "ancient-running", "ancient-undoing"}
			saveAged(t, storage, ids[0], StatusPending, 90*24*time.Hour)
			saveAged(t, storage, ids[1], StatusRunning, 90*24*time.Hour)
			saveAged(t, storage, ids[2], StatusUndoing, 90*24*time.Hour)

			result, err := RunRetention(ctx, storage, weekRetention(), testutils.TestLogger(t))
			require.NoError(t, err)

			assert.Zero(t, result.Deleted)
			assert.Zero(t, result.Scanned, "incomplete executions must not even be scanned")

			for _, id := range ids {
				assert.True(t, executionExists(t, storage, id),
					"%s must survive: recovery still needs it", id)
			}
		})
	}
}

// TestRunRetention_CapsDeletes proves a large backlog drains over several passes
// instead of one thundering herd of writes, and that hitting the cap is
// reported rather than reading as a clean sweep.
func TestRunRetention_CapsDeletes(t *testing.T) {
	for _, backend := range allStorageBackends() {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			storage := backend.make(t)

			for _, id := range []string{"a", "b", "c", "d", "e"} {
				saveAged(t, storage, id, StatusCompleted, 30*24*time.Hour)
			}

			cfg := RetentionConfig{Retention: 7 * 24 * time.Hour, MaxDeletes: 3}

			result, err := RunRetention(ctx, storage, cfg, testutils.TestLogger(t))
			require.NoError(t, err)
			assert.Equal(t, 3, result.Deleted)
			assert.True(t, result.Capped, "hitting the cap must be reported, not silently truncated")

			// The next sweep picks up the remainder: the pass is idempotent and
			// resumes rather than restarting.
			result, err = RunRetention(ctx, storage, cfg, testutils.TestLogger(t))
			require.NoError(t, err)
			assert.Equal(t, 2, result.Deleted)
			assert.False(t, result.Capped)

			result, err = RunRetention(ctx, storage, cfg, testutils.TestLogger(t))
			require.NoError(t, err)
			assert.Zero(t, result.Deleted, "a converged store must be a no-op sweep")
		})
	}
}

// TestRunRetention_ExactlyMaxDeletesIsNotCapped pins the boundary. Spending the
// whole budget on the last execution in the list is a complete sweep: there is
// nothing left to inspect. Reporting it as capped would tell an operator a
// backlog remains on a store that just converged.
func TestRunRetention_ExactlyMaxDeletesIsNotCapped(t *testing.T) {
	for _, backend := range allStorageBackends() {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			storage := backend.make(t)

			for _, id := range []string{"a", "b", "c"} {
				saveAged(t, storage, id, StatusCompleted, 30*24*time.Hour)
			}

			result, err := RunRetention(ctx, storage,
				RetentionConfig{Retention: 7 * 24 * time.Hour, MaxDeletes: 3},
				testutils.TestLogger(t))
			require.NoError(t, err)

			assert.Equal(t, 3, result.Deleted)
			assert.False(t, result.Capped,
				"consuming the budget on the final execution left nothing uninspected")
		})
	}
}

// saveChild persists a terminal child execution belonging to parentID.
func saveChild(t *testing.T, storage Storage, id, parentID string, age time.Duration) {
	t.Helper()

	finished := time.Now().Add(-age)
	require.NoError(t, storage.Save(context.Background(), &Execution{
		ID:                id,
		DefinitionName:    "ensure-shared-server",
		ParentExecutionID: parentID,
		Status:            StatusCompleted,
		InitialInputs:     map[string]any{},
		ExecutedActions:   map[string]*ActionResult{},
		ExecutionOrder:    []string{},
		CreatedAt:         finished,
		UpdatedAt:         finished,
	}))
}

// TestRunRetention_KeepsChildrenOfLiveParents covers the nested-saga hole. A
// finished child is not independently safe to collect: recovery deliberately
// does not recover children, it re-runs the parent, which re-finds the child by
// deterministic ID and reuses its result. Delete the child and that lookup
// misses, so the parent re-executes the nested saga rather than resuming it,
// which is the double-execution sagas exist to prevent.
func TestRunRetention_KeepsChildrenOfLiveParents(t *testing.T) {
	for _, backend := range allStorageBackends() {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			storage := backend.make(t)

			// An in-flight parent, its expired child, and an expired child whose
			// parent already finished.
			saveAged(t, storage, "live-parent", StatusRunning, 30*24*time.Hour)
			saveChild(t, storage, "child-of-live", "live-parent", 30*24*time.Hour)

			saveAged(t, storage, "done-parent", StatusCompleted, 30*24*time.Hour)
			saveChild(t, storage, "child-of-done", "done-parent", 30*24*time.Hour)

			// A child whose parent is gone entirely: nothing remains to re-find it.
			saveChild(t, storage, "child-of-ghost", "never-existed", 30*24*time.Hour)

			result, err := RunRetention(ctx, storage, weekRetention(), testutils.TestLogger(t))
			require.NoError(t, err)

			assert.True(t, executionExists(t, storage, "child-of-live"),
				"a child must outlive a parent that can still re-find it")
			assert.Equal(t, 1, result.Skipped, "holding a child back must be reported, not silent")

			assert.False(t, executionExists(t, storage, "child-of-done"),
				"a terminal parent will never re-run, so its child is collectable")
			assert.False(t, executionExists(t, storage, "child-of-ghost"),
				"a child whose parent is gone has nothing left to re-find it")
			assert.True(t, executionExists(t, storage, "live-parent"),
				"the in-flight parent itself is never touched at any age")
		})
	}
}

// TestRunRetention_ZeroRetentionDeletesNothing is the escape hatch: an operator
// who wants saga history frozen for an investigation sets retention to zero.
func TestRunRetention_ZeroRetentionDeletesNothing(t *testing.T) {
	for _, backend := range allStorageBackends() {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			storage := backend.make(t)

			saveAged(t, storage, "ancient", StatusCompleted, 90*24*time.Hour)

			result, err := RunRetention(ctx, storage, RetentionConfig{Retention: 0}, testutils.TestLogger(t))
			require.NoError(t, err)

			assert.Zero(t, result.Deleted)
			assert.True(t, executionExists(t, storage, "ancient"),
				"retention zero must not delete anything")
		})
	}
}

// TestListTerminal_LegacyExecutionsUseStoreTimestamp covers the upgrade path.
// Every saga written before saga timestamps were persisted reads back with a
// zero updated_at. Treating that as infinitely old would delete a saga that a
// runner on the old binary finished seconds ago, so the entity-backed stores
// fall back to the store's own timestamp.
//
// This is deliberately not part of the conformance suite: MemoryStorage has no
// second timestamp to fall back to, and the whole point is that the entity
// backends do.
func TestListTerminal_LegacyExecutionsUseStoreTimestamp(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	for _, tc := range []struct {
		name    string
		storage Storage
	}{
		{"EntityStorage", NewEntityStorage(inmem.Store, testutils.TestLogger(t))},
		{"EACStorage", NewEACStorage(inmem.EAC, testutils.TestLogger(t))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A saga saved with no timestamps at all, exactly as the old code
			// wrote it.
			id := "legacy-just-finished-" + tc.name
			require.NoError(t, tc.storage.Save(ctx, &Execution{
				ID:              id,
				DefinitionName:  "create-sandbox",
				Status:          StatusCompleted,
				InitialInputs:   map[string]any{},
				ExecutedActions: map[string]*ActionResult{},
				ExecutionOrder:  []string{},
			}))

			terminal, err := tc.storage.ListTerminal(ctx)
			require.NoError(t, err)

			var found *TerminalExecution
			for i := range terminal {
				if terminal[i].ID == id {
					found = &terminal[i]
					break
				}
			}
			require.NotNil(t, found, "the legacy execution must be listed, not dropped for lacking a timestamp")
			assert.False(t, found.FinishedAt.IsZero(),
				"the store timestamp should have supplied an age")

			result, err := RunRetention(ctx, tc.storage, weekRetention(), testutils.TestLogger(t))
			require.NoError(t, err)
			assert.Zero(t, result.Deleted,
				"a legacy saga the store stamped just now must not be treated as infinitely old")
			assert.True(t, executionExists(t, tc.storage, id))
		})
	}
}

// deleteFailingStorage fails Delete for one specific execution.
type deleteFailingStorage struct {
	Storage
	failID string
}

func (d *deleteFailingStorage) Delete(ctx context.Context, id string) error {
	if id == d.failID {
		return errors.New("simulated delete failure")
	}
	return d.Storage.Delete(ctx, id)
}

// TestRunRetention_DeleteFailureDoesNotAbortSweep pins the best-effort
// behavior: one execution the store will not part with is counted and stepped
// over, not allowed to strand every expired execution behind it. Without this,
// a single permanently-undeletable saga would wedge retention for the cluster.
func TestRunRetention_DeleteFailureDoesNotAbortSweep(t *testing.T) {
	ctx := context.Background()
	storage := &deleteFailingStorage{Storage: NewMemoryStorage(), failID: "stubborn"}

	for _, id := range []string{"first", "stubborn", "last"} {
		saveAged(t, storage, id, StatusCompleted, 30*24*time.Hour)
	}

	result, err := RunRetention(ctx, storage, weekRetention(), testutils.TestLogger(t))
	require.NoError(t, err, "a delete failure must not fail the sweep")

	assert.Equal(t, 1, result.Failed)
	assert.Equal(t, 2, result.Deleted, "the other expired executions must still be collected")
	assert.True(t, executionExists(t, storage, "stubborn"))
	assert.False(t, executionExists(t, storage, "first"))
	assert.False(t, executionExists(t, storage, "last"))
}

// TestDelete_MissingExecutionIsNotAnError keeps overlapping or retried sweeps
// converging instead of failing on work someone else already did.
func TestDelete_MissingExecutionIsNotAnError(t *testing.T) {
	for _, backend := range allStorageBackends() {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			storage := backend.make(t)

			assert.NoError(t, storage.Delete(ctx, "never-existed"))

			saveAged(t, storage, "delete-twice", StatusCompleted, time.Hour)
			require.NoError(t, storage.Delete(ctx, "delete-twice"))
			assert.NoError(t, storage.Delete(ctx, "delete-twice"),
				"deleting an already-deleted execution must be a no-op")
		})
	}
}
