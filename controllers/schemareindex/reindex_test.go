package schemareindex

import (
	"fmt"
	"log/slog"
	"testing"

	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clientv3 "go.etcd.io/etcd/client/v3"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/etcdtest"
)

const testIndexedAttr = "test/kind"

// setupStore builds a store holding entityCount entities carrying an indexed
// attribute, then drops every collection entry. That is the state a schema
// change leaves behind: entities that predate the new index and have no entries
// for it.
func setupStore(t *testing.T, entityCount int) (*entity.EtcdStore, map[entity.Id]bool, *clientv3.Client) {
	t.Helper()

	client, prefix := etcdtest.TestEtcdClient(t)
	store, err := entity.NewEtcdStore(t.Context(), slog.Default(), client, prefix)
	require.NoError(t, err)

	ctx := t.Context()

	_, err = store.CreateEntity(ctx, entity.New(
		entity.Ident, testIndexedAttr,
		entity.Doc, "a kind",
		entity.Cardinality, entity.CardinalityOne,
		entity.Type, entity.TypeStr,
		entity.Index, true,
	))
	require.NoError(t, err)

	want := map[entity.Id]bool{}
	for i := range entityCount {
		e, err := store.CreateEntity(ctx, entity.New(
			entity.Ident, entity.Id(fmt.Sprintf("entity-%02d", i)),
			entity.String(entity.Id(testIndexedAttr), "widget"),
		))
		require.NoError(t, err)
		want[e.Id()] = true
	}

	_, err = client.Delete(ctx, store.Prefix()+"/collections/", clientv3.WithPrefix())
	require.NoError(t, err)

	ids, err := store.ListIndex(ctx, entity.String(entity.Id(testIndexedAttr), "widget"))
	require.NoError(t, err)
	require.Empty(t, ids, "test setup should leave the index empty")

	return store, want, client
}

func newController(store *entity.EtcdStore, hash func() string, perPass int) *Controller {
	return &Controller{
		Log:         slog.Default(),
		Store:       store,
		CurrentHash: hash,
		Config: Config{
			MaxEntitiesPerPass: perPass,
		},
	}
}

func indexedIDs(t *testing.T, store *entity.EtcdStore) map[entity.Id]bool {
	t.Helper()

	ids, err := store.ListIndex(t.Context(), entity.String(entity.Id(testIndexedAttr), "widget"))
	require.NoError(t, err)

	got := map[entity.Id]bool{}
	for _, id := range ids {
		got[id] = true
	}
	return got
}

// TestController_ConvergesAcrossInterruptedPasses is the regression test for
// MIR-1496. A reindex that cannot finish in a single pass must resume from its
// checkpoint and eventually index every entity, and must not record the new
// schema hash until it has — recording it early is what stranded entities
// un-indexed with nothing left to retry.
func TestController_ConvergesAcrossInterruptedPasses(t *testing.T) {
	const entityCount = 12
	store, want, _ := setupStore(t, entityCount)
	ctx := t.Context()

	const targetHash = "hash-v2"
	c := newController(store, func() string { return targetHash }, 3)

	passes := 0
	for {
		passes++
		require.Less(t, passes, 100, "reindex failed to converge")

		more := c.step(ctx)
		if !more {
			break
		}

		// Mid-flight: the hash must still be unrecorded, and the checkpoint
		// must be present and pointed at this target.
		storedHash, err := store.LoadIndexHash(ctx)
		require.NoError(t, err)
		assert.NotEqual(t, targetHash, storedHash,
			"schema hash recorded before the reindex finished")

		state, err := store.LoadReindexState(ctx, slog.Default())
		require.NoError(t, err)
		require.NotNil(t, state, "an in-flight reindex must leave a checkpoint")
		assert.Equal(t, targetHash, state.TargetHash)
		assert.NotEmpty(t, state.Cursor)
	}

	assert.Greater(t, passes, 1, "test did not exercise resumption")

	storedHash, err := store.LoadIndexHash(ctx)
	require.NoError(t, err)
	assert.Equal(t, targetHash, storedHash, "completed reindex must record the hash")

	state, err := store.LoadReindexState(ctx, slog.Default())
	require.NoError(t, err)
	assert.Nil(t, state, "completed reindex must clear its checkpoint")

	assert.Equal(t, want, indexedIDs(t, store), "every entity must end up indexed")
}

// TestController_RestartsWhenSchemaChangesMidReindex covers a second schema
// change landing while a reindex is still draining. Finishing the in-flight
// scan would stamp a hash the store doesn't match, so the scan must restart
// against the new target.
func TestController_RestartsWhenSchemaChangesMidReindex(t *testing.T) {
	const entityCount = 12
	store, want, _ := setupStore(t, entityCount)
	ctx := t.Context()

	hash := "hash-v2"
	c := newController(store, func() string { return hash }, 3)

	require.True(t, c.step(ctx), "expected the first pass to leave work behind")

	state, err := store.LoadReindexState(ctx, slog.Default())
	require.NoError(t, err)
	require.NotNil(t, state)
	require.Equal(t, "hash-v2", state.TargetHash)
	firstCursor := state.Cursor
	require.NotEmpty(t, firstCursor)

	// Schema changes again before the reindex drains.
	hash = "hash-v3"

	require.True(t, c.step(ctx), "expected the restarted pass to leave work behind")

	state, err = store.LoadReindexState(ctx, slog.Default())
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, "hash-v3", state.TargetHash, "checkpoint must retarget")
	assert.LessOrEqual(t, state.Cursor, firstCursor,
		"a retargeted reindex must rescan from the head, not continue the stale cursor")

	for i := 0; c.step(ctx); i++ {
		require.Less(t, i, 100, "reindex failed to converge after retarget")
	}

	storedHash, err := store.LoadIndexHash(ctx)
	require.NoError(t, err)
	assert.Equal(t, "hash-v3", storedHash, "must record the hash it actually reindexed for")
	assert.Equal(t, want, indexedIDs(t, store))
}

// TestController_DoesNotRecordHashWhenEntitiesFail is the second half of the
// convergence guarantee. Reaching the end of the keyspace is not the same as
// having indexed everything: entities that fail are logged and skipped, so a
// pass can complete with some left un-indexed. Stamping the hash there would
// declare a consistency we never reached, and because the stored hash would
// then match, nothing would ever retry them.
func TestController_DoesNotRecordHashWhenEntitiesFail(t *testing.T) {
	store, _, client := setupStore(t, 4)
	ctx := t.Context()

	// An entity whose key reads fine but whose value cannot be decoded.
	_, err := client.Put(ctx,
		store.Prefix()+"/entity/"+base58.Encode([]byte("corrupt-entity")),
		"this is not a valid encoded entity")
	require.NoError(t, err)

	const targetHash = "hash-v2"
	c := newController(store, func() string { return targetHash }, 100)

	// One unbounded-enough pass reaches the end of the keyspace, but hits the
	// corrupt entity on the way.
	assert.False(t, c.step(ctx),
		"a failed pass must report no work pending so the retry paces at IdleInterval")

	storedHash, err := store.LoadIndexHash(ctx)
	require.NoError(t, err)
	assert.NotEqual(t, targetHash, storedHash,
		"hash must not be recorded while entities are still failing")

	state, err := store.LoadReindexState(ctx, slog.Default())
	require.NoError(t, err)
	require.NotNil(t, state, "a failed pass must keep its checkpoint so the work is retried")
	assert.Equal(t, targetHash, state.TargetHash)
	assert.Empty(t, state.Cursor,
		"cursor must rewind to the head, since it already advanced past the failures")

	// Once the bad entity is gone, the next pass converges and stamps.
	_, err = client.Delete(ctx, store.Prefix()+"/entity/"+base58.Encode([]byte("corrupt-entity")))
	require.NoError(t, err)

	for i := 0; c.step(ctx); i++ {
		require.Less(t, i, 100, "reindex failed to converge after the failure cleared")
	}

	storedHash, err = store.LoadIndexHash(ctx)
	require.NoError(t, err)
	assert.Equal(t, targetHash, storedHash, "a clean pass must record the hash")
}

// TestController_StartNormalizesConfig guards the hot-spin failure mode: a
// partial Config leaves the intervals at zero, and time.After(0) fires
// immediately, turning the loop into a continuous read of the index hash.
func TestController_StartNormalizesConfig(t *testing.T) {
	store, _, _ := setupStore(t, 1)

	c := &Controller{
		Log:         slog.Default(),
		Store:       store,
		CurrentHash: func() string { return "hash-v2" },
	}
	c.Start(t.Context())
	t.Cleanup(c.Stop)

	defaults := DefaultConfig()
	assert.Equal(t, defaults.IdleInterval, c.Config.IdleInterval)
	assert.Equal(t, defaults.ActiveInterval, c.Config.ActiveInterval)
	assert.Equal(t, defaults.MaxEntitiesPerPass, c.Config.MaxEntitiesPerPass)
}

// TestController_NoopWhenHashMatches keeps the idle path cheap: a store already
// consistent with the running schema must do no work and report nothing pending.
func TestController_NoopWhenHashMatches(t *testing.T) {
	store, _, _ := setupStore(t, 4)
	ctx := t.Context()

	const targetHash = "hash-v2"
	require.NoError(t, store.SaveIndexHash(ctx, targetHash))

	c := newController(store, func() string { return targetHash }, 3)

	assert.False(t, c.step(ctx), "matching hash must report no work pending")

	state, err := store.LoadReindexState(ctx, slog.Default())
	require.NoError(t, err)
	assert.Nil(t, state, "no-op step must not create a checkpoint")

	// The index stays empty because nothing ran, confirming the short-circuit
	// happened before any scan rather than after a silent full pass.
	assert.Empty(t, indexedIDs(t, store))
}

// TestController_ResumesAfterProcessRestart simulates the coordinator bouncing
// mid-reindex: a brand-new controller sharing only the persisted state must
// pick up from the checkpoint rather than starting over.
func TestController_ResumesAfterProcessRestart(t *testing.T) {
	const entityCount = 12
	store, want, _ := setupStore(t, entityCount)
	ctx := t.Context()

	const targetHash = "hash-v2"
	hashFn := func() string { return targetHash }

	require.True(t, newController(store, hashFn, 3).step(ctx))

	state, err := store.LoadReindexState(ctx, slog.Default())
	require.NoError(t, err)
	require.NotNil(t, state)
	resumeFrom := state.Cursor

	// A fresh controller, as if the process had restarted.
	c := newController(store, hashFn, 3)
	require.True(t, c.step(ctx))

	state, err = store.LoadReindexState(ctx, slog.Default())
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Greater(t, state.Cursor, resumeFrom,
		"a restarted controller must advance the cursor, not restart the scan")

	for i := 0; c.step(ctx); i++ {
		require.Less(t, i, 100, "reindex failed to converge after restart")
	}

	assert.Equal(t, want, indexedIDs(t, store))
}
