package entity

import (
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildConvergencePlanIsDeterministicAndRejectsConflicts(t *testing.T) {
	rules := []ConvergenceRule{
		{Attribute: "test/mode", From: StringValue("fixed"), To: RefValue("test/mode.fixed")},
		{Attribute: "test/mode", From: StringValue("auto"), To: RefValue("test/mode.auto")},
	}
	forward, err := BuildConvergencePlan(rules)
	require.NoError(t, err)
	reverse, err := BuildConvergencePlan([]ConvergenceRule{rules[1], rules[0]})
	require.NoError(t, err)
	assert.Equal(t, forward.Hash(), reverse.Hash())

	_, err = BuildConvergencePlan(append(rules,
		ConvergenceRule{Attribute: "test/mode", From: StringValue("auto"), To: RefValue("test/mode.other")},
	))
	require.ErrorContains(t, err, "maps one value to multiple targets")
}

func TestRewriteConvergentAttrsReturnsOriginalSliceWhenUnchanged(t *testing.T) {
	attrs := []Attr{
		String("test/mode", "ready"),
		Component("test/component", []Attr{String("test/nested-mode", "ready")}),
	}
	rules := map[Id][]ConvergenceRule{
		"test/other-mode": {{
			Attribute: "test/other-mode",
			From:      StringValue("ready"),
			To:        RefValue("test/other-mode.ready"),
		}},
	}

	rewritten, changed := rewriteConvergentAttrs(attrs, rules)
	assert.Zero(t, changed)
	require.Len(t, rewritten, len(attrs))
	assert.Same(t, &attrs[0], &rewritten[0])
}

func TestConvergeRewritesLegacyValuesInsideRepeatedComponents(t *testing.T) {
	store, _ := setupTestEtcdStore(t)
	ctx := t.Context()

	const (
		modeAttr  = Id("test/mode")
		itemsAttr = Id("test/items")
		ownerAttr = Id("test/owner")
		readyID   = Id("test/mode.ready")
		fixedID   = Id("test/mode.fixed")
		ownerID   = Id("test/owner.old")
	)

	for _, id := range []Id{readyID, fixedID} {
		_, err := store.CreateEntity(ctx, New(Ident, id))
		require.NoError(t, err)
	}
	_, err := store.CreateEntity(ctx, New(
		Ident, modeAttr,
		Cardinality, CardinalityOne,
		Type, TypeStr,
	))
	require.NoError(t, err)
	_, err = store.CreateEntity(ctx, New(
		Ident, itemsAttr,
		Cardinality, CardinalityMany,
		Type, TypeComponent,
	))
	require.NoError(t, err)
	_, err = store.CreateEntity(ctx, New(
		Ident, ownerAttr,
		Cardinality, CardinalityOne,
		Type, TypeRef,
	))
	require.NoError(t, err)
	_, err = store.CreateEntity(ctx, New(Ident, ownerID))
	require.NoError(t, err)

	created, err := store.CreateEntity(ctx, New(
		Ident, "test/entity",
		Component(itemsAttr, []Attr{String(modeAttr, "ready"), Ref(ownerAttr, ownerID)}),
		Component(itemsAttr, []Attr{String(modeAttr, "fixed"), Ref(ownerAttr, ownerID)}),
	))
	require.NoError(t, err)
	require.NoError(t, store.DeleteEntity(ctx, ownerID))

	_, err = store.CreateEntity(ctx, New(
		Ident, modeAttr,
		Cardinality, CardinalityOne,
		Type, TypeEnum,
		EntityElemType, TypeRef,
		EnumValues, ArrayValue(readyID, fixedID),
	), WithOverwrite)
	require.NoError(t, err)
	store.ClearSchemaCache()

	plan, err := BuildConvergencePlan([]ConvergenceRule{
		{Attribute: modeAttr, From: StringValue("ready"), To: RefValue(readyID)},
		{Attribute: modeAttr, From: StringValue("fixed"), To: RefValue(fixedID)},
	})
	require.NoError(t, err)

	stats, err := store.Converge(ctx, slog.Default(), plan, ConvergenceOptions{})
	require.NoError(t, err)
	assert.True(t, stats.Complete)
	assert.Equal(t, int64(1), stats.EntitiesRewritten)
	assert.Equal(t, int64(2), stats.ValuesRewritten)

	got, err := store.GetEntity(ctx, created.Id())
	require.NoError(t, err)
	components := got.GetAll(itemsAttr)
	require.Len(t, components, 2)
	values := make(map[Id]bool)
	for _, component := range components {
		mode, ok := component.Value.Component().Get(modeAttr)
		require.True(t, ok)
		require.Equal(t, KindId, mode.Value.Kind())
		values[mode.Value.Id()] = true
	}
	assert.Equal(t, map[Id]bool{readyID: true, fixedID: true}, values)

	second, err := store.Converge(ctx, slog.Default(), plan, ConvergenceOptions{})
	require.NoError(t, err)
	assert.Zero(t, second.EntitiesRewritten)
	assert.Zero(t, second.ValuesRewritten)
}

func TestConvergeResumesAcrossBoundedPasses(t *testing.T) {
	store, _ := setupTestEtcdStore(t)
	ctx := t.Context()

	const (
		modeAttr = Id("test/bounded-mode")
		readyID  = Id("test/bounded-mode.ready")
	)
	_, err := store.CreateEntity(ctx, New(Ident, readyID))
	require.NoError(t, err)
	_, err = store.CreateEntity(ctx, New(
		Ident, modeAttr,
		Cardinality, CardinalityOne,
		Type, TypeStr,
		Index, true,
	))
	require.NoError(t, err)

	var ids []Id
	for i := range 7 {
		created, err := store.CreateEntity(ctx, New(
			Ident, Id(fmt.Sprintf("test/bounded-%d", i)),
			String(modeAttr, "ready"),
		))
		require.NoError(t, err)
		ids = append(ids, created.Id())
	}

	_, err = store.CreateEntity(ctx, New(
		Ident, modeAttr,
		Cardinality, CardinalityOne,
		Type, TypeEnum,
		EntityElemType, TypeRef,
		EnumValues, ArrayValue(readyID),
		Index, true,
	), WithOverwrite)
	require.NoError(t, err)
	store.ClearSchemaCache()

	plan, err := BuildConvergencePlan([]ConvergenceRule{{
		Attribute: modeAttr,
		From:      StringValue("ready"),
		To:        RefValue(readyID),
	}})
	require.NoError(t, err)

	cursor := ""
	complete := false
	passes := 0
	for !complete {
		passes++
		require.Less(t, passes, 100)
		stats, err := store.Converge(ctx, slog.Default(), plan, ConvergenceOptions{
			StartKey:    cursor,
			MaxEntities: 3,
		})
		require.NoError(t, err)
		assert.LessOrEqual(t, stats.EntitiesProcessed, int64(3))
		cursor = stats.NextCursor
		complete = stats.Complete
	}
	assert.Greater(t, passes, 1)

	for _, id := range ids {
		got, err := store.GetEntity(ctx, id)
		require.NoError(t, err)
		mode, ok := got.Get(modeAttr)
		require.True(t, ok)
		assert.True(t, mode.Value.Equal(RefValue(readyID)))
	}
}

func TestConvergePreservesEntityLease(t *testing.T) {
	store, client := setupTestEtcdStore(t)
	ctx := t.Context()

	const (
		modeAttr = Id("test/leased-mode")
		readyID  = Id("test/leased-mode.ready")
	)
	_, err := store.CreateEntity(ctx, New(Ident, readyID))
	require.NoError(t, err)
	_, err = store.CreateEntity(ctx, New(
		Ident, modeAttr,
		Cardinality, CardinalityOne,
		Type, TypeStr,
		Index, true,
	))
	require.NoError(t, err)

	session, err := store.CreateSession(ctx, 60)
	require.NoError(t, err)
	created, err := store.CreateEntity(ctx, New(
		Ident, "test/leased-entity",
		String(modeAttr, "ready"),
	), BondToSession(session))
	require.NoError(t, err)

	key := store.buildKey(created.Id())
	before, err := client.Get(ctx, key)
	require.NoError(t, err)
	require.Len(t, before.Kvs, 1)
	require.NotZero(t, before.Kvs[0].Lease)

	_, err = store.CreateEntity(ctx, New(
		Ident, modeAttr,
		Cardinality, CardinalityOne,
		Type, TypeEnum,
		EntityElemType, TypeRef,
		EnumValues, ArrayValue(readyID),
		Index, true,
	), WithOverwrite)
	require.NoError(t, err)
	store.ClearSchemaCache()
	plan, err := BuildConvergencePlan([]ConvergenceRule{{
		Attribute: modeAttr,
		From:      StringValue("ready"),
		To:        RefValue(readyID),
	}})
	require.NoError(t, err)

	stats, err := store.Converge(ctx, slog.Default(), plan, ConvergenceOptions{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), stats.EntitiesRewritten)

	after, err := client.Get(ctx, key)
	require.NoError(t, err)
	require.Len(t, after.Kvs, 1)
	assert.Equal(t, before.Kvs[0].Lease, after.Kvs[0].Lease)

	canonicalIndex, err := store.ListIndex(ctx, Ref(modeAttr, readyID))
	require.NoError(t, err)
	assert.Equal(t, []Id{created.Id()}, canonicalIndex)
	require.NoError(t, store.RevokeSession(ctx, session))
	canonicalIndex, err = store.ListIndex(ctx, Ref(modeAttr, readyID))
	require.NoError(t, err)
	assert.Empty(t, canonicalIndex, "rewritten index entry should expire with the entity")
}
