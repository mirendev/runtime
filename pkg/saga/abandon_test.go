package saga

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/entity/testutils"
)

func blockedExecution() *Execution {
	undone := time.Now()
	return &Execution{
		ID:                "saga/sg-blocked",
		DefinitionName:    "create-sandbox",
		DefinitionVersion: 1,
		Status:            StatusUndoing,
		InitialInputs:     map[string]any{},
		ExecutedActions: map[string]*ActionResult{
			"allocate-ip":    {Output: []byte(`{}`), ExecutedAt: time.Now()},
			"create-volume":  {Output: []byte(`{}`), ExecutedAt: time.Now(), UndoneAt: &undone},
			"start-task":     {ExecutedAt: time.Now(), Error: "boom"},
			"configure-port": {Output: []byte(`{}`), ExecutedAt: time.Now()},
		},
		ExecutionOrder: []string{"allocate-ip", "create-volume", "configure-port", "start-task"},
		BlockedReason:  "refusing to resume: recorded at v1",
	}
}

func TestAbandon(t *testing.T) {
	t.Run("blocked execution fails and names what was left", func(t *testing.T) {
		exec := blockedExecution()
		now := time.Now()

		left, err := Abandon(exec, now)
		require.NoError(t, err)

		assert.Equal(t, []string{"allocate-ip", "configure-port"}, left,
			"undone and failed actions have nothing left to clean up")
		assert.Equal(t, StatusFailed, exec.Status)
		assert.Empty(t, exec.BlockedReason)
		assert.Contains(t, exec.Error, "allocate-ip, configure-port")
		assert.Contains(t, exec.Error, "recorded at v1", "the reason it was blocked is kept")
		assert.True(t, exec.UpdatedAt.Equal(now))
	})

	t.Run("unblocked execution is refused", func(t *testing.T) {
		exec := blockedExecution()
		exec.BlockedReason = ""

		_, err := Abandon(exec, time.Now())
		require.ErrorIs(t, err, ErrNotBlocked)
		assert.Equal(t, StatusUndoing, exec.Status)
	})

	t.Run("finished execution is refused", func(t *testing.T) {
		failed := blockedExecution()
		failed.Status = StatusFailed
		_, err := Abandon(failed, time.Now())
		require.ErrorContains(t, err, "already failed")

		completed := blockedExecution()
		completed.Status = StatusCompleted
		completed.BlockedReason = ""
		_, err = Abandon(completed, time.Now())
		require.ErrorContains(t, err, "already completed")
	})

	t.Run("completed execution blocked during compensation can be abandoned", func(t *testing.T) {
		exec := blockedExecution()
		exec.Status = StatusCompleted

		left, err := Abandon(exec, time.Now())
		require.NoError(t, err)
		assert.Equal(t, []string{"allocate-ip", "configure-port"}, left)
		assert.Equal(t, StatusFailed, exec.Status)
	})
}

func TestEACStorage_SaveAtRevisionRefusesAStaleCopy(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	storage := NewEACStorage(inmem.EAC, testutils.TestLogger(t))

	exec := blockedExecution()
	require.NoError(t, storage.Save(ctx, exec))

	read, err := inmem.EAC.Get(ctx, exec.ID)
	require.NoError(t, err)
	rev := read.Entity().Revision()

	// A binary that can resume it moves it on after the operator read it.
	exec.BlockedReason = ""
	exec.Status = StatusRunning
	require.NoError(t, storage.Save(ctx, exec))

	stale := blockedExecution()
	_, err = Abandon(stale, time.Now())
	require.NoError(t, err)
	require.Error(t, storage.SaveAtRevision(ctx, stale, rev))

	got, err := storage.Get(ctx, exec.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusRunning, got.Status, "the resumed execution's progress must survive")

	// At the current revision the write goes through.
	read, err = inmem.EAC.Get(ctx, exec.ID)
	require.NoError(t, err)
	got.BlockedReason = "blocked again"
	_, err = Abandon(got, time.Now())
	require.NoError(t, err)
	require.NoError(t, storage.SaveAtRevision(ctx, got, read.Entity().Revision()))

	got, err = storage.Get(ctx, exec.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusFailed, got.Status)
}

// A record deleted after the operator read it, say by a reconcile clearing a
// failed execution, must stay deleted rather than be recreated from the copy.
func TestEACStorage_SaveAtRevisionRefusesADeletedRecord(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	storage := NewEACStorage(inmem.EAC, testutils.TestLogger(t))

	exec := blockedExecution()
	require.NoError(t, storage.Save(ctx, exec))
	read, err := inmem.EAC.Get(ctx, exec.ID)
	require.NoError(t, err)
	rev := read.Entity().Revision()

	require.NoError(t, storage.Delete(ctx, exec.ID))

	_, err = Abandon(exec, time.Now())
	require.NoError(t, err)
	require.Error(t, storage.SaveAtRevision(ctx, exec, rev))

	_, err = storage.Get(ctx, exec.ID)
	require.ErrorIs(t, err, ErrExecutionNotFound)
}
