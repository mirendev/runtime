package saga

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type PickFactorIn struct {
	A int `saga:"a"`
}

type PickFactorOut struct {
	Factor int `saga:"factor"`
}

func PickFactor(ctx context.Context, in PickFactorIn) (PickFactorOut, error) {
	return PickFactorOut{Factor: in.A * 10}, nil
}

func UndoPickFactor(ctx context.Context, in PickFactorIn, out PickFactorOut) error {
	return nil
}

type DoubleIn struct {
	Result int `saga:"result"`
}

type DoubleOut struct {
	Doubled int `saga:"doubled"`
}

func Double(ctx context.Context, in DoubleIn) (DoubleOut, error) {
	return DoubleOut{Doubled: in.Result * 2}, nil
}

func UndoDouble(ctx context.Context, in DoubleIn, out DoubleOut) error {
	return nil
}

func TestBuilder_VersionRequiresResumesFrom(t *testing.T) {
	base := func() *Builder {
		return Define("versioned").Action("add", AddNumbers).Undo(UndoAddNumbers)
	}

	_, err := base().Build()
	require.NoError(t, err, "v1 needs no compatibility decision")

	_, err = base().Version(2).Build()
	require.ErrorContains(t, err, "must declare which older versions it can resume")

	def, err := base().Version(2).ResumesFrom().Build()
	require.NoError(t, err)
	assert.Empty(t, def.ResumesFrom)
	assert.False(t, def.CanResume(1))
	assert.True(t, def.CanResume(2))

	def, err = base().Version(3).ResumesFrom(2, 1, 2).Build()
	require.NoError(t, err)
	assert.Equal(t, []int{1, 2}, def.ResumesFrom)

	_, err = base().Version(2).ResumesFrom(2).Build()
	require.ErrorContains(t, err, "must name a version below")

	_, err = base().Version(0).Build()
	require.ErrorContains(t, err, "version must be at least 1")
}

// TestRecovery_DefinitionChanges walks the upgrade cases that matter: an
// execution recorded by a v1 binary is recovered by a binary whose definition
// has changed. Each case either resumes because the change was declared
// compatible and nothing it ran went missing, or is refused with the record
// untouched and no action run or undone.
func TestRecovery_DefinitionChanges(t *testing.T) {
	type change struct {
		name   string
		define func(ctrl *testController) *Builder
	}

	changes := []change{
		{
			name: "action added",
			define: func(ctrl *testController) *Builder {
				return Define("upgraded").Using(ctrl).
					Action("add", AddNumbers).Undo(UndoAddNumbers).
					Action("multiply", Multiply).Undo(UndoMultiply).
					Action("double", Double).Undo(UndoDouble)
			},
		},
		{
			name: "dependency changed",
			define: func(ctrl *testController) *Builder {
				return Define("upgraded").Using(ctrl).
					Action("add", AddNumbers).Undo(UndoAddNumbers).
					Action("pick-factor", PickFactor).Undo(UndoPickFactor).
					Action("multiply", Multiply).Undo(UndoMultiply)
			},
		},
		{
			name: "completed action removed",
			define: func(ctrl *testController) *Builder {
				return Define("upgraded").Using(ctrl).
					Action("multiply", Multiply).Undo(UndoMultiply)
			},
		},
		{
			name: "completed action renamed",
			define: func(ctrl *testController) *Builder {
				return Define("upgraded").Using(ctrl).
					Action("sum", AddNumbers).Undo(UndoAddNumbers).
					Action("multiply", Multiply).Undo(UndoMultiply)
			},
		},
	}

	// Removing or renaming an action that already ran leaves nothing that
	// could undo it, which no compatibility declaration can paper over.
	strandsWork := map[string]bool{
		"completed action removed": true,
		"completed action renamed": true,
	}

	recorded := func(status Status) *Execution {
		at := time.Now().Add(-time.Hour).Truncate(time.Second)
		return &Execution{
			ID:                "upgraded-exec",
			DefinitionName:    "upgraded",
			DefinitionVersion: 1,
			InitialInputs:     map[string]any{"a": float64(2), "b": float64(3), "factor": float64(4)},
			Status:            status,
			ExecutedActions: map[string]*ActionResult{
				"add": {Output: []byte(`{"Sum":5}`), ExecutedAt: at},
			},
			ExecutionOrder: []string{"add"},
			CreatedAt:      at,
			UpdatedAt:      at,
		}
	}

	for _, c := range changes {
		for _, status := range []Status{StatusRunning, StatusUndoing} {
			for _, declared := range []bool{false, true} {
				name := c.name + "/" + string(status)
				if declared {
					name += "/resumes-from-1"
				} else {
					name += "/no-resume"
				}

				t.Run(name, func(t *testing.T) {
					ctx := context.Background()
					ctrl := &testController{}
					b := c.define(ctrl).Version(2)
					if declared {
						b = b.ResumesFrom(1)
					} else {
						b = b.ResumesFrom()
					}
					registry := NewRegistry()
					require.NoError(t, b.RegisterTo(registry))

					storage := NewMemoryStorage()
					before := recorded(status)
					require.NoError(t, storage.Save(ctx, before))

					err := NewExecutor(storage, WithRegistry(registry)).Recover(ctx)

					after, getErr := storage.Get(ctx, "upgraded-exec")
					require.NoError(t, getErr)

					if declared && !strandsWork[c.name] {
						if status == StatusRunning {
							require.NoError(t, err)
							assert.Equal(t, StatusCompleted, after.Status)
							assert.Len(t, ctrl.multiplyCalls, 1)
						} else {
							// A finished undo reports the saga as failed;
							// what matters is that it ran rather than refused.
							require.NotErrorIs(t, err, ErrIncompatibleDefinition)
							assert.Equal(t, StatusFailed, after.Status)
							assert.Len(t, ctrl.undoAddCalls, 1)
						}
						return
					}

					require.ErrorIs(t, err, ErrIncompatibleDefinition)
					assert.Empty(t, ctrl.addCalls)
					assert.Empty(t, ctrl.multiplyCalls)
					assert.Empty(t, ctrl.undoAddCalls)
					assert.Empty(t, ctrl.undoMultCalls)
					assert.Equal(t, status, after.Status, "refusal must not change the status")
					assert.True(t, before.UpdatedAt.Equal(after.UpdatedAt), "a refusal is not progress")
					assert.Equal(t, 1, after.DefinitionVersion)
					assert.Contains(t, after.BlockedReason, "no action was run or undone")
				})
			}
		}
	}
}

func TestRecovery_SameVersionMissingActionRefused(t *testing.T) {
	// A definition edited without a version bump is the case a version
	// number cannot catch; the missing undo still can.
	ctx := context.Background()
	ctrl := &testController{}
	registry := NewRegistry()
	require.NoError(t, Define("unbumped").Using(ctrl).
		Action("sum", AddNumbers).Undo(UndoAddNumbers).
		RegisterTo(registry))

	storage := NewMemoryStorage()
	require.NoError(t, storage.Save(ctx, &Execution{
		ID:                "unbumped-exec",
		DefinitionName:    "unbumped",
		DefinitionVersion: 1,
		InitialInputs:     map[string]any{"a": float64(2), "b": float64(3)},
		Status:            StatusUndoing,
		ExecutedActions: map[string]*ActionResult{
			"add": {Output: []byte(`{"Sum":5}`), ExecutedAt: time.Now()},
		},
		ExecutionOrder: []string{"add"},
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}))

	err := NewExecutor(storage, WithRegistry(registry)).Recover(ctx)

	var incompatible *IncompatibleDefinitionError
	require.ErrorAs(t, err, &incompatible)
	assert.Equal(t, []string{"add"}, incompatible.MissingActions)
	assert.Empty(t, ctrl.undoAddCalls)
}

func TestExecute_NamedReentryAcrossVersions(t *testing.T) {
	ctx := context.Background()
	ctrl := &testController{}
	registry := NewRegistry()
	require.NoError(t, Define("reentry").Using(ctrl).
		Version(2).ResumesFrom().
		Action("add", AddNumbers).Undo(UndoAddNumbers).
		Action("multiply", Multiply).Undo(UndoMultiply).
		RegisterTo(registry))

	t.Run("record with work is refused", func(t *testing.T) {
		storage := NewMemoryStorage()
		require.NoError(t, storage.Save(ctx, &Execution{
			ID:                "with-work",
			DefinitionName:    "reentry",
			DefinitionVersion: 1,
			InitialInputs:     map[string]any{"a": float64(2), "b": float64(3), "factor": float64(4)},
			Status:            StatusRunning,
			ExecutedActions: map[string]*ActionResult{
				"add": {Output: []byte(`{"Sum":5}`), ExecutedAt: time.Now()},
			},
			ExecutionOrder: []string{"add"},
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}))

		err := NewExecutor(storage, WithRegistry(registry)).Start("reentry").
			Input("a", 2).Input("b", 3).Input("factor", 4).
			WithID("with-work").
			Execute(ctx)

		require.ErrorIs(t, err, ErrIncompatibleDefinition)
		assert.Empty(t, ctrl.multiplyCalls)
	})

	t.Run("record without work is re-stamped and run", func(t *testing.T) {
		storage := NewMemoryStorage()
		require.NoError(t, storage.Save(ctx, &Execution{
			ID:                "no-work",
			DefinitionName:    "reentry",
			DefinitionVersion: 1,
			InitialInputs:     map[string]any{"a": float64(2), "b": float64(3), "factor": float64(4)},
			Status:            StatusPending,
			ExecutedActions:   map[string]*ActionResult{},
			ExecutionOrder:    []string{},
			CreatedAt:         time.Now(),
			UpdatedAt:         time.Now(),
		}))

		err := NewExecutor(storage, WithRegistry(registry)).Start("reentry").
			Input("a", 2).Input("b", 3).Input("factor", 4).
			WithID("no-work").
			Execute(ctx)
		require.NoError(t, err)

		after, err := storage.Get(ctx, "no-work")
		require.NoError(t, err)
		assert.Equal(t, StatusCompleted, after.Status)
		assert.Equal(t, 2, after.DefinitionVersion)
	})

	t.Run("terminal records pass through", func(t *testing.T) {
		storage := NewMemoryStorage()
		require.NoError(t, storage.Save(ctx, &Execution{
			ID:                "done",
			DefinitionName:    "reentry",
			DefinitionVersion: 1,
			Status:            StatusCompleted,
			ExecutedActions: map[string]*ActionResult{
				"gone": {Output: []byte(`{}`), ExecutedAt: time.Now()},
			},
			ExecutionOrder: []string{"gone"},
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}))

		err := NewExecutor(storage, WithRegistry(registry)).Start("reentry").
			WithID("done").
			Execute(ctx)
		require.NoError(t, err)
	})
}

// countingStorage counts saves, to tell a repeated refusal that rewrites the
// record from one that recognizes it has already said so.
type countingStorage struct {
	*MemoryStorage
	saves int
}

func (c *countingStorage) Save(ctx context.Context, exec *Execution) error {
	c.saves++
	return c.MemoryStorage.Save(ctx, exec)
}

func TestRecovery_BlockedReasonLifecycle(t *testing.T) {
	ctx := context.Background()
	recorded := &Execution{
		ID:                "lifecycle",
		DefinitionName:    "lifecycle",
		DefinitionVersion: 1,
		InitialInputs:     map[string]any{"a": float64(2), "b": float64(3), "factor": float64(4)},
		Status:            StatusRunning,
		ExecutedActions: map[string]*ActionResult{
			"add": {Output: []byte(`{"Sum":5}`), ExecutedAt: time.Now()},
		},
		ExecutionOrder: []string{"add"},
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	storage := &countingStorage{MemoryStorage: NewMemoryStorage()}
	require.NoError(t, storage.Save(ctx, recorded))

	refusing := NewRegistry()
	require.NoError(t, Define("lifecycle").Using(&testController{}).
		Version(2).ResumesFrom().
		Action("add", AddNumbers).Undo(UndoAddNumbers).
		Action("multiply", Multiply).Undo(UndoMultiply).
		RegisterTo(refusing))

	before := storage.saves
	require.ErrorIs(t, NewExecutor(storage, WithRegistry(refusing)).Recover(ctx), ErrIncompatibleDefinition)
	assert.Equal(t, before+1, storage.saves, "the first refusal records its reason")

	before = storage.saves
	require.ErrorIs(t, NewExecutor(storage, WithRegistry(refusing)).Recover(ctx), ErrIncompatibleDefinition)
	assert.Equal(t, before, storage.saves, "a repeated refusal must not rewrite the record")

	// The release that can resume it (here, one declaring it compatible)
	// clears the block as it drives the execution forward.
	ctrl := &testController{}
	resuming := NewRegistry()
	require.NoError(t, Define("lifecycle").Using(ctrl).
		Version(2).ResumesFrom(1).
		Action("add", AddNumbers).Undo(UndoAddNumbers).
		Action("multiply", Multiply).Undo(UndoMultiply).
		RegisterTo(resuming))
	require.NoError(t, NewExecutor(storage, WithRegistry(resuming)).Recover(ctx))

	after, err := storage.Get(ctx, "lifecycle")
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, after.Status)
	assert.Empty(t, after.BlockedReason)
	assert.Len(t, ctrl.multiplyCalls, 1)
}

type PrepIn struct {
	Seed int `saga:"seed"`
}

type PrepOut struct {
	Value int `saga:"value"`
}

type prepCounter struct{ undos int }

func Prep(ctx context.Context, in PrepIn) (PrepOut, error) {
	return PrepOut{Value: in.Seed}, nil
}

func UndoPrep(ctx context.Context, in PrepIn, out PrepOut) error {
	Get[*prepCounter](ctx).undos++
	return nil
}

// TestUndoNested_RefusalBlocksParent covers a parent unwinding through a
// child its binary can no longer vouch for. The parent must stop at the child
// rather than compensate what came before it, which the child's leftover work
// may stand on, and must be blocked itself so it is visible and out of the
// stalled sweep's reach. A release that can resume the child then finishes
// both and clears both blocks.
func TestUndoNested_RefusalBlocksParent(t *testing.T) {
	ctx := context.Background()
	ctrl := &nestedTestController{}
	prep := &prepCounter{}
	storage := NewMemoryStorage()

	registryWithChild := func(child *Builder) *Registry {
		registry := NewRegistry()
		require.NoError(t, child.Using(ctrl).
			Action(ChildStep).Undo(UndoChildStep).
			RegisterTo(registry))
		require.NoError(t, Define("parent-saga").Using(ctrl).Using(prep).
			Action("prep", Prep).Undo(UndoPrep).
			Action(ParentStep).Undo(UndoParentStep).
			Action(ParentFinal).Undo(UndoParentFinal).
			RegisterTo(registry))
		return registry
	}

	v1 := registryWithChild(Define("child-saga"))
	require.NoError(t, NewExecutor(storage, WithRegistry(v1)).Start("parent-saga").
		Input("seed", 5).WithID("parent-1").Execute(ctx))

	// Something later sends the parent into undo, and it crashes before
	// finishing, so recovery picks it up under the upgraded binary.
	parent, err := storage.Get(ctx, "parent-1")
	require.NoError(t, err)
	childID := parent.ExecutedActions["parent-step"]
	require.NotNil(t, childID)
	parent.Status = StatusUndoing
	parent.Error = "forced into undo"
	require.NoError(t, storage.Save(ctx, parent))

	refusing := registryWithChild(Define("child-saga").Version(2).ResumesFrom())
	err = NewExecutor(storage, WithRegistry(refusing)).Recover(ctx)
	require.ErrorIs(t, err, ErrIncompatibleDefinition)

	assert.Zero(t, prep.undos, "actions before the refused child must not be compensated")
	assert.Zero(t, ctrl.childUndoCalls)

	parent, err = storage.Get(ctx, "parent-1")
	require.NoError(t, err)
	assert.Equal(t, StatusUndoing, parent.Status)
	assert.Contains(t, parent.BlockedReason, "nested saga")

	// The refused child is Completed, which must not read as finished: a
	// retry while it is still refused runs nothing and writes nothing.
	counting := &countingStorage{MemoryStorage: storage}
	err = NewExecutor(counting, WithRegistry(refusing)).Recover(ctx)
	require.ErrorIs(t, err, ErrIncompatibleDefinition)
	assert.Zero(t, counting.saves, "a retry that changes nothing writes nothing")

	resuming := registryWithChild(Define("child-saga").Version(2).ResumesFrom(1))
	err = NewExecutor(storage, WithRegistry(resuming)).Recover(ctx)
	require.NotErrorIs(t, err, ErrIncompatibleDefinition)

	assert.Equal(t, 1, prep.undos)
	assert.Equal(t, 1, ctrl.childUndoCalls)

	parent, err = storage.Get(ctx, "parent-1")
	require.NoError(t, err)
	assert.Equal(t, StatusFailed, parent.Status)
	assert.Empty(t, parent.BlockedReason)

	children, err := storage.ListIncompletePage(ctx, IncompleteQuery{})
	require.NoError(t, err)
	assert.Empty(t, children.Executions)
	for _, exec := range storage.executions {
		assert.Empty(t, exec.BlockedReason, "execution %s kept its block", exec.ID)
	}
}

// TestResume_RereadsAStaleBlockedCopy covers recovery holding a copy from
// before an operator abandoned the execution. Driving it would overwrite the
// abandonment and run actions the operator was told would not run.
func TestResume_RereadsAStaleBlockedCopy(t *testing.T) {
	ctx := context.Background()
	ctrl := &testController{}
	registry := NewRegistry()
	require.NoError(t, Define("stale").Using(ctrl).
		Version(2).ResumesFrom(1).
		Action("add", AddNumbers).Undo(UndoAddNumbers).
		Action("multiply", Multiply).Undo(UndoMultiply).
		RegisterTo(registry))
	def, ok := registry.Get("stale")
	require.True(t, ok)

	stale := &Execution{
		ID:                "stale-exec",
		DefinitionName:    "stale",
		DefinitionVersion: 1,
		InitialInputs:     map[string]any{"a": float64(2), "b": float64(3), "factor": float64(4)},
		Status:            StatusRunning,
		ExecutedActions: map[string]*ActionResult{
			"add": {Output: []byte(`{"Sum":5}`), ExecutedAt: time.Now()},
		},
		ExecutionOrder: []string{"add"},
		BlockedReason:  "refusing to resume: recorded at v1",
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	storage := NewMemoryStorage()
	abandoned := cloneExecution(stale)
	_, err := Abandon(abandoned, time.Now())
	require.NoError(t, err)
	require.NoError(t, storage.Save(ctx, abandoned))

	err = NewExecutor(storage, WithRegistry(registry)).resume(ctx, def, stale)
	require.ErrorContains(t, err, "saga failed")

	assert.Empty(t, ctrl.multiplyCalls)
	assert.Empty(t, ctrl.undoAddCalls)
	got, err := storage.Get(ctx, "stale-exec")
	require.NoError(t, err)
	assert.Equal(t, StatusFailed, got.Status, "the abandonment must stand")
}

// nestedUpgradeRegistry registers parent-saga (prep, then a step that runs
// child-saga, then a final step) beside the given child definition.
func nestedUpgradeRegistry(t *testing.T, ctrl *nestedTestController, prep *prepCounter, child *Builder) *Registry {
	t.Helper()
	registry := NewRegistry()
	require.NoError(t, child.Using(ctrl).
		Action(ChildStep).Undo(UndoChildStep).
		RegisterTo(registry))
	require.NoError(t, Define("parent-saga").Using(ctrl).Using(prep).
		Action("prep", Prep).Undo(UndoPrep).
		Action(ParentStep).Undo(UndoParentStep).
		Action(ParentFinal).Undo(UndoParentFinal).
		RegisterTo(registry))
	return registry
}

// crashedInParentStep records a parent that ran prep and crashed while its
// child, recorded at v1, was partway through.
func crashedInParentStep(t *testing.T, storage Storage, childStatus Status, childActions map[string]*ActionResult) string {
	t.Helper()
	ctx := context.Background()
	childID := deriveChildID("parent-1", "child-saga", "parent-step")
	order := []string{}
	for name := range childActions {
		order = append(order, name)
	}
	require.NoError(t, storage.Save(ctx, &Execution{
		ID:                childID,
		DefinitionName:    "child-saga",
		DefinitionVersion: 1,
		InitialInputs:     map[string]any{"value": float64(5)},
		ParentExecutionID: "parent-1",
		Status:            childStatus,
		ExecutedActions:   childActions,
		ExecutionOrder:    order,
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}))
	require.NoError(t, storage.Save(ctx, &Execution{
		ID:                "parent-1",
		DefinitionName:    "parent-saga",
		DefinitionVersion: 1,
		InitialInputs:     map[string]any{"seed": float64(5)},
		Status:            StatusRunning,
		ExecutedActions: map[string]*ActionResult{
			"prep": {Output: []byte(`{"Value":5}`), ExecutedAt: time.Now()},
		},
		ExecutionOrder: []string{"prep"},
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}))
	return childID
}

// TestRunNested_RefusalBlocksParent is the forward-path twin of
// TestUndoNested_RefusalBlocksParent. Handled as an ordinary action failure,
// a refused child would send the parent through compensation that skips the
// child entirely, ending Failed with the child's work still in place.
func TestRunNested_RefusalBlocksParent(t *testing.T) {
	ctx := context.Background()
	ctrl := &nestedTestController{}
	prep := &prepCounter{}
	storage := NewMemoryStorage()
	childID := crashedInParentStep(t, storage, StatusRunning, map[string]*ActionResult{
		"child-step": {Output: []byte(`{"Doubled":10}`), ExecutedAt: time.Now()},
	})

	refusing := nestedUpgradeRegistry(t, ctrl, prep, Define("child-saga").Version(2).ResumesFrom())
	err := NewExecutor(storage, WithRegistry(refusing)).Recover(ctx)
	require.ErrorIs(t, err, ErrIncompatibleDefinition)

	assert.Zero(t, prep.undos, "the parent must not compensate around a refused child")
	assert.Zero(t, ctrl.childCalls)

	parent, err := storage.Get(ctx, "parent-1")
	require.NoError(t, err)
	assert.Equal(t, StatusRunning, parent.Status)
	assert.Empty(t, parent.Error, "a refusal is not an action failure")
	assert.Contains(t, parent.BlockedReason, "nested saga")
	assert.Equal(t, childID, parent.BlockedOn)
	child, err := storage.Get(ctx, childID)
	require.NoError(t, err)
	assert.NotEmpty(t, child.BlockedReason)
	assert.Equal(t, 1, ctrl.parentCalls)

	// Retrying while the child is still refused must not re-run the action
	// that started it: in the shared-server sagas that action deletes what
	// the child half built once it looks stale. Nor should it rewrite the
	// parent, whose stored block is already the right one.
	counting := &countingStorage{MemoryStorage: storage}
	err = NewExecutor(counting, WithRegistry(refusing)).Recover(ctx)
	assert.Zero(t, counting.saves, "a retry that changes nothing writes nothing")
	require.ErrorIs(t, err, ErrIncompatibleDefinition)
	assert.Equal(t, 1, ctrl.parentCalls, "a parent blocked on a refused child runs nothing")
	parent, err = storage.Get(ctx, "parent-1")
	require.NoError(t, err)
	assert.Contains(t, parent.BlockedReason, "nested saga")

	resuming := nestedUpgradeRegistry(t, ctrl, prep, Define("child-saga").Version(2).ResumesFrom(1))
	require.NoError(t, NewExecutor(storage, WithRegistry(resuming)).Recover(ctx))

	parent, err = storage.Get(ctx, "parent-1")
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, parent.Status)
	assert.Empty(t, parent.BlockedReason)
	assert.Empty(t, parent.BlockedOn)
	child, err = storage.Get(ctx, childID)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, child.Status)
	assert.Empty(t, child.BlockedReason, "a nested resume clears the child's block too")
}

// TestRunNested_TerminalChildIsReportedNotRedriven pins that re-entering a
// parent action reports a terminal child as it stands, as resume does for a
// top-level execution, even across a definition change.
func TestRunNested_TerminalChildIsReportedNotRedriven(t *testing.T) {
	ctx := context.Background()

	t.Run("completed child returns its outputs and runs nothing", func(t *testing.T) {
		ctrl := &nestedTestController{}
		prep := &prepCounter{}
		storage := NewMemoryStorage()
		crashedInParentStep(t, storage, StatusCompleted, map[string]*ActionResult{
			"child-step": {Output: []byte(`{"Doubled":10}`), ExecutedAt: time.Now()},
		})

		registry := nestedUpgradeRegistry(t, ctrl, prep, Define("child-saga").Version(2).ResumesFrom())
		require.NoError(t, NewExecutor(storage, WithRegistry(registry)).Recover(ctx))

		assert.Zero(t, ctrl.childCalls)
		parent, err := storage.Get(ctx, "parent-1")
		require.NoError(t, err)
		assert.Equal(t, StatusCompleted, parent.Status)
	})

	t.Run("abandoned child fails the step and the parent unwinds", func(t *testing.T) {
		ctrl := &nestedTestController{}
		prep := &prepCounter{}
		storage := NewMemoryStorage()
		childID := crashedInParentStep(t, storage, StatusRunning, map[string]*ActionResult{
			"child-step": {Output: []byte(`{"Doubled":10}`), ExecutedAt: time.Now()},
		})
		child, err := storage.Get(ctx, childID)
		require.NoError(t, err)
		child.BlockedReason = "refusing to resume: recorded at v1"
		_, err = Abandon(child, time.Now())
		require.NoError(t, err)
		require.NoError(t, storage.Save(ctx, child))

		registry := nestedUpgradeRegistry(t, ctrl, prep, Define("child-saga").Version(2).ResumesFrom())
		err = NewExecutor(storage, WithRegistry(registry)).Recover(ctx)
		require.NotErrorIs(t, err, ErrIncompatibleDefinition)

		assert.Zero(t, ctrl.childCalls, "abandoned work must not be redone")
		assert.Zero(t, ctrl.childUndoCalls, "abandoned work must not be undone either")
		assert.Equal(t, 1, prep.undos, "the parent compensates its own work")

		parent, err := storage.Get(ctx, "parent-1")
		require.NoError(t, err)
		assert.Equal(t, StatusFailed, parent.Status)
		child, err = storage.Get(ctx, childID)
		require.NoError(t, err)
		assert.Empty(t, child.BlockedReason, "an abandoned child is not re-blocked")
	})
}

// TestRecovery_NothingLeftToCompensateIsAdmitted covers executions that ran
// something but have nothing left a definition change could strand.
func TestRecovery_NothingLeftToCompensateIsAdmitted(t *testing.T) {
	ctx := context.Background()
	undone := time.Now()
	ctrl := &testController{}
	registry := NewRegistry()
	require.NoError(t, Define("drained").Using(ctrl).
		Version(2).ResumesFrom().
		Action("add", AddNumbers).Undo(UndoAddNumbers).
		RegisterTo(registry))

	storage := NewMemoryStorage()
	require.NoError(t, storage.Save(ctx, &Execution{
		ID:                "drained-exec",
		DefinitionName:    "drained",
		DefinitionVersion: 1,
		InitialInputs:     map[string]any{"a": float64(2), "b": float64(3)},
		Status:            StatusUndoing,
		Error:             "something failed",
		ExecutedActions: map[string]*ActionResult{
			"gone":    {Output: []byte(`{}`), ExecutedAt: time.Now(), UndoneAt: &undone},
			"crashed": {ExecutedAt: time.Now(), Error: "boom"},
		},
		ExecutionOrder: []string{"gone", "crashed"},
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}))

	err := NewExecutor(storage, WithRegistry(registry)).Recover(ctx)
	require.NotErrorIs(t, err, ErrIncompatibleDefinition)

	got, err := storage.Get(ctx, "drained-exec")
	require.NoError(t, err)
	assert.Equal(t, StatusFailed, got.Status)
	assert.Equal(t, 2, got.DefinitionVersion)
	assert.Empty(t, got.BlockedReason)
}

// TestRecovery_RunningWithNothingRecordedIsNotExempt is the reproduction from
// review: a parent that crashed inside its first action has no actions
// recorded, but the action's work exists, here as a nested child with a
// completed step. Admitting it under a version that refuses v1 would run v2's
// actions and leave the child's work uncompensated.
func TestRecovery_RunningWithNothingRecordedIsNotExempt(t *testing.T) {
	ctx := context.Background()
	ctrl := &nestedTestController{}
	prep := &prepCounter{}
	storage := NewMemoryStorage()

	childID := deriveChildID("parent-1", "child-saga", "parent-step")
	require.NoError(t, storage.Save(ctx, &Execution{
		ID:                childID,
		DefinitionName:    "child-saga",
		DefinitionVersion: 1,
		InitialInputs:     map[string]any{"value": float64(5)},
		ParentExecutionID: "parent-1",
		Status:            StatusRunning,
		ExecutedActions: map[string]*ActionResult{
			"child-step": {Output: []byte(`{"Doubled":10}`), ExecutedAt: time.Now()},
		},
		ExecutionOrder: []string{"child-step"},
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}))
	require.NoError(t, storage.Save(ctx, &Execution{
		ID:                "parent-1",
		DefinitionName:    "parent-saga",
		DefinitionVersion: 1,
		InitialInputs:     map[string]any{"seed": float64(5)},
		Status:            StatusRunning,
		ExecutedActions:   map[string]*ActionResult{},
		ExecutionOrder:    []string{},
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}))

	// v2 of the parent drops the nested step altogether.
	registry := NewRegistry()
	require.NoError(t, Define("child-saga").Using(ctrl).
		Action(ChildStep).Undo(UndoChildStep).
		RegisterTo(registry))
	require.NoError(t, Define("parent-saga").Using(ctrl).Using(prep).
		Version(2).ResumesFrom().
		Action("prep", Prep).Undo(UndoPrep).
		RegisterTo(registry))

	err := NewExecutor(storage, WithRegistry(registry)).Recover(ctx)
	require.ErrorIs(t, err, ErrIncompatibleDefinition)

	parent, err := storage.Get(ctx, "parent-1")
	require.NoError(t, err)
	assert.Equal(t, StatusRunning, parent.Status)
	assert.Equal(t, 1, parent.DefinitionVersion, "not re-stamped")
	assert.Empty(t, parent.ExecutedActions, "v2's actions must not run")
	child, err := storage.Get(ctx, childID)
	require.NoError(t, err)
	assert.Equal(t, StatusRunning, child.Status)
}

// TestUndoNested_AbandonedCompletedChildLetsParentUnwind is the other
// reproduction from review: a completed child whose undo was refused is the
// thing to abandon, and doing so has to let its parent finish compensating.
func TestUndoNested_AbandonedCompletedChildLetsParentUnwind(t *testing.T) {
	ctx := context.Background()
	ctrl := &nestedTestController{}
	prep := &prepCounter{}
	storage := NewMemoryStorage()

	v1 := nestedUpgradeRegistry(t, ctrl, prep, Define("child-saga"))
	require.NoError(t, NewExecutor(storage, WithRegistry(v1)).Start("parent-saga").
		Input("seed", 5).WithID("parent-1").Execute(ctx))

	parent, err := storage.Get(ctx, "parent-1")
	require.NoError(t, err)
	parent.Status = StatusUndoing
	parent.Error = "forced into undo"
	require.NoError(t, storage.Save(ctx, parent))

	refusing := nestedUpgradeRegistry(t, ctrl, prep, Define("child-saga").Version(2).ResumesFrom())
	require.ErrorIs(t, NewExecutor(storage, WithRegistry(refusing)).Recover(ctx), ErrIncompatibleDefinition)

	parent, err = storage.Get(ctx, "parent-1")
	require.NoError(t, err)
	child, err := storage.Get(ctx, parent.BlockedOn)
	require.NoError(t, err)
	require.Equal(t, StatusCompleted, child.Status)
	require.NotEmpty(t, child.BlockedReason)

	left, err := Abandon(child, time.Now())
	require.NoError(t, err)
	assert.Equal(t, []string{"child-step"}, left)
	require.NoError(t, storage.Save(ctx, child))

	err = NewExecutor(storage, WithRegistry(refusing)).Recover(ctx)
	require.NotErrorIs(t, err, ErrIncompatibleDefinition)

	assert.Zero(t, ctrl.childUndoCalls, "abandoned work is not undone")
	assert.Equal(t, 1, prep.undos, "the parent carries on compensating its own work")
	parent, err = storage.Get(ctx, "parent-1")
	require.NoError(t, err)
	assert.Equal(t, StatusFailed, parent.Status)
	assert.Empty(t, parent.BlockedReason)
	assert.Empty(t, parent.BlockedOn)
}
