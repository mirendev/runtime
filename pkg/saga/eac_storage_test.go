package saga

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/entity/testutils"
)

// TestEACStorageNewNilLogger verifies that NewEACStorage handles a nil logger.
func TestEACStorageNewNilLogger(t *testing.T) {
	s := NewEACStorage(nil, nil)
	assert.NotNil(t, s)
	assert.NotNil(t, s.log)
}

// TestEACStorageImplementsStorage verifies the interface compliance at compile time.
func TestEACStorageImplementsStorage(t *testing.T) {
	var _ Storage = (*EACStorage)(nil)
}

// TestEACStorageSaveAndGet verifies that Save persists a saga execution with a
// properly typed entity ID and that Get can retrieve it. This is a regression
// test for MIR-820 where Save passed a raw string instead of entity.Id(),
// causing a panic in ForceID.
func TestEACStorageSaveAndGet(t *testing.T) {
	ctx := context.Background()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	log := testutils.TestLogger(t)
	storage := NewEACStorage(inmem.EAC, log)

	exec := &Execution{
		ID:              "create-sandbox-sandbox/my-app-web-abc123",
		DefinitionName:  "create-sandbox",
		RecoveryScope:   "node/runner-a",
		Status:          StatusPending,
		InitialInputs:   map[string]any{"sandbox_id": "sandbox/my-app-web-abc123"},
		ExecutedActions: map[string]*ActionResult{},
		ExecutionOrder:  []string{},
	}

	// Save should not panic (the bug was a panic in ForceID due to wrong ID type).
	err := storage.Save(ctx, exec)
	require.NoError(t, err)

	// Round-trip: Get should return the same execution.
	got, err := storage.Get(ctx, exec.ID)
	require.NoError(t, err)
	assert.Equal(t, exec.ID, got.ID)
	assert.Equal(t, exec.DefinitionName, got.DefinitionName)
	assert.Equal(t, exec.RecoveryScope, got.RecoveryScope)
	assert.Equal(t, exec.Status, got.Status)
}

// TestEACStorageSaveIdempotent verifies that saving the same execution twice
// does not error (Ensure is idempotent).
func TestEACStorageSaveIdempotent(t *testing.T) {
	ctx := context.Background()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	log := testutils.TestLogger(t)
	storage := NewEACStorage(inmem.EAC, log)

	exec := &Execution{
		ID:              "create-sandbox-sandbox/my-app-web-xyz789",
		DefinitionName:  "create-sandbox",
		Status:          StatusPending,
		InitialInputs:   map[string]any{},
		ExecutedActions: map[string]*ActionResult{},
		ExecutionOrder:  []string{},
	}

	require.NoError(t, storage.Save(ctx, exec))
	require.NoError(t, storage.Save(ctx, exec))

	got, err := storage.Get(ctx, exec.ID)
	require.NoError(t, err)
	assert.Equal(t, exec.ID, got.ID)
}

// TestEACStorageGetNotFound verifies that Get returns ErrExecutionNotFound for
// an unknown ID.
func TestEACStorageGetNotFound(t *testing.T) {
	ctx := context.Background()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	log := testutils.TestLogger(t)
	storage := NewEACStorage(inmem.EAC, log)

	_, err := storage.Get(ctx, "nonexistent-saga-id")
	assert.ErrorIs(t, err, ErrExecutionNotFound)
}
