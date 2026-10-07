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
