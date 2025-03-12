package entity

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
				{ID: EntityIdent, Value: "test/person"},
				{ID: EntityDoc, Value: "A test person"},
			},
			wantErr: false,
		},
		{
			name:       "invalid attribute type",
			entityType: "test",
			attrs: []Attr{
				{ID: EntityIdent, Value: 123}, // Should be string
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entity, err := store.CreateEntity(tt.attrs)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.NotEmpty(t, entity.ID)
			assert.Equal(t, 1, entity.Revision)
			assert.NotZero(t, entity.CreatedAt)
			assert.NotZero(t, entity.UpdatedAt)
			assert.Equal(t, tt.attrs, entity.Attrs)
		})
	}
}

func TestGetEntity(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Create a test entity
	attrs := []Attr{
		{ID: EntityIdent, Value: "test/person"},
		{ID: EntityDoc, Value: "A test person"},
	}
	created, err := store.CreateEntity(attrs)
	require.NoError(t, err)

	// Test getting the entity
	entity, err := store.GetEntity(EntityId(created.ID))
	require.NoError(t, err, "missing %s, %s", created.ID, err)
	assert.Equal(t, created.ID, entity.ID)
	assert.Equal(t, created.Attrs, entity.Attrs)

	// Test getting non-existent entity
	_, err = store.GetEntity("nonexistent")
	assert.ErrorIs(t, err, ErrEntityNotFound)
}

func TestUpdateEntity(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Create initial entity
	initial := []Attr{
		{ID: EntityIdent, Value: "test/person"},
		{ID: EntityDoc, Value: "A test person"},
	}
	entity, err := store.CreateEntity(initial)
	require.NoError(t, err)

	// Update the entity
	updates := []Attr{
		{ID: EntityDoc, Value: "Updated description"},
	}
	time.Sleep(10 * time.Millisecond)
	updated, err := store.UpdateEntity(EntityId(entity.ID), updates)
	require.NoError(t, err)

	assert.Equal(t, entity.ID, updated.ID)
	assert.Equal(t, entity.Revision+1, updated.Revision)
	assert.Greater(t, updated.UpdatedAt, entity.UpdatedAt)

	// Verify the update
	retrieved, err := store.GetEntity(EntityId(entity.ID))
	require.NoError(t, err)
	assert.Equal(t, updated.Attrs, retrieved.Attrs)
}

func TestDeleteEntity(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Create a test entity
	attrs := []Attr{
		{ID: EntityIdent, Value: "test/person"},
		{ID: EntityDoc, Value: "A test person"},
	}
	entity, err := store.CreateEntity(attrs)
	require.NoError(t, err)

	// Delete the entity
	err = store.DeleteEntity(EntityId(entity.ID))
	require.NoError(t, err)

	// Verify the entity is deleted
	_, err = store.GetEntity(EntityId(entity.ID))
	assert.ErrorIs(t, err, ErrEntityNotFound)

	// Try to delete non-existent entity
	err = store.DeleteEntity("nonexistent")
	assert.ErrorIs(t, err, ErrEntityNotFound)
}

func TestEntityAttributes(t *testing.T) {
	entity := &Entity{
		ID: "test",
		Attrs: []Attr{
			{ID: EntityIdent, Value: "test/person"},
			{ID: EntityDoc, Value: "A test person"},
		},
	}

	// Test Get
	attr, ok := entity.Get(EntityIdent)
	require.True(t, ok)
	assert.Equal(t, "test/person", attr.Value)

	// Test Get non-existent
	_, ok = entity.Get("nonexistent")
	assert.False(t, ok)

	// Test Set (update existing)
	entity.Set(EntityIdent, "updated/person")
	attr, ok = entity.Get(EntityIdent)
	require.True(t, ok)
	assert.Equal(t, "updated/person", attr.Value)

	// Test Set (add new)
	entity.Set("new/attr", "new value")
	attr, ok = entity.Get("new/attr")
	require.True(t, ok)
	assert.Equal(t, "new value", attr.Value)

	// Test Remove
	err := entity.Remove(EntityIdent)
	require.NoError(t, err)
	_, ok = entity.Get(EntityIdent)
	require.False(t, ok)

	// Test Remove non-existent
	err = entity.Remove("nonexistent")
	assert.ErrorIs(t, err, ErrAttributeNotFound)
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
				EntityId("test/attr1"), "value1",
				EntityId("test/attr2"), 123,
			},
			want: []Attr{
				{ID: EntityId("test/attr1"), Value: "value1"},
				{ID: EntityId("test/attr2"), Value: 123},
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
	// Create a test entity
	attrs := []Attr{
		{ID: EntityIdent, Value: "test/person"},
		{ID: EntityDoc, Value: "A test person"},
		{ID: EntityKind, Value: "person"},
	}

	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Create a test entity
	entity, err := store.CreateEntity(attrs)
	require.NoError(t, err)

	ids, err := store.ListIndex(EntityKind, "person")
	require.NoError(t, err)

	assert.Contains(t, ids, EntityId(entity.ID))
}
