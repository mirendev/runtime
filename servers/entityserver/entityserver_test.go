package entityserver

import (
	"context"
	"log/slog"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1alpha "miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/stream"
)

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
	testEntity, err := store.CreateEntity(context.Background(), []entity.Attr{
		{ID: entity.Ident, Value: entity.KeywordValue("test/entity")},
		{ID: entity.Doc, Value: entity.StringValue("Test entity")},
	})
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
				assert.Equal(t, testEntity.Attrs, result.Attrs())
				assert.Equal(t, testEntity.Revision, result.Revision())
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
	_, err := store.CreateEntity(context.Background(), []entity.Attr{
		{ID: entity.Ident, Value: entity.KeywordValue("test/entity")},
		{ID: entity.Doc, Value: entity.StringValue("Test entity")},
	})
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

		r.Equal(int64(1), op.Operation())

		r.True(op.HasEntity())

		ae := op.Entity()

		r.Len(ae.Attrs(), 1)

		r.Equal(entity.Keyword(entity.Ident, "mock/entity"), ae.Attrs()[0])

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
		_, err := store.CreateEntity(ctx, e.attrs)
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
			index:     entity.Keyword(entity.Ident, "test/entity1"),
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
	_, err := store.CreateEntity(ctx, []entity.Attr{
		{ID: entity.Ident, Value: entity.KeywordValue("test/entity1")},
		{ID: entity.EntityKind, Value: entity.KeywordValue("test")},
	})
	require.NoError(t, err)

	// Override GetEntities to return a nil entry to simulate missing entity
	store.GetEntitiesFunc = func(ctx context.Context, ids []entity.Id) ([]*entity.Entity, error) {
		// Return array with nil entry
		return []*entity.Entity{nil}, nil
	}

	// List should fail when GetEntities returns nil entry
	_, err = sc.List(ctx, entity.Keyword(entity.EntityKind, "test"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "entity not found")
}
