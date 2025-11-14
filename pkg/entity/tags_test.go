package entity_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/schema"
	"miren.dev/runtime/pkg/entity/testutils"
)

func TestGetAttributesByTag(t *testing.T) {
	ctx := context.Background()

	// Create an in-memory entity server for testing
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	store := inmem.Store

	// Register a test schema with tagged attributes
	schema.Register("test.domain", "v1", func(sb *schema.SchemaBuilder) {
		// Attribute with tag
		sb.Ref("tagged_ref_1", "test.domain/tagged.ref1",
			schema.Doc("First tagged reference"),
			schema.Indexed,
			schema.Tags("test.tag"))

		// Attribute with same tag
		sb.Ref("tagged_ref_2", "test.domain/tagged.ref2",
			schema.Doc("Second tagged reference"),
			schema.Indexed,
			schema.Tags("test.tag"))

		// Attribute with different tag
		sb.Ref("other_ref", "test.domain/other.ref",
			schema.Doc("Reference with different tag"),
			schema.Indexed,
			schema.Tags("other.tag"))

		// Attribute with no tag
		sb.Ref("untagged_ref", "test.domain/untagged.ref",
			schema.Doc("Reference without tag"),
			schema.Indexed)
	})

	// Apply test schemas to the store
	err := schema.Apply(ctx, store)
	require.NoError(t, err)

	t.Run("finds attributes with specific tag", func(t *testing.T) {
		schemas, err := entity.GetAttributesByTag(ctx, store, "test.tag")
		require.NoError(t, err)

		// Should find exactly 2 attributes with "test.tag"
		require.Len(t, schemas, 2, "expected 2 attributes with test.tag")

		// Check that we got the right attributes
		ids := make(map[entity.Id]bool)
		for _, s := range schemas {
			ids[s.ID] = true
			require.Contains(t, s.Tags, "test.tag", "attribute should have test.tag")
			require.True(t, s.Index, "all test attributes should be indexed")
		}

		require.True(t, ids[entity.Id("test.domain/tagged.ref1")], "should include tagged.ref1")
		require.True(t, ids[entity.Id("test.domain/tagged.ref2")], "should include tagged.ref2")
	})

	t.Run("finds attributes with different tag", func(t *testing.T) {
		schemas, err := entity.GetAttributesByTag(ctx, store, "other.tag")
		require.NoError(t, err)

		require.Len(t, schemas, 1, "expected 1 attribute with other.tag")
		require.Equal(t, entity.Id("test.domain/other.ref"), schemas[0].ID)
		require.Contains(t, schemas[0].Tags, "other.tag")
	})

	t.Run("returns empty for non-existent tag", func(t *testing.T) {
		schemas, err := entity.GetAttributesByTag(ctx, store, "nonexistent.tag")
		require.NoError(t, err)
		require.Len(t, schemas, 0, "should return empty slice for non-existent tag")
	})

	t.Run("returns full attribute schema info", func(t *testing.T) {
		schemas, err := entity.GetAttributesByTag(ctx, store, "test.tag")
		require.NoError(t, err)
		require.NotEmpty(t, schemas)

		// Verify we get full schema information, not just IDs
		s := schemas[0]
		require.NotEmpty(t, s.ID, "should have ID")
		require.NotEmpty(t, s.Doc, "should have documentation")
		require.NotEmpty(t, s.Type, "should have type")
		require.True(t, s.Index, "should have index flag")
		require.NotEmpty(t, s.Tags, "should have tags")
	})
}
