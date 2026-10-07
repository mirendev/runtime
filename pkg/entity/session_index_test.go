package entity

import (
	"encoding/binary"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clientv3 "go.etcd.io/etcd/client/v3"

	"miren.dev/runtime/pkg/cond"
)

// These tests pin the raw index entries MIR-1991 gives an entity written under
// a session: its matches, and the presence markers beside them (see the index
// layout comment above indexWrite). The conformance suite covers what callers
// observe; this covers the keys and leases behind it, which only EtcdStore has.

// indexEntry is one collection key as the tests inspect it.
type indexEntry struct {
	collection string
	session    string // empty for a match
	lease      int64
}

// sessionIndexFixture is a store with an indexed attribute and a session
// attribute registered, plus one live session.
type sessionIndexFixture struct {
	store    *EtcdStore
	client   *clientv3.Client
	token    []byte
	sessPart string
	lease    int64
}

func newSessionIndexFixture(t *testing.T) *sessionIndexFixture {
	t.Helper()
	store, client := setupTestEtcdStore(t)
	ctx := t.Context()

	_, err := store.CreateEntity(ctx, New(
		Ident, "test/kind",
		Doc, "indexed string",
		Cardinality, CardinalityOne,
		Type, TypeStr,
		Index, true,
	))
	require.NoError(t, err)

	_, err = store.CreateEntity(ctx, New(
		Ident, "test/state",
		Doc, "session-scoped string",
		Cardinality, CardinalityOne,
		Type, TypeStr,
		Session, true,
	))
	require.NoError(t, err)

	token, err := store.CreateSession(ctx, 30)
	require.NoError(t, err)
	lease, _ := binary.Varint(token)

	return &sessionIndexFixture{
		store:    store,
		client:   client,
		token:    token,
		sessPart: base58.Encode(token),
		lease:    lease,
	}
}

// entries returns every collection entry naming id.
func (f *sessionIndexFixture) entries(t *testing.T, id Id) []indexEntry {
	t.Helper()
	collectionPrefix := f.store.Prefix() + "/collections/"
	var out []indexEntry
	for _, kv := range collectionKVsForEntity(t, f.client, f.store.Prefix(), id) {
		key := string(kv.Key)
		e := indexEntry{collection: collectionFromKey(key, collectionPrefix), lease: kv.Lease}
		if strings.Count(strings.TrimPrefix(key, collectionPrefix), "/") > 1 {
			e.session = key[strings.LastIndexByte(key, '/')+1:]
		}
		out = append(out, e)
	}
	return out
}

func col(attr Attr) string {
	return tr.Replace(attr.CAS())
}

// plainKey and sessionKey build entries by hand, so a test can seed the
// shapes writers before MIR-1991 left behind.
func (f *sessionIndexFixture) plainKey(id Id, attr Attr) string {
	return fmt.Sprintf("%s/collections/%s/%s", f.store.Prefix(), col(attr), base58.Encode([]byte(id)))
}

func (f *sessionIndexFixture) sessionKey(id Id, attr Attr) string {
	return f.plainKey(id, attr) + "/" + f.sessPart
}

func TestEtcdStore_IndexEntriesByMeaning(t *testing.T) {
	f := newSessionIndexFixture(t)
	ctx := t.Context()

	kind := String(Id("test/kind"), "runner")
	state := String(Id("test/state"), "ready")

	t.Run("session write of an unbound entity", func(t *testing.T) {
		ent, err := f.store.CreateEntity(ctx, New(Any(Ident, "node-1"), kind, state), WithSession(f.token))
		require.NoError(t, err)

		assert.ElementsMatch(t, []indexEntry{
			{collection: col(kind)},
			{collection: col(kind), session: f.sessPart, lease: f.lease},
		}, f.entries(t, ent.Id()),
			"the value gets its unleased match and the session's marker; the session value is not indexed")

		// Re-asserting through an update rewrites the same keys.
		_, err = f.store.UpdateEntity(ctx, ent.Id(), New(state), WithSession(f.token))
		require.NoError(t, err)
		assert.Len(t, f.entries(t, ent.Id()), 2)
	})

	t.Run("session write that stores no session attributes", func(t *testing.T) {
		ent, err := f.store.CreateEntity(ctx, New(Any(Ident, "node-0"), kind), WithSession(f.token))
		require.NoError(t, err)

		assert.Equal(t, []indexEntry{{collection: col(kind)}}, f.entries(t, ent.Id()),
			"a session that holds nothing on the entity has no lapse to announce, so no marker")
	})

	t.Run("write under a session that leaves its blob be", func(t *testing.T) {
		ent, err := f.store.CreateEntity(ctx, New(Any(Ident, "node-3"), kind, state), WithSession(f.token))
		require.NoError(t, err)

		worker := String(Id("test/kind"), "worker")
		_, err = f.store.ReplaceEntity(ctx, New(Ref(DBId, ent.Id()), Any(Ident, "node-3"), worker), WithSession(f.token))
		require.NoError(t, err)

		assert.ElementsMatch(t, []indexEntry{
			{collection: col(worker)},
			{collection: col(worker), session: f.sessPart, lease: f.lease},
		}, f.entries(t, ent.Id()), "the session still holds its blob, so the new value gets its marker")
	})

	t.Run("bound entity", func(t *testing.T) {
		ent, err := f.store.CreateEntity(ctx, New(Any(Ident, "lease-1"), kind, state), BondToSession(f.token))
		require.NoError(t, err)

		assert.ElementsMatch(t, []indexEntry{
			{collection: col(kind), lease: f.lease},
			{collection: col(kind), session: f.sessPart, lease: f.lease},
		}, f.entries(t, ent.Id()))
	})

	t.Run("changing a value under a session moves its marker", func(t *testing.T) {
		ent, err := f.store.CreateEntity(ctx, New(Any(Ident, "node-2"), kind, state), WithSession(f.token))
		require.NoError(t, err)
		// Another session's marker on the old value goes with it too.
		other, err := f.store.CreateSession(ctx, 30)
		require.NoError(t, err)
		otherLease, _ := binary.Varint(other)
		_, err = f.client.Put(ctx, f.plainKey(ent.Id(), kind)+"/"+base58.Encode(other), ent.Id().String(),
			clientv3.WithLease(clientv3.LeaseID(otherLease)))
		require.NoError(t, err)

		worker := String(Id("test/kind"), "worker")
		_, err = f.store.UpdateEntity(ctx, ent.Id(), New(worker), WithSession(f.token))
		require.NoError(t, err)

		assert.ElementsMatch(t, []indexEntry{
			{collection: col(worker)},
			{collection: col(worker), session: f.sessPart, lease: f.lease},
		}, f.entries(t, ent.Id()), "nothing may be left under the old value")
	})
}

// Only what the entity key stores is indexed. A field nested in a session
// attribute lives in the session's blob, whatever its own schema says, so it
// gets no entry at all.
func TestEtcdStore_ValuesInSessionBlobsAreNotIndexed(t *testing.T) {
	f := newSessionIndexFixture(t)
	ctx := t.Context()

	_, err := f.store.CreateEntity(ctx, New(
		Ident, "test/session-spec",
		Doc, "session-scoped component",
		Cardinality, CardinalityOne,
		Type, TypeComponent,
		Session, true,
	))
	require.NoError(t, err)

	kind := String(Id("test/kind"), "nested")
	ent, err := f.store.CreateEntity(ctx, New(
		Any(Ident, "nested-1"),
		Component(Id("test/session-spec"), []Attr{kind}),
	), WithSession(f.token))
	require.NoError(t, err)

	assert.Empty(t, f.entries(t, ent.Id()))
	ids, err := f.store.ListIndex(ctx, kind)
	require.NoError(t, err)
	assert.Empty(t, ids)
}

func TestEtcdStore_DeleteEntityClearsEverySessionShape(t *testing.T) {
	f := newSessionIndexFixture(t)
	ctx := t.Context()

	kind := String(Id("test/kind"), "runner")
	ent, err := f.store.CreateEntity(ctx, New(Any(Ident, "node-1"), kind, String(Id("test/state"), "ready")),
		WithSession(f.token))
	require.NoError(t, err)

	// A marker from another session the read never saw.
	other, err := f.store.CreateSession(ctx, 30)
	require.NoError(t, err)
	otherLease, _ := binary.Varint(other)
	_, err = f.client.Put(ctx, f.plainKey(ent.Id(), kind)+"/"+base58.Encode(other), ent.Id().String(),
		clientv3.WithLease(clientv3.LeaseID(otherLease)))
	require.NoError(t, err)

	require.NoError(t, f.store.DeleteEntity(ctx, ent.Id()))

	assert.Empty(t, f.entries(t, ent.Id()), "no entry may outlive the entity, marker or match")

	blobs, err := f.client.Get(ctx, f.store.buildKey(ent.Id())+"/session/", clientv3.WithPrefix(), clientv3.WithCountOnly())
	require.NoError(t, err)
	assert.Zero(t, blobs.Count, "session blobs go with the entity")
}

// Deleting an entity must fit in one etcd transaction whenever creating it
// did, however many shapes of key each value has.
func TestEtcdStore_DeleteFitsWhereCreateDid(t *testing.T) {
	f := newSessionIndexFixture(t)
	ctx := t.Context()

	_, err := f.store.CreateEntity(ctx, New(
		Ident, "test/tags",
		Doc, "indexed many-valued string",
		Cardinality, CardinalityMany,
		Type, TypeStr,
		Index, true,
	))
	require.NoError(t, err)

	// The entity key and 127 entries are exactly etcd's default limit of 128
	// ops, the most one create can carry.
	var attrs []Attr
	for i := range 127 {
		attrs = append(attrs, String(Id("test/tags"), fmt.Sprintf("tag-%d", i)))
	}
	ent, err := f.store.CreateEntity(ctx, New(attrs))
	require.NoError(t, err)

	require.NoError(t, f.store.DeleteEntity(ctx, ent.Id()))
	assert.Empty(t, f.entries(t, ent.Id()))
}

// Writers before MIR-1991 left a session's marker under every value an entity
// moved off, and those stay until their session lapses or the sweep runs.
// Every reader of matches has to look past one, the non-paged ones included,
// or a watcher's snapshot gets a member whose marker delete it will then never
// hear as a removal.
func TestEtcdStore_LeftoverMarkerIsNotAMatch(t *testing.T) {
	f := newSessionIndexFixture(t)
	ctx := t.Context()

	runner := String(Id("test/kind"), "runner")
	ent, err := f.store.CreateEntity(ctx, New(Any(Ident, "node-1"), String(Id("test/kind"), "worker"),
		String(Id("test/state"), "ready")), WithSession(f.token))
	require.NoError(t, err)

	_, err = f.client.Put(ctx, f.sessionKey(ent.Id(), runner), ent.Id().String(),
		clientv3.WithLease(clientv3.LeaseID(f.lease)))
	require.NoError(t, err)

	ids, _, err := f.store.ListIndexRevision(ctx, runner)
	require.NoError(t, err)
	assert.Empty(t, ids)

	page, err := f.store.ListIndexPage(ctx, runner, "", 10)
	require.NoError(t, err)
	assert.Empty(t, page.Ids)
	assert.Zero(t, page.Total)

	_, err = f.store.GetOneIndex(ctx, runner)
	assert.ErrorIs(t, err, cond.ErrNotFound{})
}

// TestCleanup_DrainsPreMIR1991Shapes is the migration: a marker the old writers
// left under a value the entity has dropped is judged stale and removed, while
// the entries the new writers produce, markers included, are left alone.
func TestCleanup_DrainsPreMIR1991Shapes(t *testing.T) {
	f := newSessionIndexFixture(t)
	ctx := t.Context()

	kind := String(Id("test/kind"), "runner")
	ent, err := f.store.CreateEntity(ctx, New(Any(Ident, "node-1"), kind, String(Id("test/state"), "ready")),
		WithSession(f.token))
	require.NoError(t, err)
	want := f.entries(t, ent.Id())

	_, err = f.client.Put(ctx, f.sessionKey(ent.Id(), String(Id("test/kind"), "old")), ent.Id().String(),
		clientv3.WithLease(clientv3.LeaseID(f.lease)))
	require.NoError(t, err)
	require.Len(t, f.entries(t, ent.Id()), len(want)+1)

	stats, err := f.store.CleanupStaleCollectionEntries(ctx, slog.Default(), CleanupOptions{})
	require.NoError(t, err)
	assert.EqualValues(t, 1, stats.StaleEntriesRemoved)
	assert.EqualValues(t, 1, stats.MismatchedEntriesFound)
	assert.ElementsMatch(t, want, f.entries(t, ent.Id()))

	again, err := f.store.CleanupStaleCollectionEntries(ctx, slog.Default(), CleanupOptions{})
	require.NoError(t, err)
	assert.Zero(t, again.StaleEntriesFound, "matches and live markers are justified")
}

// TestReindex_RebuildsMatchesOnly pins what the reindex big hammer restores:
// every match, under the entity key's own lease, so a bound entity's matches
// still go with its session. Markers are its sessions' to write, and come back
// with their next write.
func TestReindex_RebuildsMatchesOnly(t *testing.T) {
	f := newSessionIndexFixture(t)
	ctx := t.Context()

	kind := String(Id("test/kind"), "runner")
	state := String(Id("test/state"), "ready")
	node, err := f.store.CreateEntity(ctx, New(Any(Ident, "node-1"), kind, state), WithSession(f.token))
	require.NoError(t, err)
	bound, err := f.store.CreateEntity(ctx, New(Any(Ident, "lease-1"), kind), BondToSession(f.token))
	require.NoError(t, err)

	_, err = f.client.Delete(ctx, f.store.Prefix()+"/collections/", clientv3.WithPrefix())
	require.NoError(t, err)

	stats, err := f.store.Reindex(ctx, slog.Default(), ReindexOptions{})
	require.NoError(t, err)
	require.Zero(t, stats.EntitiesFailed)

	assert.Equal(t, []indexEntry{{collection: col(kind)}}, f.entries(t, node.Id()))
	assert.Equal(t, []indexEntry{{collection: col(kind), lease: f.lease}}, f.entries(t, bound.Id()))

	_, err = f.store.UpdateEntity(ctx, node.Id(), New(state), WithSession(f.token))
	require.NoError(t, err)
	assert.Contains(t, f.entries(t, node.Id()),
		indexEntry{collection: col(kind), session: f.sessPart, lease: f.lease},
		"the session's next write puts its marker back")
}

// Reindex reads an entity's key and lease before writing matches from them.
// A write in between that unbinds the entity has indexed it already, with no
// lease; reindex must not put the old session's lease back on its match, or
// revoking that session would drop a live entity from the index.
func TestReindex_LeavesEntityChangedMidPassToItsWriter(t *testing.T) {
	f := newSessionIndexFixture(t)
	ctx := t.Context()

	kind := String(Id("test/kind"), "runner")
	ent, err := f.store.CreateEntity(ctx, New(Any(Ident, "node-1"), kind), BondToSession(f.token))
	require.NoError(t, err)

	reindexEntityRaceHook = func(id Id) {
		if id != ent.Id() {
			return
		}
		reindexEntityRaceHook = nil
		_, err := f.store.UpdateEntity(ctx, ent.Id(), New(kind))
		require.NoError(t, err)
	}
	t.Cleanup(func() { reindexEntityRaceHook = nil })

	stats, err := f.store.Reindex(ctx, slog.Default(), ReindexOptions{})
	require.NoError(t, err)
	require.Zero(t, stats.EntitiesFailed)

	assert.Equal(t, []indexEntry{{collection: col(kind)}}, f.entries(t, ent.Id()),
		"the unbinding write's unleased match must stand")
}

// A value repeated across components is one set of index keys, written once.
// Deleting it once per repeat would need more transaction ops than the create
// did, and an entity that can be created has to be deletable.
func TestEtcdStore_DeleteCountsARepeatedValueOnce(t *testing.T) {
	f := newSessionIndexFixture(t)
	ctx := t.Context()

	_, err := f.store.CreateEntity(ctx, New(
		Ident, "test/spec",
		Doc, "many-valued component",
		Cardinality, CardinalityMany,
		Type, TypeComponent,
	))
	require.NoError(t, err)

	kind := String(Id("test/kind"), "shared")
	var attrs []Attr
	for i := range 128 {
		attrs = append(attrs, Component(Id("test/spec"), []Attr{kind, String(Doc, fmt.Sprintf("spec-%d", i))}))
	}
	ent, err := f.store.CreateEntity(ctx, New(attrs))
	require.NoError(t, err)
	require.Equal(t, []indexEntry{{collection: col(kind)}}, f.entries(t, ent.Id()))

	require.NoError(t, f.store.DeleteEntity(ctx, ent.Id()))
	assert.Empty(t, f.entries(t, ent.Id()))
}

// A schema change can newly index more of an entity's values than any one
// write ever put. Reindex has to backfill all of them, whatever etcd's limit
// on one transaction.
func TestReindex_BackfillsMoreValuesThanOneTransactionHolds(t *testing.T) {
	f := newSessionIndexFixture(t)
	ctx := t.Context()

	_, err := f.store.CreateEntity(ctx, New(
		Ident, "test/tags",
		Doc, "many-valued string, not yet indexed",
		Cardinality, CardinalityMany,
		Type, TypeStr,
	))
	require.NoError(t, err)

	var attrs []Attr
	for i := range 129 {
		attrs = append(attrs, String(Id("test/tags"), fmt.Sprintf("tag-%d", i)))
	}
	ent, err := f.store.CreateEntity(ctx, New(attrs))
	require.NoError(t, err)
	require.Empty(t, f.entries(t, ent.Id()))

	_, err = f.store.UpdateEntity(ctx, Id("test/tags"), New(Index, true))
	require.NoError(t, err)

	stats, err := f.store.Reindex(ctx, slog.Default(), ReindexOptions{})
	require.NoError(t, err)
	assert.Zero(t, stats.EntitiesFailed)
	assert.Len(t, f.entries(t, ent.Id()), 129)
}
