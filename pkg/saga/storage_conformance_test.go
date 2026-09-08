package saga

import (
	"context"
	"fmt"
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

	// makeIndexed is set only for the backends that find their work through a
	// status index, and hands back the store underneath so a test can seed an
	// index entry pointing at an execution that is not there. MemoryStorage
	// leaves it nil: it has no index, so it has no index to corrupt.
	makeIndexed func(t *testing.T) (Storage, *entity.MockStore)
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
			makeIndexed: func(t *testing.T) (Storage, *entity.MockStore) {
				inmem, cleanup := testutils.NewInMemEntityServer(t)
				t.Cleanup(cleanup)
				return NewEntityStorage(inmem.Store, testutils.TestLogger(t)), inmem.Store
			},
		},
		{
			name: "EACStorage",
			make: func(t *testing.T) Storage {
				inmem, cleanup := testutils.NewInMemEntityServer(t)
				t.Cleanup(cleanup)
				return NewEACStorage(inmem.EAC, testutils.TestLogger(t))
			},
			makeIndexed: func(t *testing.T) (Storage, *entity.MockStore) {
				inmem, cleanup := testutils.NewInMemEntityServer(t)
				t.Cleanup(cleanup)
				return NewEACStorage(inmem.EAC, testutils.TestLogger(t)), inmem.Store
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

			incomplete, err := collectIncomplete(ctx, storage)
			require.NoError(t, err)
			assert.True(t, containsExecution(incomplete, exec.ID),
				"a pending saga must appear in ListIncomplete")

			exec.Status = StatusCompleted
			require.NoError(t, storage.Save(ctx, exec))

			incomplete, err = collectIncomplete(ctx, storage)
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

		incomplete, err := collectIncomplete(ctx, storage)
		require.NoError(t, err)
		assert.Empty(t, incomplete,
			"a failed execution must not be recovered because stale pending and running index entries survived")

		terminal, err := collectTerminal(ctx, storage)
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

		terminal, err := collectTerminal(ctx, storage)
		require.NoError(t, err)
		assert.Empty(t, terminal,
			"a running execution must not be offered to retention because a stale completed index entry survived")

		incomplete, err := collectIncomplete(ctx, storage)
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

// collectIncomplete and collectTerminal walk a storage's pages to the end and
// return everything, for tests that are asserting on the contents of the set
// rather than on how it is paged. Paging itself is exercised separately, by
// TestStorageConformance_Paging.
func collectIncomplete(ctx context.Context, s Storage) ([]*Execution, error) {
	var all []*Execution
	cursor := ""
	// A bound rather than `for {}`: a backend whose cursor failed to advance
	// would otherwise hang the suite instead of failing it, and a hung test
	// says much less than a failed one.
	for range 1000 {
		page, err := s.ListIncompletePage(ctx, IncompleteQuery{Cursor: cursor})
		if err != nil {
			return nil, err
		}
		all = append(all, page.Executions...)
		cursor = page.Cursor
		if cursor == "" {
			return all, nil
		}
	}
	return nil, fmt.Errorf("incomplete walk did not terminate")
}

func collectIncompleteSummaries(ctx context.Context, s Storage) ([]IncompleteSummary, error) {
	var all []IncompleteSummary
	cursor := ""
	for range 1000 {
		page, err := s.ListIncompleteSummaryPage(ctx, IncompleteSummaryQuery{Cursor: cursor})
		if err != nil {
			return nil, err
		}
		all = append(all, page.Executions...)
		cursor = page.Cursor
		if cursor == "" {
			return all, nil
		}
	}
	return nil, fmt.Errorf("incomplete summary walk did not terminate")
}

func collectTerminal(ctx context.Context, s Storage) ([]TerminalExecution, error) {
	var all []TerminalExecution
	cursor := ""
	for range 1000 {
		page, err := s.ListTerminalPage(ctx, TerminalQuery{Cursor: cursor})
		if err != nil {
			return nil, err
		}
		all = append(all, page.Executions...)
		cursor = page.Cursor
		if cursor == "" {
			return all, nil
		}
	}
	return nil, fmt.Errorf("terminal walk did not terminate")
}

// TestStorageConformance_Paging pins the paging contract every backend has to
// satisfy, because the callers cannot tell which one they are talking to.
//
// The properties that matter to a caller walking a backlog: a page never
// exceeds the limit it asked for, the pages together cover the set exactly
// once, the walk terminates, and a short page is not mistaken for the end. That
// last one is the trap. The etcd-backed backends finish one status index before
// starting the next and never straddle two, so a page can come back well under
// the limit with plenty still to come, and a caller that stopped on a short
// page would silently skip every running and undoing saga in the store.
func TestStorageConformance_Paging(t *testing.T) {
	for _, backend := range allStorageBackends() {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()

			t.Run("covers the incomplete set exactly once across pages", func(t *testing.T) {
				storage := backend.make(t)

				// Spread across all three incomplete statuses so a backend that
				// walks them in stages actually has stages to walk, and enough
				// per status that a limit of 2 cannot clear one in a page.
				want := map[string]bool{}
				for i := range 5 {
					for _, status := range []Status{StatusPending, StatusRunning, StatusUndoing} {
						id := fmt.Sprintf("page-%s-%02d", status, i)
						saveWithStatus(t, storage, id, status)
						want[id] = true
					}
				}

				seen := map[string]bool{}
				cursor := ""
				pages := 0

				for range 100 {
					page, err := storage.ListIncompletePage(ctx, IncompleteQuery{Cursor: cursor, Limit: 2})
					require.NoError(t, err)
					require.LessOrEqual(t, len(page.Executions), 2,
						"a page must never exceed the limit it was given")

					pages++
					for _, exec := range page.Executions {
						require.False(t, seen[exec.ID], "paging returned %s twice", exec.ID)
						seen[exec.ID] = true
					}

					cursor = page.Cursor
					if cursor == "" {
						break
					}
				}

				assert.Equal(t, "", cursor, "the walk must terminate")
				assert.Equal(t, want, seen, "the pages together must cover the set")
				assert.Greater(t, pages, 1, "15 executions at 2 per page must take more than one page")
			})

			t.Run("covers the incomplete summary set exactly once across pages", func(t *testing.T) {
				storage := backend.make(t)

				want := map[string]bool{}
				for i := range 5 {
					for _, status := range []Status{StatusPending, StatusRunning, StatusUndoing} {
						id := fmt.Sprintf("page-%s-%02d", status, i)
						saveWithStatus(t, storage, id, status)
						want[id] = true
					}
				}

				seen := map[string]bool{}
				cursor := ""

				for range 100 {
					page, err := storage.ListIncompleteSummaryPage(ctx, IncompleteSummaryQuery{Cursor: cursor, Limit: 2})
					require.NoError(t, err)
					require.LessOrEqual(t, len(page.Executions), 2,
						"a page must never exceed the limit it was given")

					for _, summary := range page.Executions {
						require.False(t, seen[summary.ID], "paging returned %s twice", summary.ID)
						seen[summary.ID] = true
					}

					cursor = page.Cursor
					if cursor == "" {
						break
					}
				}

				assert.Equal(t, "", cursor, "the walk must terminate")
				assert.Equal(t, want, seen, "the pages together must cover the set")
			})

			t.Run("covers the terminal set exactly once across pages", func(t *testing.T) {
				storage := backend.make(t)

				want := map[string]bool{}
				for i := range 5 {
					for _, status := range []Status{StatusCompleted, StatusFailed} {
						id := fmt.Sprintf("page-%s-%02d", status, i)
						saveWithStatus(t, storage, id, status)
						want[id] = true
					}
				}

				seen := map[string]bool{}
				cursor := ""

				for range 100 {
					page, err := storage.ListTerminalPage(ctx, TerminalQuery{Cursor: cursor, Limit: 2})
					require.NoError(t, err)
					require.LessOrEqual(t, len(page.Executions), 2)

					for _, exec := range page.Executions {
						require.False(t, seen[exec.ID], "paging returned %s twice", exec.ID)
						seen[exec.ID] = true
					}

					cursor = page.Cursor
					if cursor == "" {
						break
					}
				}

				assert.Equal(t, "", cursor, "the walk must terminate")
				assert.Equal(t, want, seen, "the pages together must cover the set")
			})

			t.Run("an empty store ends the walk", func(t *testing.T) {
				storage := backend.make(t)

				// Not "immediately": an empty page still carries the cursor to
				// the next status index, because a backend cannot tell an
				// exhausted index from one whose page happened to resolve to
				// nothing without asking. That costs a walk over an empty store
				// one round trip per status index, which is the price of not
				// silently skipping the rest of an index that had stale entries
				// at the front of it.
				incomplete := 0
				cursor := ""
				for range 100 {
					page, err := storage.ListIncompletePage(ctx, IncompleteQuery{Cursor: cursor})
					require.NoError(t, err)
					incomplete += len(page.Executions)
					cursor = page.Cursor
					if cursor == "" {
						break
					}
				}
				assert.Equal(t, "", cursor, "the walk must still terminate")
				assert.Zero(t, incomplete)

				terminal := 0
				cursor = ""
				for range 100 {
					page, err := storage.ListTerminalPage(ctx, TerminalQuery{Cursor: cursor})
					require.NoError(t, err)
					terminal += len(page.Executions)
					cursor = page.Cursor
					if cursor == "" {
						break
					}
				}
				assert.Equal(t, "", cursor, "the walk must still terminate")
				assert.Zero(t, terminal)
			})

			t.Run("a set that ends on a page boundary ends the walk", func(t *testing.T) {
				storage := backend.make(t)

				// One status only, four of them, read two at a time: the second
				// page fills exactly and there is nothing after it.
				for i := range 4 {
					saveWithStatus(t, storage, fmt.Sprintf("boundary-%02d", i), StatusCompleted)
				}

				seen := 0
				cursor := ""
				for range 100 {
					page, err := storage.ListTerminalPage(ctx, TerminalQuery{Cursor: cursor, Limit: 2})
					require.NoError(t, err)
					seen += len(page.Executions)
					cursor = page.Cursor
					if cursor == "" {
						break
					}
				}

				assert.Equal(t, 4, seen)
				assert.Equal(t, "", cursor, "the walk must terminate on an exact boundary too")
			})

			t.Run("stops when the context is cancelled", func(t *testing.T) {
				storage := backend.make(t)

				for i := range 10 {
					saveWithStatus(t, storage, fmt.Sprintf("cancel-%02d", i), StatusPending)
				}

				cancelled, cancel := context.WithCancel(ctx)
				cancel()

				// Not asserting on which error: what matters is that a walk
				// under a dead context does not quietly do a store's worth of
				// work on its way to noticing.
				_, err := storage.ListIncompletePage(cancelled, IncompleteQuery{})
				assert.Error(t, err)
			})
		})
	}
}

// Cursor validation is deliberately not part of this suite. A cursor is opaque
// and only ever handed back to the storage that issued it, and the backends
// have nothing in common underneath: the etcd-backed ones prefix a status-index
// stage and can spot a malformed cursor, while MemoryStorage's is a bare
// execution id, where "garbage" and "an id deleted since the last page" are the
// same string. Requiring rejection would mean giving the memory backend a
// cursor format it has no other reason to have. The stage encoding the durable
// backends do share is tested directly, in TestStageCursor.

// saveWithStatus persists an execution in the given status, going through the
// same two-step save the executor does so a backend's index sees the transition
// rather than only the final value.
func saveWithStatus(t *testing.T, storage Storage, id string, status Status) {
	t.Helper()

	now := time.Now()
	exec := &Execution{
		ID:              id,
		DefinitionName:  "paging-test",
		Status:          StatusPending,
		InitialInputs:   map[string]any{},
		ExecutedActions: map[string]*ActionResult{},
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	require.NoError(t, storage.Save(context.Background(), exec))

	if status == StatusPending {
		return
	}

	exec.Status = status
	exec.UpdatedAt = now
	require.NoError(t, storage.Save(context.Background(), exec))
}

// TestStorageConformance_PagingPastStaleEntries is the regression for a page
// that resolves to nothing.
//
// A status index can name executions that are not there. That is the whole
// premise of MIR-1735, and runners read the index across an RPC whose server
// drops the ids it cannot resolve while keeping the cursor. So a page can come
// back empty with an entire index still behind it, and a backend that read that
// as "this index is exhausted" would walk off the end of it and silently leave
// every saga after the stale run unrecovered.
//
// The stale entries sort ahead of the real ones on purpose: the first page has
// to be the empty one, or the bug hides.
func TestStorageConformance_PagingPastStaleEntries(t *testing.T) {
	for _, backend := range allStorageBackends() {
		if backend.makeIndexed == nil {
			continue
		}

		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()

			storage, store := backend.makeIndexed(t)

			pending, ok := StatusIndexAttr(StatusPending)
			require.True(t, ok)

			store.AddStaleIndexEntry(pending, entity.Id("aaa-stale-00"))
			store.AddStaleIndexEntry(pending, entity.Id("aaa-stale-01"))

			want := map[string]bool{}
			for i := range 3 {
				id := fmt.Sprintf("zzz-real-%02d", i)
				saveWithStatus(t, storage, id, StatusPending)
				want[id] = true
			}

			seen := map[string]bool{}
			cursor := ""
			for range 100 {
				page, err := storage.ListIncompletePage(ctx, IncompleteQuery{Cursor: cursor, Limit: 2})
				require.NoError(t, err)

				for _, exec := range page.Executions {
					seen[exec.ID] = true
				}

				cursor = page.Cursor
				if cursor == "" {
					break
				}
			}

			assert.Equal(t, "", cursor, "the walk must terminate")
			assert.Equal(t, want, seen,
				"a page that resolved to nothing must not end the walk of its index")
		})
	}
}

// TestStorageConformance_IncompleteSummaryAgreesWithIncompleteList pins the two
// in-flight reads to the same set.
//
// They exist for different callers and return different shapes, which is
// exactly how they could drift: a status added to one list and not the other
// would leave the stalled sweep blind to a whole class of execution while
// recovery kept resuming it, and nothing would fail.
func TestStorageConformance_IncompleteSummaryAgreesWithIncompleteList(t *testing.T) {
	for _, backend := range allStorageBackends() {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			storage := backend.make(t)

			for _, status := range []Status{StatusPending, StatusRunning, StatusUndoing, StatusCompleted, StatusFailed} {
				saveWithStatus(t, storage, "agree-"+string(status), status)
			}

			executions, err := collectIncomplete(ctx, storage)
			require.NoError(t, err)
			summaries, err := collectIncompleteSummaries(ctx, storage)
			require.NoError(t, err)

			fromExecutions := map[string]Status{}
			for _, exec := range executions {
				fromExecutions[exec.ID] = exec.Status
			}
			fromSummaries := map[string]Status{}
			for _, summary := range summaries {
				fromSummaries[summary.ID] = summary.Status
				assert.False(t, summary.LastChanged.IsZero(),
					"a summary the sweep will act on must carry a timestamp")
			}

			assert.Equal(t, fromExecutions, fromSummaries,
				"both in-flight reads must see the same executions in the same states")
			assert.Len(t, fromSummaries, 3, "the three in-flight statuses, and only those")
		})
	}
}

// TestStorageConformance_IncompleteSummaryIgnoresStaleIndex is the same guard
// ListIncompletePage has, with a different consequence: the stalled sweep
// writes to what this returns, so trusting a stale pending entry would mean
// re-failing an execution that already finished.
func TestStorageConformance_IncompleteSummaryIgnoresStaleIndex(t *testing.T) {
	runIndexBackedConformance(t, func(t *testing.T, storage Storage, store *entity.MockStore) {
		ctx := context.Background()

		saveAged(t, storage, "teardown-postgres", StatusCompleted, time.Hour)
		seedStaleStatusIndex(t, store, "teardown-postgres", StatusPending)
		seedStaleStatusIndex(t, store, "teardown-postgres", StatusRunning)

		summaries, err := collectIncompleteSummaries(ctx, storage)
		require.NoError(t, err)
		assert.Empty(t, summaries,
			"a completed execution must not be offered to the stalled sweep because stale in-flight index entries survived")
	})
}
