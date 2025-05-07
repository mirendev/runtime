package entity

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/entity/types"
)

func setupTestStore(t *testing.T) (*FileStore, func()) {
	tmpDir := t.TempDir()
	store, err := NewFileStore(tmpDir)
	require.NoError(t, err)

	cleanup := func() {
		os.RemoveAll(tmpDir)
	}

	return store, cleanup
}

func TestCreateEntity(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	tests := []struct {
		name       string
		entityType string
		attrs      []Attr
		wantErr    bool
	}{
		{
			name:       "valid entity",
			entityType: "test",
			attrs: []Attr{
				Any(Ident, KeywordValue("test/person")),
				String(Doc, "A test person"),
			},
			wantErr: false,
		},
		{
			name:       "invalid attribute type",
			entityType: "test",
			attrs: []Attr{
				Int(Ident, 123), // Should be string
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entity, err := store.CreateEntity(t.Context(), tt.attrs)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.NotEmpty(t, entity.ID)
			assert.Equal(t, int64(1), entity.Revision)
			assert.NotZero(t, entity.CreatedAt)
			assert.NotZero(t, entity.UpdatedAt)
			assert.Equal(t, SortedAttrs(tt.attrs), entity.Attrs)
		})
	}
}

func TestGetEntity(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Create a test entity
	attrs := []Attr{
		Any(Ident, "test/person"),
		Any(Doc, "A test person"),
	}
	created, err := store.CreateEntity(t.Context(), attrs)
	require.NoError(t, err)

	// Test getting the entity
	entity, err := store.GetEntity(t.Context(), Id(created.ID))
	require.NoError(t, err, "missing %s, %s", created.ID, err)
	assert.Equal(t, created.ID, entity.ID)
	assert.Equal(t, created.Attrs, entity.Attrs)

	// Test getting non-existent entity
	_, err = store.GetEntity(t.Context(), "nonexistent")
	assert.ErrorIs(t, err, ErrEntityNotFound)
}

func TestUpdateEntity(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Create initial entity
	initial := []Attr{
		Any(Ident, "test/person"),
		Any(Doc, "A test person"),
	}
	entity, err := store.CreateEntity(t.Context(), initial)
	require.NoError(t, err)

	// Update the entity
	updates := []Attr{
		Any(Doc, "Updated description"),
	}
	time.Sleep(10 * time.Millisecond)
	updated, err := store.UpdateEntity(t.Context(), Id(entity.ID), updates)
	require.NoError(t, err)

	assert.Equal(t, entity.ID, updated.ID)
	assert.Equal(t, entity.Revision+1, updated.Revision)
	assert.Greater(t, updated.UpdatedAt, entity.UpdatedAt)

	// Verify the update
	retrieved, err := store.GetEntity(t.Context(), Id(entity.ID))
	require.NoError(t, err)
	assert.Equal(t, updated.Attrs, retrieved.Attrs)
}

func TestDeleteEntity(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Create a test entity
	attrs := []Attr{
		Any(Ident, "test/person"),
		Any(Doc, "A test person"),
	}
	entity, err := store.CreateEntity(t.Context(), attrs)
	require.NoError(t, err)

	// Delete the entity
	err = store.DeleteEntity(t.Context(), Id(entity.ID))
	require.NoError(t, err)

	// Verify the entity is deleted
	_, err = store.GetEntity(t.Context(), Id(entity.ID))
	assert.ErrorIs(t, err, ErrEntityNotFound)

	// Try to delete non-existent entity
	err = store.DeleteEntity(t.Context(), "nonexistent")
	assert.ErrorIs(t, err, ErrEntityNotFound)
}

func TestEntityAttributes(t *testing.T) {
	entity := &Entity{
		ID: "test",
		Attrs: []Attr{
			Any(Ident, KeywordValue("test/person")),
			Any(Doc, "A test person"),
		},
	}

	// Test Get
	attr, ok := entity.Get(Ident)
	require.True(t, ok)
	assert.Equal(t, types.Keyword("test/person"), attr.Value.Any())

	// Test Get non-existent
	_, ok = entity.Get("nonexistent")
	assert.False(t, ok)
}

func TestEntityComponentAttributes(t *testing.T) {
	component := &EntityComponent{
		Attrs: []Attr{
			Any(Doc, "A test component"),
			Any(Type, "test/type"),
		},
	}

	// Test Get
	attr, ok := component.Get(Doc)
	require.True(t, ok)
	assert.Equal(t, "A test component", attr.Value.Any())

	// Test Get non-existent
	_, ok = component.Get("nonexistent")
	assert.False(t, ok)
}

func TestAttrsHelper(t *testing.T) {
	tests := []struct {
		name      string
		input     []any
		wantPanic bool
		want      []Attr
	}{
		{
			name: "valid pairs",
			input: []any{
				Id("test/attr1"), "value1",
				Id("test/attr2"), 123,
			},
			want: []Attr{
				Any(Id("test/attr1"), "value1"),
				Any(Id("test/attr2"), 123),
			},
		},
		{
			name:      "odd number of arguments",
			input:     []any{"test/attr1"},
			wantPanic: true,
		},
		{
			name: "invalid key type",
			input: []any{
				123, "value1",
			},
			wantPanic: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantPanic {
				assert.Panics(t, func() {
					Attrs(tt.input...)
				})
				return
			}

			attrs := Attrs(tt.input...)
			assert.Equal(t, tt.want, attrs)
		})
	}
}

func TestIndices(t *testing.T) {

	store, cleanup := setupTestStore(t)
	defer cleanup()

	pk, err := store.CreateEntity(t.Context(), Attrs(
		Any(Ident, "attr/person"),
		Any(Index, true),
	))
	require.NoError(t, err)

	// Create a test entity
	attrs := []Attr{
		Any(Ident, "test/person"),
		Any(Doc, "A test person"),
		Ref(EntityKind, pk.ID),
	}

	// Create a test entity
	entity, err := store.CreateEntity(t.Context(), attrs)
	require.NoError(t, err)

	ids, err := store.ListIndex(t.Context(), Ref(EntityKind, pk.ID))
	require.NoError(t, err)

	assert.Contains(t, ids, Id(entity.ID))
}

func TestValidKeyword(t *testing.T) {
	tests := []struct {
		name  string
		input string
		bad   bool
	}{
		{
			name:  "bare",
			input: "test",
		},
		{
			name:  "namespaced",
			input: "test/foo",
		},
		{
			name:  "deep namespaced",
			input: "bar/test/foo",
		},
		{
			name:  "snaked",
			input: "bar_bar",
		},
		{
			name:  "kabob",
			input: "bar-bar",
		},
		{
			name:  "dots",
			input: "bar.bar",
		},
		{
			name:  "colon",
			input: "bar:bar",
		},
		{
			name:  "numbers",
			input: "test/bar18",
		},
		{
			name:  "bad char",
			input: "test*",
			bad:   true,
		},
		{
			name:  "separator at end",
			input: "test/",
			bad:   true,
		},
		{
			name:  "number at start",
			input: "18test",
			bad:   true,
		},
		{
			name:  "has spaces",
			input: "foo bar",
			bad:   true,
		},
		{
			name:  "has special",
			input: "foo\r\b",
			bad:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok := ValidKeyword(tt.input)
			require.Equal(t, !tt.bad, ok)
		})
	}
}

func TestRemove(t *testing.T) {
	// Create a test entity
	attrs := []Attr{
		Any(Ident, "test/person"),
		Any(Doc, "A test person"),
	}

	ent := &Entity{Attrs: attrs}

	ent.Remove(Doc)

	r := require.New(t)

	r.Len(ent.Attrs, 1)
}

func TestEntity(t *testing.T) {
	t.Run("dedups attributes", func(t *testing.T) {
		r := require.New(t)

		attrs := []Attr{
			Any(Ident, "test/person"),
			Any(Doc, "A test person"),
			Any(Doc, "A test person"),
		}

		ent := &Entity{Attrs: attrs}

		ent.Fixup()

		r.Len(ent.Attrs, 2)
	})
}
