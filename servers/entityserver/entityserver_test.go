package entityserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.etcd.io/etcd/api/v3/etcdserverpb"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
	v1alpha "miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	saga "miren.dev/runtime/api/saga/saga_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/schema"
	etypes "miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/etcdtest"
	"miren.dev/runtime/pkg/model"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/stream"
)

func setupTestEtcd(t *testing.T) (*clientv3.Client, string) {
	return etcdtest.TestEtcdClient(t)
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

func TestEntityServer_MakeAttrEnumEncodings(t *testing.T) {
	store := entity.NewMockStore()
	sb := schema.Builder("make_attr_enum_test", "v1")
	sb.Ref("Ref", "make_attr_enum_test/ref", schema.EnumValues(entity.Id("make_attr_enum_test/ref.ready")))
	sb.String("String", "make_attr_enum_test/string", schema.EnumValues("ready"))
	sb.Keyword("Keyword", "make_attr_enum_test/keyword", schema.EnumValues(etypes.Keyword("ready")))
	sb.Enum("TypeEnum", "make_attr_enum_test/type_enum", []any{"ready"})
	sb.Singleton("make_attr_enum_test/canonical.ready")
	sb.Enum("CanonicalEnum", "make_attr_enum_test/canonical_enum", []any{
		entity.Id("make_attr_enum_test/canonical.ready"),
		"ready",
	}, schema.ElementType(entity.TypeRef))
	sb.Enum("LegacyWriteEnum", "make_attr_enum_test/legacy_write_enum", []any{
		entity.Id("make_attr_enum_test/canonical.ready"),
		"ready",
	}, schema.ElementType(entity.TypeStr))
	require.NoError(t, sb.Apply(t.Context(), store))

	client := v1alpha.EntityAccessClient{
		Client: rpc.LocalClient(v1alpha.AdaptEntityAccess(&EntityServer{
			Log:   slog.Default(),
			Store: store,
		})),
	}

	tests := []struct {
		name  string
		id    string
		input string
		want  entity.Value
	}{
		{
			name:  "ref",
			id:    "make_attr_enum_test/ref",
			input: "make_attr_enum_test/ref.ready",
			want:  entity.RefValue("make_attr_enum_test/ref.ready"),
		},
		{
			name:  "string",
			id:    "make_attr_enum_test/string",
			input: "ready",
			want:  entity.StringValue("ready"),
		},
		{
			name:  "keyword",
			id:    "make_attr_enum_test/keyword",
			input: "ready",
			want:  entity.KeywordValue("ready"),
		},
		{
			name:  "type enum",
			id:    "make_attr_enum_test/type_enum",
			input: "ready",
			want:  entity.StringValue("ready"),
		},
		{
			name:  "canonical ref enum prefers ref over legacy string",
			id:    "make_attr_enum_test/canonical_enum",
			input: "ready",
			want:  entity.RefValue("make_attr_enum_test/canonical.ready"),
		},
		{
			name:  "legacy-write enum prefers string over canonical ref",
			id:    "make_attr_enum_test/legacy_write_enum",
			input: "ready",
			want:  entity.StringValue("ready"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := client.MakeAttr(t.Context(), tt.id, tt.input)
			require.NoError(t, err)
			assert.True(t, tt.want.Equal(result.Attr().Value))
		})
	}

	_, err := client.MakeAttr(t.Context(), "make_attr_enum_test/string", "nope")
	require.ErrorContains(t, err, "invalid enum value: nope")
}

func TestEnumValueFromStringRejectsAmbiguousShortRef(t *testing.T) {
	values := []entity.Value{
		entity.RefValue("test/first.ready"),
		entity.RefValue("test/second.ready"),
	}

	exact, ok := enumValueFromString(values, entity.TypeRef, "test/second.ready")
	require.True(t, ok)
	assert.True(t, exact.Equal(entity.RefValue("test/second.ready")))

	_, ok = enumValueFromString(values, entity.TypeRef, "ready")
	assert.False(t, ok)
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	index := entity.Keyword(entity.Ident, "test/index")

	// Track received events
	eventReceived := make(chan struct{})
	watchDone := make(chan error, 1)

	// Start watch in background
	go func() {
		_, err := sc.WatchIndex(ctx, index, 0, stream.Callback(func(op *v1alpha.EntityOp) error {
			r.NotNil(op)
			r.Equal(int64(v1alpha.EntityOperationCreate), op.Operation())
			r.True(op.HasEntity())

			ae := op.Entity()
			// Entity should have the ident attribute we're watching
			r.Contains(ae.Attrs(), index)

			close(eventReceived)
			return nil
		}))
		watchDone <- err
	}()

	// Wait for watch to be established
	err := store.WaitForIndexWatcher(ctx, index)
	r.NoError(err)

	// Create an entity that matches the watch index
	testEntity := entity.New(
		entity.Ref(entity.DBId, "test/entity-1"),
		index, // This makes the entity match the watch
	)
	_, err = store.CreateEntity(ctx, testEntity)
	r.NoError(err)

	// Wait for event to be received
	select {
	case <-eventReceived:
		// Success - cancel context to stop watch
		cancel()
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for watch event")
	}

	// Wait for watch goroutine to finish
	select {
	case <-watchDone:
		// Watch finished (error is expected due to context cancellation)
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for watch to finish")
	}
}

func TestEntityServer_WatchEntity_DeleteIncludesEntity(t *testing.T) {
	r := require.New(t)

	store := entity.NewMockStore()
	server := &EntityServer{
		Log:   slog.Default(),
		Store: store,
	}

	sc := v1alpha.EntityAccessClient{
		Client: rpc.LocalClient(v1alpha.AdaptEntityAccess(server)),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create an entity first
	testEntity := entity.New(
		entity.Ref(entity.DBId, "test/watch-delete"),
		entity.Keyword(entity.Ident, "test/watch-delete"),
		entity.String(entity.Doc, "entity to be deleted"),
	)
	created, err := store.CreateEntity(ctx, testEntity)
	r.NoError(err)

	// Track received events
	var receivedOps []*v1alpha.EntityOp
	deleteReceived := make(chan struct{})
	watchDone := make(chan error, 1)

	// Start watch in background
	go func() {
		_, err := sc.WatchEntity(ctx, created.Id().String(), stream.Callback(func(op *v1alpha.EntityOp) error {
			receivedOps = append(receivedOps, op)
			if op.Operation() == int64(v1alpha.EntityOperationDelete) {
				close(deleteReceived)
			}
			return nil
		}))
		watchDone <- err
	}()

	// Wait for watch to be established
	err = store.WaitForEntityWatcher(ctx, created.Id())
	r.NoError(err)

	// Delete the entity
	err = store.DeleteEntity(ctx, created.Id())
	r.NoError(err)

	// Wait for delete event
	select {
	case <-deleteReceived:
		// Find the delete event
		var deleteOp *v1alpha.EntityOp
		for _, op := range receivedOps {
			if op.Operation() == int64(v1alpha.EntityOperationDelete) {
				deleteOp = op
				break
			}
		}
		r.NotNil(deleteOp, "should have received a delete event")
		r.True(deleteOp.HasEntity(), "delete event should include entity data")

		ae := deleteOp.Entity()
		r.Equal(created.Id().String(), ae.Id())
		r.Contains(ae.Attrs(), entity.String(entity.Doc, "entity to be deleted"))
		cancel()
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for delete event")
	}

	select {
	case <-watchDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for watch to finish")
	}
}

func TestEntityServer_WatchIndex_DeleteIncludesEntity(t *testing.T) {
	r := require.New(t)

	store := entity.NewMockStore()
	server := &EntityServer{
		Log:   slog.Default(),
		Store: store,
	}

	sc := v1alpha.EntityAccessClient{
		Client: rpc.LocalClient(v1alpha.AdaptEntityAccess(server)),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	index := entity.Keyword(entity.Ident, "test/index-delete")

	// Track received events
	deleteReceived := make(chan *v1alpha.EntityOp, 1)
	watchDone := make(chan error, 1)

	// Start watch in background
	go func() {
		_, err := sc.WatchIndex(ctx, index, 0, stream.Callback(func(op *v1alpha.EntityOp) error {
			if op.Operation() == int64(v1alpha.EntityOperationDelete) {
				deleteReceived <- op
			}
			return nil
		}))
		watchDone <- err
	}()

	// Wait for watch to be established
	err := store.WaitForIndexWatcher(ctx, index)
	r.NoError(err)

	// Create an entity that matches the watch index
	testEntity := entity.New(
		entity.Ref(entity.DBId, "test/entity-delete"),
		index,
		entity.String(entity.Doc, "will be deleted"),
	)
	created, err := store.CreateEntity(ctx, testEntity)
	r.NoError(err)

	// Delete the entity
	err = store.DeleteEntity(ctx, created.Id())
	r.NoError(err)

	// Wait for delete event
	select {
	case deleteOp := <-deleteReceived:
		r.True(deleteOp.HasEntity(), "delete event should include entity data")

		ae := deleteOp.Entity()
		r.Equal(created.Id().String(), ae.Id())
		r.Contains(ae.Attrs(), entity.String(entity.Doc, "will be deleted"))
		cancel()
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for delete event")
	}

	select {
	case <-watchDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for watch to finish")
	}
}

func TestEntityServer_WatchEntity_DeleteIncludesEntity_Etcd(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client, prefix := setupTestEtcd(t)
	store, err := entity.NewEtcdStore(ctx, slog.Default(), client, prefix)
	require.NoError(t, err)

	server, err := NewEntityServer(slog.Default(), store)
	require.NoError(t, err)

	sc := v1alpha.EntityAccessClient{
		Client: rpc.LocalClient(v1alpha.AdaptEntityAccess(server)),
	}

	// Create an entity
	created, err := store.CreateEntity(ctx, entity.New(
		entity.String(entity.Ident, "test-watch-delete-etcd"),
		entity.String(entity.Doc, "entity for etcd delete test"),
	))
	require.NoError(t, err)

	deleteReceived := make(chan *v1alpha.EntityOp, 1)
	watchDone := make(chan error, 1)

	go func() {
		_, err := sc.WatchEntity(ctx, created.Id().String(), stream.Callback(func(op *v1alpha.EntityOp) error {
			if op.Operation() == int64(v1alpha.EntityOperationDelete) {
				deleteReceived <- op
			}
			return nil
		}))
		watchDone <- err
	}()

	// Give the watch time to establish
	time.Sleep(100 * time.Millisecond)

	// Delete the entity
	err = store.DeleteEntity(ctx, created.Id())
	require.NoError(t, err)

	select {
	case deleteOp := <-deleteReceived:
		require.True(t, deleteOp.HasEntity(), "delete event should include entity data")
		ae := deleteOp.Entity()
		assert.Equal(t, created.Id().String(), ae.Id())
		assert.Contains(t, ae.Attrs(), entity.String(entity.Doc, "entity for etcd delete test"))
		cancel()
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for delete event")
	}

	select {
	case <-watchDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for watch to finish")
	}
}

func TestEntityServer_WatchIndex_DeleteIncludesEntity_Etcd(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client, prefix := setupTestEtcd(t)
	store, err := entity.NewEtcdStore(ctx, slog.Default(), client, prefix)
	require.NoError(t, err)

	server, err := NewEntityServer(slog.Default(), store)
	require.NoError(t, err)

	sc := v1alpha.EntityAccessClient{
		Client: rpc.LocalClient(v1alpha.AdaptEntityAccess(server)),
	}

	// Create an indexed attribute schema
	indexAttr, err := store.CreateEntity(ctx, entity.New(
		entity.String(entity.Ident, "test/watch-idx"),
		entity.Ref(entity.Type, entity.TypeStr),
		entity.Bool(entity.Index, true),
	))
	require.NoError(t, err)

	index := entity.String(indexAttr.Id(), "watch-val")

	deleteReceived := make(chan *v1alpha.EntityOp, 1)
	watchDone := make(chan error, 1)

	go func() {
		_, err := sc.WatchIndex(ctx, index, 0, stream.Callback(func(op *v1alpha.EntityOp) error {
			if op.Operation() == int64(v1alpha.EntityOperationDelete) {
				deleteReceived <- op
			}
			return nil
		}))
		watchDone <- err
	}()

	// Give the watch time to establish
	time.Sleep(100 * time.Millisecond)

	// Create an entity that matches the watch index
	created, err := store.CreateEntity(ctx, entity.New(
		entity.String(entity.Ident, "test-idx-delete-etcd"),
		index,
		entity.String(entity.Doc, "indexed entity for delete test"),
	))
	require.NoError(t, err)

	// Delete the entity
	err = store.DeleteEntity(ctx, created.Id())
	require.NoError(t, err)

	select {
	case deleteOp := <-deleteReceived:
		require.True(t, deleteOp.HasEntity(), "delete event should include entity data")
		ae := deleteOp.Entity()
		assert.Equal(t, created.Id().String(), ae.Id())
		assert.Contains(t, ae.Attrs(), entity.String(entity.Doc, "indexed entity for delete test"))
		cancel()
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for delete event")
	}

	select {
	case <-watchDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for watch to finish")
	}
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

// watchIndexServer wires an EntityAccessClient to an in-process EntityServer
// whose store delivers raw watch responses on the returned channel, so tests can
// drive progress notifications, the initial Created response, and compactions
// directly.
func watchIndexServer(t *testing.T) (*entity.MockStore, v1alpha.EntityAccessClient, chan clientv3.WatchResponse) {
	t.Helper()
	store := entity.NewMockStore()
	watchCh := make(chan clientv3.WatchResponse, 8)
	store.OnWatchIndex = func(ctx context.Context, attr entity.Attr) (clientv3.WatchChan, error) {
		return watchCh, nil
	}
	server := &EntityServer{Log: slog.Default(), Store: store}
	sc := v1alpha.EntityAccessClient{
		Client: rpc.LocalClient(v1alpha.AdaptEntityAccess(server)),
	}
	return store, sc, watchCh
}

func putResponse(id string, rev int64) clientv3.WatchResponse {
	return clientv3.WatchResponse{Events: []*clientv3.Event{{
		Type: clientv3.EventTypePut,
		Kv: &mvccpb.KeyValue{
			Key:            []byte("k/" + id),
			Value:          []byte(id),
			CreateRevision: rev,
			ModRevision:    rev,
		},
	}}}
}

// TestEntityServer_WatchIndex_LegacyClientIgnoresProgress proves a from-now
// (from_revision==0) client never receives a Progress op, even though the server
// now sets WithProgressNotify on the underlying watch. Legacy consumers predate
// the Progress op type and dereference op.Entity() for any non-delete op, so
// forwarding a watermark to them is a crash (this is bug 1a).
func TestEntityServer_WatchIndex_LegacyClientIgnoresProgress(t *testing.T) {
	r := require.New(t)

	store, sc, watchCh := watchIndexServer(t)

	ctx := t.Context()

	index := entity.Keyword(entity.Ident, "test/index")

	// The entity must exist so the server can read it when the put arrives.
	_, err := store.CreateEntity(ctx, entity.New(entity.Ref(entity.DBId, "widget-1"), index))
	r.NoError(err)

	gotOps := make(chan *v1alpha.EntityOp, 16)
	go func() {
		_, _ = sc.WatchIndex(ctx, index, 0, stream.Callback(func(op *v1alpha.EntityOp) error {
			gotOps <- op
			return nil
		}))
	}()

	// An idle progress watermark followed by a real change. The legacy client
	// must see only the change.
	watchCh <- clientv3.WatchResponse{Header: etcdserverpb.ResponseHeader{Revision: 50}}
	watchCh <- putResponse("widget-1", 60)

	select {
	case op := <-gotOps:
		r.Equal(int64(v1alpha.EntityOperationCreate), op.Operation(),
			"legacy client should receive the create, not a progress watermark")
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for op")
	}
}

// TestEntityServer_WatchIndex_ProgressForwardedWhenResuming documents the
// intended positive behavior the fix must preserve: a client resuming from a
// revision (from_revision>0) does receive progress watermarks so it can advance
// its cursor while idle.
func TestEntityServer_WatchIndex_ProgressForwardedWhenResuming(t *testing.T) {
	r := require.New(t)

	_, sc, watchCh := watchIndexServer(t)

	ctx := t.Context()

	index := entity.Keyword(entity.Ident, "test/index")

	gotOps := make(chan *v1alpha.EntityOp, 16)
	go func() {
		_, _ = sc.WatchIndex(ctx, index, 5, stream.Callback(func(op *v1alpha.EntityOp) error {
			gotOps <- op
			return nil
		}))
	}()

	watchCh <- clientv3.WatchResponse{Header: etcdserverpb.ResponseHeader{Revision: 50}}

	select {
	case op := <-gotOps:
		r.Equal(int64(v1alpha.EntityOperationProgress), op.Operation(),
			"resuming client should receive the progress watermark")
		r.Equal(int64(50), op.Revision())
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for op")
	}
}

// TestEntityServer_WatchIndex_CreatedResponseNotProgress proves the initial
// etcd Created response (empty events, Created=true) is NOT forwarded as a
// progress watermark. Its header revision is the current store revision, but it
// arrives BEFORE the historical backlog replays — forwarding it would advance a
// resuming client's cursor past events it has not yet seen, re-opening the very
// gap this abstraction closes (bug 1b). etcd's own IsProgressNotify() excludes
// Created for exactly this reason.
func TestEntityServer_WatchIndex_CreatedResponseNotProgress(t *testing.T) {
	r := require.New(t)

	store, sc, watchCh := watchIndexServer(t)

	ctx := t.Context()

	index := entity.Keyword(entity.Ident, "test/index")

	_, err := store.CreateEntity(ctx, entity.New(entity.Ref(entity.DBId, "widget-1"), index))
	r.NoError(err)

	gotOps := make(chan *v1alpha.EntityOp, 16)
	go func() {
		// A resuming client (from_revision>0) is the one that would be harmed by a
		// spurious progress watermark, so assert against it directly.
		_, _ = sc.WatchIndex(ctx, index, 5, stream.Callback(func(op *v1alpha.EntityOp) error {
			gotOps <- op
			return nil
		}))
	}()

	// The Created confirmation carries the current revision but no backlog yet.
	watchCh <- clientv3.WatchResponse{Created: true, Header: etcdserverpb.ResponseHeader{Revision: 100}}
	// Then the first replayed change.
	watchCh <- putResponse("widget-1", 6)

	select {
	case op := <-gotOps:
		r.Equal(int64(v1alpha.EntityOperationCreate), op.Operation(),
			"Created response must not be forwarded as a progress watermark")
		r.Equal(int64(6), op.Revision())
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for op")
	}
}

func TestEntityServer_ListDocuments(t *testing.T) {
	const testKind = "dev.miren.core/kind.project"

	setup := func(t *testing.T, count int) v1alpha.EntityAccessClient {
		t.Helper()
		r := require.New(t)

		store := entity.NewMockStore()
		server, err := NewEntityServer(slog.Default(), store)
		r.NoError(err)

		r.NoError(schema.Apply(context.TODO(), store))

		for i := range count {
			_, err := store.CreateEntity(context.TODO(), entity.New([]entity.Attr{
				{ID: entity.Ident, Value: entity.KeywordValue(fmt.Sprintf("test/entity%02d", i))},
				{ID: entity.EntityKind, Value: entity.RefValue(testKind)},
			}))
			r.NoError(err)
		}

		return v1alpha.EntityAccessClient{
			Client: rpc.LocalClient(v1alpha.AdaptEntityAccess(server)),
		}
	}

	index := entity.Attr{ID: entity.EntityKind, Value: entity.RefValue(testKind)}

	decode := func(t *testing.T, raw []byte) []*model.Document {
		t.Helper()
		var docs []*model.Document
		require.NoError(t, json.Unmarshal(raw, &docs))
		return docs
	}

	t.Run("returns every match when unlimited", func(t *testing.T) {
		r := require.New(t)
		sc := setup(t, 5)

		res, err := sc.ListDocuments(context.TODO(), index, "", 0, 0)
		r.NoError(err)

		docs := decode(t, res.Documents())
		r.Len(docs, 5)
		r.Equal("", res.Cursor(), "an exhausted index offers no cursor")
	})

	t.Run("pages through the index without repeating or dropping", func(t *testing.T) {
		r := require.New(t)
		sc := setup(t, 5)

		seen := map[string]bool{}
		cursor := ""

		for range 10 {
			res, err := sc.ListDocuments(context.TODO(), index, cursor, 2, 0)
			r.NoError(err)

			for _, d := range decode(t, res.Documents()) {
				r.False(seen[d.Id], "paging repeated %s", d.Id)
				seen[d.Id] = true
			}

			cursor = res.Cursor()
			if cursor == "" {
				break
			}
		}

		r.Equal("", cursor, "paging must terminate")
		r.Len(seen, 5, "the pages together must cover the index")
	})

	t.Run("reports the total only when starting from the head", func(t *testing.T) {
		r := require.New(t)
		sc := setup(t, 5)

		first, err := sc.ListDocuments(context.TODO(), index, "", 2, 0)
		r.NoError(err)
		r.Equal(int64(5), first.Total(), "the caller needs the full count to say what it is hiding")

		next, err := sc.ListDocuments(context.TODO(), index, first.Cursor(), 2, 0)
		r.NoError(err)
		r.Equal(int64(0), next.Total())
	})

	t.Run("elides oversized values only when asked", func(t *testing.T) {
		r := require.New(t)

		store := entity.NewMockStore()
		server, err := NewEntityServer(slog.Default(), store)
		r.NoError(err)
		r.NoError(schema.Apply(context.TODO(), store))

		_, err = store.CreateEntity(context.TODO(), entity.New([]entity.Attr{
			{ID: entity.Ident, Value: entity.KeywordValue("saga/big")},
			{ID: entity.EntityKind, Value: entity.RefValue(saga.KindSaga)},
			{ID: saga.SagaInitialInputsId, Value: entity.BytesValue(make([]byte, 4096))},
		}))
		r.NoError(err)

		sc := v1alpha.EntityAccessClient{
			Client: rpc.LocalClient(v1alpha.AdaptEntityAccess(server)),
		}

		sagaIndex := entity.Attr{ID: entity.EntityKind, Value: entity.RefValue(saga.KindSaga)}

		// Find the field by name rather than by position, so a change in
		// ordering fails the assertion instead of panicking on an index.
		initialInputs := func(t *testing.T, raw []byte) model.Field {
			t.Helper()
			docs := decode(t, raw)
			require.Len(t, docs, 1)
			for _, f := range docs[0].Facets {
				for _, fl := range f.Fields {
					if fl.Name == "initial_inputs" {
						return fl
					}
				}
			}
			t.Fatal("initial_inputs missing from the document")
			return model.Field{}
		}

		elided, err := sc.ListDocuments(context.TODO(), sagaIndex, "", 0, 32)
		r.NoError(err)
		r.True(initialInputs(t, elided.Documents()).Truncated)

		whole, err := sc.ListDocuments(context.TODO(), sagaIndex, "", 0, 0)
		r.NoError(err)
		r.False(initialInputs(t, whole.Documents()).Truncated,
			"the JSON path asks for zero and must get the whole value")
	})

	t.Run("encodes an empty page as an array rather than null", func(t *testing.T) {
		r := require.New(t)
		sc := setup(t, 0)

		res, err := sc.ListDocuments(context.TODO(), index, "", 0, 0)
		r.NoError(err)
		r.Equal("[]", string(res.Documents()), "consumers should not have to null-check")
	})
}

func TestEntityServer_GetDocument(t *testing.T) {
	r := require.New(t)

	store := entity.NewMockStore()
	server, err := NewEntityServer(slog.Default(), store)
	r.NoError(err)
	r.NoError(schema.Apply(context.TODO(), store))

	_, err = store.CreateEntity(context.TODO(), entity.New([]entity.Attr{
		{ID: entity.Ident, Value: entity.KeywordValue("saga/one")},
		{ID: entity.EntityKind, Value: entity.RefValue(saga.KindSaga)},
		{ID: saga.SagaDefinitionNameId, Value: entity.StringValue("deploy")},
	}))
	r.NoError(err)

	sc := v1alpha.EntityAccessClient{
		Client: rpc.LocalClient(v1alpha.AdaptEntityAccess(server)),
	}

	res, err := sc.GetDocument(context.TODO(), "saga/one", 0)
	r.NoError(err)

	var doc model.Document
	r.NoError(json.Unmarshal(res.Document(), &doc))

	r.Equal("saga/one", doc.Id)
	r.Len(doc.Facets, 1)
	r.Equal("saga/saga", doc.Facets[0].Label)
}

// A store that cannot answer must not be mistaken for an absent entity. The
// short-id fallback made every GetEntity error look like a miss, so an etcd
// outage reported "entity not found" and sent the operator hunting for a
// deletion that never happened.
func TestEntityServer_ResolveEntityPropagatesRealErrors(t *testing.T) {
	boom := errors.New("failed to get entity from etcd: connection refused")

	store := &failingGetStore{MockStore: entity.NewMockStore(), err: boom}

	server, err := NewEntityServer(slog.Default(), store)
	require.NoError(t, err)

	_, err = server.resolveEntity(context.TODO(), "saga/whatever")
	require.Error(t, err)

	assert.ErrorIs(t, err, boom, "a transport failure must reach the caller unchanged")
	assert.False(t, isNotFound(err), "and must not be reported as a missing entity")
}

func TestEntityServer_ResolveEntityReportsGenuineMiss(t *testing.T) {
	server, err := NewEntityServer(slog.Default(), entity.NewMockStore())
	require.NoError(t, err)

	_, err = server.resolveEntity(context.TODO(), "saga/nope")
	require.Error(t, err)
	assert.True(t, isNotFound(err))
}

// failingGetStore fails every read while behaving like a MockStore otherwise.
type failingGetStore struct {
	*entity.MockStore
	err error
}

func (f *failingGetStore) GetEntity(ctx context.Context, id entity.Id) (*entity.Entity, error) {
	return nil, f.err
}
