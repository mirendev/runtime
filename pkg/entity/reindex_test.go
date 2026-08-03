package entity

import (
	"fmt"
	"log/slog"
	"testing"

	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clientv3 "go.etcd.io/etcd/client/v3"
	"miren.dev/runtime/pkg/etcdtest"
)

func setupReindexTestStore(t *testing.T) (*EtcdStore, *clientv3.Client) {
	t.Helper()
	client, prefix := etcdtest.TestEtcdClient(t)
	store, err := NewEtcdStore(t.Context(), slog.Default(), client, prefix)
	require.NoError(t, err)
	return store, client
}

func TestReindex_BasicIndexing(t *testing.T) {
	store, client := setupReindexTestStore(t)
	ctx := t.Context()

	// Create an indexed attribute schema
	_, err := store.CreateEntity(ctx, New(
		Ident, "test/kind",
		Doc, "a kind",
		Cardinality, CardinalityOne,
		Type, TypeStr,
		Index, true,
	))
	require.NoError(t, err)

	// Create entities with the indexed attribute
	e1, err := store.CreateEntity(ctx, New(
		Ident, "entity-1",
		String(Id("test/kind"), "widget"),
	))
	require.NoError(t, err)

	e2, err := store.CreateEntity(ctx, New(
		Ident, "entity-2",
		String(Id("test/kind"), "widget"),
	))
	require.NoError(t, err)

	// Verify indexes exist via ListIndex
	ids, err := store.ListIndex(ctx, String(Id("test/kind"), "widget"))
	require.NoError(t, err)
	assert.Len(t, ids, 2)

	// Now manually delete the collection entries to simulate missing indexes
	collectionPrefix := store.Prefix() + "/collections/"
	_, err = client.Delete(ctx, collectionPrefix, clientv3.WithPrefix())
	require.NoError(t, err)

	// Verify indexes are now gone
	ids, err = store.ListIndex(ctx, String(Id("test/kind"), "widget"))
	require.NoError(t, err)
	assert.Len(t, ids, 0)

	// Run reindex
	stats, err := store.Reindex(ctx, slog.Default(), ReindexOptions{
		DryRun:       false,
		CleanupStale: false,
	})
	require.NoError(t, err)
	assert.Greater(t, stats.EntitiesProcessed, int64(0))
	assert.Greater(t, stats.IndexesRebuilt, int64(0))

	// Verify indexes are back
	ids, err = store.ListIndex(ctx, String(Id("test/kind"), "widget"))
	require.NoError(t, err)
	assert.Len(t, ids, 2)

	foundIDs := map[Id]bool{ids[0]: true, ids[1]: true}
	assert.True(t, foundIDs[e1.Id()])
	assert.True(t, foundIDs[e2.Id()])
}

func TestReindex_Idempotent(t *testing.T) {
	store, _ := setupReindexTestStore(t)
	ctx := t.Context()

	// Create an indexed attribute schema
	_, err := store.CreateEntity(ctx, New(
		Ident, "test/kind",
		Doc, "a kind",
		Cardinality, CardinalityOne,
		Type, TypeStr,
		Index, true,
	))
	require.NoError(t, err)

	// Create entities
	_, err = store.CreateEntity(ctx, New(
		Ident, "entity-1",
		String(Id("test/kind"), "widget"),
	))
	require.NoError(t, err)

	// Run reindex twice
	stats1, err := store.Reindex(ctx, slog.Default(), ReindexOptions{
		DryRun:       false,
		CleanupStale: false,
	})
	require.NoError(t, err)

	stats2, err := store.Reindex(ctx, slog.Default(), ReindexOptions{
		DryRun:       false,
		CleanupStale: false,
	})
	require.NoError(t, err)

	// Same entities processed both times
	assert.Equal(t, stats1.EntitiesProcessed, stats2.EntitiesProcessed)

	// Verify indexes still work correctly
	ids, err := store.ListIndex(ctx, String(Id("test/kind"), "widget"))
	require.NoError(t, err)
	assert.Len(t, ids, 1)
}

// TestReindex_ResumesAcrossBoundedPasses is the core convergence property: a
// store too large to reindex in one pass must still reach every entity by
// resuming from the cursor, and must not report completion until it has.
func TestReindex_ResumesAcrossBoundedPasses(t *testing.T) {
	store, client := setupReindexTestStore(t)
	ctx := t.Context()

	_, err := store.CreateEntity(ctx, New(
		Ident, "test/kind",
		Doc, "a kind",
		Cardinality, CardinalityOne,
		Type, TypeStr,
		Index, true,
	))
	require.NoError(t, err)

	const entityCount = 12
	want := map[Id]bool{}
	for i := range entityCount {
		e, err := store.CreateEntity(ctx, New(
			Ident, Id(fmt.Sprintf("entity-%02d", i)),
			String(Id("test/kind"), "widget"),
		))
		require.NoError(t, err)
		want[e.Id()] = true
	}

	// Drop every collection entry, simulating entities written before the
	// indexed attribute existed.
	_, err = client.Delete(ctx, store.Prefix()+"/collections/", clientv3.WithPrefix())
	require.NoError(t, err)

	ids, err := store.ListIndex(ctx, String(Id("test/kind"), "widget"))
	require.NoError(t, err)
	require.Empty(t, ids)

	const perPass = 3
	var (
		cursor   string
		passes   int
		complete bool
	)
	for !complete {
		passes++
		require.Less(t, passes, 100, "reindex failed to converge")

		stats, err := store.Reindex(ctx, slog.Default(), ReindexOptions{
			StartKey:    cursor,
			MaxEntities: perPass,
		})
		require.NoError(t, err)
		assert.LessOrEqual(t, stats.EntitiesProcessed, int64(perPass),
			"pass overran its entity budget")

		if !stats.Complete {
			require.NotEmpty(t, stats.NextCursor, "incomplete pass must report a cursor")
			require.Greater(t, stats.NextCursor, cursor, "cursor must move forward")
		}

		cursor = stats.NextCursor
		complete = stats.Complete
	}

	// The store holds schema entities too, so the exact pass count depends on
	// the base schema; what matters is that it took more than one.
	assert.Greater(t, passes, 1, "test did not exercise resumption")

	ids, err = store.ListIndex(ctx, String(Id("test/kind"), "widget"))
	require.NoError(t, err)
	assert.Len(t, ids, entityCount)

	got := map[Id]bool{}
	for _, id := range ids {
		got[id] = true
	}
	assert.Equal(t, want, got, "every entity must be indexed once the passes complete")
}

// TestReindex_BoundedPassIsIdempotentOnReplay covers the cursor lagging by one
// entity after a crash: replaying a pass from a stale cursor must not corrupt
// or duplicate index entries.
func TestReindex_BoundedPassIsIdempotentOnReplay(t *testing.T) {
	store, client := setupReindexTestStore(t)
	ctx := t.Context()

	_, err := store.CreateEntity(ctx, New(
		Ident, "test/kind",
		Doc, "a kind",
		Cardinality, CardinalityOne,
		Type, TypeStr,
		Index, true,
	))
	require.NoError(t, err)

	for i := range 6 {
		_, err := store.CreateEntity(ctx, New(
			Ident, Id(fmt.Sprintf("entity-%02d", i)),
			String(Id("test/kind"), "widget"),
		))
		require.NoError(t, err)
	}

	_, err = client.Delete(ctx, store.Prefix()+"/collections/", clientv3.WithPrefix())
	require.NoError(t, err)

	first, err := store.Reindex(ctx, slog.Default(), ReindexOptions{MaxEntities: 4})
	require.NoError(t, err)
	require.False(t, first.Complete)

	// Replay the same range from the beginning rather than from the cursor,
	// then continue. Convergence must not depend on each entity being visited
	// exactly once.
	_, err = store.Reindex(ctx, slog.Default(), ReindexOptions{MaxEntities: 4})
	require.NoError(t, err)

	cursor := first.NextCursor
	for {
		stats, err := store.Reindex(ctx, slog.Default(), ReindexOptions{
			StartKey:    cursor,
			MaxEntities: 4,
		})
		require.NoError(t, err)
		cursor = stats.NextCursor
		if stats.Complete {
			break
		}
	}

	ids, err := store.ListIndex(ctx, String(Id("test/kind"), "widget"))
	require.NoError(t, err)
	assert.Len(t, ids, 6, "replayed passes must not duplicate index entries")
}

// TestReindex_UnboundedPassCompletesInOne keeps the manual `miren debug
// reindex` contract intact: no budget means one complete pass, no cursor.
func TestReindex_UnboundedPassCompletesInOne(t *testing.T) {
	store, _ := setupReindexTestStore(t)
	ctx := t.Context()

	_, err := store.CreateEntity(ctx, New(
		Ident, "test/kind",
		Doc, "a kind",
		Cardinality, CardinalityOne,
		Type, TypeStr,
		Index, true,
	))
	require.NoError(t, err)

	_, err = store.CreateEntity(ctx, New(
		Ident, "entity-1",
		String(Id("test/kind"), "widget"),
	))
	require.NoError(t, err)

	stats, err := store.Reindex(ctx, slog.Default(), ReindexOptions{})
	require.NoError(t, err)
	assert.True(t, stats.Complete)
	assert.Empty(t, stats.NextCursor)
}

func TestEntityIDFromKey(t *testing.T) {
	log := slog.Default()
	id := base58.Encode([]byte("some-entity"))

	t.Run("decodes an entity key", func(t *testing.T) {
		got, ok := entityIDFromKey(log, "/p/entity/", "/p/entity/"+id)
		require.True(t, ok)
		assert.Equal(t, Id("some-entity"), got)
	})

	t.Run("skips session attribute keys", func(t *testing.T) {
		_, ok := entityIDFromKey(log, "/p/entity/", "/p/entity/"+id+"/session/"+id)
		assert.False(t, ok)
	})

	t.Run("tolerates a store prefix containing /session/", func(t *testing.T) {
		// Matching on the whole key would skip every entity in a store whose
		// prefix happens to contain this segment, and the pass would report a
		// clean reindex having indexed nothing.
		prefix := "/tenants/session/acme/entity/"
		got, ok := entityIDFromKey(log, prefix, prefix+id)
		require.True(t, ok)
		assert.Equal(t, Id("some-entity"), got)
	})

	t.Run("skips the bare prefix", func(t *testing.T) {
		_, ok := entityIDFromKey(log, "/p/entity/", "/p/entity/")
		assert.False(t, ok)
	})

	t.Run("skips undecodable keys", func(t *testing.T) {
		_, ok := entityIDFromKey(log, "/p/entity/", "/p/entity/not!valid!base58")
		assert.False(t, ok)
	})
}

// TestReindex_CountsFailedEntities pins the distinction between a pass that
// reached the end of the keyspace and one that actually indexed everything.
// Without the count, an unreadable entity is silently skipped, the pass reports
// Complete, and the caller stamps a schema hash the store does not match.
func TestReindex_CountsFailedEntities(t *testing.T) {
	store, client := setupReindexTestStore(t)
	ctx := t.Context()

	_, err := store.CreateEntity(ctx, New(
		Ident, "test/kind",
		Doc, "a kind",
		Cardinality, CardinalityOne,
		Type, TypeStr,
		Index, true,
	))
	require.NoError(t, err)

	_, err = store.CreateEntity(ctx, New(
		Ident, "entity-1",
		String(Id("test/kind"), "widget"),
	))
	require.NoError(t, err)

	clean, err := store.Reindex(ctx, slog.Default(), ReindexOptions{})
	require.NoError(t, err)
	require.True(t, clean.Complete)
	require.Zero(t, clean.EntitiesFailed, "healthy store must report no failures")

	// Write an entity key whose value can't be decoded, standing in for any
	// entity the pass can read the key of but not load.
	_, err = client.Put(ctx,
		store.Prefix()+"/entity/"+base58.Encode([]byte("corrupt-entity")),
		"this is not a valid encoded entity")
	require.NoError(t, err)

	stats, err := store.Reindex(ctx, slog.Default(), ReindexOptions{})
	require.NoError(t, err)
	assert.True(t, stats.Complete, "scan still reaches the end of the keyspace")
	assert.Positive(t, stats.EntitiesFailed, "unreadable entity must be counted as a failure")
}

func TestReindex_StaleCleanup(t *testing.T) {
	store, client := setupReindexTestStore(t)
	ctx := t.Context()

	// Create an indexed attribute schema
	_, err := store.CreateEntity(ctx, New(
		Ident, "test/kind",
		Doc, "a kind",
		Cardinality, CardinalityOne,
		Type, TypeStr,
		Index, true,
	))
	require.NoError(t, err)

	// Create an entity
	e1, err := store.CreateEntity(ctx, New(
		Ident, "entity-1",
		String(Id("test/kind"), "widget"),
	))
	require.NoError(t, err)

	// Manually insert a stale collection entry pointing to a non-existent entity
	fakeEntityID := Id("fake/nonexistent")
	fakeKey := base58.Encode([]byte(fakeEntityID))
	staleKey := store.Prefix() + "/collections/test_kind_widget/" + fakeKey
	_, err = client.Put(ctx, staleKey, string(fakeEntityID))
	require.NoError(t, err)

	// Run reindex with stale cleanup
	stats, err := store.Reindex(ctx, slog.Default(), ReindexOptions{
		DryRun:       false,
		CleanupStale: true,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), stats.StaleEntriesFound)
	assert.Equal(t, int64(1), stats.StaleEntriesRemoved)

	// Verify only the real entity is in the index
	ids, err := store.ListIndex(ctx, String(Id("test/kind"), "widget"))
	require.NoError(t, err)
	assert.Len(t, ids, 1)
	assert.Equal(t, e1.Id(), ids[0])
}

func TestReindex_DryRun(t *testing.T) {
	store, client := setupReindexTestStore(t)
	ctx := t.Context()

	// Create an indexed attribute schema
	_, err := store.CreateEntity(ctx, New(
		Ident, "test/kind",
		Doc, "a kind",
		Cardinality, CardinalityOne,
		Type, TypeStr,
		Index, true,
	))
	require.NoError(t, err)

	// Create entities
	_, err = store.CreateEntity(ctx, New(
		Ident, "entity-1",
		String(Id("test/kind"), "widget"),
	))
	require.NoError(t, err)

	// Delete all collection entries
	collectionPrefix := store.Prefix() + "/collections/"
	_, err = client.Delete(ctx, collectionPrefix, clientv3.WithPrefix())
	require.NoError(t, err)

	// Run dry-run reindex
	stats, err := store.Reindex(ctx, slog.Default(), ReindexOptions{
		DryRun:       true,
		CleanupStale: false,
	})
	require.NoError(t, err)
	assert.Greater(t, stats.EntitiesProcessed, int64(0))
	assert.Equal(t, int64(0), stats.IndexesRebuilt) // No writes in dry-run

	// Verify indexes are still missing (dry-run didn't write)
	ids, err := store.ListIndex(ctx, String(Id("test/kind"), "widget"))
	require.NoError(t, err)
	assert.Len(t, ids, 0)
}
