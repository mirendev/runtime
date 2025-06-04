package entity

import (
	"context"
	"fmt"
	"log/slog"
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

	e, err := store.CreateEntity(t.Context(), Attrs(
		Ident, "test/addresses",
		Doc, "A list of addresses",
		Cardinality, CardinalityMany,
		Type, TypeStr,
	))
	require.NoError(t, err)

	require.Equal(t, Id("test/addresses"), e.ID)

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
				Any(Ident, KeywordValue("test1")),
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
			wantErr: false,
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
			assert.Equal(t, SortedAttrs(tt.attrs), entity.Attrs)
			assert.Greater(t, entity.Revision, int64(0))
			assert.NotZero(t, entity.CreatedAt)
			assert.NotZero(t, entity.UpdatedAt)
		})
	}

	t.Run("with session attributes", func(t *testing.T) {
		r := require.New(t)

		_, err := store.CreateEntity(t.Context(), Attrs(
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

		entity, err := store.CreateEntity(t.Context(), attrs, WithSession(sid))
		require.NoError(t, err)

		e2, err := store.GetEntity(t.Context(), entity.ID)
		r.NoError(err)

		sa, ok := e2.Get(Id("test/status"))
		r.True(ok)

		r.Equal(addr, sa)

		r.NoError(store.RevokeSession(t.Context(), sid))

		e3, err := store.GetEntity(t.Context(), entity.ID)
		r.NoError(err)

		_, ok = e3.Get(Id("test/status"))
		r.False(ok)
	})
}

func TestEtcdStore_AttrPred(t *testing.T) {
	client := setupTestEtcd(t)
	store, err := NewEtcdStore(t.Context(), slog.Default(), client, "/test-entities")
	require.NoError(t, err)

	e, err := store.CreateEntity(t.Context(), Attrs(
		Ident, "test/address",
		Doc, "An address",
		Cardinality, CardinalityMany,
		Type, TypeStr,
		AttrPred, Id("db/pred.ip"),
	))
	require.NoError(t, err)

	require.Equal(t, Id("test/address"), e.ID)

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
			entity, err := store.CreateEntity(t.Context(), tt.attrs)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.NotEmpty(t, entity.ID)
			assert.Equal(t, tt.attrs, entity.Attrs)
			assert.Greater(t, entity.Revision, int64(0))
			assert.NotZero(t, entity.CreatedAt)
			assert.NotZero(t, entity.UpdatedAt)
		})
	}
}

func TestEtcdStore_GetEntity(t *testing.T) {
	client := setupTestEtcd(t)
	store, err := NewEtcdStore(t.Context(), slog.Default(), client, "/test-entities")
	require.NoError(t, err)

	// Create a test entity
	created, err := store.CreateEntity(t.Context(), []Attr{
		Any(Ident, "test1"),
		Any(Doc, "test document"),
	})
	require.NoError(t, err)

	tests := []struct {
		name    string
		id      Id
		want    *Entity
		wantErr bool
	}{
		{
			name:    "existing entity",
			id:      Id(created.ID),
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
			assert.Equal(t, tt.want.ID, got.ID)
			assert.Equal(t, tt.want.Attrs, got.Attrs)
		})
	}
}

func TestEtcdStore_UpdateEntity(t *testing.T) {
	client := setupTestEtcd(t)
	store, err := NewEtcdStore(t.Context(), slog.Default(), client, "/test-entities")
	require.NoError(t, err)

	// Create a test entity
	entity, err := store.CreateEntity(t.Context(), []Attr{
		Any(Ident, "test1"),
		Any(Doc, "original doc"),
	})
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
			id:   Id(entity.ID),
			attrs: []Attr{
				Any(Doc, "updated doc"),
			},
			wantAttrs: 2,
			wantErr:   false,
		},
		{
			name: "invalid attribute",
			id:   Id(entity.ID),
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
			updated, err := store.UpdateEntity(t.Context(), tt.id, tt.attrs)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantAttrs, len(updated.Attrs))
			assert.NotEqual(t, 0, updated.Revision)
			assert.NotEqual(t, updated.CreatedAt, updated.UpdatedAt)
		})
	}

	t.Run("with session attributes", func(t *testing.T) {
		r := require.New(t)

		_, err := store.CreateEntity(t.Context(), Attrs(
			Ident, "test/kind",
			Doc, "a kind",
			Cardinality, CardinalityOne,
			Type, TypeStr,
			Index, true,
		))
		require.NoError(t, err)

		_, err = store.CreateEntity(t.Context(), Attrs(
			Ident, "test/status",
			Doc, "A Status",
			Cardinality, CardinalityMany,
			Type, TypeStr,
			Session, true,
		))
		require.NoError(t, err)

		sid, err := store.CreateSession(t.Context(), 30)
		r.NoError(err)

		entity, err := store.CreateEntity(t.Context(), []Attr{
			String(Id("test/kind"), "foo"),
		})
		require.NoError(t, err)

		addr := String(Id("test/status"), "foo")

		attrs := []Attr{addr}

		_, err = store.UpdateEntity(t.Context(), entity.ID, attrs, WithSession(sid))
		require.NoError(t, err)

		e2, err := store.GetEntity(t.Context(), entity.ID)
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
			r.Equal(x.Events[0].PrevKv.Value, []byte(entity.ID))
			// This delete should be for the session-based index, not the main entity one
			r.Contains(string(x.Events[0].PrevKv.Key), base58.Encode(sid))
		}

		// Entity should still exist, but no longer have the test/status
		e3, err := store.GetEntity(t.Context(), entity.ID)
		r.NoError(err)

		_, ok = e3.Get(Id("test/status"))
		r.False(ok)
	})

	t.Run("from a fixed revision", func(t *testing.T) {
		r := require.New(t)

		e, err := store.GetEntity(t.Context(), entity.ID)
		r.NoError(err)

		_, err = store.UpdateEntity(t.Context(), e.ID, []Attr{
			Any(Doc, "updated document"),
		}, WithFromRevision(e.Revision-1))
		r.Error(err)

		_, err = store.UpdateEntity(t.Context(), e.ID, []Attr{
			Any(Doc, "updated document from rev"),
		}, WithFromRevision(e.Revision))
		r.NoError(err)

		e2, err := store.GetEntity(t.Context(), entity.ID)
		r.NoError(err)

		a, ok := e2.Get(Doc)
		r.True(ok)

		r.Equal("updated document from rev", a.Value.String())
	})
}

func TestEtcdStore_DeleteEntity(t *testing.T) {
	client := setupTestEtcd(t)
	store, err := NewEtcdStore(t.Context(), slog.Default(), client, "/test-entities")
	require.NoError(t, err)

	// Create a test entity
	entity, err := store.CreateEntity(t.Context(), []Attr{
		Any(Ident, "test1"),
		Any(Doc, "test document"),
	})
	require.NoError(t, err)

	tests := []struct {
		name    string
		id      Id
		wantErr bool
	}{
		{
			name:    "existing entity",
			id:      Id(entity.ID),
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

	k, err := store.CreateEntity(t.Context(), []Attr{
		Any(Ident, KeywordValue("test/kind")),
	})
	require.NoError(t, err)

	// Create test entities with indexed attributes
	_, err = store.CreateEntity(t.Context(), []Attr{
		Any(Ident, KeywordValue("test1")),
		Ref(EntityKind, k.ID),
	})
	require.NoError(t, err)

	_, err = store.CreateEntity(t.Context(), []Attr{
		Any(Ident, KeywordValue("test2")),
		Ref(EntityKind, k.ID),
	})
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
			value:     RefValue(k.ID),
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
	attr, err := store.CreateEntity(ctx, []Attr{
		String(Ident, "test-index"),
		Ref(Type, TypeStr),
		Bool(Index, true),
	})
	if err != nil {
		t.Fatalf("Failed to create schema: %v", err)
	}

	// Start watching the index before creating any entities
	watcher, err := store.WatchIndex(ctx, String(attr.ID, "value1"))
	if err != nil {
		t.Fatalf("Failed to create watcher: %v", err)
	}

	// Create an entity with the indexed attribute
	entity1, err := store.CreateEntity(ctx, []Attr{
		String(attr.ID, "value1"),
		String(Ident, "test-entity-1"),
	})
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
			if string(event.Kv.Value) != string(entity1.ID) {
				t.Errorf("Expected entity ID %s, got %s", entity1.ID, event.Kv.Value)
			}
		}
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for creation event")
	}

	// Update the entity with a new value for the indexed attribute
	_, err = store.UpdateEntity(ctx, entity1.ID, []Attr{
		String(attr.ID, "value2"),
	})
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
	watcher2, err := store.WatchIndex(ctx, String(attr.ID, "value2"))
	if err != nil {
		t.Fatalf("Failed to create second watcher: %v", err)
	}

	// Delete the entity
	err = store.DeleteEntity(ctx, entity1.ID)
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
	nonIndexedSchema, err := store.CreateEntity(ctx, []Attr{
		String(Ident, "non-indexed"),
		Ref(Type, TypeStr),
		Bool(Index, false),
	})
	if err != nil {
		t.Fatalf("Failed to create non-indexed schema: %v", err)
	}

	_, err = store.WatchIndex(ctx, String(nonIndexedSchema.ID, "value"))
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
	entity1, err := store.CreateEntity(ctx, []Attr{
		String(Ident, "test-entity-1"),
		String(Doc, "test document"),
	})
	if err != nil {
		t.Fatalf("Failed to create entity: %v", err)
	}

	// Start watching the index before creating any entities
	watcher, err := store.WatchIndex(ctx, Ref(DBId, "test-entity-1"))
	if err != nil {
		t.Fatalf("Failed to create watcher: %v", err)
	}

	_, err = store.UpdateEntity(ctx, entity1.ID, []Attr{
		String(Doc, "now with more doc"),
	})
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
			if string(event.Kv.Value) != string(entity1.ID) {
				t.Errorf("Expected entity ID %s, got %s", entity1.ID, event.Kv.Value)
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
	kindAttr, err := store.CreateEntity(t.Context(), Attrs(
		Ident, "test/kind",
		Doc, "a kind",
		Cardinality, CardinalityOne,
		Type, TypeStr,
		Index, true,
	))
	require.NoError(t, err)

	// Create an indexed attribute schema that we'll change below (similar to our default app boolean)
	indexedAttr, err := store.CreateEntity(t.Context(), []Attr{
		String(Ident, "test/default"),
		Ref(Type, TypeBool),
		Bool(Index, true),
		Ref(Cardinality, CardinalityOne),
	})
	require.NoError(t, err)

	// Create first entity with default=true
	entity1, err := store.CreateEntity(t.Context(), []Attr{
		String(Ident, "app1"),
		String(kindAttr.ID, "test/app"),
		Bool(indexedAttr.ID, true),
	})
	require.NoError(t, err)

	// Create second entity with default=false
	entity2, err := store.CreateEntity(t.Context(), []Attr{
		String(Ident, "app2"),
		String(kindAttr.ID, "test/app"),
		Bool(indexedAttr.ID, false),
	})
	require.NoError(t, err)

	// Verify initial state: only entity1 should be found when searching for default=true
	entitiesWithTrue, err := store.ListIndex(t.Context(), Bool(indexedAttr.ID, true))
	require.NoError(t, err)
	assert.Len(t, entitiesWithTrue, 1)
	assert.Equal(t, entity1.ID, entitiesWithTrue[0])

	// Verify only entity2 is found when searching for default=false
	entitiesWithFalse, err := store.ListIndex(t.Context(), Bool(indexedAttr.ID, false))
	require.NoError(t, err)
	assert.Len(t, entitiesWithFalse, 1)
	assert.Equal(t, entity2.ID, entitiesWithFalse[0])

	// Now update: set entity2 to default=true
	_, err = store.UpdateEntity(t.Context(), entity2.ID, []Attr{
		Bool(indexedAttr.ID, true),
	})
	require.NoError(t, err)

	// Verify after update: both entities should be found with default=true
	entitiesWithTrueAfter, err := store.ListIndex(t.Context(), Bool(indexedAttr.ID, true))
	require.NoError(t, err)
	assert.Len(t, entitiesWithTrueAfter, 2)

	// Sort for consistent comparison
	foundIDs := []string{string(entitiesWithTrueAfter[0]), string(entitiesWithTrueAfter[1])}
	if foundIDs[0] > foundIDs[1] {
		foundIDs[0], foundIDs[1] = foundIDs[1], foundIDs[0]
	}
	expectedIDs := []string{string(entity1.ID), string(entity2.ID)}
	if expectedIDs[0] > expectedIDs[1] {
		expectedIDs[0], expectedIDs[1] = expectedIDs[1], expectedIDs[0]
	}
	assert.Equal(t, expectedIDs, foundIDs)

	// Verify none have default=false
	entitiesWithFalseAfter, err := store.ListIndex(t.Context(), Bool(indexedAttr.ID, false))
	require.NoError(t, err)
	assert.Len(t, entitiesWithFalseAfter, 0)
}

func TestEtcdStore_GetEntities(t *testing.T) {
	client := setupTestEtcd(t)
	store, err := NewEtcdStore(t.Context(), slog.Default(), client, "/test-entities")
	require.NoError(t, err)

	// Create multiple test entities
	entity1, err := store.CreateEntity(t.Context(), []Attr{
		Any(Ident, "test-entity-1"),
		Any(Doc, "first test document"),
	})
	require.NoError(t, err)

	entity2, err := store.CreateEntity(t.Context(), []Attr{
		Any(Ident, "test-entity-2"),
		Any(Doc, "second test document"),
	})
	require.NoError(t, err)

	entity3, err := store.CreateEntity(t.Context(), []Attr{
		Any(Ident, "test-entity-3"),
		Any(Doc, "third test document"),
	})
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
			ids:        []Id{entity1.ID},
			wantCount:  1,
			wantNils:   0,
			checkOrder: true,
			wantErr:    false,
		},
		{
			name:       "multiple existing entities",
			ids:        []Id{entity1.ID, entity2.ID, entity3.ID},
			wantCount:  3,
			wantNils:   0,
			checkOrder: true,
			wantErr:    false,
		},
		{
			name:       "multiple existing entities in different order",
			ids:        []Id{entity3.ID, entity1.ID, entity2.ID},
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
			ids:        []Id{entity1.ID, "non-existent", entity2.ID, "another-non-existent"},
			wantCount:  4,
			wantNils:   2,
			checkOrder: true,
			wantErr:    false,
		},
		{
			name:       "duplicate IDs",
			ids:        []Id{entity1.ID, entity1.ID, entity2.ID},
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
						assert.Equal(t, id, got[i].ID, "Entity at index %d should have ID %s", i, id)
					}
				}
			}

			// Verify entity contents for non-nil results
			for i, entity := range got {
				if entity != nil {
					// Verify it has expected attributes
					assert.NotEmpty(t, entity.Attrs)
					assert.Greater(t, entity.Revision, int64(0))
					assert.NotZero(t, entity.CreatedAt)
					assert.NotZero(t, entity.UpdatedAt)
					
					// Check that the ID matches what we requested
					assert.Equal(t, tt.ids[i], entity.ID)
				}
			}
		})
	}

	t.Run("with session attributes", func(t *testing.T) {
		r := require.New(t)

		// Create a schema for session attribute
		_, err := store.CreateEntity(t.Context(), Attrs(
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
		entity4, err := store.CreateEntity(t.Context(), []Attr{
			Any(Ident, "test-entity-4"),
		}, WithSession(sid))
		r.NoError(err)

		// Add session attribute
		_, err = store.UpdateEntity(t.Context(), entity4.ID, []Attr{
			String(Id("test/session-status"), "active"),
		}, WithSession(sid))
		r.NoError(err)

		entity5, err := store.CreateEntity(t.Context(), []Attr{
			Any(Ident, "test-entity-5"),
			String(Id("test/session-status"), "pending"),
		}, WithSession(sid))
		r.NoError(err)

		// Get entities with session attributes
		entities, err := store.GetEntities(t.Context(), []Id{entity4.ID, entity5.ID})
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
		entities, err = store.GetEntities(t.Context(), []Id{entity4.ID, entity5.ID})
		r.NoError(err)
		r.Len(entities, 2)

		// Verify session attributes are removed
		for i, e := range entities {
			r.NotNil(e)
			_, ok := e.Get(Id("test/session-status"))
			r.False(ok, "Entity %d should not have session status after revoke", i)
		}
	})

	t.Run("with TTL/lease", func(t *testing.T) {
		r := require.New(t)

		// Create session with TTL
		sid, err := store.CreateSession(t.Context(), 30)
		r.NoError(err)

		// Create entities bound to session (will have TTL)
		entity6, err := store.CreateEntity(t.Context(), []Attr{
			Any(Ident, "test-entity-6"),
		}, BondToSession(sid))
		r.NoError(err)

		entity7, err := store.CreateEntity(t.Context(), []Attr{
			Any(Ident, "test-entity-7"),
		}, BondToSession(sid))
		r.NoError(err)

		// Get entities and verify TTL attribute is added
		entities, err := store.GetEntities(t.Context(), []Id{entity6.ID, entity7.ID})
		r.NoError(err)
		r.Len(entities, 2)

		for i, e := range entities {
			r.NotNil(e)
			ttlAttr, ok := e.Get(TTL)
			r.True(ok, "Entity %d should have TTL attribute", i)
			
			// TTL should be a duration
			ttlDuration := ttlAttr.Value.Duration()
			r.Greater(ttlDuration, time.Duration(0))
			r.LessOrEqual(ttlDuration, 30*time.Second)
		}

		// Clean up
		r.NoError(store.RevokeSession(t.Context(), sid))
	})

	t.Run("large batch", func(t *testing.T) {
		r := require.New(t)

		// Create a larger batch of entities
		var ids []Id
		numEntities := 50
		for i := 0; i < numEntities; i++ {
			entity, err := store.CreateEntity(t.Context(), []Attr{
				Any(Ident, fmt.Sprintf("batch-entity-%d", i)),
				Any(Doc, fmt.Sprintf("Batch document %d", i)),
			})
			r.NoError(err)
			ids = append(ids, entity.ID)
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
			r.Equal(ids[i], entities[i].ID)
		}

		// Last two should be nil
		r.Nil(entities[numEntities])
		r.Nil(entities[numEntities+1])
	})
}
