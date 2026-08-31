package saga

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Child saga types ---

type ChildStepIn struct {
	Value int `saga:"value"`
}

type ChildStepOut struct {
	Doubled int `saga:"doubled"`
}

func ChildStep(ctx context.Context, in ChildStepIn) (ChildStepOut, error) {
	ctrl := Get[*nestedTestController](ctx)
	ctrl.childCalls++
	if ctrl.failChild {
		return ChildStepOut{}, errors.New("child step failed")
	}
	return ChildStepOut{Doubled: in.Value * 2}, nil
}

func UndoChildStep(ctx context.Context, in ChildStepIn, out ChildStepOut) error {
	ctrl := Get[*nestedTestController](ctx)
	ctrl.childUndoCalls++
	return nil
}

// --- Parent saga types ---

type ParentStepIn struct {
	Value int `saga:"value"`
}

type ParentStepOut struct {
	ChildExecID string `saga:"childexecid"`
	Result      int    `saga:"result"`
}

type ParentFinalIn struct {
	Result int `saga:"result"`
}

type ParentFinalOut struct {
	Done bool `saga:"done"`
}

type nestedTestController struct {
	childCalls     int
	childUndoCalls int
	parentCalls    int
	failChild      bool
	failParent     bool
}

func ParentStep(ctx context.Context, in ParentStepIn) (ParentStepOut, error) {
	ctrl := Get[*nestedTestController](ctx)
	ctrl.parentCalls++
	if ctrl.failParent {
		return ParentStepOut{}, errors.New("parent step failed")
	}

	result, err := RunNested(ctx, "child-saga",
		WithNestedInput("value", in.Value),
	)
	if err != nil {
		return ParentStepOut{}, err
	}

	var doubled int
	if err := result.Get("doubled", &doubled); err != nil {
		return ParentStepOut{}, err
	}

	return ParentStepOut{
		ChildExecID: result.ExecutionID,
		Result:      doubled,
	}, nil
}

func UndoParentStep(ctx context.Context, in ParentStepIn, out ParentStepOut) error {
	if out.ChildExecID != "" {
		return UndoNested(ctx, out.ChildExecID)
	}
	return nil
}

func ParentFinal(ctx context.Context, in ParentFinalIn) (ParentFinalOut, error) {
	return ParentFinalOut{Done: true}, nil
}

func UndoParentFinal(ctx context.Context, in ParentFinalIn, out ParentFinalOut) error {
	return nil
}

func setupNestedSagas(t *testing.T, ctrl *nestedTestController) (*Registry, Storage) {
	t.Helper()

	registry := NewRegistry()

	err := Define("child-saga").
		Using(ctrl).
		Action(ChildStep).Undo(UndoChildStep).
		RegisterTo(registry)
	require.NoError(t, err)

	err = Define("parent-saga").
		Using(ctrl).
		Action(ParentStep).Undo(UndoParentStep).
		Action(ParentFinal).Undo(UndoParentFinal).
		RegisterTo(registry)
	require.NoError(t, err)

	storage := NewMemoryStorage()
	return registry, storage
}

func TestRunNested_Success(t *testing.T) {
	ctrl := &nestedTestController{}
	registry, storage := setupNestedSagas(t, ctrl)

	executor := NewExecutor(storage, WithRegistry(registry))
	err := executor.Start("parent-saga").
		Input("value", 5).
		WithID("parent-1").
		Execute(context.Background())
	require.NoError(t, err)

	// Both parent and child should have been called
	assert.Equal(t, 1, ctrl.parentCalls)
	assert.Equal(t, 1, ctrl.childCalls)

	// Parent execution should be completed
	exec, err := storage.Get(context.Background(), "parent-1")
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, exec.Status)

	// Verify the result flowed through: 5 * 2 = 10
	result, err := executor.ExecutionOutputs(context.Background(), "parent-1")
	require.NoError(t, err)

	var done bool
	require.NoError(t, result.Get("done", &done))
	assert.True(t, done)
}

func TestRunNested_ChildOutputsAccessible(t *testing.T) {
	ctrl := &nestedTestController{}
	registry, storage := setupNestedSagas(t, ctrl)

	executor := NewExecutor(storage, WithRegistry(registry))
	err := executor.Start("parent-saga").
		Input("value", 7).
		WithID("parent-2").
		Execute(context.Background())
	require.NoError(t, err)

	// Find child execution via storage to verify its outputs
	result, err := executor.ExecutionOutputs(context.Background(), "parent-2")
	require.NoError(t, err)

	var finalResult int
	require.NoError(t, result.Get("result", &finalResult))
	assert.Equal(t, 14, finalResult) // 7 * 2
}

func TestRunNested_ParentExecutionIDSet(t *testing.T) {
	ctrl := &nestedTestController{}
	registry, storage := setupNestedSagas(t, ctrl)

	executor := NewExecutor(storage,
		WithRegistry(registry),
		WithRecoveryScope("node/runner-a"),
	)
	err := executor.Start("parent-saga").
		Input("value", 3).
		WithID("parent-3").
		Execute(context.Background())
	require.NoError(t, err)

	// Get the parent execution to find child execution ID
	parentResult, err := executor.ExecutionOutputs(context.Background(), "parent-3")
	require.NoError(t, err)

	var childExecID string
	require.NoError(t, parentResult.Get("childexecid", &childExecID))
	require.NotEmpty(t, childExecID)

	// Get the child execution and verify ParentExecutionID
	childExec, err := storage.Get(context.Background(), childExecID)
	require.NoError(t, err)
	assert.Equal(t, "parent-3", childExec.ParentExecutionID)
	assert.Equal(t, "node/runner-a", childExec.RecoveryScope)
}

func TestRunNested_ChildFailurePropagates(t *testing.T) {
	ctrl := &nestedTestController{failChild: true}
	registry, storage := setupNestedSagas(t, ctrl)

	executor := NewExecutor(storage, WithRegistry(registry))
	err := executor.Start("parent-saga").
		Input("value", 5).
		WithID("parent-4").
		Execute(context.Background())
	require.Error(t, err)

	// Parent should be in failed state (child failure triggers parent undo)
	exec, err := storage.Get(context.Background(), "parent-4")
	require.NoError(t, err)
	assert.Equal(t, StatusFailed, exec.Status)

	// Child step was called, and parent undo should not call UndoNested
	// because ParentStep itself failed (so ParentStepOut is empty)
	assert.Equal(t, 1, ctrl.childCalls)
}

func TestRunNested_ActionCancellationStillCompensatesChild(t *testing.T) {
	registry := NewRegistry()
	ctrl := &testController{}
	started := make(chan struct{}, 1)
	waitForCancellation := func(ctx context.Context, _ MultiplyIn) (MultiplyOut, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return MultiplyOut{}, ctx.Err()
	}

	err := Define("cancel-child").
		Using(ctrl).
		Action("add", AddNumbers).Undo(UndoAddNumbers).
		Action("wait", waitForCancellation).Undo(UndoMultiply).
		RegisterTo(registry)
	require.NoError(t, err)

	runChild := func(ctx context.Context, in AddNumbersIn) (AddNumbersOut, error) {
		_, err := RunNested(ctx, "cancel-child",
			WithNestedID("cancel-child-1"),
			WithNestedInput("a", in.A),
			WithNestedInput("b", in.B),
			WithNestedInput("factor", 2),
		)
		return AddNumbersOut{}, err
	}
	undoParent := func(context.Context, AddNumbersIn, AddNumbersOut) error { return nil }
	err = Define("cancel-parent").
		Using(ctrl).
		Action("nested", runChild).Undo(undoParent).
		RegisterTo(registry)
	require.NoError(t, err)

	storage := NewMemoryStorage()
	executor := NewExecutor(storage, WithRegistry(registry))
	actionCtx, cancelAction := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- executor.Start("cancel-parent").
			Input("a", 2).
			Input("b", 3).
			WithActionContext(actionCtx).
			WithID("cancel-parent-1").
			Execute(context.Background())
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("nested child did not reach cancellable action")
	}
	cancelAction()

	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("nested child did not finish compensation")
	}

	child, err := storage.Get(context.Background(), "cancel-child-1")
	require.NoError(t, err)
	assert.Equal(t, StatusFailed, child.Status)
	assert.NotNil(t, child.ExecutedActions["add"].UndoneAt)
	assert.Len(t, ctrl.undoAddCalls, 1)

	parent, err := storage.Get(context.Background(), "cancel-parent-1")
	require.NoError(t, err)
	assert.Equal(t, StatusFailed, parent.Status)
}

func TestRunNested_ChildUsesParentStorage(t *testing.T) {
	ctrl := &nestedTestController{}
	registry, storage := setupNestedSagas(t, ctrl)

	executor := NewExecutor(storage, WithRegistry(registry))
	err := executor.Start("parent-saga").
		Input("value", 5).
		WithID("parent-5").
		Execute(context.Background())
	require.NoError(t, err)

	// Child execution should be persisted in the same storage
	parentResult, err := executor.ExecutionOutputs(context.Background(), "parent-5")
	require.NoError(t, err)

	var childExecID string
	require.NoError(t, parentResult.Get("childexecid", &childExecID))

	childExec, err := storage.Get(context.Background(), childExecID)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, childExec.Status)
	assert.Equal(t, "child-saga", childExec.DefinitionName)
}

func TestRunNested_OutsideContext(t *testing.T) {
	// RunNested outside of a saga should return an error
	_, err := RunNested(context.Background(), "some-saga")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no executor in context")
}

func TestUndoNested_OutsideContext(t *testing.T) {
	err := UndoNested(context.Background(), "some-id")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no executor in context")
}

func TestRunNested_WithNestedID(t *testing.T) {
	ctrl := &nestedTestController{}
	registry := NewRegistry()

	// Register child saga with explicit ID test
	err := Define("id-child").
		Using(ctrl).
		Action(ChildStep).Undo(UndoChildStep).
		RegisterTo(registry)
	require.NoError(t, err)

	// Use a parent that calls RunNested with WithNestedID
	parentWithIDFn := func(ctx context.Context, in ParentStepIn) (ParentStepOut, error) {
		ctrl := Get[*nestedTestController](ctx)
		ctrl.parentCalls++

		result, err := RunNested(ctx, "id-child",
			WithNestedInput("value", in.Value),
			WithNestedID("custom-child-id"),
		)
		if err != nil {
			return ParentStepOut{}, err
		}

		var doubled int
		if err := result.Get("doubled", &doubled); err != nil {
			return ParentStepOut{}, err
		}

		return ParentStepOut{
			ChildExecID: result.ExecutionID,
			Result:      doubled,
		}, nil
	}

	err = Define("id-parent").
		Using(ctrl).
		Action("parent-step", parentWithIDFn).Undo(UndoParentStep).
		RegisterTo(registry)
	require.NoError(t, err)

	storage := NewMemoryStorage()
	executor := NewExecutor(storage, WithRegistry(registry))
	err = executor.Start("id-parent").
		Input("value", 4).
		WithID("id-parent-1").
		Execute(context.Background())
	require.NoError(t, err)

	// Verify the child used the custom ID
	childExec, err := storage.Get(context.Background(), "custom-child-id")
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, childExec.Status)
	assert.Equal(t, "id-child", childExec.DefinitionName)
}

func TestRunNested_DeterministicChildID(t *testing.T) {
	ctrl := &nestedTestController{}
	registry, storage := setupNestedSagas(t, ctrl)

	// Run the parent saga twice with the same parent ID.
	// The second run should produce the same child ID (idempotent retry).
	executor := NewExecutor(storage, WithRegistry(registry))
	err := executor.Start("parent-saga").
		Input("value", 5).
		WithID("det-parent-1").
		Execute(context.Background())
	require.NoError(t, err)

	// Get the child execution ID from parent outputs
	result1, err := executor.ExecutionOutputs(context.Background(), "det-parent-1")
	require.NoError(t, err)
	var childID1 string
	require.NoError(t, result1.Get("childexecid", &childID1))
	require.NotEmpty(t, childID1)

	// Verify the child ID is deterministic (derived from parent exec ID + saga name + action name)
	expectedID := deriveChildID("det-parent-1", "child-saga", "parent-step")
	assert.Equal(t, expectedID, childID1)

	// Run again with a different parent ID — child ID should differ
	err = executor.Start("parent-saga").
		Input("value", 5).
		WithID("det-parent-2").
		Execute(context.Background())
	require.NoError(t, err)

	result2, err := executor.ExecutionOutputs(context.Background(), "det-parent-2")
	require.NoError(t, err)
	var childID2 string
	require.NoError(t, result2.Get("childexecid", &childID2))
	assert.NotEqual(t, childID1, childID2, "different parent IDs should produce different child IDs")
}

func TestRecover_SkipsChildExecutions(t *testing.T) {
	ctrl := &nestedTestController{}
	registry, storage := setupNestedSagas(t, ctrl)

	// Simulate a crash mid-way: both parent and child are incomplete.
	// The child has ParentExecutionID set.
	childExec := &Execution{
		ID:                "child-crash-1",
		DefinitionName:    "child-saga",
		DefinitionVersion: 1,
		ParentExecutionID: "parent-crash-1",
		Status:            StatusRunning,
		ExecutedActions:   map[string]*ActionResult{},
		ExecutionOrder:    []string{},
	}
	parentExec := &Execution{
		ID:                "parent-crash-1",
		DefinitionName:    "parent-saga",
		DefinitionVersion: 1,
		Status:            StatusRunning,
		InitialInputs:     map[string]any{"value": float64(5)},
		ExecutedActions:   map[string]*ActionResult{},
		ExecutionOrder:    []string{},
	}

	require.NoError(t, storage.Save(context.Background(), childExec))
	require.NoError(t, storage.Save(context.Background(), parentExec))

	executor := NewExecutor(storage, WithRegistry(registry))
	err := executor.Recover(context.Background())
	require.NoError(t, err)

	// Parent should have been recovered (re-executed its actions)
	assert.Equal(t, 1, ctrl.parentCalls, "parent should be recovered")

	// The child-crash-1 execution should NOT have been independently recovered.
	// Instead, RunNested inside parent created a new child execution.
	// The original child-crash-1 should still be in its crashed state (Running)
	// because Recover() skipped it.
	crashedChild, err := storage.Get(context.Background(), "child-crash-1")
	require.NoError(t, err)
	assert.Equal(t, StatusRunning, crashedChild.Status, "original child should be untouched by Recover()")
}

func TestNestedResult_Get(t *testing.T) {
	nr := &NestedResult{
		ExecutionID: "test",
		outputs: map[string]json.RawMessage{
			"name":  json.RawMessage(`"hello"`),
			"count": json.RawMessage(`42`),
		},
	}

	var name string
	require.NoError(t, nr.Get("name", &name))
	assert.Equal(t, "hello", name)

	var count int
	require.NoError(t, nr.Get("count", &count))
	assert.Equal(t, 42, count)

	// Missing key
	err := nr.Get("missing", &name)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestNestedResult_Has(t *testing.T) {
	nr := &NestedResult{
		ExecutionID: "test",
		outputs: map[string]json.RawMessage{
			"name": json.RawMessage(`"hello"`),
		},
	}

	assert.True(t, nr.Has("name"))
	assert.False(t, nr.Has("missing"))
}
