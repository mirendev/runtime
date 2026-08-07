package model

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	core "miren.dev/runtime/api/core/core_v1alpha"
	saga "miren.dev/runtime/api/saga/saga_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/schema"
)

func testCache(t *testing.T) (*entity.SchemaCache, *entity.MockStore) {
	t.Helper()
	r := require.New(t)

	store := entity.NewMockStore()
	r.NoError(schema.Apply(context.TODO(), store))

	sc, err := entity.NewSchemaCache(store)
	r.NoError(err)

	return sc, store
}

// build runs an entity through the store so it picks up the same bookkeeping a
// real one carries, then builds its document.
func build(t *testing.T, attrs []entity.Attr, opts Options) *Document {
	t.Helper()
	r := require.New(t)

	sc, store := testCache(t)

	ent, err := store.CreateEntity(context.TODO(), entity.New(attrs))
	r.NoError(err)

	return BuildDocument(context.TODO(), sc, ent, opts)
}

func TestBuildDocument(t *testing.T) {
	t.Run("groups fields under the facet that claims them", func(t *testing.T) {
		r := require.New(t)

		// A scheduled sandbox: the scheduler grafts the schedule facet onto an
		// entity that already carries sandbox and metadata.
		doc := build(t, []entity.Attr{
			{ID: entity.Ident, Value: entity.KeywordValue("sandbox/sb-test")},
			{ID: entity.EntityKind, Value: entity.RefValue(compute.KindSandbox)},
			{ID: entity.EntityKind, Value: entity.RefValue(compute.KindSchedule)},
			{ID: entity.EntityKind, Value: entity.RefValue(core.KindMetadata)},
			{ID: core.MetadataNameId, Value: entity.StringValue("meet-web")},
		}, Options{})

		r.Len(doc.Kinds, 3, "all three facets must be reported")

		byLabel := map[string]Facet{}
		for _, f := range doc.Facets {
			byLabel[f.Label] = f
		}

		r.Contains(byLabel, "compute/sandbox")
		r.Contains(byLabel, "compute/schedule")
		r.Contains(byLabel, "core/metadata")

		// The name belongs to metadata, not to sandbox.
		var found bool
		for _, f := range byLabel["core/metadata"].Fields {
			if f.Name == "name" {
				found = true
				r.Equal("meet-web", f.Value)
			}
		}
		r.True(found, "metadata.name should render under the metadata facet")
	})

	t.Run("surfaces attributes no facet claims", func(t *testing.T) {
		r := require.New(t)

		doc := build(t, []entity.Attr{
			{ID: entity.Ident, Value: entity.KeywordValue("saga/orphan")},
			{ID: entity.EntityKind, Value: entity.RefValue(saga.KindSaga)},
			{ID: saga.SagaDefinitionNameId, Value: entity.StringValue("deploy")},
			// Belongs to no schema on this entity's facets.
			{ID: core.MetadataNameId, Value: entity.StringValue("stray")},
		}, Options{})

		var names []string
		for _, f := range doc.Unclaimed {
			names = append(names, f.Name)
		}

		r.Contains(names, string(core.MetadataNameId),
			"an attribute outside every facet's schema must not vanish")

		// And a claimed one must not be duplicated into unclaimed.
		r.NotContains(names, string(saga.SagaDefinitionNameId))
	})

	t.Run("elides oversized values and reports the real size", func(t *testing.T) {
		r := require.New(t)

		big := make([]byte, 4096)

		doc := build(t, []entity.Attr{
			{ID: entity.Ident, Value: entity.KeywordValue("saga/big")},
			{ID: entity.EntityKind, Value: entity.RefValue(saga.KindSaga)},
			{ID: saga.SagaInitialInputsId, Value: entity.BytesValue(big)},
		}, Options{MaxValueLen: 32})

		var field Field
		for _, f := range doc.Facets[0].Fields {
			if f.Name == "initial_inputs" {
				field = f
			}
		}

		r.True(field.Truncated)
		r.Equal(4096, field.Size, "the true byte length must survive the elision")
		r.Less(len(field.Value.(string)), 40)
		r.True(strings.HasSuffix(field.Value.(string), ellipsis))
	})

	t.Run("keeps values whole when no limit is set", func(t *testing.T) {
		r := require.New(t)

		big := make([]byte, 4096)

		doc := build(t, []entity.Attr{
			{ID: entity.Ident, Value: entity.KeywordValue("saga/big")},
			{ID: entity.EntityKind, Value: entity.RefValue(saga.KindSaga)},
			{ID: saga.SagaInitialInputsId, Value: entity.BytesValue(big)},
		}, Options{})

		for _, f := range doc.Facets[0].Fields {
			if f.Name == "initial_inputs" {
				r.False(f.Truncated, "the JSON path must never be lossy")
			}
		}
	})

	t.Run("truncation inside a component reaches Elided", func(t *testing.T) {
		r := require.New(t)

		// Components are where the big values live, so a truncation that never
		// surfaces means the "--max-value-len 0 shows them whole" hint stays
		// silent exactly when it is needed.
		doc := build(t, []entity.Attr{
			{ID: entity.Ident, Value: entity.KeywordValue("sandbox/nested")},
			{ID: entity.EntityKind, Value: entity.RefValue(compute.KindSandbox)},
			{ID: compute.SandboxContainerId, Value: entity.ComponentValue([]entity.Attr{
				{ID: compute.ContainerImageId, Value: entity.StringValue(strings.Repeat("x", 300))},
			})},
		}, Options{MaxValueLen: 32})

		r.True(doc.Elided(), "a value elided inside a component still counts as elided")

		var field Field
		for _, f := range doc.Facets[0].Fields {
			if f.Name == "container" {
				field = f
			}
		}

		r.True(field.Truncated, "the enclosing field must report its nested truncation")
		r.Equal(300, field.Size, "and carry the real size of what it hid")
	})

	t.Run("a truncated repeated field reports its size", func(t *testing.T) {
		r := require.New(t)

		// Truncated with a zero Size renders as "0 B truncated", which tells
		// the reader nothing about what was hidden.
		doc := build(t, []entity.Attr{
			{ID: entity.Ident, Value: entity.KeywordValue("sandbox/many")},
			{ID: entity.EntityKind, Value: entity.RefValue(compute.KindSandbox)},
			{ID: compute.SandboxLabelsId, Value: entity.StringValue(strings.Repeat("x", 200))},
			{ID: compute.SandboxLabelsId, Value: entity.StringValue("short")},
		}, Options{MaxValueLen: 32})

		var field Field
		for _, f := range doc.Facets[0].Fields {
			if f.Name == "labels" {
				field = f
			}
		}

		r.True(field.Truncated)
		r.Equal(200, field.Size, "a multi-valued field must report the size it elided")
	})

	t.Run("renders a kindless attribute definition as an attribute", func(t *testing.T) {
		r := require.New(t)

		sc, _ := testCache(t)

		// The shape every attribute definition in the schema has.
		ent := entity.New([]entity.Attr{
			{ID: entity.Ident, Value: entity.KeywordValue("db/id")},
			{ID: entity.Type, Value: entity.RefValue(entity.TypeRef)},
			{ID: entity.Cardinality, Value: entity.RefValue(entity.CardinalityOne)},
			{ID: entity.Uniq, Value: entity.RefValue(entity.UniqueId)},
			{ID: entity.Doc, Value: entity.StringValue("Internal entity ID")},
		})

		doc := BuildDocument(context.TODO(), sc, ent, Options{})

		r.Empty(doc.Kinds)
		r.NotNil(doc.Attribute, "attribute definitions carry no kind and need their own shape")
		r.Equal("ref", doc.Attribute.Type)
		r.Equal("one", doc.Attribute.Cardinality)
		r.NotEmpty(doc.Attribute.Doc)

		// What the attribute view renders must not also show up as unclaimed.
		var names []string
		for _, f := range doc.Unclaimed {
			names = append(names, f.Name)
		}
		r.NotContains(names, string(entity.Type))
	})

	t.Run("reports a malformed kind instead of panicking", func(t *testing.T) {
		r := require.New(t)

		sc, _ := testCache(t)

		// Value.Id panics on a keyword; the builder must not hand it one.
		ent := entity.New([]entity.Attr{
			{ID: entity.Ident, Value: entity.KeywordValue("test/broken")},
			{ID: entity.EntityKind, Value: entity.KeywordValue("not-a-ref")},
		})

		doc := BuildDocument(context.TODO(), sc, ent, Options{})

		r.NotEmpty(doc.Problems)
		r.Contains(doc.Problems[0], "expected a reference")
	})
}

func TestDocumentElided(t *testing.T) {
	r := require.New(t)

	// The CLI only offers the way out of an elision when there was one, so
	// this has to notice truncation wherever it happened.
	r.False((&Document{
		Facets: []Facet{{Fields: []Field{{Name: "a", Value: "short"}}}},
	}).Elided())

	r.True((&Document{
		Facets: []Facet{{Fields: []Field{{Name: "a", Value: "cut", Truncated: true}}}},
	}).Elided())

	r.True((&Document{
		Unclaimed: []Field{{Name: "db/schema", Value: "cut", Truncated: true}},
	}).Elided())
}

func TestRenderCard(t *testing.T) {
	r := require.New(t)

	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	updated := now.Add(-45 * time.Second)

	doc := &Document{
		Id:        "sandbox/sb-K2p9x",
		ShortId:   "K2p9x",
		Revision:  4471,
		UpdatedAt: &updated,
		Kinds: []string{
			"dev.miren.compute/kind.sandbox",
			"dev.miren.compute/kind.schedule",
			"dev.miren.core/kind.metadata",
		},
		Facets: []Facet{
			{Label: "compute/sandbox", Fields: []Field{
				{Name: "status", Value: "running"},
				{Name: "network", Value: []any{
					map[string]any{"name": "default"},
					map[string]any{"name": "mesh"},
				}},
			}},
			{Label: "core/metadata", Fields: []Field{
				{Name: "name", Value: "meet-web"},
				{Name: "labels", Value: []any{"tier=web", "env=prod"}},
			}},
		},
		Unclaimed: []Field{
			{Name: "db/attr.session", Value: "47ez92Uzb6UtQ"},
		},
	}

	var b strings.Builder
	RenderCard(&b, doc, RenderOptions{Now: now})
	out := b.String()

	t.Logf("\n%s", out)

	r.Contains(out, "sandbox/sb-K2p9x")
	r.Contains(out, "rev 4471 · updated 45s ago")
	r.Contains(out, "facets  compute/sandbox  compute/schedule  core/metadata")

	// Fan-out collapses rather than flooding the card.
	r.Contains(out, "2 components")
	r.Contains(out, "--expand")

	// Repeated scalars are short enough to read inline.
	r.Contains(out, "tier=web  env=prod")
	r.Contains(out, "unclaimed")
}

func TestRenderTable(t *testing.T) {
	r := require.New(t)

	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	updated := now.Add(-12 * time.Second)

	docs := []*Document{
		{
			Id: "sandbox/sb-A", ShortId: "A", Revision: 10, UpdatedAt: &updated,
			Kinds: []string{"dev.miren.compute/kind.sandbox", "dev.miren.core/kind.metadata"},
			Facets: []Facet{{Kind: "dev.miren.compute/kind.sandbox", Label: "compute/sandbox", Fields: []Field{
				{Name: "status", Value: "running"},
				{Name: "network", Value: []any{map[string]any{}}},
			}}},
		},
		{
			Id: "sandbox/sb-B", ShortId: "B", Revision: 11, UpdatedAt: &updated,
			Kinds: []string{"dev.miren.compute/kind.sandbox", "dev.miren.compute/kind.schedule"},
			Facets: []Facet{{Kind: "dev.miren.compute/kind.sandbox", Label: "compute/sandbox", Fields: []Field{
				{Name: "status", Value: "pending"},
			}}},
		},
	}

	cols := TableColumns(docs, "dev.miren.compute/kind.sandbox", nil)

	// Components make no sense as a cell, so they must not become columns.
	r.Equal([]string{"status"}, cols)

	var b strings.Builder
	RenderTable(&b, docs, cols, RenderOptions{Now: now})
	out := b.String()

	t.Logf("\n%s", out)

	r.Contains(out, "ID")
	r.Contains(out, "STATUS")

	// The facet column is what tells the two rows apart.
	r.Contains(out, "compute/sandbox+compute/schedule")
	r.Contains(out, "running")
	r.Contains(out, "12s ago")
}
