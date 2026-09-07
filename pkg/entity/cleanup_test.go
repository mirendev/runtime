package entity

import (
	"log/slog"
	"testing"

	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// seedStaleEntry writes a collection entry under the given collection segment
// pointing at an entity id that does not exist, mimicking a leaked index entry.
func seedStaleEntry(t *testing.T, store *EtcdStore, client *clientv3.Client, collection string, entityID Id) string {
	t.Helper()
	key := store.Prefix() + "/collections/" + collection + "/" + base58.Encode([]byte(entityID))
	_, err := client.Put(t.Context(), key, string(entityID))
	require.NoError(t, err)
	return key
}

func TestCleanup_RemovesStaleKeepsLive(t *testing.T) {
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

	live, err := store.CreateEntity(ctx, New(
		Ident, "entity-1",
		String(Id("test/kind"), "widget"),
	))
	require.NoError(t, err)

	seedStaleEntry(t, store, client, "test_kind_widget", Id("fake/nonexistent"))

	stats, err := store.CleanupStaleCollectionEntries(ctx, slog.Default(), CleanupOptions{})
	require.NoError(t, err)
	assert.EqualValues(t, 1, stats.StaleEntriesFound)
	assert.EqualValues(t, 1, stats.OrphanedEntriesFound,
		"the entity is gone, so this is an orphan and not a mismatch")
	assert.EqualValues(t, 0, stats.MismatchedEntriesFound)
	assert.EqualValues(t, 1, stats.StaleEntriesRemoved)
	assert.EqualValues(t, 0, stats.CASConflicts)
	// Removal is attributed to the specific collection so a persistent leak is
	// visible per-collection rather than only in aggregate.
	assert.Equal(t, int64(1), stats.RemovedByCollection["test_kind_widget"])

	// The live entity's index entry must be untouched.
	ids, err := store.ListIndex(ctx, String(Id("test/kind"), "widget"))
	require.NoError(t, err)
	require.Len(t, ids, 1)
	assert.Equal(t, live.Id(), ids[0])
}

// collectionSegmentFor returns the collection segment the write path uses for
// attr. The segment is a hash, so it cannot be spelled out by hand.
func collectionSegmentFor(attr Attr) string {
	return tr.Replace(attr.CAS())
}

// TestCleanup_RemovesMismatchedEntryForLiveEntity seeds its entry at the real
// hashed segment rather than an invented one, so it is indistinguishable from an
// entry the store wrote itself.
func TestCleanup_RemovesMismatchedEntryForLiveEntity(t *testing.T) {
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

	before := String(Id("test/kind"), "widget")
	after := String(Id("test/kind"), "gadget")

	live, err := store.CreateEntity(ctx, New(
		Ident, "entity-1",
		before,
	))
	require.NoError(t, err)

	_, err = store.ReplaceEntity(ctx, New(
		Ref(DBId, live.Id()),
		Any(Ident, "entity-1"),
		after,
	))
	require.NoError(t, err)

	// Re-seed the entry the old writer would have left under the replaced value.
	seedStaleEntry(t, store, client, collectionSegmentFor(before), live.Id())

	ids, err := store.ListIndex(ctx, before)
	require.NoError(t, err)
	require.Contains(t, ids, live.Id(), "the seeded entry must be a real index hit before the sweep")

	stats, err := store.CleanupStaleCollectionEntries(ctx, slog.Default(), CleanupOptions{})
	require.NoError(t, err)
	assert.EqualValues(t, 1, stats.StaleEntriesFound)
	assert.EqualValues(t, 1, stats.MismatchedEntriesFound,
		"the entity exists, so this is a mismatch and not an orphan")
	assert.EqualValues(t, 0, stats.OrphanedEntriesFound)
	assert.EqualValues(t, 1, stats.StaleEntriesRemoved)

	ids, err = store.ListIndex(ctx, before)
	require.NoError(t, err)
	assert.Empty(t, ids, "the replaced value must no longer return the entity")

	ids, err = store.ListIndex(ctx, after)
	require.NoError(t, err)
	assert.Equal(t, []Id{live.Id()}, ids, "the current value must still return it")

	got, err := store.GetEntity(ctx, live.Id())
	require.NoError(t, err)
	assert.NotNil(t, got, "repairing an index must never touch the entity")
}

// TestCleanup_LeavesUndecodableEntityAlone covers the gap between "the entity is
// gone" and "the entity is here but I cannot read it". Both come back as a nil
// from the batched read, and treating the second as the first deletes the index
// entries of an entity that still exists, which is the one outcome the sweep
// must never produce.
func TestCleanup_LeavesUndecodableEntityAlone(t *testing.T) {
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

	indexed := String(Id("test/kind"), "widget")
	live, err := store.CreateEntity(ctx, New(
		Ident, "entity-1",
		indexed,
	))
	require.NoError(t, err)

	ids, err := store.ListIndex(ctx, indexed)
	require.NoError(t, err)
	require.Contains(t, ids, live.Id())

	// Corrupt the stored payload, leaving the key itself in place: the entity
	// exists, we just cannot say anything about its attributes.
	_, err = client.Put(ctx, store.Prefix()+"/entity/"+base58.Encode([]byte(live.Id())),
		"\xff\xff not valid cbor \xff\xff")
	require.NoError(t, err)

	stats, err := store.CleanupStaleCollectionEntries(ctx, slog.Default(), CleanupOptions{})
	require.NoError(t, err)

	assert.EqualValues(t, 0, stats.StaleEntriesFound,
		"an entity we cannot read is not an entity we can prove is gone")
	assert.EqualValues(t, 0, stats.OrphanedEntriesFound)
	assert.EqualValues(t, 0, stats.StaleEntriesRemoved)

	ids, err = store.ListIndex(ctx, indexed)
	require.NoError(t, err)
	assert.Contains(t, ids, live.Id(), "the index entry must survive")
}

// TestCleanup_LeavesEntityWithUnreadableSessionAttrAlone is the session-scoped
// half of the same hazard. An entity whose session blob will not decode comes
// back missing those attributes, and judging it by what it carries then reads
// the absence as "unjustified". No session attribute is indexed today, so this
// pins the behaviour before that changes rather than after.
func TestCleanup_LeavesEntityWithUnreadableSessionAttrAlone(t *testing.T) {
	store, client := setupReindexTestStore(t)
	ctx := t.Context()

	_, err := store.CreateEntity(ctx, New(
		Ident, "test/session-kind",
		Doc, "an indexed session-scoped attribute",
		Cardinality, CardinalityOne,
		Type, TypeStr,
		Session, true,
		Index, true,
	))
	require.NoError(t, err)

	sid, err := store.CreateSession(ctx, 30)
	require.NoError(t, err)

	indexed := String(Id("test/session-kind"), "widget")
	live, err := store.CreateEntity(ctx, New([]Attr{indexed}), WithSession(sid))
	require.NoError(t, err)

	ids, err := store.ListIndex(ctx, indexed)
	require.NoError(t, err)
	require.Contains(t, ids, live.Id(), "the session attribute must be indexed to begin with")

	// Corrupt the session blob, leaving the entity key itself intact.
	sessionPrefix := store.Prefix() + "/entity/" + base58.Encode([]byte(live.Id())) + "/session/"
	resp, err := client.Get(ctx, sessionPrefix, clientv3.WithPrefix())
	require.NoError(t, err)
	require.NotEmpty(t, resp.Kvs, "expected a session blob to corrupt")
	_, err = client.Put(ctx, string(resp.Kvs[0].Key), "\xff\xff not valid cbor \xff\xff")
	require.NoError(t, err)

	stats, err := store.CleanupStaleCollectionEntries(ctx, slog.Default(), CleanupOptions{})
	require.NoError(t, err)
	assert.EqualValues(t, 0, stats.StaleEntriesFound,
		"an entity whose session attributes will not decode is not an entity missing them")

	ids, err = store.ListIndex(ctx, indexed)
	require.NoError(t, err)
	assert.Contains(t, ids, live.Id(), "the index entry must survive")
}

// TestCleanup_LeavesAConsistentStoreAlone is the safety net for the mismatch
// verdict: a bug in the justified set shows up as deletions on a store with
// nothing wrong with it, system and schema entities included.
func TestCleanup_LeavesAConsistentStoreAlone(t *testing.T) {
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
		Ident, "test/labels",
		Doc, "an indexed cardinality-many attribute",
		Cardinality, CardinalityMany,
		Type, TypeStr,
		Index, true,
	))
	require.NoError(t, err)

	for _, name := range []string{"one", "two", "three"} {
		_, err = store.CreateEntity(ctx, New(
			Any(Ident, name),
			String(Id("test/kind"), "widget"),
			String(Id("test/labels"), "alpha"),
			String(Id("test/labels"), "beta"),
		))
		require.NoError(t, err)
	}

	stats, err := store.CleanupStaleCollectionEntries(ctx, slog.Default(), CleanupOptions{})
	require.NoError(t, err)

	assert.Positive(t, stats.CollectionEntriesScanned, "the sweep must actually have looked at entries")
	assert.EqualValues(t, 0, stats.StaleEntriesFound,
		"a consistent store has nothing stale; removals here mean the justified set is wrong")
	assert.EqualValues(t, 0, stats.StaleEntriesRemoved)

	ids, err := store.ListIndex(ctx, String(Id("test/labels"), "alpha"))
	require.NoError(t, err)
	assert.Len(t, ids, 3, "every cardinality-many value must survive the sweep")
}

func TestCleanup_DryRunRemovesNothing(t *testing.T) {
	store, client := setupReindexTestStore(t)
	ctx := t.Context()

	key := seedStaleEntry(t, store, client, "test_kind_widget", Id("fake/nonexistent"))

	stats, err := store.CleanupStaleCollectionEntries(ctx, slog.Default(), CleanupOptions{DryRun: true})
	require.NoError(t, err)
	assert.EqualValues(t, 1, stats.StaleEntriesFound)
	assert.EqualValues(t, 0, stats.StaleEntriesRemoved)

	resp, err := client.Get(ctx, key)
	require.NoError(t, err)
	assert.Len(t, resp.Kvs, 1, "dry run must not delete")
}

func TestCleanup_MaxDeletesBoundsAndConverges(t *testing.T) {
	store, client := setupReindexTestStore(t)
	ctx := t.Context()

	for _, id := range []Id{"fake/a", "fake/b", "fake/c"} {
		seedStaleEntry(t, store, client, "test_kind_widget", id)
	}

	// First bounded sweep removes only MaxDeletes. The streaming sweep stops
	// scanning once the budget is hit, so it need not have seen every stale entry.
	first, err := store.CleanupStaleCollectionEntries(ctx, slog.Default(), CleanupOptions{MaxDeletes: 2})
	require.NoError(t, err)
	assert.EqualValues(t, 2, first.StaleEntriesRemoved)

	// A subsequent sweep drains the remainder: the pass converges over time.
	second, err := store.CleanupStaleCollectionEntries(ctx, slog.Default(), CleanupOptions{MaxDeletes: 2})
	require.NoError(t, err)
	assert.EqualValues(t, 1, second.StaleEntriesFound)
	assert.EqualValues(t, 1, second.StaleEntriesRemoved)

	// And once drained, a sweep finds nothing.
	third, err := store.CleanupStaleCollectionEntries(ctx, slog.Default(), CleanupOptions{MaxDeletes: 2})
	require.NoError(t, err)
	assert.EqualValues(t, 0, third.StaleEntriesFound)
	assert.EqualValues(t, 0, third.StaleEntriesRemoved)
}

// TestCleanup_CASGuardSkipsRecreatedSlot proves the ABA guard: if a collection
// slot's mod-revision changes between the scan that captured it and the delete
// (i.e. it was legitimately recreated/re-leased), the CAS compare fails and the
// entry is left alone rather than clobbered.
func TestCleanup_CASGuardSkipsRecreatedSlot(t *testing.T) {
	store, client := setupReindexTestStore(t)
	ctx := t.Context()

	key := seedStaleEntry(t, store, client, "test_kind_widget", Id("fake/x"))

	// Capture the mod-revision as a scan would have, then mutate the slot so its
	// current mod-revision no longer matches.
	got, err := client.Get(ctx, key)
	require.NoError(t, err)
	require.Len(t, got.Kvs, 1)
	scannedModRev := got.Kvs[0].ModRevision

	_, err = client.Put(ctx, key, "fake/x-recreated")
	require.NoError(t, err)

	stale := []staleCollectionEntry{{key: key, modRev: scannedModRev, collection: "test_kind_widget"}}

	stats := &CleanupStats{RemovedByCollection: map[string]int64{}}
	store.deleteStaleBatch(ctx, slog.Default(), stale, stats)
	assert.EqualValues(t, 0, stats.StaleEntriesRemoved, "stale delete must not clobber a recreated slot")
	assert.EqualValues(t, 1, stats.CASConflicts)

	resp, err := client.Get(ctx, key)
	require.NoError(t, err)
	require.Len(t, resp.Kvs, 1, "recreated slot must survive")

	// With the up-to-date mod-revision, the delete goes through.
	stale[0].modRev = resp.Kvs[0].ModRevision
	stats2 := &CleanupStats{RemovedByCollection: map[string]int64{}}
	store.deleteStaleBatch(ctx, slog.Default(), stale, stats2)
	assert.EqualValues(t, 1, stats2.StaleEntriesRemoved)

	resp, err = client.Get(ctx, key)
	require.NoError(t, err)
	assert.Len(t, resp.Kvs, 0)
}

// TestCleanup_BatchFallbackOnConflict proves that when a single batched delete
// can't commit because one entry changed, the per-entry fallback still removes
// the unchanged entries and only the changed one is counted as a conflict.
func TestCleanup_BatchFallbackOnConflict(t *testing.T) {
	store, client := setupReindexTestStore(t)
	ctx := t.Context()

	goodKey := seedStaleEntry(t, store, client, "test_kind_widget", Id("fake/good"))
	changedKey := seedStaleEntry(t, store, client, "test_kind_widget", Id("fake/changed"))

	get := func(key string) int64 {
		r, err := client.Get(ctx, key)
		require.NoError(t, err)
		require.Len(t, r.Kvs, 1)
		return r.Kvs[0].ModRevision
	}

	batch := []staleCollectionEntry{
		{key: goodKey, modRev: get(goodKey), collection: "test_kind_widget"},
		{key: changedKey, modRev: get(changedKey), collection: "test_kind_widget"},
	}

	// Mutate one entry so the batched compare fails and forces the fallback path.
	_, err := client.Put(ctx, changedKey, "fake/changed-again")
	require.NoError(t, err)

	stats := &CleanupStats{RemovedByCollection: map[string]int64{}}
	store.deleteStaleBatch(ctx, slog.Default(), batch, stats)
	assert.EqualValues(t, 1, stats.StaleEntriesRemoved, "unchanged entry removed via fallback")
	assert.EqualValues(t, 1, stats.CASConflicts, "changed entry skipped, not clobbered")

	// The good entry is gone; the changed one survives with its new value.
	r, err := client.Get(ctx, goodKey)
	require.NoError(t, err)
	assert.Len(t, r.Kvs, 0)
	r, err = client.Get(ctx, changedKey)
	require.NoError(t, err)
	require.Len(t, r.Kvs, 1)
	assert.Equal(t, "fake/changed-again", string(r.Kvs[0].Value))
}
