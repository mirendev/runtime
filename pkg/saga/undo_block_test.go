package saga

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/version"
)

// TestBuildIdentity: patch releases from one release branch share a version
// string, and a fixed one still has to count as a different build.
func TestBuildIdentity(t *testing.T) {
	assert.NotEqual(t,
		buildIdentity(version.Info{Version: "v0.14", Commit: "1111111aaaa"}),
		buildIdentity(version.Info{Version: "v0.14", Commit: "2222222bbbb"}))
	assert.Equal(t, "main:1111111@1111111aaaa", buildIdentity(version.Info{Version: "main:1111111", Commit: "1111111aaaa"}))
	assert.Equal(t, "dev", buildIdentity(version.Info{Version: "dev", Commit: "unknown"}))
}

type stuckUndoController struct {
	fail  bool
	calls int
}

type stuckIn struct{}

type stuckOut struct {
	Made bool `json:"made" saga:"made"`
}

func stuckStep(ctx context.Context, _ stuckIn) (stuckOut, error) {
	return stuckOut{Made: true}, nil
}

func undoStuckStep(ctx context.Context, _ stuckIn, _ stuckOut) error {
	ctrl := Get[*stuckUndoController](ctx)
	ctrl.calls++
	if ctrl.fail {
		return errors.New("cleanup target is gone")
	}
	return nil
}

func stuckRegistry(t *testing.T, ctrl *stuckUndoController) *Registry {
	t.Helper()
	registry := NewRegistry()
	require.NoError(t, Define("stuck").Using(ctrl).
		Action("step", stuckStep).Undo(undoStuckStep).
		RegisterTo(registry))
	return registry
}

// saveUndoing seeds an execution compensating one action whose undo has
// already failed attempts times, the first of them failingFor ago.
func saveUndoing(t *testing.T, storage Storage, id string, attempts int, failingFor time.Duration) {
	t.Helper()
	now := time.Now()
	since := now.Add(-failingFor)
	result := &ActionResult{
		Output:     []byte(`{"made":true}`),
		ExecutedAt: since.Add(-time.Minute),
	}
	if attempts > 0 {
		result.UndoAttempts = attempts
		result.UndoError = "cleanup target is gone"
		result.UndoFailingSince = &since
	}
	require.NoError(t, storage.Save(context.Background(), &Execution{
		ID:                id,
		DefinitionName:    "stuck",
		DefinitionVersion: 1,
		Status:            StatusUndoing,
		Error:             "a later action failed",
		ExecutedActions:   map[string]*ActionResult{"step": result},
		ExecutionOrder:    []string{"step"},
		CreatedAt:         since,
		UpdatedAt:         now,
	}))
}

// TestUndoBlock_BlocksOnTheBuildThatGaveUp is MIR-2007's safety net. An undo
// that keeps failing stops being retried by the build that gave up on it, and
// the execution says so instead of looking like compensation in progress.
func TestUndoBlock_BlocksOnTheBuildThatGaveUp(t *testing.T) {
	ctx := context.Background()
	ctrl := &stuckUndoController{fail: true}
	registry := stuckRegistry(t, ctrl)
	storage := NewMemoryStorage()
	saveUndoing(t, storage, "stuck-1", undoBlockAttempts-1, 2*undoBlockAfter)

	err := NewExecutor(storage, WithRegistry(registry), WithBuild("main:aaa")).Recover(ctx)
	require.ErrorIs(t, err, ErrUndoBlocked)
	assert.Equal(t, 1, ctrl.calls)

	exec, err := storage.Get(ctx, "stuck-1")
	require.NoError(t, err)
	assert.Equal(t, StatusUndoing, exec.Status, "blocking is not compensating; the work is still there")
	assert.Equal(t, "main:aaa", exec.ExecutedActions["step"].UndoBlockedBuild)
	assert.Equal(t, undoBlockAttempts, exec.ExecutedActions["step"].UndoAttempts)
	assert.Contains(t, exec.BlockedReason, `undo of "step"`)
	assert.Contains(t, exec.BlockedReason, "cleanup target is gone")
	assert.Contains(t, exec.BlockedReason, "main:aaa")

	t.Run("the same build runs and writes nothing", func(t *testing.T) {
		counting := &countingStorage{MemoryStorage: storage}
		err := NewExecutor(counting, WithRegistry(registry), WithBuild("main:aaa")).Recover(ctx)
		require.ErrorIs(t, err, ErrUndoBlocked)
		assert.Equal(t, 1, ctrl.calls, "the undo must not be attempted again")
		assert.Zero(t, counting.saves)

		exec, err := storage.Get(ctx, "stuck-1")
		require.NoError(t, err)
		assert.NotEmpty(t, exec.BlockedReason)
	})

	t.Run("the stalled sweep leaves it alone", func(t *testing.T) {
		result, err := RunStalledSweep(ctx, storage,
			StalledConfig{StaleAfter: time.Nanosecond, MaxForces: 10}, testutils.TestLogger(t))
		require.NoError(t, err)
		assert.Equal(t, 1, result.Blocked)
	})

	t.Run("abandon accepts it and names the work left behind", func(t *testing.T) {
		exec, err := storage.Get(ctx, "stuck-1")
		require.NoError(t, err)
		left, err := Abandon(exec, time.Now())
		require.NoError(t, err)
		assert.Equal(t, []string{"step"}, left)
	})
}

// TestUndoBlock_NewBuildTriesOnceMore covers the hands-off half: a deploy is
// how a permanent undo failure usually gets fixed, so a different build tries
// once, and either finishes the rollback or blocks again under its own name.
func TestUndoBlock_NewBuildTriesOnceMore(t *testing.T) {
	ctx := context.Background()

	block := func(t *testing.T, storage Storage) {
		t.Helper()
		ctrl := &stuckUndoController{fail: true}
		saveUndoing(t, storage, "stuck-1", undoBlockAttempts-1, 2*undoBlockAfter)
		err := NewExecutor(storage, WithRegistry(stuckRegistry(t, ctrl)), WithBuild("main:aaa")).Recover(ctx)
		require.ErrorIs(t, err, ErrUndoBlocked)
	}

	t.Run("and the fix lets it finish", func(t *testing.T) {
		storage := NewMemoryStorage()
		block(t, storage)

		fixed := &stuckUndoController{}
		_ = NewExecutor(storage, WithRegistry(stuckRegistry(t, fixed)), WithBuild("main:bbb")).Recover(ctx)
		assert.Equal(t, 1, fixed.calls)

		exec, err := storage.Get(ctx, "stuck-1")
		require.NoError(t, err)
		assert.Equal(t, StatusFailed, exec.Status, "the rollback finished")
		assert.Empty(t, exec.BlockedReason)
		assert.NotNil(t, exec.ExecutedActions["step"].UndoneAt)
		assert.Empty(t, exec.ExecutedActions["step"].UndoBlockedBuild)
	})

	t.Run("and blocks again when it fails too", func(t *testing.T) {
		storage := NewMemoryStorage()
		block(t, storage)

		still := &stuckUndoController{fail: true}
		registry := stuckRegistry(t, still)
		err := NewExecutor(storage, WithRegistry(registry), WithBuild("main:bbb")).Recover(ctx)
		require.ErrorIs(t, err, ErrUndoBlocked)
		assert.Equal(t, 1, still.calls, "one attempt, not a retry loop")

		exec, err := storage.Get(ctx, "stuck-1")
		require.NoError(t, err)
		assert.Equal(t, "main:bbb", exec.ExecutedActions["step"].UndoBlockedBuild)
		assert.Equal(t, undoBlockAttempts+1, exec.ExecutedActions["step"].UndoAttempts)
		assert.Contains(t, exec.BlockedReason, "main:bbb")

		err = NewExecutor(storage, WithRegistry(registry), WithBuild("main:bbb")).Recover(ctx)
		require.ErrorIs(t, err, ErrUndoBlocked)
		assert.Equal(t, 1, still.calls)
	})
}

// TestUndoBlock_NeedsBothThresholds keeps a short outage from parking work.
// MIR-2007's first pass failed twice inside one second on a coordinator blip.
func TestUndoBlock_NeedsBothThresholds(t *testing.T) {
	cases := []struct {
		name       string
		attempts   int
		failingFor time.Duration
	}{
		{"many attempts in a burst", 10, time.Minute},
		{"few attempts over a long time", undoBlockAttempts - 2, 2 * undoBlockAfter},
		{"the first failure", 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			ctrl := &stuckUndoController{fail: true}
			storage := NewMemoryStorage()
			saveUndoing(t, storage, "stuck-1", tc.attempts, tc.failingFor)

			err := NewExecutor(storage, WithRegistry(stuckRegistry(t, ctrl)), WithBuild("main:aaa")).Recover(ctx)
			require.Error(t, err)
			require.NotErrorIs(t, err, ErrUndoBlocked)

			exec, err := storage.Get(ctx, "stuck-1")
			require.NoError(t, err)
			assert.Empty(t, exec.BlockedReason)
			assert.Empty(t, exec.ExecutedActions["step"].UndoBlockedBuild)
			assert.Equal(t, tc.attempts+1, exec.ExecutedActions["step"].UndoAttempts)
		})
	}
}

// TestUndoBlock_NestedChildBlocksParent: a parent unwinding into a child whose
// undo is blocked stops there, as it does for a refused child, rather than
// counting a failure and compensating the actions the child may stand on.
func TestUndoBlock_NestedChildBlocksParent(t *testing.T) {
	ctx := context.Background()
	ctrl := &nestedTestController{}
	prep := &prepCounter{}
	storage := NewMemoryStorage()

	registry := NewRegistry()
	require.NoError(t, Define("child-saga").Using(ctrl).
		Action(ChildStep).Undo(UndoChildStep).
		RegisterTo(registry))
	require.NoError(t, Define("parent-saga").Using(ctrl).Using(prep).
		Action("prep", Prep).Undo(UndoPrep).
		Action(ParentStep).Undo(UndoParentStep).
		Action(ParentFinal).Undo(UndoParentFinal).
		RegisterTo(registry))

	require.NoError(t, NewExecutor(storage, WithRegistry(registry)).Start("parent-saga").
		Input("seed", 5).WithID("parent-1").Execute(ctx))

	parent, err := storage.Get(ctx, "parent-1")
	require.NoError(t, err)
	var parentStep struct {
		ChildExecID string `json:"ChildExecID"`
	}
	require.NoError(t, json.Unmarshal(parent.ExecutedActions["parent-step"].Output, &parentStep))
	require.NotEmpty(t, parentStep.ChildExecID)

	// The child gave up on its undo under main:aaa in an earlier unwind.
	child, err := storage.Get(ctx, parentStep.ChildExecID)
	require.NoError(t, err)
	since := time.Now().Add(-2 * undoBlockAfter)
	childStep := child.ExecutedActions[child.ExecutionOrder[0]]
	childStep.UndoAttempts = undoBlockAttempts
	childStep.UndoError = "cleanup target is gone"
	childStep.UndoFailingSince = &since
	childStep.UndoBlockedBuild = "main:aaa"
	require.NoError(t, storage.Save(ctx, child))

	parent.Status = StatusUndoing
	parent.Error = "forced into undo"
	require.NoError(t, storage.Save(ctx, parent))

	err = NewExecutor(storage, WithRegistry(registry), WithBuild("main:aaa")).Recover(ctx)
	require.ErrorIs(t, err, ErrUndoBlocked)
	assert.Zero(t, ctrl.childUndoCalls)
	assert.Zero(t, prep.undos, "actions before the blocked child must not be compensated")

	parent, err = storage.Get(ctx, "parent-1")
	require.NoError(t, err)
	assert.Equal(t, StatusUndoing, parent.Status)
	assert.Equal(t, parentStep.ChildExecID, parent.BlockedOn)
	assert.Contains(t, parent.BlockedReason, "undo is blocked")

	t.Run("the same build leaves both where they are", func(t *testing.T) {
		_ = NewExecutor(storage, WithRegistry(registry), WithBuild("main:aaa")).Recover(ctx)
		assert.Zero(t, ctrl.childUndoCalls)
		assert.Zero(t, prep.undos)
	})

	t.Run("a new build unwinds both", func(t *testing.T) {
		_ = NewExecutor(storage, WithRegistry(registry), WithBuild("main:bbb")).Recover(ctx)
		assert.Equal(t, 1, ctrl.childUndoCalls)
		assert.Equal(t, 1, prep.undos)

		parent, err := storage.Get(ctx, "parent-1")
		require.NoError(t, err)
		assert.Equal(t, StatusFailed, parent.Status)
		assert.Empty(t, parent.BlockedReason)
		assert.Empty(t, parent.BlockedOn)
	})
}
