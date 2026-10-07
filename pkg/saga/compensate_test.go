package saga

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompensatePreservesDependenciesUntilCleanupSucceeds(t *testing.T) {
	registry := NewRegistry()
	ctrl := &testController{}
	failCleanup := true
	require.NoError(t, Define("compensate").Using(ctrl).
		Action("add", AddNumbers).Undo(UndoAddNumbers).
		Action("multiply", Multiply).Undo(func(ctx context.Context, in MultiplyIn, out MultiplyOut) error {
		if failCleanup {
			return errors.New("cleanup unavailable")
		}
		return UndoMultiply(ctx, in, out)
	}).RegisterTo(registry))
	storage := NewMemoryStorage()
	executor := NewExecutor(storage, WithRegistry(registry))
	ctx := t.Context()
	require.NoError(t, executor.Start("compensate").WithID("preview").Input("a", 2).Input("b", 5).Input("factor", 3).Execute(ctx))
	require.ErrorContains(t, executor.Compensate(ctx, "preview"), "cleanup unavailable")
	exec, err := storage.Get(ctx, "preview")
	require.NoError(t, err)
	require.Equal(t, StatusUndoing, exec.Status)
	require.Nil(t, exec.ExecutedActions["add"].UndoneAt, "keep the dependency for retry")
	require.Empty(t, ctrl.undoAddCalls)
	failCleanup = false
	require.NoError(t, executor.Compensate(ctx, "preview"))
	require.NoError(t, executor.Compensate(ctx, "preview"), "cleanup is idempotent")
	require.Equal(t, []MultiplyOut{{Result: 21}}, ctrl.undoMultCalls)
	require.Equal(t, []AddNumbersOut{{Sum: 7}}, ctrl.undoAddCalls)
}

func TestCompensateRefusesIncompatibleCompletedExecution(t *testing.T) {
	ctrl := &testController{}
	oldRegistry := NewRegistry()
	require.NoError(t, Define("changed").Using(ctrl).
		Action("add", AddNumbers).Undo(UndoAddNumbers).RegisterTo(oldRegistry))
	storage := NewMemoryStorage()
	ctx := t.Context()
	require.NoError(t, NewExecutor(storage, WithRegistry(oldRegistry)).Start("changed").
		WithID("preview").Input("a", 2).Input("b", 5).Execute(ctx))

	newRegistry := NewRegistry()
	require.NoError(t, Define("changed").Version(2).ResumesFrom().Using(ctrl).
		Action("add", AddNumbers).Undo(UndoAddNumbers).RegisterTo(newRegistry))
	err := NewExecutor(storage, WithRegistry(newRegistry)).Compensate(ctx, "preview")
	require.ErrorIs(t, err, ErrIncompatibleDefinition)
	require.Empty(t, ctrl.undoAddCalls)
	exec, err := storage.Get(ctx, "preview")
	require.NoError(t, err)
	require.Equal(t, StatusCompleted, exec.Status)
	require.NotEmpty(t, exec.BlockedReason)
}

func TestCompensateHonorsUndoBlock(t *testing.T) {
	storage := NewMemoryStorage()
	ctrl := &stuckUndoController{fail: true}
	registry := stuckRegistry(t, ctrl)
	saveUndoing(t, storage, "preview", undoBlockAttempts-1, 2*undoBlockAfter)
	executor := NewExecutor(storage, WithRegistry(registry), WithBuild("old"))
	require.ErrorIs(t, executor.Compensate(t.Context(), "preview"), ErrUndoBlocked)
	require.Equal(t, 1, ctrl.calls)
	require.ErrorIs(t, executor.Compensate(t.Context(), "preview"), ErrUndoBlocked)
	require.Equal(t, 1, ctrl.calls, "the same build must not retry a blocked undo")
	ctrl.fail = false
	require.NoError(t, NewExecutor(storage, WithRegistry(registry), WithBuild("new")).Compensate(t.Context(), "preview"))
	require.Equal(t, 2, ctrl.calls)
	exec, err := storage.Get(t.Context(), "preview")
	require.NoError(t, err)
	require.Equal(t, StatusFailed, exec.Status)
}

func TestCompensateRecoversInterruptedAction(t *testing.T) {
	registry := NewRegistry()
	ctrl := &testController{}
	require.NoError(t, Define("compensate-running").Using(ctrl).
		Action("add", AddNumbers).Undo(UndoAddNumbers).
		Action("multiply", Multiply).Undo(UndoMultiply).RegisterTo(registry))
	storage := NewMemoryStorage()
	executor := NewExecutor(storage, WithRegistry(registry))
	ctx := t.Context()
	require.NoError(t, executor.Start("compensate-running").WithID("preview").Input("a", 2).Input("b", 5).Input("factor", 3).Execute(ctx))
	exec, err := storage.Get(ctx, "preview")
	require.NoError(t, err)
	delete(exec.ExecutedActions, "multiply")
	exec.ExecutionOrder = []string{"add"}
	exec.Status = StatusRunning
	require.NoError(t, storage.Save(ctx, exec))
	require.NoError(t, executor.Compensate(ctx, "preview"))
	require.Len(t, ctrl.addCalls, 1)
	require.Len(t, ctrl.multiplyCalls, 2, "recover the missing action checkpoint")
	require.Equal(t, []MultiplyOut{{Result: 21}}, ctrl.undoMultCalls)
	require.Equal(t, []AddNumbersOut{{Sum: 7}}, ctrl.undoAddCalls)
}
