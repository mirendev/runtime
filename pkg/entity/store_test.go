package entity

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

func setupTestEtcd(t *testing.T) *clientv3.Client {
	client, err := clientv3.New(clientv3.Config{
		Endpoints:       []string{"etcd:2379"},
		DialTimeout:     2 * time.Second,
		MaxUnaryRetries: 2,
	})
	require.NoError(t, err)

	// Clean up any existing test data
	ctx := context.Background()

	_, err = client.Delete(ctx, "/test-entities/", clientv3.WithPrefix())
	require.NoError(t, err)

	t.Cleanup(func() {
		client.Close()
	})

	return client
}

func TestEtcdStore_CreateEntity(t *testing.T) {
	client := setupTestEtcd(t)
	store, err := NewEtcdStore(t.Context(), slog.Default(), client, "/test-entities")
	require.NoError(t, err)

	e, err := store.CreateEntity(t.Context(), New(
		Ident, "test/addresses",
		Doc, "A list of addresses",
		Cardinality, CardinalityMany,
		Type, TypeStr,
	))
	require.NoError(t, err)

	require.Equal(t, Id("test/addresses"), e.Id())

	tests := []struct {
		name       string
		entityType string
		attrs      []Attr
		expected   []Attr
		wantErr    bool
		errMsg     string
	}{
		{
			name:       "valid entity",
			entityType: "test",
			attrs: []Attr{
				Any(Ident, KeywordValue("test1")),
				Any(Doc, "test document"),
			},
			expected: []Attr{
				Ref(DBId, Id("test1")),
				Any(Doc, "test document"),
			},
			wantErr: false,
		},
		{
			name:       "duplicate entity",
			entityType: "test",
			attrs: []Attr{
				Any(Ident, KeywordValue("test1")),
				Any(Doc, "duplicate"),
			},
			wantErr: true,
		},
		{
			name:       "invalid attribute",
			entityType: "test",
			attrs: []Attr{
				Any(Ident, 123), // Wrong type for ident
			},
			wantErr: true,
			errMsg:  "invalid attribute",
		},
		{
			name:       "duplicate cardinality.one attribute",
			entityType: "test",
			attrs: []Attr{
				Any(Ident, KeywordValue("test4")),
				Any(Doc, "first doc"),
				Any(Doc, "second doc"), // EntityDoc is cardinality.one
			},
			wantErr: true,
			errMsg:  "cardinality violation",
		},
		{
			name:       "valid cardinality.many attribute",
			entityType: "test",
			attrs: []Attr{
				Any(Ident, KeywordValue("test5")),
				Any(Id("test/addresses"), "val1"),
				Any(Id("test/addresses"), "val2"),
			},
			expected: []Attr{
				Ref(DBId, Id("test5")),
				Any(Id("test/addresses"), "val1"),
				Any(Id("test/addresses"), "val2"),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entity, err := store.CreateEntity(t.Context(), New(tt.attrs))
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.NotEmpty(t, entity.Id())
			tt.attrs = append(tt.attrs, Ref(DBId, entity.Id()))
			// Check that each expected attribute is present (entity will have additional system attributes)
			expected := tt.expected
			if expected == nil {
				expected = tt.attrs
			}
			for _, expectedAttr := range expected {
				found := false
				for _, actualAttr := range entity.Attrs() {
					if expectedAttr.Equal(actualAttr) {
						found = true
						break
					}
				}
				assert.True(t, found, "expected attribute %s not found in entity %s", expectedAttr.ID, entity.Id())
			}
			assert.Greater(t, entity.GetRevision(), int64(0))
			assert.False(t, entity.GetCreatedAt().IsZero())
			assert.False(t, entity.GetUpdatedAt().IsZero())
		})
	}

	t.Run("with session attributes", func(t *testing.T) {
		r := require.New(t)

		_, err := store.CreateEntity(t.Context(), New(
			Ident, "test/status",
			Doc, "A Status",
			Cardinality, CardinalityMany,
			Type, TypeStr,
			Session, true,
		))
		require.NoError(t, err)

		sid, err := store.CreateSession(t.Context(), 30)
		r.NoError(err)

		addr := String(Id("test/status"), "foo")
		//addr.Session = sid

		attrs := []Attr{addr}

		entity, err := store.CreateEntity(t.Context(), New(attrs), WithSession(sid))
		require.NoError(t, err)

		e2, err := store.GetEntity(t.Context(), entity.Id())
		r.NoError(err)

		sa, ok := e2.Get(Id("test/status"))
		r.True(ok)

		r.Equal(addr, sa)

		r.NoError(store.RevokeSession(t.Context(), sid))

		e3, err := store.GetEntity(t.Context(), entity.Id())
		r.NoError(err)

		_, ok = e3.Get(Id("test/status"))
		r.False(ok)
	})
}

func TestEtcdStore_AttrPred(t *testing.T) {
	client := setupTestEtcd(t)
	store, err := NewEtcdStore(t.Context(), slog.Default(), client, "/test-entities")
	require.NoError(t, err)

	e, err := store.CreateEntity(t.Context(), New(
		Ident, "test/address",
		Doc, "An address",
		Cardinality, CardinalityMany,
		Type, TypeStr,
		AttrPred, Id("db/pred.ip"),
	))
	require.NoError(t, err)

	require.Equal(t, Id("test/address"), e.Id())

	tests := []struct {
		name       string
		entityType string
		attrs      []Attr
		wantErr    bool
		errMsg     string
	}{
		{
			name:       "valid entity",
			entityType: "test",
			attrs: []Attr{
				Any(Id("test/address"), "10.0.1.1"),
			},
			wantErr: false,
		},
		{
			name:       "invalid attribute",
			entityType: "test",
			attrs: []Attr{
				Any(Id("test/address"), "hello"),
			},
			wantErr: true,
			errMsg:  "invalid attribute",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entity, err := store.CreateEntity(t.Context(), New(tt.attrs))
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.NotEmpty(t, entity.Id())
			tt.attrs = append(tt.attrs, Ref(DBId, entity.Id()))
			// Check that each expected attribute is present (entity will have additional system attributes)
			for _, expectedAttr := range tt.attrs {
				found := false
				for _, actualAttr := range entity.Attrs() {
					if expectedAttr.Equal(actualAttr) {
						found = true
						break
					}
				}
				assert.True(t, found, "expected attribute %s not found in entity", expectedAttr.ID)
			}
			assert.Greater(t, entity.GetRevision(), int64(0))
			assert.False(t, entity.GetCreatedAt().IsZero())
			assert.False(t, entity.GetUpdatedAt().IsZero())
		})
	}
}

func TestEtcdStore_GetEntity(t *testing.T) {
	client := setupTestEtcd(t)
	store, err := NewEtcdStore(t.Context(), slog.Default(), client, "/test-entities")
	require.NoError(t, err)

	// Create a test entity
	created, err := store.CreateEntity(t.Context(), New(
		Any(Ident, "test1"),
		Any(Doc, "test document"),
	))
	require.NoError(t, err)

	tests := []struct {
		name    string
		id      Id
		want    *Entity
		wantErr bool
	}{
		{
			name:    "existing entity",
			id:      Id(created.Id()),
			want:    created,
			wantErr: false,
		},
		{
			name:    "non-existent entity",
			id:      "nonexistent",
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := store.GetEntity(t.Context(), tt.id)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assertEntityEqual(t, tt.want, got)
		})
	}
}

func TestEtcdStore_UpdateEntity(t *testing.T) {
	client := setupTestEtcd(t)
	store, err := NewEtcdStore(t.Context(), slog.Default(), client, "/test-entities")
	require.NoError(t, err)

	// Create a test entity
	entity, err := store.CreateEntity(t.Context(), New(
		Any(Ident, "test1"),
		Any(Doc, "original doc"),
	))
	require.NoError(t, err)

	tests := []struct {
		name      string
		id        Id
		attrs     []Attr
		wantAttrs int
		wantErr   bool
	}{
		{
			name: "valid update",
			id:   Id(entity.Id()),
			attrs: []Attr{
				Any(Doc, "updated doc"),
			},
			wantAttrs: 5,
			wantErr:   false,
		},
		{
			name: "invalid attribute",
			id:   Id(entity.Id()),
			attrs: []Attr{
				Any(Ident, 123), // Wrong type
			},
			wantErr: true,
		},
		{
			name: "non-existent entity",
			id:   "nonexistent",
			attrs: []Attr{
				Any(Doc, "won't work"),
			},
			wantErr: true,
		},
	}

	time.Sleep(10 * time.Millisecond)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updated, err := store.UpdateEntity(t.Context(), tt.id, New(tt.attrs))
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantAttrs, len(updated.Attrs()))
			assert.NotEqual(t, 0, updated.GetRevision())
			assert.NotEqual(t, updated.GetCreatedAt(), updated.GetUpdatedAt())
		})
	}

	t.Run("with session attributes", func(t *testing.T) {
		r := require.New(t)

		_, err := store.CreateEntity(t.Context(), New(
			Ident, "test/kind",
			Doc, "a kind",
			Cardinality, CardinalityOne,
			Type, TypeStr,
			Index, true,
		))
		require.NoError(t, err)

		_, err = store.CreateEntity(t.Context(), New(
			Ident, "test/status",
			Doc, "A Status",
			Cardinality, CardinalityMany,
			Type, TypeStr,
			Session, true,
		))
		require.NoError(t, err)

		sid, err := store.CreateSession(t.Context(), 30)
		r.NoError(err)

		entity, err := store.CreateEntity(t.Context(), New(
			String(Id("test/kind"), "foo"),
		))
		require.NoError(t, err)

		addr := String(Id("test/status"), "foo")

		attrs := []Attr{addr}

		_, err = store.UpdateEntity(t.Context(), entity.Id(), New(attrs), WithSession(sid))
		require.NoError(t, err)

		e2, err := store.GetEntity(t.Context(), entity.Id())
		r.NoError(err)

		sa, ok := e2.Get(Id("test/status"))
		r.True(ok)

		r.Equal(addr, sa)

		wc, err := store.WatchIndex(t.Context(), String(Id("test/kind"), "foo"))
		r.NoError(err)

		r.NoError(store.RevokeSession(t.Context(), sid))

		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()

		select {
		case <-ctx.Done():
			t.Error("timed out waiting for session revoke")
		case <-t.Context().Done():
			r.NoError(t.Context().Err())
		case x := <-wc:
			// The session revoke should send an event to the index watcher
			r.Len(x.Events, 1)
			r.Equal(x.Events[0].Type, mvccpb.DELETE)
			r.Equal(x.Events[0].PrevKv.Value, []byte(entity.Id()))
			// This delete should be for the session-based index, not the main entity one
			r.Contains(string(x.Events[0].PrevKv.Key), base58.Encode(sid))
		}

		// Entity should still exist, but no longer have the test/status
		e3, err := store.GetEntity(t.Context(), entity.Id())
		r.NoError(err)

		_, ok = e3.Get(Id("test/status"))
		r.False(ok)
	})

	t.Run("from a fixed revision", func(t *testing.T) {
		r := require.New(t)

		e, err := store.GetEntity(t.Context(), entity.Id())
		r.NoError(err)

		_, err = store.UpdateEntity(t.Context(), e.Id(), New(
			Any(Doc, "updated document"),
		), WithFromRevision(e.GetRevision()-1))
		r.Error(err)

		_, err = store.UpdateEntity(t.Context(), e.Id(), New(
			Any(Doc, "updated document from rev"),
		), WithFromRevision(e.GetRevision()))
		r.NoError(err)

		e2, err := store.GetEntity(t.Context(), entity.Id())
		r.NoError(err)

		a, ok := e2.Get(Doc)
		r.True(ok)

		r.Equal("updated document from rev", a.Value.String())
	})
}

func TestEtcdStore_GetEntities_Batching(t *testing.T) {
	client := setupTestEtcd(t)
	store, err := NewEtcdStore(t.Context(), slog.Default(), client, "/test-entities")
	require.NoError(t, err)

	// Create test entities - more than maxEntitiesPerBatch to test batching
	numEntities := 150 // This will require 3 batches (64 + 64 + 22)
	var entityIds []Id

	for i := 0; i < numEntities; i++ {
		entity, err := store.CreateEntity(t.Context(), New(
			Any(Ident, KeywordValue(fmt.Sprintf("test-entity-%d", i))),
			Any(Doc, fmt.Sprintf("Test entity %d", i)),
		))
		require.NoError(t, err)
		entityIds = append(entityIds, entity.Id())
	}

	// Test getting all entities at once
	t.Run("get all entities in batches", func(t *testing.T) {
		entities, err := store.GetEntities(t.Context(), entityIds)
		require.NoError(t, err)
		require.Len(t, entities, numEntities)

		// Verify all entities were retrieved correctly
		for i, entity := range entities {
			require.NotNil(t, entity, "entity at index %d should not be nil", i)

			// Check the ident attribute matches what we created
			identAttr, ok := entity.Get(DBId)
			require.True(t, ok)
			expectedIdent := fmt.Sprintf("test-entity-%d", i)
			require.Equal(t, RefValue(Id(expectedIdent)), identAttr.Value)
		}
	})

	// Test order preservation across batch boundaries
	t.Run("order preservation", func(t *testing.T) {
		// Create a specific order that crosses batch boundaries
		// Include indices from different batches: 0-63 (batch 1), 64-127 (batch 2), 128-149 (batch 3)
		orderedIds := []Id{
			entityIds[10],  // batch 1
			entityIds[70],  // batch 2
			entityIds[130], // batch 3
			entityIds[63],  // last of batch 1
			entityIds[64],  // first of batch 2
			entityIds[127], // last of batch 2
			entityIds[128], // first of batch 3
			entityIds[0],   // first overall
			entityIds[149], // last overall
		}

		entities, err := store.GetEntities(t.Context(), orderedIds)
		require.NoError(t, err)
		require.Len(t, entities, len(orderedIds))

		// Verify order is preserved
		expectedIndices := []int{10, 70, 130, 63, 64, 127, 128, 0, 149}
		for i, expectedIdx := range expectedIndices {
			require.NotNil(t, entities[i])
			identAttr, ok := entities[i].Get(DBId)
			require.True(t, ok)
			expectedIdent := fmt.Sprintf("test-entity-%d", expectedIdx)
			require.Equal(t, RefValue(Id(expectedIdent)), identAttr.Value)

			// Also verify the entity ID matches
			require.Equal(t, orderedIds[i], entities[i].Id())
		}
	})

	// Test with empty slice
	t.Run("empty slice", func(t *testing.T) {
		entities, err := store.GetEntities(t.Context(), []Id{})
		require.NoError(t, err)
		require.Empty(t, entities)
	})

	// Test with some non-existent entities mixed in
	t.Run("mixed existing and non-existing", func(t *testing.T) {
		mixedIds := []Id{
			entityIds[0],
			"non-existent-1",
			entityIds[1],
			"non-existent-2",
			entityIds[2],
		}

		entities, err := store.GetEntities(t.Context(), mixedIds)
		require.NoError(t, err)
		require.Len(t, entities, 5)

		// Check that existing entities are returned and non-existent are nil
		require.NotNil(t, entities[0])
		require.Nil(t, entities[1])
		require.NotNil(t, entities[2])
		require.Nil(t, entities[3])
		require.NotNil(t, entities[4])
	})

	// Test exact batch boundary
	t.Run("exact batch size", func(t *testing.T) {
		exactBatchIds := entityIds[:maxEntitiesPerBatch]
		entities, err := store.GetEntities(t.Context(), exactBatchIds)
		require.NoError(t, err)
		require.Len(t, entities, maxEntitiesPerBatch)

		for _, entity := range entities {
			require.NotNil(t, entity)
		}
	})
}

func TestEtcdStore_DeleteEntity(t *testing.T) {
	client := setupTestEtcd(t)
	store, err := NewEtcdStore(t.Context(), slog.Default(), client, "/test-entities")
	require.NoError(t, err)

	// Create a test entity
	entity, err := store.CreateEntity(t.Context(), New(
		Any(Ident, "test1"),
		Any(Doc, "test document"),
	))
	require.NoError(t, err)

	tests := []struct {
		name    string
		id      Id
		wantErr bool
	}{
		{
			name:    "existing entity",
			id:      Id(entity.Id()),
			wantErr: false,
		},
		{
			name:    "non-existent entity",
			id:      "nonexistent",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.DeleteEntity(t.Context(), tt.id)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)

			// Verify entity is gone
			_, err = store.GetEntity(t.Context(), tt.id)
			assert.Error(t, err)
		})
	}
}

func TestEtcdStore_ListIndex(t *testing.T) {
	client := setupTestEtcd(t)
	store, err := NewEtcdStore(t.Context(), slog.Default(), client, "/test-entities")
	require.NoError(t, err)

	k, err := store.CreateEntity(t.Context(), New(
		Any(Ident, KeywordValue("test/kind")),
	))
	require.NoError(t, err)

	// Create test entities with indexed attributes
	_, err = store.CreateEntity(t.Context(), New(
		Any(Ident, KeywordValue("test1")),
		Ref(EntityKind, k.Id()),
	))
	require.NoError(t, err)

	_, err = store.CreateEntity(t.Context(), New(
		Any(Ident, KeywordValue("test2")),
		Ref(EntityKind, k.Id()),
	))
	require.NoError(t, err)

	tests := []struct {
		name      string
		attrID    Id
		value     Value
		wantCount int
		wantErr   bool
	}{
		{
			name:      "valid index",
			attrID:    EntityKind,
			value:     RefValue(k.Id()),
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:      "non-existent value",
			attrID:    EntityKind,
			value:     RefValue("xx"),
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:      "non-indexed attribute",
			attrID:    Doc,
			value:     StringValue("test"),
			wantCount: 0,
			wantErr:   true,
		},
		{
			name:      "indexed by dbid, with wrong kind of attribute value",
			attrID:    DBId,
			value:     StringValue("test1"),
			wantCount: 0,
			wantErr:   true,
		},
		{
			name:      "indexed by dbid",
			attrID:    DBId,
			value:     RefValue("test1"),
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "indexed by dbid when the id is not there",
			attrID:    DBId,
			value:     RefValue("not-there"),
			wantCount: 0,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entities, err := store.ListIndex(t.Context(), Attr{ID: tt.attrID, Value: tt.value})
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, entities, tt.wantCount)
		})
	}
}

func TestWatchIndex(t *testing.T) {
	ctx := context.Background()
	client := setupTestEtcd(t)
	store, err := NewEtcdStore(t.Context(), slog.Default(), client, "/test-entities")
	require.NoError(t, err)

	// Create a attr for an indexed attribute
	attr, err := store.CreateEntity(ctx, New(
		String(Ident, "test-index"),
		Ref(Type, TypeStr),
		Bool(Index, true),
	))
	if err != nil {
		t.Fatalf("Failed to create schema: %v", err)
	}

	// Start watching the index before creating any entities
	watcher, err := store.WatchIndex(ctx, String(attr.Id(), "value1"))
	if err != nil {
		t.Fatalf("Failed to create watcher: %v", err)
	}

	// Create an entity with the indexed attribute
	entity1, err := store.CreateEntity(ctx, New(
		String(attr.Id(), "value1"),
		String(Ident, "test-entity-1"),
	))
	if err != nil {
		t.Fatalf("Failed to create entity: %v", err)
	}

	// Wait for and verify the creation event
	select {
	case wr := <-watcher:
		assert.Len(t, wr.Events, 1)
		for _, event := range wr.Events {
			if event.Type != clientv3.EventTypePut {
				t.Errorf("Expected PUT event, got %v", event.Type)
			}
			if string(event.Kv.Value) != string(entity1.Id()) {
				t.Errorf("Expected entity ID %s, got %s", entity1.Id(), event.Kv.Value)
			}
		}
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for creation event")
	}

	// Update the entity with a new value for the indexed attribute
	_, err = store.UpdateEntity(ctx, entity1.Id(), New(
		String(attr.Id(), "value2"),
	))
	if err != nil {
		t.Fatalf("Failed to update entity: %v", err)
	}

	/* FIXME
	// Wait for and verify the deletion event (old value removed from index)
	select {
	case wr := <-watcher:
		assert.Len(t, wr.Events, 1)
		for _, event := range wr.Events {
			if event.Type != clientv3.EventTypeDelete {
				t.Errorf("Expected DELETE event, got %v", event.Type)
			}
		}
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for deletion event")
	}
	*/

	// Create a new watcher for the new value
	watcher2, err := store.WatchIndex(ctx, String(attr.Id(), "value2"))
	if err != nil {
		t.Fatalf("Failed to create second watcher: %v", err)
	}

	// Delete the entity
	err = store.DeleteEntity(ctx, entity1.Id())
	if err != nil {
		t.Fatalf("Failed to delete entity: %v", err)
	}

	// Wait for and verify the deletion event
	select {
	case wr := <-watcher2:
		assert.Len(t, wr.Events, 1)
		for _, event := range wr.Events {
			if event.Type != clientv3.EventTypeDelete {
				t.Errorf("Expected DELETE event, got %v", event.Type)
			}
		}
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for deletion event")
	}

	// Test watching a non-indexed attribute
	nonIndexedSchema, err := store.CreateEntity(ctx, New(
		String(Ident, "non-indexed"),
		Ref(Type, TypeStr),
		Bool(Index, false),
	))
	if err != nil {
		t.Fatalf("Failed to create non-indexed schema: %v", err)
	}

	_, err = store.WatchIndex(ctx, String(nonIndexedSchema.Id(), "value"))
	if err == nil {
		t.Error("Expected error when watching non-indexed attribute")
	}
}

func TestWatchIndex_DBID(t *testing.T) {
	ctx := context.Background()
	client := setupTestEtcd(t)
	store, err := NewEtcdStore(t.Context(), slog.Default(), client, "/test-entities")
	require.NoError(t, err)

	// Create an entity with the indexed attribute
	entity1, err := store.CreateEntity(ctx, New(
		String(Ident, "test-entity-1"),
		String(Doc, "test document"),
	))
	if err != nil {
		t.Fatalf("Failed to create entity: %v", err)
	}

	// Start watching the index before creating any entities
	watcher, err := store.WatchIndex(ctx, Ref(DBId, "test-entity-1"))
	if err != nil {
		t.Fatalf("Failed to create watcher: %v", err)
	}

	_, err = store.UpdateEntity(ctx, entity1.Id(), New(
		String(Doc, "now with more doc"),
	))
	if err != nil {
		t.Fatalf("Failed to update entity: %v", err)
	}

	// Wait for and verify the creation event
	select {
	case wr := <-watcher:
		assert.Len(t, wr.Events, 1)
		for _, event := range wr.Events {
			if event.Type != clientv3.EventTypePut {
				t.Errorf("Expected PUT event, got %v", event.Type)
			}
			if string(event.Kv.Value) != string(entity1.Id()) {
				t.Errorf("Expected entity ID %s, got %s", entity1.Id(), event.Kv.Value)
			}
		}
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for creation event")
	}
}

func TestEtcdStore_UpdateIndexedAttribute_Bug(t *testing.T) {
	client := setupTestEtcd(t)
	store, err := NewEtcdStore(t.Context(), slog.Default(), client, "/test-entities")
	require.NoError(t, err)

	// Create an index attribute that won't change
	kindAttr, err := store.CreateEntity(t.Context(), New(
		Ident, "test/kind",
		Doc, "a kind",
		Cardinality, CardinalityOne,
		Type, TypeStr,
		Index, true,
	))
	require.NoError(t, err)

	// Create an indexed attribute schema that we'll change below (similar to our default route boolean)
	indexedAttr, err := store.CreateEntity(t.Context(), New(
		String(Ident, "test/default"),
		Ref(Type, TypeBool),
		Bool(Index, true),
		Ref(Cardinality, CardinalityOne),
	))
	require.NoError(t, err)

	// Create first entity with default=true
	entity1, err := store.CreateEntity(t.Context(), New(
		String(Ident, "app1"),
		String(kindAttr.Id(), "test/app"),
		Bool(indexedAttr.Id(), true),
	))
	require.NoError(t, err)

	// Create second entity with default=false
	entity2, err := store.CreateEntity(t.Context(), New(
		String(Ident, "app2"),
		String(kindAttr.Id(), "test/app"),
		Bool(indexedAttr.Id(), false),
	))
	require.NoError(t, err)

	// Verify initial state: only entity1 should be found when searching for default=true
	entitiesWithTrue, err := store.ListIndex(t.Context(), Bool(indexedAttr.Id(), true))
	require.NoError(t, err)
	assert.Len(t, entitiesWithTrue, 1)
	assert.Equal(t, entity1.Id(), entitiesWithTrue[0])

	// Verify only entity2 is found when searching for default=false
	entitiesWithFalse, err := store.ListIndex(t.Context(), Bool(indexedAttr.Id(), false))
	require.NoError(t, err)
	assert.Len(t, entitiesWithFalse, 1)
	assert.Equal(t, entity2.Id(), entitiesWithFalse[0])

	// Now update: set entity2 to default=true
	_, err = store.UpdateEntity(t.Context(), entity2.Id(), New(
		Bool(indexedAttr.Id(), true),
	))
	require.NoError(t, err)

	// Verify after update: both entities should be found with default=true
	entitiesWithTrueAfter, err := store.ListIndex(t.Context(), Bool(indexedAttr.Id(), true))
	require.NoError(t, err)
	assert.Len(t, entitiesWithTrueAfter, 2)

	// Sort for consistent comparison
	foundIDs := []string{string(entitiesWithTrueAfter[0]), string(entitiesWithTrueAfter[1])}
	if foundIDs[0] > foundIDs[1] {
		foundIDs[0], foundIDs[1] = foundIDs[1], foundIDs[0]
	}
	expectedIDs := []string{string(entity1.Id()), string(entity2.Id())}
	if expectedIDs[0] > expectedIDs[1] {
		expectedIDs[0], expectedIDs[1] = expectedIDs[1], expectedIDs[0]
	}
	assert.Equal(t, expectedIDs, foundIDs)

	// Verify none have default=false
	entitiesWithFalseAfter, err := store.ListIndex(t.Context(), Bool(indexedAttr.Id(), false))
	require.NoError(t, err)
	assert.Len(t, entitiesWithFalseAfter, 0)
}

func TestEtcdStore_GetEntities(t *testing.T) {
	client := setupTestEtcd(t)
	store, err := NewEtcdStore(t.Context(), slog.Default(), client, "/test-entities")
	require.NoError(t, err)

	// Create multiple test entities
	entity1, err := store.CreateEntity(t.Context(), New(
		Any(Ident, "test-entity-1"),
		Any(Doc, "first test document"),
	))
	require.NoError(t, err)

	entity2, err := store.CreateEntity(t.Context(), New(
		Any(Ident, "test-entity-2"),
		Any(Doc, "second test document"),
	))
	require.NoError(t, err)

	entity3, err := store.CreateEntity(t.Context(), New(
		Any(Ident, "test-entity-3"),
		Any(Doc, "third test document"),
	))
	require.NoError(t, err)

	tests := []struct {
		name       string
		ids        []Id
		wantCount  int
		wantNils   int
		checkOrder bool
		wantErr    bool
	}{
		{
			name:      "empty ID list",
			ids:       []Id{},
			wantCount: 0,
			wantNils:  0,
			wantErr:   false,
		},
		{
			name:       "single existing entity",
			ids:        []Id{entity1.Id()},
			wantCount:  1,
			wantNils:   0,
			checkOrder: true,
			wantErr:    false,
		},
		{
			name:       "multiple existing entities",
			ids:        []Id{entity1.Id(), entity2.Id(), entity3.Id()},
			wantCount:  3,
			wantNils:   0,
			checkOrder: true,
			wantErr:    false,
		},
		{
			name:       "multiple existing entities in different order",
			ids:        []Id{entity3.Id(), entity1.Id(), entity2.Id()},
			wantCount:  3,
			wantNils:   0,
			checkOrder: true,
			wantErr:    false,
		},
		{
			name:      "non-existent entities",
			ids:       []Id{"non-existent-1", "non-existent-2"},
			wantCount: 2,
			wantNils:  2,
			wantErr:   false,
		},
		{
			name:       "mixed existing and non-existing entities",
			ids:        []Id{entity1.Id(), "non-existent", entity2.Id(), "another-non-existent"},
			wantCount:  4,
			wantNils:   2,
			checkOrder: true,
			wantErr:    false,
		},
		{
			name:       "duplicate IDs",
			ids:        []Id{entity1.Id(), entity1.Id(), entity2.Id()},
			wantCount:  3,
			wantNils:   0,
			checkOrder: true,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := store.GetEntities(t.Context(), tt.ids)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, got, tt.wantCount)

			// Count nils
			nilCount := 0
			for _, e := range got {
				if e == nil {
					nilCount++
				}
			}
			assert.Equal(t, tt.wantNils, nilCount)

			// Check order preservation
			if tt.checkOrder {
				for i, id := range tt.ids {
					if got[i] != nil {
						assert.Equal(t, id, got[i].Id(), "Entity at index %d should have ID %s", i, id)
					}
				}
			}

			// Verify entity contents for non-nil results
			for i, entity := range got {
				if entity != nil {
					// Verify it has expected attributes
					assert.NotEmpty(t, entity.Attrs())
					assert.Greater(t, entity.GetRevision(), int64(0))
					assert.False(t, entity.GetCreatedAt().IsZero())
					assert.False(t, entity.GetUpdatedAt().IsZero())

					// Check that the ID matches what we requested
					assert.Equal(t, tt.ids[i], entity.Id())
				}
			}
		})
	}

	t.Run("with session attributes", func(t *testing.T) {
		r := require.New(t)

		// Create a schema for session attribute
		_, err := store.CreateEntity(t.Context(), New(
			Ident, "test/session-status",
			Doc, "A session status",
			Cardinality, CardinalityMany,
			Type, TypeStr,
			Session, true,
		))
		r.NoError(err)

		// Create session
		sid, err := store.CreateSession(t.Context(), 30)
		r.NoError(err)

		// Create entities with session attributes
		entity4, err := store.CreateEntity(t.Context(), New(
			Any(Ident, "test-entity-4"),
		), WithSession(sid))
		r.NoError(err)

		// Add session attribute
		_, err = store.UpdateEntity(t.Context(), entity4.Id(), New(
			String(Id("test/session-status"), "active"),
		), WithSession(sid))
		r.NoError(err)

		entity5, err := store.CreateEntity(t.Context(), New(
			Any(Ident, "test-entity-5"),
			String(Id("test/session-status"), "pending"),
		), WithSession(sid))
		r.NoError(err)

		// Get entities with session attributes
		entities, err := store.GetEntities(t.Context(), []Id{entity4.Id(), entity5.Id()})
		r.NoError(err)
		r.Len(entities, 2)

		// Verify session attributes are included
		for i, e := range entities {
			r.NotNil(e)
			status, ok := e.Get(Id("test/session-status"))
			r.True(ok, "Entity %d should have session status", i)
			r.NotEmpty(status.Value.String())

			// Check for session ID attribute
			sessionAttr, ok := e.Get(AttrSession)
			r.True(ok, "Entity %d should have session attribute", i)
			r.NotEmpty(sessionAttr.Value.String())
		}

		// Revoke session and verify session attributes are gone
		r.NoError(store.RevokeSession(t.Context(), sid))

		// Get entities again
		entities, err = store.GetEntities(t.Context(), []Id{entity4.Id(), entity5.Id()})
		r.NoError(err)
		r.Len(entities, 2)

		// Verify session attributes are removed
		for i, e := range entities {
			r.NotNil(e)
			_, ok := e.Get(Id("test/session-status"))
			r.False(ok, "Entity %d should not have session status after revoke", i)
		}
	})

	t.Run("large batch", func(t *testing.T) {
		r := require.New(t)

		// Create a larger batch of entities
		var ids []Id
		numEntities := 50
		for i := 0; i < numEntities; i++ {
			entity, err := store.CreateEntity(t.Context(), New(
				Any(Ident, fmt.Sprintf("batch-entity-%d", i)),
				Any(Doc, fmt.Sprintf("Batch document %d", i)),
			))
			r.NoError(err)
			ids = append(ids, entity.Id())
		}

		// Add some non-existent IDs
		ids = append(ids, "non-existent-batch-1", "non-existent-batch-2")

		// Get all entities
		entities, err := store.GetEntities(t.Context(), ids)
		r.NoError(err)
		r.Len(entities, numEntities+2)

		// Verify we got the right number of valid entities
		validCount := 0
		for _, e := range entities {
			if e != nil {
				validCount++
			}
		}
		r.Equal(numEntities, validCount)

		// Verify order is preserved
		for i := 0; i < numEntities; i++ {
			r.NotNil(entities[i])
			r.Equal(ids[i], entities[i].Id())
		}

		// Last two should be nil
		r.Nil(entities[numEntities])
		r.Nil(entities[numEntities+1])
	})
}

func TestEtcdStore_ReplaceEntity(t *testing.T) {
	client := setupTestEtcd(t)
	store, err := NewEtcdStore(t.Context(), slog.Default(), client, "/test-entities")
	require.NoError(t, err)

	// Create schema for multi-valued attribute
	_, err = store.CreateEntity(t.Context(), New(
		Ident, "test/tags",
		Doc, "Tags for testing",
		Cardinality, CardinalityMany,
		Type, TypeStr,
	))
	require.NoError(t, err)

	t.Run("replace all attributes with db/id", func(t *testing.T) {
		// Create initial entity with multiple attributes
		initial, err := store.CreateEntity(t.Context(), New(
			Keyword(Ident, "test-replace-1"),
			Any(Doc, "Initial document"),
			String(Id("test/tags"), "tag1"),
			String(Id("test/tags"), "tag2"),
			String(Id("test/tags"), "tag3"),
		))
		require.NoError(t, err)
		require.Equal(t, Id("test-replace-1"), initial.Id())

		// Replace with new attributes using db/id
		replaced, err := store.ReplaceEntity(t.Context(), New(
			Ref(DBId, initial.Id()),
			Any(Doc, "Replaced document"),
			String(Id("test/tags"), "new-tag"),
		))
		require.NoError(t, err)
		assert.Equal(t, initial.Id(), replaced.Id())
		assert.Greater(t, replaced.GetRevision(), initial.GetRevision())

		// Verify old tags are removed
		retrieved, err := store.GetEntity(t.Context(), initial.Id())
		require.NoError(t, err)

		tags := retrieved.GetAll(Id("test/tags"))
		assert.Len(t, tags, 1, "Should only have one tag after replace")
		assert.Equal(t, "new-tag", tags[0].Value.String())

		doc, ok := retrieved.Get(Doc)
		require.True(t, ok)
		assert.Equal(t, "Replaced document", doc.Value.String())
	})

	t.Run("replace without db/id fails", func(t *testing.T) {
		_, err := store.ReplaceEntity(t.Context(), New(
			Any(Doc, "No ID provided"),
		))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db/id attribute is required")
	})

	t.Run("replace non-existent entity fails", func(t *testing.T) {
		_, err := store.ReplaceEntity(t.Context(), New(
			Any(DBId, Id("non-existent")),
			Any(Doc, "Will not exist"),
		))
		assert.Error(t, err)
	})

	t.Run("replace with revision check succeeds", func(t *testing.T) {
		initial, err := store.CreateEntity(t.Context(), New(
			Any(Ident, "test-replace-revision"),
			Any(Doc, "Initial"),
		))
		require.NoError(t, err)

		replaced, err := store.ReplaceEntity(t.Context(), New(
			Any(DBId, initial.Id()),
			Any(Doc, "Replaced with revision"),
		), WithFromRevision(initial.GetRevision()))
		require.NoError(t, err)
		assert.Greater(t, replaced.GetRevision(), initial.GetRevision())
	})

	t.Run("replace with wrong revision fails", func(t *testing.T) {
		initial, err := store.CreateEntity(t.Context(), New(
			Any(Ident, "test-replace-bad-revision"),
			Any(Doc, "Initial"),
		))
		require.NoError(t, err)

		_, err = store.ReplaceEntity(t.Context(), New(
			Any(DBId, initial.Id()),
			Any(Doc, "Will fail"),
		), WithFromRevision(9999))
		assert.Error(t, err)
	})
}

func TestEtcdStore_PatchEntity(t *testing.T) {
	client := setupTestEtcd(t)
	store, err := NewEtcdStore(t.Context(), slog.Default(), client, "/test-entities")
	require.NoError(t, err)

	// Create schema for multi-valued attribute
	_, err = store.CreateEntity(t.Context(), New(
		Ident, "test/labels",
		Doc, "Labels for testing",
		Cardinality, CardinalityMany,
		Type, TypeStr,
	))
	require.NoError(t, err)

	t.Run("patch adds to cardinality many", func(t *testing.T) {
		initial, err := store.CreateEntity(t.Context(), New(
			Any(Ident, "test-patch-1"),
			Any(Doc, "Initial document"),
			String(Id("test/labels"), "label1"),
			String(Id("test/labels"), "label2"),
		))
		require.NoError(t, err)

		// Patch to add more labels
		patched, err := store.PatchEntity(t.Context(), New(
			Any(DBId, initial.Id()),
			String(Id("test/labels"), "label3"),
			String(Id("test/labels"), "label4"),
		))
		require.NoError(t, err)
		assert.Greater(t, patched.GetRevision(), initial.GetRevision())

		// Verify all labels exist
		retrieved, err := store.GetEntity(t.Context(), initial.Id())
		require.NoError(t, err)

		labels := retrieved.GetAll(Id("test/labels"))
		assert.Len(t, labels, 4, "Should have all labels after patch")
	})

	t.Run("patch replaces cardinality one", func(t *testing.T) {
		initial, err := store.CreateEntity(t.Context(), New(
			Any(Ident, "test-patch-2"),
			Any(Doc, "Initial document"),
		))
		require.NoError(t, err)

		// Patch to update doc
		_, err = store.PatchEntity(t.Context(), New(
			Any(DBId, initial.Id()),
			Any(Doc, "Updated document"),
		))
		require.NoError(t, err)

		// Verify doc was replaced
		retrieved, err := store.GetEntity(t.Context(), initial.Id())
		require.NoError(t, err)

		doc, ok := retrieved.Get(Doc)
		require.True(t, ok)
		assert.Equal(t, "Updated document", doc.Value.String())
	})

	t.Run("patch without db/id fails", func(t *testing.T) {
		_, err := store.PatchEntity(t.Context(), New(
			Any(Doc, "No ID provided"),
		))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db/id attribute is required")
	})

	t.Run("patch non-existent entity fails", func(t *testing.T) {
		_, err := store.PatchEntity(t.Context(), New(
			Any(DBId, Id("non-existent-patch")),
			Any(Doc, "Will not exist"),
		))
		assert.Error(t, err)
	})

	t.Run("patch with revision check succeeds", func(t *testing.T) {
		initial, err := store.CreateEntity(t.Context(), New(
			Any(Ident, "test-patch-revision"),
			Any(Doc, "Initial"),
		))
		require.NoError(t, err)

		patched, err := store.PatchEntity(t.Context(), New(
			Any(DBId, initial.Id()),
			Any(Doc, "Patched with revision"),
		), WithFromRevision(initial.GetRevision()))
		require.NoError(t, err)
		assert.Greater(t, patched.GetRevision(), initial.GetRevision())
	})

	t.Run("patch with wrong revision fails", func(t *testing.T) {
		initial, err := store.CreateEntity(t.Context(), New(
			Any(Ident, "test-patch-bad-revision"),
			Any(Doc, "Initial"),
		))
		require.NoError(t, err)

		_, err = store.PatchEntity(t.Context(), New(
			Any(DBId, initial.Id()),
			Any(Doc, "Will fail"),
		), WithFromRevision(9999))
		assert.Error(t, err)
	})
}

func TestEtcdStore_EnsureEntity(t *testing.T) {
	client := setupTestEtcd(t)
	store, err := NewEtcdStore(t.Context(), slog.Default(), client, "/test-entities")
	require.NoError(t, err)

	t.Run("ensure creates entity when not exists", func(t *testing.T) {
		entity, created, err := store.EnsureEntity(t.Context(), New(
			Ref(DBId, Id("test-ensure-1")),
			Any(Doc, "Ensured document"),
		))
		require.NoError(t, err)
		assert.True(t, created, "Should be created on first ensure")
		assert.Equal(t, Id("test-ensure-1"), entity.Id())

		// Verify entity was created
		retrieved, err := store.GetEntity(t.Context(), Id("test-ensure-1"))
		require.NoError(t, err)
		assert.Equal(t, entity.Id(), retrieved.Id())
	})

	t.Run("ensure returns existing entity when exists", func(t *testing.T) {
		// Create entity first
		initial, err := store.CreateEntity(t.Context(), New(
			Any(Ident, "test-ensure-2"),
			Any(Doc, "Initial document"),
		))
		require.NoError(t, err)

		// Ensure with same ID should return existing
		entity, created, err := store.EnsureEntity(t.Context(), New(
			Any(DBId, initial.Id()),
			Any(Doc, "Different document"), // This should be ignored
		))
		require.NoError(t, err)
		assert.False(t, created, "Should not be created on second ensure")
		assert.Equal(t, initial.Id(), entity.Id())
		assert.Equal(t, initial.GetRevision(), entity.GetRevision())

		// Verify original attributes are unchanged
		doc, ok := entity.Get(Doc)
		require.True(t, ok)
		assert.Equal(t, "Initial document", doc.Value.String())
	})

	t.Run("ensure without db/id fails", func(t *testing.T) {
		_, _, err := store.EnsureEntity(t.Context(), New(
			Any(Doc, "No ID provided"),
		))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db/id attribute is required")
	})

	t.Run("ensure is idempotent", func(t *testing.T) {
		// First ensure creates
		entity1, created1, err := store.EnsureEntity(t.Context(), New(
			Any(DBId, Id("test-ensure-idempotent")),
			Any(Doc, "Idempotent test"),
		))
		require.NoError(t, err)
		assert.True(t, created1)

		// Second ensure returns existing
		entity2, created2, err := store.EnsureEntity(t.Context(), New(
			Any(DBId, Id("test-ensure-idempotent")),
			Any(Doc, "Different data"),
		))
		require.NoError(t, err)
		assert.False(t, created2)
		assert.Equal(t, entity1.Id(), entity2.Id())
		assert.Equal(t, entity1.GetRevision(), entity2.GetRevision())

		// Third ensure also returns existing
		entity3, created3, err := store.EnsureEntity(t.Context(), New(
			Any(DBId, Id("test-ensure-idempotent")),
			Any(Doc, "Yet more different data"),
		))
		require.NoError(t, err)
		assert.False(t, created3)
		assert.Equal(t, entity1.Id(), entity3.Id())
		assert.Equal(t, entity1.GetRevision(), entity3.GetRevision())
	})

	t.Run("concurrent ensure creates only once", func(t *testing.T) {
		// This tests race condition handling
		entityID := Id("test-ensure-concurrent")

		// Try to ensure concurrently (simulate race condition)
		entity1, created1, err1 := store.EnsureEntity(t.Context(), New(
			Any(DBId, entityID),
			Any(Doc, "First attempt"),
		))
		require.NoError(t, err1)

		entity2, created2, err2 := store.EnsureEntity(t.Context(), New(
			Any(DBId, entityID),
			Any(Doc, "Second attempt"),
		))
		require.NoError(t, err2)

		// One should be created, one should return existing
		assert.True(t, created1 != created2, "Exactly one should be created")
		assert.Equal(t, entity1.Id(), entity2.Id())

		// The one that wasn't created should have the same revision
		if !created2 {
			assert.Equal(t, entity1.GetRevision(), entity2.GetRevision())
		}
	})
}

func TestEtcdStore_NestedComponentFieldIndexing(t *testing.T) {
	client := setupTestEtcd(t)
	store, err := NewEtcdStore(t.Context(), slog.Default(), client, "/test-entities")
	require.NoError(t, err)

	t.Run("top-level indexed fields work", func(t *testing.T) {
		// Create version entities first
		v1 := Id("version/v1")
		v2 := Id("version/v2")

		_, err := store.CreateEntity(t.Context(), New(
			Ref(DBId, v1),
		))
		require.NoError(t, err)

		_, err = store.CreateEntity(t.Context(), New(
			Ref(DBId, v2),
		))
		require.NoError(t, err)

		// Create an indexed attribute at the top level
		_, err = store.CreateEntity(t.Context(), New(
			Ident, "test/version",
			Doc, "Top-level version field",
			Cardinality, CardinalityOne,
			Type, TypeRef,
			Index, true,
		))
		require.NoError(t, err)

		// Create entities with this top-level indexed field
		entity1, err := store.CreateEntity(t.Context(), New(
			Ident, "sandbox1",
			Ref(Id("test/version"), v1),
		))
		require.NoError(t, err)

		entity2, err := store.CreateEntity(t.Context(), New(
			Ident, "sandbox2",
			Ref(Id("test/version"), v1),
		))
		require.NoError(t, err)

		entity3, err := store.CreateEntity(t.Context(), New(
			Ident, "sandbox3",
			Ref(Id("test/version"), v2),
		))
		require.NoError(t, err)

		// Query by the top-level indexed field - THIS WORKS
		results, err := store.ListIndex(t.Context(), Ref(Id("test/version"), v1))
		require.NoError(t, err)
		assert.Len(t, results, 2, "Should find both entities with v1")

		// Verify we got the right entities
		foundIds := map[Id]bool{results[0]: true, results[1]: true}
		assert.True(t, foundIds[entity1.Id()])
		assert.True(t, foundIds[entity2.Id()])
		assert.False(t, foundIds[entity3.Id()])
	})

	t.Run("nested component fields can be indexed", func(t *testing.T) {
		// This test verifies that nested fields within components are automatically indexed
		// when marked with indexed: true in the schema.

		// Create a component attribute type
		_, err := store.CreateEntity(t.Context(), New(
			Ident, "test/indexed-spec",
			Doc, "A component type",
			Cardinality, CardinalityOne,
			Type, TypeComponent,
		))
		require.NoError(t, err)

		// Create an indexed field that will be nested in the component
		_, err = store.CreateEntity(t.Context(), New(
			Ident, "test/indexed-spec.version",
			Doc, "Version field within indexed-spec component",
			Cardinality, CardinalityOne,
			Type, TypeRef,
			Index, true, // Mark nested field as indexed
		))
		require.NoError(t, err)

		// Create version entities
		v1 := Id("version/indexed-v1")
		v2 := Id("version/indexed-v2")

		_, err = store.CreateEntity(t.Context(), New(Ref(DBId, v1)))
		require.NoError(t, err)

		_, err = store.CreateEntity(t.Context(), New(Ref(DBId, v2)))
		require.NoError(t, err)

		// Create entities with components containing the indexed nested field
		entity1, err := store.CreateEntity(t.Context(), New([]Attr{
			Keyword(Ident, "indexed-resource1"),
			Component(Id("test/indexed-spec"), []Attr{
				Ref(Id("test/indexed-spec.version"), v1), // Nested indexed field
			}),
		}))
		require.NoError(t, err)

		entity2, err := store.CreateEntity(t.Context(), New([]Attr{
			Keyword(Ident, "indexed-resource2"),
			Component(Id("test/indexed-spec"), []Attr{
				Ref(Id("test/indexed-spec.version"), v1), // Same version
			}),
		}))
		require.NoError(t, err)

		entity3, err := store.CreateEntity(t.Context(), New([]Attr{
			Keyword(Ident, "indexed-resource3"),
			Component(Id("test/indexed-spec"), []Attr{
				Ref(Id("test/indexed-spec.version"), v2), // Different version
			}),
		}))
		require.NoError(t, err)

		// Query by the nested indexed field
		results, err := store.ListIndex(t.Context(), Ref(Id("test/indexed-spec.version"), v1))
		require.NoError(t, err)
		assert.Len(t, results, 2, "Should find both entities with v1")

		// Verify we got the right entities
		foundIds := map[Id]bool{results[0]: true, results[1]: true}
		assert.True(t, foundIds[entity1.Id()])
		assert.True(t, foundIds[entity2.Id()])
		assert.False(t, foundIds[entity3.Id()])

		// Query by v2
		resultsV2, err := store.ListIndex(t.Context(), Ref(Id("test/indexed-spec.version"), v2))
		require.NoError(t, err)
		assert.Len(t, resultsV2, 1, "Should find entity with v2")
		assert.Equal(t, entity3.Id(), resultsV2[0])
	})

	t.Run("PatchEntity rebuilds indexes for nested component fields", func(t *testing.T) {
		// This test verifies that PatchEntity properly handles nested indexed fields
		// when updating entities - both creating new indexes and removing old ones.

		// Create a component attribute type
		_, err := store.CreateEntity(t.Context(), New(
			Ident, "test/patch-spec",
			Doc, "A component type for patch testing",
			Cardinality, CardinalityOne,
			Type, TypeComponent,
		))
		require.NoError(t, err)

		// Create an indexed field that will be nested in the component
		_, err = store.CreateEntity(t.Context(), New(
			Ident, "test/patch-spec.version",
			Doc, "Version field within patch-spec component",
			Cardinality, CardinalityOne,
			Type, TypeRef,
			Index, true, // Mark nested field as indexed
		))
		require.NoError(t, err)

		// Create version entities
		v1 := Id("version/patch-v1")
		v2 := Id("version/patch-v2")

		_, err = store.CreateEntity(t.Context(), New(Ref(DBId, v1)))
		require.NoError(t, err)

		_, err = store.CreateEntity(t.Context(), New(Ref(DBId, v2)))
		require.NoError(t, err)

		// Create an entity with nested indexed field pointing to v1
		entity, err := store.CreateEntity(t.Context(), New([]Attr{
			Keyword(Ident, "patch-resource"),
			Component(Id("test/patch-spec"), []Attr{
				Ref(Id("test/patch-spec.version"), v1),
			}),
		}))
		require.NoError(t, err)

		// Verify it's indexed under v1
		results, err := store.ListIndex(t.Context(), Ref(Id("test/patch-spec.version"), v1))
		require.NoError(t, err)
		assert.Len(t, results, 1, "Should find entity indexed under v1")
		assert.Equal(t, entity.Id(), results[0])

		// Verify it's NOT indexed under v2
		results, err = store.ListIndex(t.Context(), Ref(Id("test/patch-spec.version"), v2))
		require.NoError(t, err)
		assert.Len(t, results, 0, "Should not find entity indexed under v2")

		// Patch the entity to change the nested version from v1 to v2
		patched := New([]Attr{
			Ref(DBId, entity.Id()),
			Component(Id("test/patch-spec"), []Attr{
				Ref(Id("test/patch-spec.version"), v2), // Changed to v2
			}),
		})

		_, err = store.PatchEntity(t.Context(), patched, WithFromRevision(entity.GetRevision()))
		require.NoError(t, err)

		// After patch, entity should be indexed under v2
		results, err = store.ListIndex(t.Context(), Ref(Id("test/patch-spec.version"), v2))
		require.NoError(t, err)
		assert.Len(t, results, 1, "Should find entity indexed under v2 after patch")
		assert.Equal(t, entity.Id(), results[0])

		// After patch, entity should NO LONGER be indexed under v1
		results, err = store.ListIndex(t.Context(), Ref(Id("test/patch-spec.version"), v1))
		require.NoError(t, err)
		assert.Len(t, results, 0, "Should not find entity indexed under v1 after patch")
	})

	// Note: DeleteEntity nested index cleanup is tested at the entityserver level
	// in servers/entityserver/entityserver_test.go::TestEntityServer_List_NestedIndexCleanup
	// That test properly catches the bug by testing the full RPC path that production uses.
}

// TestEtcdStore_DeleteEntity_IndexCleanup_DirectQuery tests that DeleteEntity properly
// cleans up indexes by directly querying etcd to inspect the index collections.
//
// Why direct etcd queries are necessary:
//   - ListIndex internally deduplicates results, so calling ListIndex after deletion
//     may return the correct count even if stale index entries remain in etcd
//   - This test queries etcd directly to verify that the actual index collection keys
//     are removed, not just that ListIndex returns the right IDs
//   - When the bug exists, this test shows 2 keys remain in etcd after deletion
//   - When fixed, only 1 key remains (the non-deleted entity)
func TestEtcdStore_DeleteEntity_IndexCleanup_DirectQuery(t *testing.T) {
	client := setupTestEtcd(t)

	// Clean up test data from previous runs
	ctx := context.Background()
	_, err := client.Delete(ctx, "/test-entities-debug/", clientv3.WithPrefix())
	require.NoError(t, err)

	store, err := NewEtcdStore(t.Context(), slog.Default(), client, "/test-entities-debug")
	require.NoError(t, err)

	// Create schema for a component with a nested indexed field
	_, err = store.CreateEntity(t.Context(), New(
		Ident, "debug/spec",
		Doc, "A component type",
		Cardinality, CardinalityOne,
		Type, TypeComponent,
	))
	require.NoError(t, err)

	_, err = store.CreateEntity(t.Context(), New(
		Ident, "debug/spec.version",
		Doc, "Version field within spec component",
		Cardinality, CardinalityOne,
		Type, TypeRef,
		Index, true,
	))
	require.NoError(t, err)

	// Create a version entity
	v1 := Id("version/debug-v1")
	_, err = store.CreateEntity(t.Context(), New(Ref(DBId, v1)))
	require.NoError(t, err)

	// Create two entities with nested indexed fields
	entity1, err := store.CreateEntity(t.Context(), New([]Attr{
		Keyword(Ident, "debug-resource1"),
		Component(Id("debug/spec"), []Attr{
			Ref(Id("debug/spec.version"), v1),
		}),
	}))
	require.NoError(t, err)

	entity2, err := store.CreateEntity(t.Context(), New([]Attr{
		Keyword(Ident, "debug-resource2"),
		Component(Id("debug/spec"), []Attr{
			Ref(Id("debug/spec.version"), v1),
		}),
	}))
	require.NoError(t, err)

	// Query etcd directly to see the collection keys BEFORE deletion
	attr := Ref(Id("debug/spec.version"), v1)
	collectionKey := attr.CAS()
	prefix, err := store.CollectionPrefix(t.Context(), collectionKey)
	require.NoError(t, err)

	t.Logf("Collection prefix: %s", prefix)

	resp, err := client.Get(t.Context(), prefix, clientv3.WithPrefix())
	require.NoError(t, err)
	t.Logf("BEFORE DELETE: Found %d keys in etcd collection", len(resp.Kvs))
	for _, kv := range resp.Kvs {
		t.Logf("  Key: %s, Value: %s", string(kv.Key), string(kv.Value))
	}
	require.Len(t, resp.Kvs, 2, "Should have 2 index entries before deletion")

	// Delete entity1
	err = store.DeleteEntity(t.Context(), entity1.Id())
	require.NoError(t, err)

	// Query etcd directly AFTER deletion
	resp, err = client.Get(t.Context(), prefix, clientv3.WithPrefix())
	require.NoError(t, err)
	t.Logf("AFTER DELETE: Found %d keys in etcd collection", len(resp.Kvs))
	for _, kv := range resp.Kvs {
		t.Logf("  Key: %s, Value: %s", string(kv.Key), string(kv.Value))
	}

	// CRITICAL: If the bug exists, this should show 2 keys (stale index)
	// If the fix works, this should show 1 key
	require.Len(t, resp.Kvs, 1, "Should have only 1 index entry after deletion - entity1's entry should be removed")

	// Verify the remaining entry is for entity2
	assert.Equal(t, entity2.Id().String(), string(resp.Kvs[0].Value))
}

func TestEntity_Fixup_DbId(t *testing.T) {
	t.Run("db/id takes precedence over db/ident", func(t *testing.T) {
		entity := New(
			Any(DBId, Id("id-from-dbid")),
			Any(Ident, "id-from-ident"),
		)
		assert.Equal(t, Id("id-from-dbid"), entity.Id(), "db/id should take precedence")
	})

	t.Run("db/ident used when db/id not present", func(t *testing.T) {
		entity := New(
			Any(Ident, "id-from-ident"),
		)
		entity.Fixup() // Fixup converts db/ident to db/id
		assert.Equal(t, Id("id-from-ident"), entity.Id(), "db/ident should be used as fallback")
	})

	t.Run("db/ident with string value", func(t *testing.T) {
		entity := New(
			Any(Ident, "string-id"),
		)
		entity.Fixup() // Fixup converts db/ident to db/id
		assert.Equal(t, Id("string-id"), entity.Id())
	})

	t.Run("db/id with Id value", func(t *testing.T) {
		entity := New(
			Any(DBId, Id("typed-id")),
		)
		assert.Equal(t, Id("typed-id"), entity.Id())
	})

	t.Run("db/ident with keyword value", func(t *testing.T) {
		entity := New(
			Keyword(Ident, "keyword-id"),
		)
		entity.Fixup() // Fixup converts db/ident to db/id
		assert.Equal(t, Id("keyword-id"), entity.Id())
	})

	t.Run("invalid db/id type fails", func(t *testing.T) {
		// New no longer returns errors, invalid types are handled during Fixup
		_ = New(
			Any(DBId, 12345), // Invalid type
		)
		// This test is no longer applicable since New doesn't return errors
	})

	t.Run("fixup populates a temporary Id", func(t *testing.T) {
		entity := New(
			Any(Doc, "Just a document"),
		)

		entity.ForceID()
		assert.NotEqual(t, Id(""), entity.Id())
	})

	t.Run("New uses entity kind for ID prefix when no ID provided", func(t *testing.T) {
		entity := New([]Attr{
			Ref(EntityKind, "test/project"),
			Any(Doc, "A test project"),
		})
		require.NotNil(t, entity)

		entity.ForceID()

		// ID should be auto-generated with kind prefix (using last segment after / or .)
		assert.NotEqual(t, Id(""), entity.Id())
		// For "test/project", should use "project" as prefix
		assert.True(t, strings.HasPrefix(string(entity.Id()), "project-"), "ID %s should start with project-", entity.Id())
	})

	t.Run("New uses first kind for ID prefix when multiple kinds", func(t *testing.T) {
		entity := New([]Attr{
			Ref(EntityKind, "test/project"),
			Ref(EntityKind, "test/resource"),
			Any(Doc, "A multi-kind entity"),
		})
		require.NotNil(t, entity)

		entity.ForceID()

		// ID should use the first kind's last segment as prefix
		assert.True(t, strings.HasPrefix(string(entity.Id()), "project-"), "ID %s should start with project-", entity.Id())
	})

	t.Run("New uses last segment after dot when kind has dots", func(t *testing.T) {
		entity := New([]Attr{
			Ref(EntityKind, "dev.miren.core/kind.project"),
			Any(Doc, "A project with dotted kind"),
		})
		require.NotNil(t, entity)

		entity.ForceID()
		// For "dev.miren.core/kind.project", rightmost separator is "." so should use "project" as prefix
		assert.True(t, strings.HasPrefix(string(entity.Id()), "project-"), "ID %s should start with project-", entity.Id())
	})

	t.Run("New falls back to generic ID when no kind provided", func(t *testing.T) {
		entity := New([]Attr{
			Any(Doc, "An entity without a kind"),
		})
		require.NotNil(t, entity)

		entity.ForceID()
		// ID should be auto-generated with generic prefix (e-)
		assert.NotEqual(t, Id(""), entity.Id())
		// Should start with generic 'e-' prefix
		assert.True(t, strings.HasPrefix(string(entity.Id()), "e-"), "ID %s should start with e-", entity.Id())
	})
}

func TestExtractEntityId(t *testing.T) {
	t.Run("handles types.Id", func(t *testing.T) {
		attrs := []Attr{
			Ref(DBId, Id("test/id1")),
		}
		id, err := extractEntityId(attrs)
		require.NoError(t, err)
		assert.Equal(t, Id("test/id1"), id)
	})

	t.Run("handles types.Keyword", func(t *testing.T) {
		// This test verifies the fix for production issue where db/id
		// values were being unmarshaled as types.Keyword instead of types.Id
		attrs := []Attr{
			Keyword(DBId, "test/id2"),
		}
		id, err := extractEntityId(attrs)
		require.NoError(t, err)
		assert.Equal(t, Id("test/id2"), id)
	})

	t.Run("handles string", func(t *testing.T) {
		attrs := []Attr{
			String(DBId, "test/id3"),
		}
		id, err := extractEntityId(attrs)
		require.NoError(t, err)
		assert.Equal(t, Id("test/id3"), id)
	})

	t.Run("returns error when db/id missing", func(t *testing.T) {
		attrs := []Attr{
			String(Doc, "some doc"),
		}
		_, err := extractEntityId(attrs)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "db/id attribute is required")
	})

	t.Run("returns error for invalid type", func(t *testing.T) {
		attrs := []Attr{
			Int64(DBId, 123),
		}
		_, err := extractEntityId(attrs)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid db/id attribute type")
	})
}
