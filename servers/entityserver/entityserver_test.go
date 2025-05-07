package entityserver

import (
	"context"
	"log/slog"
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
