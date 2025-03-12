package entity

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clientv3 "go.etcd.io/etcd/client/v3"
)

func setupTestEtcd(t *testing.T) *clientv3.Client {
	client, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{"localhost:2379"},
		DialTimeout: 2 * time.Second,
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
	store, err := NewEtcdStore(client, "/test-entities")
	require.NoError(t, err)

	e, err := store.CreateEntity(Attrs(
		EntityIdent, "test/addresses",
		EntityDoc, "A list of addresses",
		EntityCard, EntityCardMany,
		EntityType, EntityTypeStr,
	))
	require.NoError(t, err)

	require.Equal(t, "test/addresses", e.ID)

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
				{ID: EntityIdent, Value: "test1"},
				{ID: EntityDoc, Value: "test document"},
			},
			wantErr: false,
		},
		{
			name:       "duplicate entity",
			entityType: "test",
			attrs: []Attr{
				{ID: EntityIdent, Value: "test1"},
				{ID: EntityDoc, Value: "duplicate"},
			},
			wantErr: true,
		},
		{
			name:       "invalid attribute",
			entityType: "test",
			attrs: []Attr{
				{ID: EntityIdent, Value: 123}, // Wrong type for ident
			},
			wantErr: true,
			errMsg:  "invalid attribute",
		},
		{
			name:       "duplicate cardinality.one attribute",
			entityType: "test",
			attrs: []Attr{
				{ID: EntityIdent, Value: "test4"},
				{ID: EntityDoc, Value: "first doc"},
				{ID: EntityDoc, Value: "second doc"}, // EntityDoc is cardinality.one
			},
			wantErr: true,
			errMsg:  "cardinality violation",
		},
		{
			name:       "valid cardinality.many attribute",
			entityType: "test",
			attrs: []Attr{
				{ID: EntityIdent, Value: "test5"},
				{ID: EntityId("test/addresses"), Value: "val1"},
				{ID: EntityId("test/addresses"), Value: "val2"},
			},
			wantErr: false,
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
			assert.Equal(t, tt.attrs, entity.Attrs)
			assert.Greater(t, entity.Revision, 0)
			assert.NotZero(t, entity.CreatedAt)
			assert.NotZero(t, entity.UpdatedAt)
		})
	}
}

func TestEtcdStore_CustomType(t *testing.T) {
	client := setupTestEtcd(t)
	store, err := NewEtcdStore(client, "/test-entities")
	require.NoError(t, err)

	e, err := store.CreateEntity(Attrs(
		EntityIdent, "test/address",
		EntityDoc, "An address",
		EntityCard, EntityCardMany,
		EntityType, EntityTypeStr,
		EntityAttrPred, EntityId("db/pred.ip"),
	))
	require.NoError(t, err)

	require.Equal(t, "test/address", e.ID)

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
				{ID: EntityId("test/address"), Value: "10.0.1.1"},
			},
			wantErr: false,
		},
		{
			name:       "invalid attribute",
			entityType: "test",
			attrs: []Attr{
				{ID: EntityId("test/address"), Value: "hello"},
			},
			wantErr: true,
			errMsg:  "invalid attribute",
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
			assert.Equal(t, tt.attrs, entity.Attrs)
			assert.Greater(t, entity.Revision, 0)
			assert.NotZero(t, entity.CreatedAt)
			assert.NotZero(t, entity.UpdatedAt)
		})
	}
}

func TestEtcdStore_GetEntity(t *testing.T) {
	client := setupTestEtcd(t)
	store, err := NewEtcdStore(client, "/test-entities")
	require.NoError(t, err)

	// Create a test entity
	created, err := store.CreateEntity([]Attr{
		{ID: EntityIdent, Value: "test1"},
		{ID: EntityDoc, Value: "test document"},
	})
	require.NoError(t, err)

	tests := []struct {
		name    string
		id      EntityId
		want    *Entity
		wantErr bool
	}{
		{
			name:    "existing entity",
			id:      EntityId(created.ID),
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
			got, err := store.GetEntity(tt.id)
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
	store, err := NewEtcdStore(client, "/test-entities")
	require.NoError(t, err)

	// Create a test entity
	entity, err := store.CreateEntity([]Attr{
		{ID: EntityIdent, Value: "test1"},
		{ID: EntityDoc, Value: "original doc"},
	})
	require.NoError(t, err)

	tests := []struct {
		name      string
		id        EntityId
		attrs     []Attr
		wantAttrs int
		wantErr   bool
	}{
		{
			name: "valid update",
			id:   EntityId(entity.ID),
			attrs: []Attr{
				{ID: EntityDoc, Value: "updated doc"},
			},
			wantAttrs: 3, // Original 2 + 1 new
			wantErr:   false,
		},
		{
			name: "invalid attribute",
			id:   EntityId(entity.ID),
			attrs: []Attr{
				{ID: EntityIdent, Value: 123}, // Wrong type
			},
			wantErr: true,
		},
		{
			name: "non-existent entity",
			id:   "nonexistent",
			attrs: []Attr{
				{ID: EntityDoc, Value: "won't work"},
			},
			wantErr: true,
		},
	}

	time.Sleep(10 * time.Millisecond)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updated, err := store.UpdateEntity(tt.id, tt.attrs)
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
}

func TestEtcdStore_DeleteEntity(t *testing.T) {
	client := setupTestEtcd(t)
	store, err := NewEtcdStore(client, "/test-entities")
	require.NoError(t, err)

	// Create a test entity
	entity, err := store.CreateEntity([]Attr{
		{ID: EntityIdent, Value: "test1"},
		{ID: EntityDoc, Value: "test document"},
	})
	require.NoError(t, err)

	tests := []struct {
		name    string
		id      EntityId
		wantErr bool
	}{
		{
			name:    "existing entity",
			id:      EntityId(entity.ID),
			wantErr: false,
		},
		{
			name:    "non-existent entity",
			id:      "nonexistent",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.DeleteEntity(tt.id)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)

			// Verify entity is gone
			_, err = store.GetEntity(tt.id)
			assert.Error(t, err)
		})
	}
}

func TestEtcdStore_ListIndex(t *testing.T) {
	client := setupTestEtcd(t)
	store, err := NewEtcdStore(client, "/test-entities")
	require.NoError(t, err)

	// Create test entities with indexed attributes
	_, err = store.CreateEntity([]Attr{
		{ID: EntityIdent, Value: "test1"},
		{ID: EntityKind, Value: "user"},
	})
	require.NoError(t, err)

	_, err = store.CreateEntity([]Attr{
		{ID: EntityIdent, Value: "test2"},
		{ID: EntityKind, Value: "user"},
	})
	require.NoError(t, err)

	tests := []struct {
		name      string
		attrID    EntityId
		value     any
		wantCount int
		wantErr   bool
	}{
		{
			name:      "valid index",
			attrID:    EntityKind,
			value:     "user",
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:      "non-existent value",
			attrID:    EntityKind,
			value:     "admin",
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:      "non-indexed attribute",
			attrID:    EntityDoc,
			value:     "test",
			wantCount: 0,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entities, err := store.ListIndex(tt.attrID, tt.value)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, entities, tt.wantCount)
		})
	}
}
