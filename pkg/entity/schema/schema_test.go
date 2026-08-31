package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/entity"
)

func init() {
	// Register a test schema with indexed attributes for testing
	sb := &SchemaBuilder{
		domain: "test-index-hash",
		attrs:  make(map[entity.Id]*entity.Entity),
	}

	// Add an indexed attribute
	sb.attrs[entity.Id("test-index-hash/name")] = entity.New(
		entity.Ident, "test-index-hash/name",
		entity.Type, entity.TypeStr,
		entity.Cardinality, entity.CardinalityOne,
		entity.Index, true,
	)

	// Add a non-indexed attribute
	sb.attrs[entity.Id("test-index-hash/doc")] = entity.New(
		entity.Ident, "test-index-hash/doc",
		entity.Type, entity.TypeStr,
		entity.Cardinality, entity.CardinalityOne,
	)

	// Add another indexed attribute
	sb.attrs[entity.Id("test-index-hash/kind")] = entity.New(
		entity.Ident, "test-index-hash/kind",
		entity.Type, entity.TypeRef,
		entity.Cardinality, entity.CardinalityOne,
		entity.Index, true,
	)

	defaultRegistry.schemas["test-index-hash"] = sb
}

func TestIndexedAttributeIDs(t *testing.T) {
	ids := IndexedAttributeIDs()

	require.Greater(t, len(ids), 0, "should have at least one indexed attribute")

	// Verify the list is sorted
	for i := 1; i < len(ids); i++ {
		assert.True(t, ids[i-1] < ids[i], "IDs should be sorted: %s >= %s", ids[i-1], ids[i])
	}

	// Verify no duplicates
	seen := make(map[string]bool)
	for _, id := range ids {
		assert.False(t, seen[string(id)], "duplicate indexed attribute ID: %s", id)
		seen[string(id)] = true
	}

	// Verify our test indexed attributes are present
	assert.True(t, seen["test-index-hash/name"], "test-index-hash/name should be indexed")
	assert.True(t, seen["test-index-hash/kind"], "test-index-hash/kind should be indexed")

	// Verify non-indexed attribute is NOT present
	assert.False(t, seen["test-index-hash/doc"], "test-index-hash/doc should not be indexed")
}

// TestRefChoicesEmitsEnumValues guards MIR-1425: declaring schema.Choices on a
// ref attribute must surface those choices as an EnumValues attribute on the
// built schema entity, which is where the validator reads them from.
func TestRefChoicesEmitsEnumValues(t *testing.T) {
	sb := &SchemaBuilder{
		domain: "test-choices",
		attrs:  make(map[entity.Id]*entity.Entity),
	}

	id := sb.Ref("status", "test-choices/status",
		Doc("The status"),
		Choices(entity.Id("test-choices/status.a"), entity.Id("test-choices/status.b")),
	)

	ent := sb.attrs[id]
	require.NotNil(t, ent)

	attr, ok := ent.Get(entity.EnumValues)
	require.True(t, ok, "Choices should surface as an EnumValues attribute")

	vals, ok := attr.Value.Any().([]entity.Value)
	require.True(t, ok, "EnumValues should be a []Value")
	require.Len(t, vals, 2)
	assert.Equal(t, entity.Id("test-choices/status.a"), vals[0].Any())
	assert.Equal(t, entity.Id("test-choices/status.b"), vals[1].Any())
}

func TestEnumEmitsHeterogeneousValues(t *testing.T) {
	sb := &SchemaBuilder{
		domain: "test-enum",
		attrs:  make(map[entity.Id]*entity.Entity),
	}

	id := sb.Enum("state", "test-enum/state", []any{
		entity.Id("test-enum/state.ready"),
		"legacy",
		entity.MustKeyword("test-enum/state.disabled"),
	}, ElementType(entity.TypeRef))

	ent := sb.attrs[id]
	require.NotNil(t, ent)
	typeAttr, ok := ent.Get(entity.Type)
	require.True(t, ok)
	assert.Equal(t, entity.TypeEnum, typeAttr.Value.Id())
	elemType, ok := ent.Get(entity.EntityElemType)
	require.True(t, ok)
	assert.Equal(t, entity.TypeRef, elemType.Value.Id())

	choices, ok := ent.Get(entity.EnumValues)
	require.True(t, ok)
	values, ok := choices.Value.Any().([]entity.Value)
	require.True(t, ok)
	require.Len(t, values, 3)
	assert.Equal(t, entity.KindId, values[0].Kind())
	assert.Equal(t, entity.KindString, values[1].Kind())
	assert.Equal(t, entity.KindKeyword, values[2].Kind())
}

func TestSingletonDeduplicatesSharedEnumMembers(t *testing.T) {
	sb := &SchemaBuilder{}

	first := sb.Singleton("test-enum/state.ready")
	second := sb.Singleton("test-enum/state.ready")

	assert.Equal(t, first, second)
	assert.Equal(t, []entity.Id{"test-enum/state.ready"}, sb.singletons)
}

func TestIndexHash_Deterministic(t *testing.T) {
	hash1 := IndexHash()
	hash2 := IndexHash()

	require.NotEmpty(t, hash1, "hash should not be empty")
	assert.Equal(t, hash1, hash2, "hash should be deterministic across calls")
}
