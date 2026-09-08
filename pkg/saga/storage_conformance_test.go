package saga

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

// storageFactory builds a fresh, empty Storage for one test. It registers any
// teardown via t.Cleanup so callers get a clean backend per subtest.
type storageFactory struct {
	name string
	make func(t *testing.T) Storage
}

// allStorageBackends returns every production Storage implementation behind a
// uniform factory. The whole point of this suite is that every backend must
// satisfy the same contract: the bug in MIR-441 lived in two of these three
// (EntityStorage and EACStorage both used create-if-absent semantics and
// silently dropped every save after the first), while MemoryStorage was
// correct, so memory-backed unit tests stayed green while production froze
// every saga at its initial pending state. Running the same scenarios against
// all backends is what closes that gap.
//
// This suite tests a different question than pkg/entity's Store conformance
// suite, and the two are complementary. The entity suite proves MockStore
// behaves like the real EtcdStore on the underlying write primitives. This
// suite proves saga storage *uses* those primitives correctly (the MIR-441 bug
// was a create-only call where an upsert was needed, which no amount of
// store-level testing would catch). Because the entity suite vouches for the
// mock, the EntityStorage backend here is mock-backed on purpose and we do not
// add a real-etcd backend; the EACStorage backend additionally exercises the
// EntityAccessClient RPC path, which the entity suite does not reach.
func allStorageBackends() []storageFactory {
	return []storageFactory{
		{
			name: "MemoryStorage",
			make: func(t *testing.T) Storage {
				return NewMemoryStorage()
			},
		},
		{
			name: "EntityStorage",
			make: func(t *testing.T) Storage {
				inmem, cleanup := testutils.NewInMemEntityServer(t)
				t.Cleanup(cleanup)
				return NewEntityStorage(inmem.Store, testutils.TestLogger(t))
			},
		},
		{
			name: "EACStorage",
			make: func(t *testing.T) Storage {
				inmem, cleanup := testutils.NewInMemEntityServer(t)
				t.Cleanup(cleanup)
				return NewEACStorage(inmem.EAC, testutils.TestLogger(t))
			},
		},
	}
}

// TestStorageConformance_SaveUpdatesExistingExecution is the direct regression
// for the EnsureEntity/Ensure create-if-absent bug. A saga is saved repeatedly
// as it progresses (pending -> running -> completed, accumulating action
// results). Every save after the first must overwrite the stored state. The
// buggy backends dropped these updates, so Get returned a pending execution
// with no recorded actions even though the saga had completed.
func TestStorageConformance_SaveUpdatesExistingExecution(t *testing.T) {
	for _, backend := range allStorageBackends() {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			storage := backend.make(t)

			exec := &Execution{
				ID:              "build-from-prepared-abc123",
				DefinitionName:  "build-from-tar",
				Status:          StatusPending,
				InitialInputs:   map[string]any{"app_name": "demo"},
				ExecutedActions: map[string]*ActionResult{},
				ExecutionOrder:  []string{},
			}

			// First save: creates the entity.
			require.NoError(t, storage.Save(ctx, exec))

			// Progress the saga and save again. Under the old create-if-absent
			// behavior this save was a silent no-op.
			exec.Status = StatusRunning
			exec.ExecutedActions["receive-tar"] = &ActionResult{
				Output:     []byte(`{"source_dir":"/tmp/build"}`),
				ExecutedAt: time.Unix(0, 0),
			}
			exec.ExecutionOrder = []string{"receive-tar"}
			require.NoError(t, storage.Save(ctx, exec))

			// Complete the saga and save a final time.
			exec.Status = StatusCompleted
			exec.ExecutedActions["create-version"] = &ActionResult{
				Output:     []byte(`{"version_name":"demo-v1"}`),
				ExecutedAt: time.Unix(0, 0),
			}
			exec.ExecutionOrder = []string{"receive-tar", "create-version"}
			require.NoError(t, storage.Save(ctx, exec))

			got, err := storage.Get(ctx, exec.ID)
			require.NoError(t, err)

			assert.Equal(t, StatusCompleted, got.Status,
				"final status must persist; a stuck pending status means saves after the first were dropped")
			assert.Len(t, got.ExecutedActions, 2,
				"executed actions recorded across saves must all persist")
			assert.Equal(t, []string{"receive-tar", "create-version"}, got.ExecutionOrder)

			if action, ok := got.ExecutedActions["create-version"]; assert.True(t, ok, "create-version action must persist") {
				assert.JSONEq(t, `{"version_name":"demo-v1"}`, string(action.Output))
			}
		})
	}
}

// TestStorageConformance_CompletedExecutionLeavesIncompleteList verifies that a
// saga which has reached a terminal status no longer shows up in
// ListIncomplete. This is the operational consequence of the same bug: when the
// completed save was dropped, the execution stayed pending in storage forever,
// so every process restart re-ran an already-finished saga during recovery.
func TestStorageConformance_CompletedExecutionLeavesIncompleteList(t *testing.T) {
	for _, backend := range allStorageBackends() {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			storage := backend.make(t)

			exec := &Execution{
				ID:              "saga-incomplete-check",
				DefinitionName:  "build-from-tar",
				Status:          StatusPending,
				InitialInputs:   map[string]any{},
				ExecutedActions: map[string]*ActionResult{},
				ExecutionOrder:  []string{},
			}
			require.NoError(t, storage.Save(ctx, exec))

			incomplete, err := storage.ListIncomplete(ctx)
			require.NoError(t, err)
			assert.True(t, containsExecution(incomplete, exec.ID),
				"a pending saga must appear in ListIncomplete")

			exec.Status = StatusCompleted
			require.NoError(t, storage.Save(ctx, exec))

			incomplete, err = storage.ListIncomplete(ctx)
			require.NoError(t, err)
			assert.False(t, containsExecution(incomplete, exec.ID),
				"a completed saga must NOT appear in ListIncomplete; if it does, recovery will re-run finished work")
		})
	}
}

// TestStorageConformance_TimestampsRoundTrip pins the timestamps to storage.
// The executor has always maintained Execution.CreatedAt and UpdatedAt in
// memory, stamping UpdatedAt on every state transition, but the saga schema had
// no fields to hold them and Save dropped them on the floor. Anything that
// needs a saga's age — retention GC most of all — reads back zero times without
// this, so a TTL sweep would either delete everything or nothing.
func TestStorageConformance_TimestampsRoundTrip(t *testing.T) {
	for _, backend := range allStorageBackends() {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			storage := backend.make(t)

			created := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
			updated := time.Date(2026, 7, 6, 12, 5, 0, 0, time.UTC)

			exec := &Execution{
				ID:              "saga-timestamps",
				DefinitionName:  "create-sandbox",
				Status:          StatusRunning,
				InitialInputs:   map[string]any{},
				ExecutedActions: map[string]*ActionResult{},
				ExecutionOrder:  []string{},
				CreatedAt:       created,
				UpdatedAt:       created,
			}
			require.NoError(t, storage.Save(ctx, exec))

			exec.Status = StatusCompleted
			exec.UpdatedAt = updated
			require.NoError(t, storage.Save(ctx, exec))

			got, err := storage.Get(ctx, exec.ID)
			require.NoError(t, err)

			assert.True(t, created.Equal(got.CreatedAt),
				"CreatedAt must survive a round trip, got %v", got.CreatedAt)
			assert.True(t, updated.Equal(got.UpdatedAt),
				"UpdatedAt must reflect the latest save, got %v", got.UpdatedAt)
		})
	}
}

func TestStorageConformance_RecoveryScopeRoundTrip(t *testing.T) {
	for _, backend := range allStorageBackends() {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			storage := backend.make(t)

			exec := &Execution{
				ID:              "create-sandbox-scoped",
				DefinitionName:  "create-sandbox",
				RecoveryScope:   "node/runner-a",
				Status:          StatusRunning,
				InitialInputs:   map[string]any{"sandbox_id": "sandbox/example"},
				ExecutedActions: map[string]*ActionResult{},
				ExecutionOrder:  []string{},
			}
			require.NoError(t, storage.Save(ctx, exec))

			got, err := storage.Get(ctx, exec.ID)
			require.NoError(t, err)
			assert.Equal(t, "node/runner-a", got.RecoveryScope)
		})
	}
}

func containsExecution(execs []*Execution, id string) bool {
	for _, e := range execs {
		if e != nil && e.ID == id {
			return true
		}
	}
	return false
}

// runIndexBackedConformance runs scenario against the storages that answer from
// the entity index, handing over the mock store beneath so it can seed drift.
// MemoryStorage is absent: it has no index to make stale.
func runIndexBackedConformance(t *testing.T, scenario func(t *testing.T, storage Storage, store *entity.MockStore)) {
	t.Helper()

	backends := []struct {
		name string
		make func(t *testing.T, inmem *testutils.InMemEntityServer) Storage
	}{
		{"EntityStorage", func(t *testing.T, inmem *testutils.InMemEntityServer) Storage {
			return NewEntityStorage(inmem.Store, testutils.TestLogger(t))
		}},
		{"EACStorage", func(t *testing.T, inmem *testutils.InMemEntityServer) Storage {
			return NewEACStorage(inmem.EAC, testutils.TestLogger(t))
		}},
	}

	for _, backend := range backends {
		t.Run(backend.name, func(t *testing.T) {
			inmem, cleanup := testutils.NewInMemEntityServer(t)
			t.Cleanup(cleanup)
			scenario(t, backend.make(t, inmem), inmem.Store)
		})
	}
}

// seedStaleStatusIndex makes the status index report id under status while the
// stored execution says otherwise.
func seedStaleStatusIndex(t *testing.T, store *entity.MockStore, id string, status Status) {
	t.Helper()

	attr, ok := StatusIndexAttr(status)
	require.True(t, ok, "status %v has no index attribute", status)
	store.AddStaleIndexEntry(attr, entity.Id(id))
}

// TestStorageConformance_IncompleteListIgnoresStaleTerminalIndex is
// CompletedExecutionLeavesIncompleteList again, with the index lying.
func TestStorageConformance_IncompleteListIgnoresStaleTerminalIndex(t *testing.T) {
	runIndexBackedConformance(t, func(t *testing.T, storage Storage, store *entity.MockStore) {
		ctx := context.Background()

		saveAged(t, storage, "teardown-postgres", StatusFailed, time.Hour)

		// Every status this execution passed through on its way to failed.
		seedStaleStatusIndex(t, store, "teardown-postgres", StatusPending)
		seedStaleStatusIndex(t, store, "teardown-postgres", StatusRunning)

		incomplete, err := storage.ListIncomplete(ctx)
		require.NoError(t, err)
		assert.Empty(t, incomplete,
			"a failed execution must not be recovered because stale pending and running index entries survived")

		terminal, err := storage.ListTerminal(ctx)
		require.NoError(t, err)
		require.Len(t, terminal, 1, "the execution is still terminal and retention must still see it")
		assert.Equal(t, "teardown-postgres", terminal[0].ID)
	})
}

// TestStorageConformance_TerminalListIgnoresStaleIncompleteIndex is the
// symmetric guard, with the sharper stake: retention deletes what ListTerminal
// returns, so trusting a stale entry here deletes a running saga.
func TestStorageConformance_TerminalListIgnoresStaleIncompleteIndex(t *testing.T) {
	runIndexBackedConformance(t, func(t *testing.T, storage Storage, store *entity.MockStore) {
		ctx := context.Background()

		saveAged(t, storage, "create-sandbox", StatusRunning, 30*24*time.Hour)
		seedStaleStatusIndex(t, store, "create-sandbox", StatusCompleted)

		terminal, err := storage.ListTerminal(ctx)
		require.NoError(t, err)
		assert.Empty(t, terminal,
			"a running execution must not be offered to retention because a stale completed index entry survived")

		incomplete, err := storage.ListIncomplete(ctx)
		require.NoError(t, err)
		require.Len(t, incomplete, 1, "the execution is still in flight and recovery must still find it")
		assert.Equal(t, "create-sandbox", incomplete[0].ID)
	})
}

// TestStorageConformance_RetentionCollectsChildOfStaleIncompleteParent covers
// the stall one layer up: retention holds back children of a live parent, and
// reads ListIncomplete to decide which parents are live.
func TestStorageConformance_RetentionCollectsChildOfStaleIncompleteParent(t *testing.T) {
	runIndexBackedConformance(t, func(t *testing.T, storage Storage, store *entity.MockStore) {
		ctx := context.Background()

		saveAged(t, storage, "finished-parent", StatusCompleted, 30*24*time.Hour)
		saveChild(t, storage, "expired-child", "finished-parent", 30*24*time.Hour)
		seedStaleStatusIndex(t, store, "finished-parent", StatusPending)

		result, err := RunRetention(ctx, storage, weekRetention(), testutils.TestLogger(t))
		require.NoError(t, err)

		assert.Zero(t, result.Skipped,
			"the parent is terminal, so its expired child is collectable no matter what the pending index claims")
		assert.Equal(t, 2, result.Deleted)
		assert.False(t, executionExists(t, storage, "expired-child"))
		assert.False(t, executionExists(t, storage, "finished-parent"))
	})
}
