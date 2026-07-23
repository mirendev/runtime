package entity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/cond"
)

// Store conformance suite.
//
// The codebase has two Store implementations: EtcdStore (production) and
// MockStore (the in-memory fake most unit tests run against). Their behavioral
// parity has historically been maintained by hand, with comments in mock.go
// like "Mirrors EtcdStore.UpdateEntity (store.go:687)". Nothing enforced it,
// so a consumer that relied on a subtle write semantic could pass against the
// mock and break against etcd (or vice versa). MIR-441 hit exactly this: saga
// storage leaned on EnsureEntity's create-if-absent semantics, the mock and
// etcd happened to agree, and the bug surfaced two layers up instead of here.
//
// These tests run the same scenarios against both backends so the fake is
// provably faithful to the real store on the semantics callers depend on.
// Where the mock is a deliberate approximation rather than a faithful model
// (historical revisions, sessions), the gap is documented with an explicit
// skipMock call so the divergence is recorded rather than hidden.
//
// The EtcdStore backend needs a reachable etcd (the dev environment); run via
// `make test`, `hack/it pkg/entity`, or `hack/run pkg/entity <name>`.

type storeBackend struct {
	name string
	new  func(t *testing.T) Store
}

// conformanceBackends returns every Store implementation behind a uniform
// factory. Each factory yields a fresh, isolated store with system entities
// initialized the same way production construction does.
func conformanceBackends() []storeBackend {
	return []storeBackend{
		{
			name: "MockStore",
			new: func(t *testing.T) Store {
				store := NewMockStore()
				// EtcdStore seeds system entities in NewEtcdStore; do the same
				// for the mock so both backends start from the same schema.
				require.NoError(t, InitSystemEntities(func(e *Entity) error {
					store.AddEntity(e.Id(), e)
					return nil
				}))
				return store
			},
		},
		{
			name: "EtcdStore",
			new: func(t *testing.T) Store {
				store, _ := setupTestEtcdStore(t)
				return store
			},
		},
	}
}

// runStoreConformance executes scenario against every backend as a subtest.
func runStoreConformance(t *testing.T, scenario func(t *testing.T, store Store)) {
	t.Helper()
	for _, backend := range conformanceBackends() {
		t.Run(backend.name, func(t *testing.T) {
			scenario(t, backend.new(t))
		})
	}
}

func isNotFound(err error) bool {
	return errors.Is(err, ErrEntityNotFound) || errors.Is(err, cond.ErrNotFound{})
}

// skipMock records a place where MockStore intentionally does not model
// EtcdStore. Keeping the scenario in the suite (skipped, with a reason) makes
// the divergence visible instead of silently absent.
func skipMock(t *testing.T, store Store, reason string) {
	t.Helper()
	if _, ok := store.(*MockStore); ok {
		t.Skipf("MockStore does not model EtcdStore here: %s", reason)
	}
}

// applyConformanceSchema registers the custom attributes the write/index
// scenarios rely on. Both backends read schema from the store itself, so this
// works uniformly once system entities are seeded.
func applyConformanceSchema(t *testing.T, store Store) {
	t.Helper()
	ctx := t.Context()

	_, err := store.CreateEntity(ctx, New(
		Ident, "conf/note",
		Doc, "cardinality-one string",
		Cardinality, CardinalityOne,
		Type, TypeStr,
	))
	require.NoError(t, err)

	_, err = store.CreateEntity(ctx, New(
		Ident, "conf/labels",
		Doc, "cardinality-many string",
		Cardinality, CardinalityMany,
		Type, TypeStr,
	))
	require.NoError(t, err)

	_, err = store.CreateEntity(ctx, New(
		Ident, "conf/ref",
		Doc, "indexed cardinality-one ref",
		Cardinality, CardinalityOne,
		Type, TypeRef,
		Index, true,
	))
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// Reads
// ---------------------------------------------------------------------------

func TestStoreConformance_GetEntity(t *testing.T) {
	runStoreConformance(t, func(t *testing.T, store Store) {
		ctx := t.Context()

		created, err := store.CreateEntity(ctx, New(
			Ref(DBId, Id("conf-get")),
			Any(Doc, "hello"),
		))
		require.NoError(t, err)

		got, err := store.GetEntity(ctx, created.Id())
		require.NoError(t, err)
		assert.Equal(t, created.Id(), got.Id())
		doc, ok := got.Get(Doc)
		require.True(t, ok)
		assert.Equal(t, "hello", doc.Value.String())
	})
}

func TestStoreConformance_GetMissing(t *testing.T) {
	runStoreConformance(t, func(t *testing.T, store Store) {
		_, err := store.GetEntity(t.Context(), Id("conf-does-not-exist"))
		assert.Error(t, err)
		assert.True(t, isNotFound(err), "get on a missing entity should report not-found, got: %v", err)
	})
}

func TestStoreConformance_GetEntities(t *testing.T) {
	runStoreConformance(t, func(t *testing.T, store Store) {
		ctx := t.Context()

		a, err := store.CreateEntity(ctx, New(Ref(DBId, Id("conf-batch-a"))))
		require.NoError(t, err)
		b, err := store.CreateEntity(ctx, New(Ref(DBId, Id("conf-batch-b"))))
		require.NoError(t, err)

		got, err := store.GetEntities(ctx, []Id{a.Id(), Id("conf-batch-missing"), b.Id()})
		require.NoError(t, err)
		require.Len(t, got, 3, "GetEntities must return one slot per requested id, positionally")
		require.NotNil(t, got[0])
		assert.Equal(t, a.Id(), got[0].Id())
		assert.Nil(t, got[1], "missing id must produce a nil slot, not be skipped")
		require.NotNil(t, got[2])
		assert.Equal(t, b.Id(), got[2].Id())
	})
}

func TestStoreConformance_GetAttributeSchema(t *testing.T) {
	runStoreConformance(t, func(t *testing.T, store Store) {
		applyConformanceSchema(t, store)

		schema, err := store.GetAttributeSchema(t.Context(), Id("conf/labels"))
		require.NoError(t, err)
		require.NotNil(t, schema)
		assert.True(t, schema.AllowMany, "conf/labels is declared cardinality-many")
	})
}

func TestStoreConformance_GetEntityAtRevision_Current(t *testing.T) {
	runStoreConformance(t, func(t *testing.T, store Store) {
		ctx := t.Context()
		created, err := store.CreateEntity(ctx, New(
			Ref(DBId, Id("conf-rev-current")),
			Any(Doc, "current"),
		))
		require.NoError(t, err)

		got, err := store.GetEntityAtRevision(ctx, created.Id(), created.GetRevision())
		require.NoError(t, err)
		doc, ok := got.Get(Doc)
		require.True(t, ok)
		assert.Equal(t, "current", doc.Value.String())
	})
}

// ---------------------------------------------------------------------------
// Writes
// ---------------------------------------------------------------------------

func TestStoreConformance_CreateEntity(t *testing.T) {
	runStoreConformance(t, func(t *testing.T, store Store) {
		created, err := store.CreateEntity(t.Context(), New(
			Any(Ident, "conf-create"),
			Any(Doc, "fresh"),
		))
		require.NoError(t, err)
		assert.NotEmpty(t, created.Id(), "create must assign an id")
		// Revision schemes differ between backends: MockStore numbers each
		// entity from 1, while EtcdStore stamps the global etcd revision (so a
		// fresh entity can be at revision 290). The shared contract callers can
		// rely on is "positive, and strictly increasing per entity across
		// writes" (the latter is pinned by Replace/Patch/Update below), not an
		// absolute starting value.
		assert.Positive(t, created.GetRevision(), "create must assign a positive revision")
	})
}

// TestStoreConformance_CreateRejectsMistypedDBId pins the MIR-601 contract: a
// db/id supplied with the wrong value kind (a bare string, KindString, rather
// than an entity.Id, KindId) must fail loudly on every backend instead of
// silently minting a fresh auto-id and discarding the caller's id. This is the
// mistake that surfaced in MIR-600, where createDiskLease passed a plain string
// for db/id. Before MIR-601, MockStore.CreateEntity skipped ForceID entirely,
// so the divergence hid here: production regenerated the id, but mock-backed
// tests keyed the entity under an empty id without complaint.
func TestStoreConformance_CreateRejectsMistypedDBId(t *testing.T) {
	runStoreConformance(t, func(t *testing.T, store Store) {
		assert.Panics(t, func() {
			_, _ = store.CreateEntity(t.Context(), New(
				DBId, "conf-mistyped-id",
				Any(Doc, "wrong kind"),
			))
		}, "a mistyped db/id (KindString) must fail loudly, not silently regenerate the id")
	})
}

// TestStoreConformance_CreateRejectsDuplicateId pins that CreateEntity is
// put-if-absent: a second create against an id that already exists (with
// different attributes) must fail with a conflict rather than silently
// overwrite the existing entity. Production (EtcdStore) enforces this with an
// etcd CreateRevision==0 transaction; MockStore must match it so mock-backed
// tests can't pass while relying on an overwrite that production rejects. This
// is the store-layer backstop behind runner_id uniqueness at Join (MIR-1225):
// two joins that resolve the same node/<runner_id> ident cannot clobber each
// other.
func TestStoreConformance_CreateRejectsDuplicateId(t *testing.T) {
	runStoreConformance(t, func(t *testing.T, store Store) {
		ctx := t.Context()
		id := Id("conf-create-dup")

		_, err := store.CreateEntity(ctx, New(
			Ref(DBId, id),
			Any(Doc, "first"),
		))
		require.NoError(t, err)

		_, err = store.CreateEntity(ctx, New(
			Ref(DBId, id),
			Any(Doc, "second"),
		))
		require.Error(t, err, "create against an existing id must fail")
		assert.True(t, errors.Is(err, cond.ErrConflict{}),
			"duplicate create should report a conflict, got: %v", err)

		// The original entity must be untouched by the rejected create.
		got, err := store.GetEntity(ctx, id)
		require.NoError(t, err)
		doc, ok := got.Get(Doc)
		require.True(t, ok)
		assert.Equal(t, "first", doc.Value.String(),
			"rejected create must not overwrite the existing entity")
	})
}

// TestStoreConformance_EnsureEntity pins the create-if-absent contract that
// saga storage depends on: the first Ensure creates and reports created=true;
// a second Ensure with the same id returns the existing entity with
// created=false and leaves its attributes untouched.
func TestStoreConformance_EnsureEntity(t *testing.T) {
	runStoreConformance(t, func(t *testing.T, store Store) {
		ctx := t.Context()
		id := Id("conf-ensure")

		ent, created, err := store.EnsureEntity(ctx, New(
			Ref(DBId, id),
			Any(Doc, "original"),
		))
		require.NoError(t, err)
		assert.True(t, created, "first ensure must create")
		assert.Equal(t, id, ent.Id())
		firstRev := ent.GetRevision()

		ent2, created2, err := store.EnsureEntity(ctx, New(
			Ref(DBId, id),
			Any(Doc, "should be ignored"),
		))
		require.NoError(t, err)
		assert.False(t, created2, "second ensure must not create")
		assert.Equal(t, firstRev, ent2.GetRevision(), "ensure on an existing entity must not bump revision")

		doc, ok := ent2.Get(Doc)
		require.True(t, ok)
		assert.Equal(t, "original", doc.Value.String(),
			"ensure must not overwrite an existing entity's attributes")
	})
}

func TestStoreConformance_EnsureRequiresDBId(t *testing.T) {
	runStoreConformance(t, func(t *testing.T, store Store) {
		_, _, err := store.EnsureEntity(t.Context(), New(
			Any(Doc, "no id"),
		))
		assert.Error(t, err, "ensure without db/id must fail")
	})
}

// TestStoreConformance_ReplaceEntity pins replace semantics: it requires an
// existing entity (errors when absent), overwrites all attributes, and bumps
// the revision.
func TestStoreConformance_ReplaceEntity(t *testing.T) {
	runStoreConformance(t, func(t *testing.T, store Store) {
		ctx := t.Context()
		id := Id("conf-replace")

		_, err := store.ReplaceEntity(ctx, New(
			Ref(DBId, id),
			Any(Doc, "nope"),
		))
		assert.Error(t, err, "replace on a missing entity must fail")
		assert.True(t, isNotFound(err), "replace-missing should report not-found, got: %v", err)

		created, _, err := store.EnsureEntity(ctx, New(
			Ref(DBId, id),
			Any(Doc, "before"),
		))
		require.NoError(t, err)
		beforeRev := created.GetRevision()

		replaced, err := store.ReplaceEntity(ctx, New(
			Ref(DBId, id),
			Any(Doc, "after"),
		))
		require.NoError(t, err)

		doc, ok := replaced.Get(Doc)
		require.True(t, ok)
		assert.Equal(t, "after", doc.Value.String(), "replace must overwrite attributes")
		assert.Greater(t, replaced.GetRevision(), beforeRev, "replace must bump revision")

		got, err := store.GetEntity(ctx, id)
		require.NoError(t, err)
		gotDoc, ok := got.Get(Doc)
		require.True(t, ok)
		assert.Equal(t, "after", gotDoc.Value.String())
	})
}

// TestStoreConformance_UpdateEntity pins the cardinality-aware merge: a
// cardinality-one attribute is replaced, a cardinality-many attribute
// accumulates. This is the behavior mock.go hand-mirrors from store.go.
func TestStoreConformance_UpdateEntity(t *testing.T) {
	runStoreConformance(t, func(t *testing.T, store Store) {
		ctx := t.Context()
		applyConformanceSchema(t, store)

		initial, err := store.CreateEntity(ctx, New(
			Any(Ident, "conf-update"),
			String(Id("conf/note"), "one"),
			String(Id("conf/labels"), "a"),
		))
		require.NoError(t, err)

		_, err = store.UpdateEntity(ctx, initial.Id(), New(
			Any(DBId, initial.Id()),
			String(Id("conf/note"), "two"),
			String(Id("conf/labels"), "b"),
		))
		require.NoError(t, err)

		got, err := store.GetEntity(ctx, initial.Id())
		require.NoError(t, err)

		note, ok := got.Get(Id("conf/note"))
		require.True(t, ok)
		assert.Equal(t, "two", note.Value.String(), "cardinality-one attribute must be replaced")

		labels := got.GetAll(Id("conf/labels"))
		assert.Len(t, labels, 2, "cardinality-many attribute must accumulate")
	})
}

// TestStoreConformance_PatchEntity pins patch semantics: cardinality-one
// replaces, cardinality-many adds.
func TestStoreConformance_PatchEntity(t *testing.T) {
	runStoreConformance(t, func(t *testing.T, store Store) {
		ctx := t.Context()
		applyConformanceSchema(t, store)

		initial, err := store.CreateEntity(ctx, New(
			Any(Ident, "conf-patch"),
			Any(Doc, "before"),
			String(Id("conf/labels"), "a"),
			String(Id("conf/labels"), "b"),
		))
		require.NoError(t, err)

		patched, err := store.PatchEntity(ctx, New(
			Any(DBId, initial.Id()),
			Any(Doc, "after"),
			String(Id("conf/labels"), "c"),
		))
		require.NoError(t, err)
		assert.Greater(t, patched.GetRevision(), initial.GetRevision())

		got, err := store.GetEntity(ctx, initial.Id())
		require.NoError(t, err)

		doc, ok := got.Get(Doc)
		require.True(t, ok)
		assert.Equal(t, "after", doc.Value.String(), "patch must replace cardinality-one")

		labels := got.GetAll(Id("conf/labels"))
		assert.Len(t, labels, 3, "patch must add to cardinality-many")
	})
}

// TestStoreConformance_UpdateEntityRevisionConflict pins optimistic concurrency
// control: an update pinned (via WithFromRevision) to a stale revision must be
// rejected with cond.ErrConflict, while one pinned to the current revision
// succeeds. Both backends must agree so mock-backed tests exercise the same CAS
// behavior production relies on — e.g. the addon controller swings an app's
// active version under OCC so concurrent addon provisions don't clobber each
// other's env vars (MIR-1458).
func TestStoreConformance_UpdateEntityRevisionConflict(t *testing.T) {
	runStoreConformance(t, func(t *testing.T, store Store) {
		ctx := t.Context()
		applyConformanceSchema(t, store)

		initial, err := store.CreateEntity(ctx, New(
			Any(Ident, "conf-occ-update"),
			String(Id("conf/note"), "one"),
		))
		require.NoError(t, err)
		staleRev := initial.GetRevision()

		// Bump the revision out from under the stale reader.
		bumped, err := store.UpdateEntity(ctx, initial.Id(), New(
			Any(DBId, initial.Id()),
			String(Id("conf/note"), "two"),
		))
		require.NoError(t, err)
		require.Greater(t, bumped.GetRevision(), staleRev)

		// An update pinned to the stale revision must conflict.
		_, err = store.UpdateEntity(ctx, initial.Id(), New(
			Any(DBId, initial.Id()),
			String(Id("conf/note"), "three"),
		), WithFromRevision(staleRev))
		require.Error(t, err)
		assert.True(t, errors.Is(err, cond.ErrConflict{}),
			"stale-revision update must return cond.ErrConflict, got %v", err)

		// An update pinned to the current revision must succeed.
		_, err = store.UpdateEntity(ctx, initial.Id(), New(
			Any(DBId, initial.Id()),
			String(Id("conf/note"), "four"),
		), WithFromRevision(bumped.GetRevision()))
		require.NoError(t, err)

		got, err := store.GetEntity(ctx, initial.Id())
		require.NoError(t, err)
		note, ok := got.Get(Id("conf/note"))
		require.True(t, ok)
		assert.Equal(t, "four", note.Value.String(), "the winning current-revision update must be applied")
	})
}

// TestStoreConformance_PatchEntityRevisionConflict pins the same OCC semantics
// for PatchEntity, which is the path the entity-server Patch RPC (and therefore
// the addon controller's active-version swing) actually takes.
func TestStoreConformance_PatchEntityRevisionConflict(t *testing.T) {
	runStoreConformance(t, func(t *testing.T, store Store) {
		ctx := t.Context()
		applyConformanceSchema(t, store)

		initial, err := store.CreateEntity(ctx, New(
			Any(Ident, "conf-occ-patch"),
			Any(Doc, "before"),
		))
		require.NoError(t, err)
		staleRev := initial.GetRevision()

		bumped, err := store.PatchEntity(ctx, New(
			Any(DBId, initial.Id()),
			Any(Doc, "bumped"),
		))
		require.NoError(t, err)
		require.Greater(t, bumped.GetRevision(), staleRev)

		_, err = store.PatchEntity(ctx, New(
			Any(DBId, initial.Id()),
			Any(Doc, "stale"),
		), WithFromRevision(staleRev))
		require.Error(t, err)
		assert.True(t, errors.Is(err, cond.ErrConflict{}),
			"stale-revision patch must return cond.ErrConflict, got %v", err)

		_, err = store.PatchEntity(ctx, New(
			Any(DBId, initial.Id()),
			Any(Doc, "current"),
		), WithFromRevision(bumped.GetRevision()))
		require.NoError(t, err)
	})
}

// TestStoreConformance_ReplaceEntityRevisionConflict pins the same OCC semantics
// for ReplaceEntity, the path CreateOrReplace and SetInitialEnvVars take. A stale
// pinned revision must conflict; the current revision must succeed.
func TestStoreConformance_ReplaceEntityRevisionConflict(t *testing.T) {
	runStoreConformance(t, func(t *testing.T, store Store) {
		ctx := t.Context()
		applyConformanceSchema(t, store)

		initial, err := store.CreateEntity(ctx, New(
			Any(Ident, "conf-occ-replace"),
			Any(Doc, "before"),
		))
		require.NoError(t, err)
		staleRev := initial.GetRevision()

		bumped, err := store.ReplaceEntity(ctx, New(
			Any(DBId, initial.Id()),
			Any(Ident, "conf-occ-replace"),
			Any(Doc, "bumped"),
		))
		require.NoError(t, err)
		require.Greater(t, bumped.GetRevision(), staleRev)

		_, err = store.ReplaceEntity(ctx, New(
			Any(DBId, initial.Id()),
			Any(Ident, "conf-occ-replace"),
			Any(Doc, "stale"),
		), WithFromRevision(staleRev))
		require.Error(t, err)
		assert.True(t, errors.Is(err, cond.ErrConflict{}),
			"stale-revision replace must return cond.ErrConflict, got %v", err)

		_, err = store.ReplaceEntity(ctx, New(
			Any(DBId, initial.Id()),
			Any(Ident, "conf-occ-replace"),
			Any(Doc, "current"),
		), WithFromRevision(bumped.GetRevision()))
		require.NoError(t, err)
	})
}

// TestStoreConformance_EnsureThenReplaceUpsert exercises the exact pattern saga
// storage uses to persist progress: Ensure to create-or-detect, then Replace
// when it already existed. The end state must reflect the latest write.
func TestStoreConformance_EnsureThenReplaceUpsert(t *testing.T) {
	runStoreConformance(t, func(t *testing.T, store Store) {
		ctx := t.Context()
		id := Id("conf-upsert")

		upsert := func(doc string) error {
			ent := New(Ref(DBId, id), Any(Doc, doc))
			_, created, err := store.EnsureEntity(ctx, ent)
			if err != nil {
				return err
			}
			if !created {
				_, err = store.ReplaceEntity(ctx, ent)
			}
			return err
		}

		require.NoError(t, upsert("v1"))
		require.NoError(t, upsert("v2"))
		require.NoError(t, upsert("v3"))

		got, err := store.GetEntity(ctx, id)
		require.NoError(t, err)
		doc, ok := got.Get(Doc)
		require.True(t, ok)
		assert.Equal(t, "v3", doc.Value.String(),
			"successive upserts must converge on the latest value")
	})
}

func TestStoreConformance_DeleteEntity(t *testing.T) {
	runStoreConformance(t, func(t *testing.T, store Store) {
		ctx := t.Context()

		created, err := store.CreateEntity(ctx, New(Ref(DBId, Id("conf-delete"))))
		require.NoError(t, err)

		require.NoError(t, store.DeleteEntity(ctx, created.Id()))

		_, err = store.GetEntity(ctx, created.Id())
		assert.True(t, isNotFound(err), "entity must be gone after delete, got: %v", err)

		// Deleting a missing entity is a no-op on both backends.
		assert.NoError(t, store.DeleteEntity(ctx, Id("conf-delete-missing")))
	})
}

// ---------------------------------------------------------------------------
// Indexes and collections
// ---------------------------------------------------------------------------

func TestStoreConformance_ListIndex(t *testing.T) {
	runStoreConformance(t, func(t *testing.T, store Store) {
		ctx := t.Context()
		applyConformanceSchema(t, store)

		target := Id("conf-target/v1")
		other := Id("conf-target/v2")
		_, err := store.CreateEntity(ctx, New(Ref(DBId, target)))
		require.NoError(t, err)
		_, err = store.CreateEntity(ctx, New(Ref(DBId, other)))
		require.NoError(t, err)

		a, err := store.CreateEntity(ctx, New(Any(Ident, "conf-idx-a"), Ref(Id("conf/ref"), target)))
		require.NoError(t, err)
		b, err := store.CreateEntity(ctx, New(Any(Ident, "conf-idx-b"), Ref(Id("conf/ref"), target)))
		require.NoError(t, err)
		_, err = store.CreateEntity(ctx, New(Any(Ident, "conf-idx-c"), Ref(Id("conf/ref"), other)))
		require.NoError(t, err)

		ids, err := store.ListIndex(ctx, Ref(Id("conf/ref"), target))
		require.NoError(t, err)
		found := map[Id]bool{}
		for _, id := range ids {
			found[id] = true
		}
		assert.True(t, found[a.Id()], "indexed lookup must find a")
		assert.True(t, found[b.Id()], "indexed lookup must find b")
		assert.Len(t, ids, 2, "indexed lookup must not return the entity with a different value")
	})
}

func TestStoreConformance_ListIndexRevision(t *testing.T) {
	runStoreConformance(t, func(t *testing.T, store Store) {
		ctx := t.Context()
		applyConformanceSchema(t, store)

		target := Id("conf-rev-target/v1")
		_, err := store.CreateEntity(ctx, New(Ref(DBId, target)))
		require.NoError(t, err)
		a, err := store.CreateEntity(ctx, New(Any(Ident, "conf-rev-idx-a"), Ref(Id("conf/ref"), target)))
		require.NoError(t, err)

		ids, rev, err := store.ListIndexRevision(ctx, Ref(Id("conf/ref"), target))
		require.NoError(t, err)
		assert.Contains(t, ids, a.Id())
		assert.Greater(t, rev, int64(0), "ListIndexRevision must report a positive revision to resume a watch from")
	})
}

// TestStoreConformance_ListCollection pins collection listing. A "collection"
// is the CAS string of an indexed attribute value; both backends resolve it to
// the entities carrying that value (EtcdStore via a collection index, MockStore
// by scanning), so the results must match.
func TestStoreConformance_ListCollection(t *testing.T) {
	runStoreConformance(t, func(t *testing.T, store Store) {
		ctx := t.Context()
		applyConformanceSchema(t, store)

		target := Id("conf-coll-target/v1")
		_, err := store.CreateEntity(ctx, New(Ref(DBId, target)))
		require.NoError(t, err)

		a, err := store.CreateEntity(ctx, New(Any(Ident, "conf-coll-a"), Ref(Id("conf/ref"), target)))
		require.NoError(t, err)
		b, err := store.CreateEntity(ctx, New(Any(Ident, "conf-coll-b"), Ref(Id("conf/ref"), target)))
		require.NoError(t, err)

		collection := Ref(Id("conf/ref"), target).CAS()
		ids, err := store.ListCollection(ctx, collection)
		require.NoError(t, err)

		found := map[Id]bool{}
		for _, id := range ids {
			found[id] = true
		}
		assert.True(t, found[a.Id()], "collection must include a")
		assert.True(t, found[b.Id()], "collection must include b")
	})
}

// ---------------------------------------------------------------------------
// Watches
// ---------------------------------------------------------------------------

func TestStoreConformance_WatchEntity(t *testing.T) {
	runStoreConformance(t, func(t *testing.T, store Store) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		id := Id("conf-watch")
		created, err := store.CreateEntity(ctx, New(Ref(DBId, id), Any(Doc, "v1")))
		require.NoError(t, err)

		ch, err := store.WatchEntity(ctx, id)
		require.NoError(t, err)

		_, err = store.ReplaceEntity(ctx, New(Ref(DBId, id), Any(Doc, "v2")))
		require.NoError(t, err)
		_ = created

		select {
		case op := <-ch:
			assert.Equal(t, id, op.Id(), "watch must deliver an op for the watched entity")
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for a watch event")
		}
	})
}

// TestStoreConformance_WatchIndex verifies that a watch established on an
// indexed attribute value delivers an event when a new entity carrying that
// value is created.
func TestStoreConformance_WatchIndex(t *testing.T) {
	runStoreConformance(t, func(t *testing.T, store Store) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		applyConformanceSchema(t, store)

		target := Id("conf-watchidx-target/v1")
		_, err := store.CreateEntity(ctx, New(Ref(DBId, target)))
		require.NoError(t, err)

		// Establish the current revision, then watch forward from it.
		_, rev, err := store.ListIndexRevision(ctx, Ref(Id("conf/ref"), target))
		require.NoError(t, err)

		ch, err := store.WatchIndex(ctx, Ref(Id("conf/ref"), target), rev)
		require.NoError(t, err)

		_, err = store.CreateEntity(ctx, New(Any(Ident, "conf-watchidx-a"), Ref(Id("conf/ref"), target)))
		require.NoError(t, err)

		select {
		case resp := <-ch:
			assert.NotEmpty(t, resp.Events, "WatchIndex must deliver an event for a new index entry")
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for a WatchIndex event")
		}
	})
}

// ---------------------------------------------------------------------------
// Documented divergences: scenarios where MockStore is a deliberate stub.
// These run against EtcdStore (the real contract) and skip on MockStore with a
// recorded reason, so the gap is part of the suite rather than invisible.
// ---------------------------------------------------------------------------

// TestStoreConformance_GetEntityAtRevision_Historical documents that EtcdStore
// returns the entity as it was at an older revision, while MockStore ignores
// the revision argument and always returns the current value.
func TestStoreConformance_GetEntityAtRevision_Historical(t *testing.T) {
	runStoreConformance(t, func(t *testing.T, store Store) {
		skipMock(t, store, "GetEntityAtRevision ignores the rev argument and returns the current entity")

		ctx := t.Context()
		id := Id("conf-rev-historical")
		v1, err := store.CreateEntity(ctx, New(Ref(DBId, id), Any(Doc, "v1")))
		require.NoError(t, err)
		rev1 := v1.GetRevision()

		_, err = store.ReplaceEntity(ctx, New(Ref(DBId, id), Any(Doc, "v2")))
		require.NoError(t, err)

		old, err := store.GetEntityAtRevision(ctx, id, rev1)
		require.NoError(t, err)
		doc, ok := old.Get(Doc)
		require.True(t, ok)
		assert.Equal(t, "v1", doc.Value.String(), "reading at an old revision must return the historical value")
	})
}

// TestStoreConformance_Sessions pins the minimal session lifecycle both
// backends support (create yields a non-empty token; ping and revoke succeed).
// MockStore's sessions are stubs that do not enforce scoping or expiry, so
// anything beyond this shared contract is left out by design.
func TestStoreConformance_Sessions(t *testing.T) {
	runStoreConformance(t, func(t *testing.T, store Store) {
		ctx := t.Context()

		token, err := store.CreateSession(ctx, 60)
		require.NoError(t, err)
		assert.NotEmpty(t, token, "CreateSession must return a token")

		assert.NoError(t, store.PingSession(ctx, token), "ping on a fresh session must succeed")
		assert.NoError(t, store.RevokeSession(ctx, token), "revoke on a session must succeed")
	})
}

// TestStoreConformance_ListSessionEntities documents that EtcdStore scopes
// session entities (a fresh session owns none), while MockStore's stub returns
// every entity in the store regardless of session. Anything relying on session
// scoping must use the real store.
func TestStoreConformance_ListSessionEntities(t *testing.T) {
	runStoreConformance(t, func(t *testing.T, store Store) {
		skipMock(t, store, "ListSessionEntities returns all entities and does not model session scoping")

		ctx := t.Context()
		token, err := store.CreateSession(ctx, 60)
		require.NoError(t, err)

		ids, err := store.ListSessionEntities(ctx, token)
		require.NoError(t, err)
		assert.Empty(t, ids, "a fresh session owns no session-scoped entities")
	})
}
