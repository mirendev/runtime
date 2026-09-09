package saga

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

func weekStalled() StalledConfig {
	return StalledConfig{StaleAfter: 7 * 24 * time.Hour, MaxForces: 1000}
}

// stalledStorage is StalledStorage plus the Save a test needs to seed with.
type stalledStorage interface {
	StalledStorage
	executionSaver
}

// stalledBackends returns every storage the sweep can be handed. EACStorage is
// absent by construction rather than omission: it cannot express a conditional
// write, so adding it here would not compile.
func stalledBackends() []struct {
	name string
	make func(t *testing.T) stalledStorage
} {
	return []struct {
		name string
		make func(t *testing.T) stalledStorage
	}{
		{"MemoryStorage", func(t *testing.T) stalledStorage {
			return NewMemoryStorage()
		}},
		{"EntityStorage", func(t *testing.T) stalledStorage {
			inmem, cleanup := testutils.NewInMemEntityServer(t)
			t.Cleanup(cleanup)
			return NewEntityStorage(inmem.Store, testutils.TestLogger(t))
		}},
	}
}

func statusOf(t *testing.T, storage executionGetter, id string) Status {
	t.Helper()

	exec, err := storage.Get(context.Background(), id)
	require.NoError(t, err)
	return exec.Status
}

// TestRunStalledSweep_ForcesStrandedExecutions is the core of MIR-1788: an
// in-flight execution nothing has touched for longer than the window is what
// neither convergence nor retention will ever deal with.
//
// Undoing qualifies too, which is worth naming: retrying undos writes a
// timestamp on every attempt, so an undoing execution old enough to reach here
// is one where the retrying stopped.
func TestRunStalledSweep_ForcesStrandedExecutions(t *testing.T) {
	for _, backend := range stalledBackends() {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			storage := backend.make(t)

			saveAged(t, storage, "stranded-pending", StatusPending, 30*24*time.Hour)
			saveAged(t, storage, "stranded-running", StatusRunning, 8*24*time.Hour)
			saveAged(t, storage, "stranded-undoing", StatusUndoing, 90*24*time.Hour)
			saveAged(t, storage, "working-pending", StatusPending, 1*time.Hour)
			saveAged(t, storage, "working-undoing", StatusUndoing, 6*24*time.Hour)

			result, err := RunStalledSweep(ctx, storage, weekStalled(), testutils.TestLogger(t))
			require.NoError(t, err)

			assert.Equal(t, 3, result.Forced)
			assert.Equal(t, 5, result.Scanned)
			assert.Zero(t, result.Failed)
			assert.Zero(t, result.Recovered)

			for _, id := range []string{"stranded-pending", "stranded-running", "stranded-undoing"} {
				assert.Equal(t, StatusFailed, statusOf(t, storage, id),
					"%s had sat untouched past the window and must be forced", id)
			}
			assert.Equal(t, StatusPending, statusOf(t, storage, "working-pending"),
				"an execution inside the window is still someone's work in progress")
			assert.Equal(t, StatusUndoing, statusOf(t, storage, "working-undoing"),
				"an undoing execution that is still retrying keeps a recent timestamp and must survive")
		})
	}
}

// TestRunStalledSweep_RecordsWhyItForced covers what an operator sees. A forced
// execution is indistinguishable from one that genuinely ran and failed unless
// the record says otherwise, and the week before retention collects it is
// exactly when someone would go looking.
func TestRunStalledSweep_RecordsWhyItForced(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	saveAged(t, storage, "stranded", StatusPending, 30*24*time.Hour)

	_, err := RunStalledSweep(ctx, storage, weekStalled(), testutils.TestLogger(t))
	require.NoError(t, err)

	exec, err := storage.Get(ctx, "stranded")
	require.NoError(t, err)
	assert.Equal(t, StalledError, exec.Error)
	assert.WithinDuration(t, time.Now(), exec.UpdatedAt, time.Minute,
		"the transition is a state change and must be stamped like one, so retention dates it from here")
}

// TestRunStalledSweep_NeverTouchesTerminalExecutions is the boundary with
// retention. The two sweeps must not both have an opinion about the same
// execution: a terminal one is retention's, at any age.
func TestRunStalledSweep_NeverTouchesTerminalExecutions(t *testing.T) {
	for _, backend := range stalledBackends() {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			storage := backend.make(t)

			saveAged(t, storage, "ancient-completed", StatusCompleted, 90*24*time.Hour)
			saveAged(t, storage, "ancient-failed", StatusFailed, 90*24*time.Hour)

			result, err := RunStalledSweep(ctx, storage, weekStalled(), testutils.TestLogger(t))
			require.NoError(t, err)

			assert.Zero(t, result.Forced)
			assert.Zero(t, result.Scanned, "terminal executions must not even be scanned")
			assert.Equal(t, StatusCompleted, statusOf(t, storage, "ancient-completed"),
				"the sweep must never rewrite a recorded success")
			assert.Equal(t, StatusFailed, statusOf(t, storage, "ancient-failed"))
		})
	}
}

// saveStalledChild persists an in-flight child execution belonging to parentID.
func saveStalledChild(t *testing.T, storage executionSaver, id, parentID string, age time.Duration) {
	t.Helper()

	changed := time.Now().Add(-age)
	require.NoError(t, storage.Save(context.Background(), &Execution{
		ID:                id,
		DefinitionName:    "ensure-shared-server",
		ParentExecutionID: parentID,
		Status:            StatusPending,
		InitialInputs:     map[string]any{},
		ExecutedActions:   map[string]*ActionResult{},
		ExecutionOrder:    []string{},
		CreatedAt:         changed,
		UpdatedAt:         changed,
	}))
}

// TestRunStalledSweep_KeepsChildrenOfLiveParents covers the nested-saga hole,
// which is retention's hole pointing the other way. A parent resumes by
// re-finding its children rather than re-running them, so failing a child under
// a live parent fails the parent for a reason that was never true.
func TestRunStalledSweep_KeepsChildrenOfLiveParents(t *testing.T) {
	for _, backend := range stalledBackends() {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			storage := backend.make(t)

			saveAged(t, storage, "live-parent", StatusRunning, 1*time.Hour)
			saveStalledChild(t, storage, "child-of-live", "live-parent", 30*24*time.Hour)

			saveAged(t, storage, "done-parent", StatusCompleted, 30*24*time.Hour)
			saveStalledChild(t, storage, "child-of-done", "done-parent", 30*24*time.Hour)

			saveStalledChild(t, storage, "child-of-ghost", "never-existed", 30*24*time.Hour)

			result, err := RunStalledSweep(ctx, storage, weekStalled(), testutils.TestLogger(t))
			require.NoError(t, err)

			assert.Equal(t, StatusPending, statusOf(t, storage, "child-of-live"),
				"a child must outlive a parent that can still re-find it")
			assert.Equal(t, 1, result.Skipped, "holding a child back must be reported, not silent")

			assert.Equal(t, StatusFailed, statusOf(t, storage, "child-of-done"),
				"a terminal parent will never resume, so its stranded child is nobody's work")
			assert.Equal(t, StatusFailed, statusOf(t, storage, "child-of-ghost"),
				"a child whose parent is gone has nothing left to re-find it")
			assert.Equal(t, StatusRunning, statusOf(t, storage, "live-parent"),
				"the live parent is inside the window and must not be touched")
		})
	}
}

// TestRunStalledSweep_ConvergesOnStrandedTrees is the case a liveness check
// alone would deadlock on. "Live" means not terminal, so a stranded parent
// shields its own children from the sweep that is about to deal with it. The
// property that matters is not how fast a tree goes but that it goes.
//
// The per-sweep counts below depend on walk order: "stranded-child" sorts ahead
// of "stranded-parent", so the child is read while its parent is still pending.
// Reverse the names and both go in one sweep, which is a better outcome and a
// failed assertion. If a rename breaks this, that is why, and the property to
// keep asserting is that the tree is fully forced by the end.
func TestRunStalledSweep_ConvergesOnStrandedTrees(t *testing.T) {
	for _, backend := range stalledBackends() {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			storage := backend.make(t)

			saveAged(t, storage, "stranded-parent", StatusPending, 30*24*time.Hour)
			saveStalledChild(t, storage, "stranded-child", "stranded-parent", 30*24*time.Hour)

			result, err := RunStalledSweep(ctx, storage, weekStalled(), testutils.TestLogger(t))
			require.NoError(t, err)
			assert.Equal(t, 1, result.Forced)
			assert.Equal(t, 1, result.Skipped)
			assert.Equal(t, StatusFailed, statusOf(t, storage, "stranded-parent"))
			assert.Equal(t, StatusPending, statusOf(t, storage, "stranded-child"),
				"the child is shielded while its parent is still non-terminal")

			result, err = RunStalledSweep(ctx, storage, weekStalled(), testutils.TestLogger(t))
			require.NoError(t, err)
			assert.Equal(t, 1, result.Forced)
			assert.Zero(t, result.Skipped)
			assert.Equal(t, StatusFailed, statusOf(t, storage, "stranded-child"),
				"forcing the parent is what releases the child on the next pass")

			result, err = RunStalledSweep(ctx, storage, weekStalled(), testutils.TestLogger(t))
			require.NoError(t, err)
			assert.Zero(t, result.Forced, "and then the tree is done")
		})
	}
}

// TestRunStalledSweep_CapsForces proves a large backlog drains over several
// passes rather than one thundering herd of writes, and that hitting the cap is
// reported rather than reading as a clean sweep.
func TestRunStalledSweep_CapsForces(t *testing.T) {
	for _, backend := range stalledBackends() {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			storage := backend.make(t)

			for _, id := range []string{"a", "b", "c", "d", "e"} {
				saveAged(t, storage, id, StatusPending, 30*24*time.Hour)
			}

			cfg := StalledConfig{StaleAfter: 7 * 24 * time.Hour, MaxForces: 3}

			result, err := RunStalledSweep(ctx, storage, cfg, testutils.TestLogger(t))
			require.NoError(t, err)
			assert.Equal(t, 3, result.Forced)
			assert.True(t, result.Capped, "hitting the cap must be reported, not silently truncated")

			result, err = RunStalledSweep(ctx, storage, cfg, testutils.TestLogger(t))
			require.NoError(t, err)
			assert.Equal(t, 2, result.Forced)
			assert.False(t, result.Capped)

			result, err = RunStalledSweep(ctx, storage, cfg, testutils.TestLogger(t))
			require.NoError(t, err)
			assert.Zero(t, result.Forced, "a converged store must be a no-op sweep")
			assert.Zero(t, result.Scanned, "and there is nothing in flight left to scan")
		})
	}
}

// TestRunStalledSweep_ExactlyMaxForcesIsNotCapped pins the boundary, for the
// same reason retention pins it: reporting a converged sweep as capped tells an
// operator a backlog remains on a store that has none.
func TestRunStalledSweep_ExactlyMaxForcesIsNotCapped(t *testing.T) {
	for _, backend := range stalledBackends() {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			storage := backend.make(t)

			for _, id := range []string{"a", "b", "c"} {
				saveAged(t, storage, id, StatusPending, 30*24*time.Hour)
			}

			result, err := RunStalledSweep(ctx, storage,
				StalledConfig{StaleAfter: 7 * 24 * time.Hour, MaxForces: 3},
				testutils.TestLogger(t))
			require.NoError(t, err)

			assert.Equal(t, 3, result.Forced)
			assert.False(t, result.Capped,
				"consuming the budget on the final execution left nothing uninspected")
		})
	}
}

// TestRunStalledSweep_ZeroStaleAfterForcesNothing is the escape hatch: an
// operator who wants in-flight sagas left exactly as they are during an
// investigation sets the window to zero.
func TestRunStalledSweep_ZeroStaleAfterForcesNothing(t *testing.T) {
	for _, backend := range stalledBackends() {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			storage := backend.make(t)

			saveAged(t, storage, "ancient", StatusPending, 90*24*time.Hour)

			result, err := RunStalledSweep(ctx, storage, StalledConfig{StaleAfter: 0}, testutils.TestLogger(t))
			require.NoError(t, err)

			assert.Zero(t, result.Forced)
			assert.Zero(t, result.Scanned, "a disabled sweep must not read the store at all")
			assert.Equal(t, StatusPending, statusOf(t, storage, "ancient"))
		})
	}
}

// changeAfterPageStorage mutates an execution the moment the sweep has a page
// naming it, standing in for a runner that picked the saga back up between the
// listing and the transition.
type changeAfterPageStorage struct {
	stalledStorage
	targetID string
	change   func(t *testing.T, s stalledStorage, id string)
	t        *testing.T
}

func (c *changeAfterPageStorage) ListIncompleteSummaryPage(ctx context.Context, q IncompleteSummaryQuery) (*IncompleteSummaryPage, error) {
	page, err := c.stalledStorage.ListIncompleteSummaryPage(ctx, q)
	if err != nil {
		return nil, err
	}
	for _, summary := range page.Executions {
		if summary.ID == c.targetID {
			c.change(c.t, c.stalledStorage, c.targetID)
		}
	}
	return page, nil
}

func resumeExecution(status Status) func(*testing.T, stalledStorage, string) {
	return func(t *testing.T, s stalledStorage, id string) {
		t.Helper()

		exec, err := s.Get(context.Background(), id)
		require.NoError(t, err)
		exec.Status = status
		exec.UpdatedAt = time.Now()
		require.NoError(t, s.Save(context.Background(), exec))
	}
}

// TestRunStalledSweep_RefusesCandidatesThatMovedAfterTheirPage is the safety
// property that matters most. A page says an execution sat untouched for a
// month; by the time the sweep reaches it, it may have been resumed, finished
// or deleted. Forcing on the page's word alone would declare dead a saga that
// had just started running again.
func TestRunStalledSweep_RefusesCandidatesThatMovedAfterTheirPage(t *testing.T) {
	for _, backend := range stalledBackends() {
		t.Run(backend.name, func(t *testing.T) {
			for _, tc := range []struct {
				name   string
				status Status
			}{
				{"resumed after its page", StatusRunning},
				{"finished after its page", StatusCompleted},
			} {
				t.Run(tc.name, func(t *testing.T) {
					ctx := context.Background()
					inner := backend.make(t)

					saveAged(t, inner, "moved", StatusPending, 30*24*time.Hour)
					saveAged(t, inner, "genuinely-stranded", StatusPending, 30*24*time.Hour)

					storage := &changeAfterPageStorage{
						stalledStorage: inner,
						targetID:       "moved",
						change:         resumeExecution(tc.status),
						t:              t,
					}

					result, err := RunStalledSweep(ctx, storage, weekStalled(), testutils.TestLogger(t))
					require.NoError(t, err)

					assert.Equal(t, 1, result.Recovered,
						"an execution that moved after its page is counted, not forced")
					assert.Equal(t, tc.status, statusOf(t, inner, "moved"),
						"and its own progress is left exactly as it wrote it")

					assert.Equal(t, 1, result.Forced)
					assert.Equal(t, StatusFailed, statusOf(t, inner, "genuinely-stranded"),
						"one candidate turning out to be alive must not spare the rest")
				})
			}
		})
	}
}

// TestRunStalledSweep_DeletedAfterItsPageIsNotAnError keeps overlapping or
// retried sweeps converging rather than erroring on work another pass already
// did.
func TestRunStalledSweep_DeletedAfterItsPageIsNotAnError(t *testing.T) {
	for _, backend := range stalledBackends() {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			inner := backend.make(t)

			saveAged(t, inner, "vanishing", StatusPending, 30*24*time.Hour)

			storage := &changeAfterPageStorage{
				stalledStorage: inner,
				targetID:       "vanishing",
				change: func(t *testing.T, s stalledStorage, id string) {
					t.Helper()
					require.NoError(t, s.(Storage).Delete(context.Background(), id))
				},
				t: t,
			}

			result, err := RunStalledSweep(ctx, storage, weekStalled(), testutils.TestLogger(t))
			require.NoError(t, err)

			assert.Zero(t, result.Failed)
			assert.Zero(t, result.Forced)
			assert.Equal(t, 1, result.Recovered)
		})
	}
}

// forceFailingStorage fails the transition for one specific execution.
type forceFailingStorage struct {
	stalledStorage
	failID string
}

func (f *forceFailingStorage) ForceFailed(ctx context.Context, id string, cutoff time.Time, reason string) (bool, error) {
	if id == f.failID {
		return false, errors.New("simulated write failure")
	}
	return f.stalledStorage.ForceFailed(ctx, id, cutoff, reason)
}

// TestRunStalledSweep_WriteFailureDoesNotAbortSweep pins the best-effort
// behavior. Without it, a single execution the store will not accept a write
// for would wedge the sweep for the whole cluster.
func TestRunStalledSweep_WriteFailureDoesNotAbortSweep(t *testing.T) {
	ctx := context.Background()
	inner := NewMemoryStorage()
	storage := &forceFailingStorage{stalledStorage: inner, failID: "stubborn"}

	for _, id := range []string{"first", "stubborn", "last"} {
		saveAged(t, inner, id, StatusPending, 30*24*time.Hour)
	}

	result, err := RunStalledSweep(ctx, storage, weekStalled(), testutils.TestLogger(t))
	require.NoError(t, err, "a write failure must not fail the sweep")

	assert.Equal(t, 1, result.Failed)
	assert.Equal(t, 2, result.Forced, "the other stranded executions must still be forced")
	assert.Equal(t, StatusPending, statusOf(t, inner, "stubborn"))
	assert.Equal(t, StatusFailed, statusOf(t, inner, "first"))
	assert.Equal(t, StatusFailed, statusOf(t, inner, "last"))
}

// TestStalledSweep_LegacyExecutionsUseStoreTimestamp is the upgrade path, and
// the reason the sweep reads summaries rather than executions. v0.11.1's saga
// schema had no timestamp fields, so its executions read back with a zero one,
// and an age taken from the execution alone would force the whole population on
// sight. Not in the conformance suite because MemoryStorage has no second
// timestamp to fall back to, which is the point.
func TestStalledSweep_LegacyExecutionsUseStoreTimestamp(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	for _, tc := range []struct {
		name    string
		storage stalledStorage
	}{
		{"EntityStorage", NewEntityStorage(inmem.Store, testutils.TestLogger(t))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A saga saved with no timestamps at all, exactly as v0.11.1 wrote
			// it, and still in flight right now.
			id := "legacy-just-started-" + tc.name
			require.NoError(t, tc.storage.Save(ctx, &Execution{
				ID:              id,
				DefinitionName:  "create-sandbox",
				Status:          StatusPending,
				InitialInputs:   map[string]any{},
				ExecutedActions: map[string]*ActionResult{},
				ExecutionOrder:  []string{},
			}))

			summaries, err := collectIncompleteSummaries(ctx, tc.storage)
			require.NoError(t, err)

			var found *IncompleteSummary
			for i := range summaries {
				if summaries[i].ID == id {
					found = &summaries[i]
					break
				}
			}
			require.NotNil(t, found, "the legacy execution must be summarized, not dropped for lacking a timestamp")
			assert.False(t, found.LastChanged.IsZero(),
				"the store timestamp should have supplied an age")

			result, err := RunStalledSweep(ctx, tc.storage, weekStalled(), testutils.TestLogger(t))
			require.NoError(t, err)
			assert.Zero(t, result.Forced,
				"a legacy saga the store stamped just now must not be treated as infinitely old")
			assert.Equal(t, StatusPending, statusOf(t, tc.storage, id))
		})
	}
}

// mutateOnGetStore writes a new revision of an entity immediately after handing
// it to a reader, so a caller that reads then writes always finds its read
// stale. Wrapping the store rather than the storage is the only way to reach
// the gap inside ForceFailed, between its read and its write.
type mutateOnGetStore struct {
	entity.Store
	targetID entity.Id
	mutate   func(t *testing.T, store entity.Store, ent *entity.Entity)
	t        *testing.T
	fired    bool
}

func (m *mutateOnGetStore) GetEntity(ctx context.Context, id entity.Id) (*entity.Entity, error) {
	ent, err := m.Store.GetEntity(ctx, id)
	if err != nil || id != m.targetID || m.fired {
		return ent, err
	}

	// Once only: the mutation is itself a read-modify-write, and re-entering
	// here would recurse.
	m.fired = true
	m.mutate(m.t, m.Store, ent)
	return ent, nil
}

// TestEntityStorageForceFailed_RefusesAStaleWrite pins the conditional write.
//
// A runner can resume an aged execution after ForceFailed has read it and
// decided but before it writes. An unconditional upsert would replace that
// runner's status and action outputs with a stale copy marked failed, and the
// retry a recorded failure unblocks would re-run actions that already ran.
func TestEntityStorageForceFailed_RefusesAStaleWrite(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	const id = "raced-execution"

	seed := NewEntityStorage(inmem.Store, testutils.TestLogger(t))
	saveAged(t, seed, id, StatusPending, 30*24*time.Hour)

	// The runner's resume: fresh status, fresh outputs, fresh timestamp,
	// landing between our read and our write.
	resumed := &Execution{
		ID:             id,
		DefinitionName: "create-sandbox",
		Status:         StatusRunning,
		InitialInputs:  map[string]any{"app": "demo"},
		ExecutedActions: map[string]*ActionResult{
			"allocate-ip": {Output: []byte(`{"ip":"10.0.0.9"}`), ExecutedAt: time.Now()},
		},
		ExecutionOrder: []string{"allocate-ip"},
		CreatedAt:      time.Now().Add(-30 * 24 * time.Hour),
		UpdatedAt:      time.Now(),
	}

	racing := &mutateOnGetStore{
		Store:    inmem.Store,
		targetID: entity.Id(id),
		t:        t,
		mutate: func(t *testing.T, store entity.Store, _ *entity.Entity) {
			t.Helper()
			require.NoError(t, NewEntityStorage(store, testutils.TestLogger(t)).Save(ctx, resumed))
		},
	}

	storage := NewEntityStorage(racing, testutils.TestLogger(t))

	forced, err := storage.ForceFailed(ctx, id, time.Now().Add(-7*24*time.Hour), StalledError)
	require.NoError(t, err, "losing the race is an ordinary outcome, not an error")
	assert.False(t, forced, "a record that changed under us must refuse the transition")

	after, err := seed.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, StatusRunning, after.Status,
		"the resuming runner's status must survive")
	assert.Empty(t, after.Error, "and it must not be carrying our failure message")
	assert.Contains(t, after.ExecutedActions, "allocate-ip",
		"nor may its recorded action outputs be replaced by our stale copy")
}
