package entityserver

import (
	"context"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clientv3 "go.etcd.io/etcd/client/v3"
	v1alpha "miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/stream"
)

func setupTestEtcd(t *testing.T) (*clientv3.Client, string) {
	client, err := clientv3.New(clientv3.Config{
		Endpoints:       []string{"etcd:2379"},
		DialTimeout:     2 * time.Second,
		MaxUnaryRetries: 2,
	})
	require.NoError(t, err)

	// Generate random prefix for isolation
	prefix := "/" + idgen.Gen("test")

	t.Cleanup(func() {
		// Delete all keys with this prefix
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := client.Delete(ctx, prefix, clientv3.WithPrefix())
		if err != nil {
			t.Logf("warning: failed to cleanup etcd prefix %s: %v", prefix, err)
		}
		client.Close()
	})

	return client, prefix
}

func TestEntityServer_Get(t *testing.T) {
	store := entity.NewMockStore()
	server := &EntityServer{
		Log:   slog.Default(),
		Store: store,
	}

	sc := v1alpha.EntityAccessClient{
		Client: rpc.LocalClient(v1alpha.AdaptEntityAccess(server)),
	}

	ctx := context.TODO()

	// Create a test entity
	testEntity, err := store.CreateEntity(context.Background(), entity.New([]entity.Attr{
		{ID: entity.Ident, Value: entity.KeywordValue("test/entity")},
		{ID: entity.Doc, Value: entity.StringValue("Test entity")},
	}))
	require.NoError(t, err)

	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{
			name:    "get existing entity",
			id:      "test/entity",
			wantErr: false,
		},
		{
			name:    "get non-existent entity",
			id:      "nonexistent",
			wantErr: true,
		},
		{
			name:    "empty id",
			id:      "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := sc.Get(ctx, tt.id)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)

				result := resp.Entity()

				assert.Equal(t, tt.id, result.Id())
				assert.Len(t, result.Attrs(), len(testEntity.Attrs()))
				for i, attr := range testEntity.Attrs() {
					assert.Equal(t, 0, attr.Compare(result.Attrs()[i]))
				}
				assert.Equal(t, testEntity.GetRevision(), result.Revision())
			}
		})
	}
}

func TestEntityServer_Put(t *testing.T) {
	store := entity.NewMockStore()
	server := &EntityServer{
		Log:   slog.Default(),
		Store: store,
	}

	sc := v1alpha.EntityAccessClient{
		Client: rpc.LocalClient(v1alpha.AdaptEntityAccess(server)),
	}

	ctx := context.TODO()

	tests := []struct {
		name    string
		attrs   []entity.Attr
		wantErr bool
	}{
		{
			name: "create valid entity",
			attrs: []entity.Attr{
				{ID: entity.Ident, Value: entity.KeywordValue("test/entity1")},
				{ID: entity.Doc, Value: entity.StringValue("Test entity")},
			},
			wantErr: false,
		},
		{
			name:    "create entity with no attributes",
			attrs:   []entity.Attr{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			var rpcEntity v1alpha.Entity
			rpcEntity.SetAttrs(tt.attrs)

			resp, err := sc.Put(ctx, &rpcEntity)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Greater(t, resp.Revision(), int64(0))
			}
		})
	}
}

func TestEntityServer_Delete(t *testing.T) {
	store := entity.NewMockStore()
	server := &EntityServer{
		Log:   slog.Default(),
		Store: store,
	}

	sc := v1alpha.EntityAccessClient{
		Client: rpc.LocalClient(v1alpha.AdaptEntityAccess(server)),
	}

	ctx := context.TODO()

	// Create a test entity
	_, err := store.CreateEntity(context.Background(), entity.New([]entity.Attr{
		{ID: entity.Ident, Value: entity.KeywordValue("test/entity")},
		{ID: entity.Doc, Value: entity.StringValue("Test entity")},
	}))
	require.NoError(t, err)

	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{
			name:    "delete existing entity",
			id:      "test/entity",
			wantErr: false,
		},
		{
			name:    "delete non-existent entity",
			id:      "nonexistent",
			wantErr: false, // Delete is idempotent
		},
		{
			name:    "empty id",
			id:      "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := sc.Delete(ctx, tt.id)
			if tt.wantErr {
				require.Error(t, err)
				return
			} else {
				require.NoError(t, err)
			}

			_, err = store.GetEntity(context.Background(), entity.Id(tt.id))
			assert.Error(t, err)
		})
	}
}

func TestEntityServer_WatchIndex(t *testing.T) {
	r := require.New(t)

	store := entity.NewMockStore()
	server := &EntityServer{
		Log:   slog.Default(),
		Store: store,
	}

	sc := v1alpha.EntityAccessClient{
		Client: rpc.LocalClient(v1alpha.AdaptEntityAccess(server)),
	}

	ctx := context.TODO()

	index := entity.Keyword(entity.Ident, "test/index")

	_, err := sc.WatchIndex(ctx, index, stream.Callback(func(op *v1alpha.EntityOp) error {
		r.NotNil(op)

		r.Equal(int64(v1alpha.EntityOperationCreate), op.Operation())

		r.True(op.HasEntity())

		ae := op.Entity()

		r.Len(ae.Attrs(), 4)

		r.Equal(entity.Ref(entity.DBId, "mock/entity"), ae.Attrs()[2])

		return nil
	}))
	require.NoError(t, err)

}

func TestEntityServer_List(t *testing.T) {
	store := entity.NewMockStore()
	server := &EntityServer{
		Log:   slog.Default(),
		Store: store,
	}

	sc := v1alpha.EntityAccessClient{
		Client: rpc.LocalClient(v1alpha.AdaptEntityAccess(server)),
	}

	ctx := context.TODO()

	// Create test entities
	entities := []struct {
		ident string
		attrs []entity.Attr
	}{
		{
			ident: "test/entity1",
			attrs: []entity.Attr{
				{ID: entity.Ident, Value: entity.KeywordValue("test/entity1")},
				{ID: entity.Doc, Value: entity.StringValue("Test entity 1")},
				{ID: entity.EntityKind, Value: entity.KeywordValue("test")},
			},
		},
		{
			ident: "test/entity2",
			attrs: []entity.Attr{
				{ID: entity.Ident, Value: entity.KeywordValue("test/entity2")},
				{ID: entity.Doc, Value: entity.StringValue("Test entity 2")},
				{ID: entity.EntityKind, Value: entity.KeywordValue("test")},
			},
		},
		{
			ident: "other/entity1",
			attrs: []entity.Attr{
				{ID: entity.Ident, Value: entity.KeywordValue("other/entity1")},
				{ID: entity.Doc, Value: entity.StringValue("Other entity 1")},
				{ID: entity.EntityKind, Value: entity.KeywordValue("other")},
			},
		},
	}

	for _, e := range entities {
		_, err := store.CreateEntity(ctx, entity.New(e.attrs))
		require.NoError(t, err)
	}

	tests := []struct {
		name      string
		index     entity.Attr
		wantCount int
		wantIDs   []string
		wantErr   bool
	}{
		{
			name:      "list by kind - test",
			index:     entity.Keyword(entity.EntityKind, "test"),
			wantCount: 2,
			wantIDs:   []string{"test/entity1", "test/entity2"},
			wantErr:   false,
		},
		{
			name:      "list by kind - other",
			index:     entity.Keyword(entity.EntityKind, "other"),
			wantCount: 1,
			wantIDs:   []string{"other/entity1"},
			wantErr:   false,
		},
		{
			name:      "list by non-existent index",
			index:     entity.Keyword(entity.EntityKind, "nonexistent"),
			wantCount: 0,
			wantIDs:   []string{},
			wantErr:   false,
		},
		{
			name:      "list by specific ident",
			index:     entity.Ref(entity.DBId, "test/entity1"),
			wantCount: 1,
			wantIDs:   []string{"test/entity1"},
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := sc.List(ctx, tt.index)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			results := resp.Values()
			assert.Equal(t, tt.wantCount, len(results))

			// Collect IDs from results
			gotIDs := make([]string, 0)
			for _, result := range results {
				gotIDs = append(gotIDs, result.Id())
			}

			// Sort for consistent comparison
			slices.Sort(gotIDs)
			slices.Sort(tt.wantIDs)

			assert.Equal(t, tt.wantIDs, gotIDs)

			// Verify each entity has the expected attributes
			for _, result := range results {
				assert.NotEmpty(t, result.Attrs())
				assert.Greater(t, result.Revision(), int64(0))
			}
		})
	}
}

func TestEntityServer_List_WithMissingEntity(t *testing.T) {
	store := entity.NewMockStore()
	server := &EntityServer{
		Log:   slog.Default(),
		Store: store,
	}

	sc := v1alpha.EntityAccessClient{
		Client: rpc.LocalClient(v1alpha.AdaptEntityAccess(server)),
	}

	ctx := context.TODO()

	// Create an entity
	_, err := store.CreateEntity(ctx, entity.New([]entity.Attr{
		{ID: entity.Ident, Value: entity.KeywordValue("test/entity1")},
		{ID: entity.EntityKind, Value: entity.KeywordValue("test")},
	}))
	require.NoError(t, err)

	// Override GetEntities to return a nil entry to simulate missing entity
	store.GetEntitiesFunc = func(ctx context.Context, ids []entity.Id) ([]*entity.Entity, error) {
		// Return array with nil entry
		return []*entity.Entity{nil}, nil
	}

	// List should succeed and skip the missing entity
	resp, err := sc.List(ctx, entity.Keyword(entity.EntityKind, "test"))
	require.NoError(t, err)
	results := resp.Values()
	assert.Len(t, results, 0, "should return empty list when all entities are missing")
}

// TestEntityServer_List_NestedIndexCleanup tests that DeleteEntity properly cleans up
// nested component field indexes. This is a regression test for the bug where DeleteEntity
// only cleaned up top-level indexed fields, leaving stale index entries for nested fields.
// This test uses a real etcd-backed store to verify the full List RPC flow.
func TestEntityServer_List_NestedIndexCleanup(t *testing.T) {
	// This test requires etcd to be available (run with ./hack/run or ./hack/it)
	ctx := context.Background()

	// Setup etcd-backed store with random prefix for isolation
	client, prefix := setupTestEtcd(t)
	store, err := entity.NewEtcdStore(ctx, slog.Default(), client, prefix)
	require.NoError(t, err)

	server, err := NewEntityServer(slog.Default(), store)
	require.NoError(t, err)

	sc := v1alpha.EntityAccessClient{
		Client: rpc.LocalClient(v1alpha.AdaptEntityAccess(server)),
	}

	// Create schema for a component with a nested indexed field
	_, err = store.CreateEntity(ctx, entity.New(
		entity.Ident, "test/spec",
		entity.Doc, "A component type",
		entity.Cardinality, entity.CardinalityOne,
		entity.Type, entity.TypeComponent,
	))
	require.NoError(t, err)

	_, err = store.CreateEntity(ctx, entity.New(
		entity.Ident, "test/spec.version",
		entity.Doc, "Version field within spec component",
		entity.Cardinality, entity.CardinalityOne,
		entity.Type, entity.TypeRef,
		entity.Index, true, // Nested indexed field
	))
	require.NoError(t, err)

	// Create a version entity to reference
	v1 := entity.Id("version/v1")
	_, err = store.CreateEntity(ctx, entity.New(entity.Ref(entity.DBId, v1)))
	require.NoError(t, err)

	// Create two entities with nested indexed fields
	entity1, err := store.CreateEntity(ctx, entity.New([]entity.Attr{
		entity.Keyword(entity.Ident, "resource1"),
		entity.Component(entity.Id("test/spec"), []entity.Attr{
			entity.Ref(entity.Id("test/spec.version"), v1),
		}),
	}))
	require.NoError(t, err)

	entity2, err := store.CreateEntity(ctx, entity.New([]entity.Attr{
		entity.Keyword(entity.Ident, "resource2"),
		entity.Component(entity.Id("test/spec"), []entity.Attr{
			entity.Ref(entity.Id("test/spec.version"), v1),
		}),
	}))
	require.NoError(t, err)

	// Verify both are indexed and can be listed
	resp, err := sc.List(ctx, entity.Ref(entity.Id("test/spec.version"), v1))
	require.NoError(t, err)
	results := resp.Values()
	assert.Len(t, results, 2, "Should find both entities before deletion")

	// Delete entity1
	err = store.DeleteEntity(ctx, entity1.Id())
	require.NoError(t, err)

	// Now list by the nested index - this is the critical test
	// If DeleteEntity doesn't clean up nested indexes, the index will still contain
	// entity1's ID, GetEntities will return nil for it, and List RPC will error
	resp, err = sc.List(ctx, entity.Ref(entity.Id("test/spec.version"), v1))
	require.NoError(t, err, "List should not fail with 'entity not found' error due to stale index")

	results = resp.Values()
	assert.Len(t, results, 1, "Should find only one entity after deletion")
	assert.Equal(t, entity2.Id().String(), results[0].Id(), "Remaining entity should be entity2")
}
