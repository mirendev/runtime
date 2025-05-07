package entity

import (
	"context"
	"log/slog"
	"testing"
	"time"

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

		select {
		case <-t.Context().Done():
			r.NoError(t.Context().Err())
		case x := <-wc:
			r.Len(x.Events, 1)
			r.Equal(x.Events[0].Type, mvccpb.DELETE)
			r.Equal(x.Events[0].PrevKv.Value, []byte(entity.ID))
		}

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
