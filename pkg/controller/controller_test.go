package controller

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"log/slog"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/slogfmt"
	"miren.dev/runtime/servers/entityserver"
)

func TestReconcileController_Lifecycle(t *testing.T) {
	log := slog.New(slogfmt.NewTestHandler(t, &slog.HandlerOptions{Level: slog.LevelDebug}))

	store := entity.NewMockStore()
	server := &entityserver.EntityServer{
		Log:   log,
		Store: store,
	}

	sc := &entityserver_v1alpha.EntityAccessClient{
		Client: rpc.LocalClient(entityserver_v1alpha.AdaptEntityAccess(server)),
	}

	testIndex := entity.Any(entity.Type, "test/type")

	var handlerCalls atomic.Uint64
	handler := func(ctx context.Context, event Event) ([]entity.Attr, error) {
		handlerCalls.Add(1)
		return nil, nil
	}

	controller := NewReconcileController(
		"test-controller",
		log,
		testIndex,
		sc,
		handler,
		0, // no resync
		1, // workers
	)

	// Test Start
	ctx := t.Context()

	err := controller.Start(ctx)
	require.NoError(t, err)

	// Wait for watch to be established
	err = store.WaitForIndexWatcher(ctx, testIndex)
	require.NoError(t, err)

	// Create an entity that matches the watch index
	testEntity := entity.New(
		entity.Ref(entity.DBId, "test/entity1"),
		entity.String(entity.Type, "test/type"),
	)
	_, err = store.CreateEntity(ctx, testEntity)
	require.NoError(t, err)

	// Wait for the event to be processed
	require.Eventually(t, func() bool {
		return handlerCalls.Load() >= 1
	}, 5*time.Second, 10*time.Millisecond, "handler should be called at least once")

	// Test Stop
	controller.Stop()
}

func TestReconcileController_Resync(t *testing.T) {
	log := slog.New(slogfmt.NewTestHandler(t, &slog.HandlerOptions{Level: slog.LevelDebug}))

	store := entity.NewMockStore()
	server := &entityserver.EntityServer{
		Log:   log,
		Store: store,
	}

	sc := &entityserver_v1alpha.EntityAccessClient{
		Client: rpc.LocalClient(entityserver_v1alpha.AdaptEntityAccess(server)),
	}

	testIndex := entity.Any(entity.Type, "test/type")

	// Setup test entities
	store.AddEntity(entity.Id("test/entity1"), entity.New(
		entity.Ident, "test/entity1",
		entity.Type, "test/type",
	))

	resyncCalls := 0
	eventsChan := make(chan Event, 10)
	handler := func(ctx context.Context, event Event) ([]entity.Attr, error) {
		if event.Type == EventUpdated {
			resyncCalls++
		}
		eventsChan <- event
		return nil, nil
	}

	controller := NewReconcileController(
		"test-controller",
		log,
		testIndex,
		sc,
		handler,
		100*time.Millisecond, // short resync period for testing
		1,                    // single worker
	)

	// Start controller
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	err := controller.Start(ctx)
	require.NoError(t, err)

	// Wait for at least 2 resyncs
	<-ctx.Done()
	controller.Stop()

	// Should have at least 2 resync calls
	assert.GreaterOrEqual(t, resyncCalls, 2)
}

// Test entity for AdaptController tests
type TestEntity struct {
	ID   string
	Name string
}

var NameAttr = entity.Id("name")

func (e *TestEntity) Decode(getter entity.AttrGetter) {
	e.ID = entity.MustGet(getter, entity.DBId).Value.String()
	if attr, ok := getter.Get(NameAttr); ok {
		e.Name = attr.Value.String()
	}
}

func (e *TestEntity) Encode() []entity.Attr {
	return entity.New(
		entity.Ident, e.ID,
		NameAttr, e.Name,
	).Attrs()
}

// Controller that only implements GenericController (no Update method)
type BasicController struct {
	CreateCalls []string
	DeleteCalls []string
}

func (c *BasicController) Init(ctx context.Context) error { return nil }

func (c *BasicController) Create(ctx context.Context, obj *TestEntity, meta *entity.Meta) error {
	c.CreateCalls = append(c.CreateCalls, obj.ID)
	return nil
}

func (c *BasicController) Delete(ctx context.Context, id entity.Id, obj *TestEntity) error {
	c.DeleteCalls = append(c.DeleteCalls, string(id))
	return nil
}

// Controller that implements both GenericController and UpdatingController
type UpdatingControllerImpl struct {
	*BasicController
	UpdateCalls []string
}

func (c *UpdatingControllerImpl) Update(ctx context.Context, obj *TestEntity, meta *entity.Meta) error {
	c.UpdateCalls = append(c.UpdateCalls, obj.ID)
	return nil
}

func TestAdaptController_WithoutUpdateMethod(t *testing.T) {
	basicController := &BasicController{}
	handler := AdaptController[TestEntity](basicController)

	// Test EventAdded - should call Create
	entity1 := entity.New(
		entity.Ident, "test1",
		NameAttr, "Test Entity 1",
	)

	event := Event{
		Type:   EventAdded,
		Id:     "test1",
		Entity: entity1,
	}

	_, err := handler(context.Background(), event)
	require.NoError(t, err)

	// Test EventUpdated - should call Create (fallback)
	event.Type = EventUpdated
	_, err = handler(context.Background(), event)
	require.NoError(t, err)

	// Verify calls
	assert.Equal(t, []string{"id: test1", "id: test1"}, basicController.CreateCalls)
	assert.Empty(t, basicController.DeleteCalls)
}

func TestAdaptController_WithUpdateMethod(t *testing.T) {
	updatingController := &UpdatingControllerImpl{
		BasicController: &BasicController{},
	}
	handler := AdaptController[TestEntity](updatingController)

	// Test EventAdded - should call Create
	entity1 := entity.New(
		entity.Ident, "test1",
		NameAttr, "Test Entity 1",
	)

	event := Event{
		Type:   EventAdded,
		Id:     "test1",
		Entity: entity1,
	}

	_, err := handler(context.Background(), event)
	require.NoError(t, err)

	// Test EventUpdated - should call Update
	event.Type = EventUpdated
	_, err = handler(context.Background(), event)
	require.NoError(t, err)

	// Verify calls
	assert.Equal(t, []string{"id: test1"}, updatingController.CreateCalls)
	assert.Equal(t, []string{"id: test1"}, updatingController.UpdateCalls)
	assert.Empty(t, updatingController.DeleteCalls)
}

func TestReconcileController_PutRecordsRevision(t *testing.T) {
	log := slog.New(slogfmt.NewTestHandler(t, &slog.HandlerOptions{Level: slog.LevelDebug}))

	store := entity.NewMockStore()
	server := &entityserver.EntityServer{
		Log:   log,
		Store: store,
	}

	sc := &entityserver_v1alpha.EntityAccessClient{
		Client: rpc.LocalClient(entityserver_v1alpha.AdaptEntityAccess(server)),
	}

	testIndex := entity.Any(entity.Type, "test/type")

	// Add entity to store
	store.AddEntity(entity.Id("test/entity1"), entity.New(
		entity.Ident, "test/entity1",
		entity.Type, "test/type",
	))

	handler := func(ctx context.Context, event Event) ([]entity.Attr, error) {
		// Controller returns updates, which triggers a Put
		return []entity.Attr{
			entity.Any("updated", "true"),
		}, nil
	}

	controller := NewReconcileController(
		"test-controller",
		log,
		testIndex,
		sc,
		handler,
		0, // no resync
		1, // single worker
	)

	store.OnWatchIndex = func(ctx context.Context, attr entity.Attr) (clientv3.WatchChan, error) {
		ch := make(chan clientv3.WatchResponse)

		go func() {
			// Send watch event - controller will process and make update
			ch <- clientv3.WatchResponse{
				Events: []*clientv3.Event{
					{
						Type: clientv3.EventTypePut,
						Kv: &mvccpb.KeyValue{
							Key:            []byte("test/entity1"),
							Value:          []byte("test/entity1"),
							ModRevision:    100,
							CreateRevision: 1,
						},
					},
				},
			}
		}()

		return ch, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := controller.Start(ctx)
	require.NoError(t, err)

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	controller.Stop()

	// Verify that the controller recorded a revision from its Put
	// We can't easily intercept RecordWrite, but we can check the ring
	// The MockStore increments revisions starting from 1, so after one Put,
	// we should have revision 2 in the ring
	hasRecordedRevision := false
	for rev := int64(1); rev <= 10; rev++ {
		if controller.recentWrites.Contains(rev) {
			hasRecordedRevision = true
			t.Logf("Found recorded revision: %d", rev)
		}
	}

	assert.True(t, hasRecordedRevision, "Controller should have recorded at least one revision from its Put calls")
}

func TestReconcileController_FailedWriteDoesNotRecordRevision(t *testing.T) {
	log := slog.New(slogfmt.NewTestHandler(t, &slog.HandlerOptions{Level: slog.LevelDebug}))

	store := entity.NewMockStore()
	server := &entityserver.EntityServer{
		Log:   log,
		Store: store,
	}

	sc := &entityserver_v1alpha.EntityAccessClient{
		Client: rpc.LocalClient(entityserver_v1alpha.AdaptEntityAccess(server)),
	}

	testIndex := entity.Any(entity.Type, "test/type")

	// Add entity to store with revision 1
	entity1Rev1 := entity.New(
		entity.Ident, "test/entity1",
		entity.Type, "test/type",
	)
	entity1Rev1.SetRevision(1)
	store.AddEntity(entity.Id("test/entity1"), entity1Rev1)

	processedEvents := make(chan Event, 10)
	callCount := 0

	handler := func(ctx context.Context, event Event) ([]entity.Attr, error) {
		processedEvents <- event
		callCount++

		// First call: return updates that will fail to write
		// We'll simulate failure by removing the entity from the store
		if callCount == 1 {
			store.RemoveEntity(entity.Id("test/entity1"))
			return []entity.Attr{
				entity.Any("updated", "true"),
			}, nil
		}

		// Second call: no updates
		return nil, nil
	}

	controller := NewReconcileController(
		"test-controller",
		log,
		testIndex,
		sc,
		handler,
		0, // no resync
		1, // single worker
	)

	store.OnWatchIndex = func(ctx context.Context, attr entity.Attr) (clientv3.WatchChan, error) {
		ch := make(chan clientv3.WatchResponse)

		go func() {
			time.Sleep(20 * time.Millisecond)

			// First watch event - handler will try to update but it will fail
			ch <- clientv3.WatchResponse{
				Events: []*clientv3.Event{
					{
						Type: clientv3.EventTypePut,
						Kv: &mvccpb.KeyValue{
							Key:            []byte("index/key"),
							Value:          []byte("test/entity1"),
							CreateRevision: 100,
							ModRevision:    101,
						},
						PrevKv: &mvccpb.KeyValue{
							Value:       []byte("test/entity1"),
							ModRevision: 100,
						},
					},
				},
			}

			time.Sleep(50 * time.Millisecond)

			// Re-add entity with revision 1 for second event
			entity1Rev1Again := entity.New(
				entity.Ident, "test/entity1",
				entity.Type, "test/type",
			)
			entity1Rev1Again.SetRevision(1)
			store.AddEntity(entity.Id("test/entity1"), entity1Rev1Again)

			// Second watch event - should be processed since failed write wasn't recorded
			ch <- clientv3.WatchResponse{
				Events: []*clientv3.Event{
					{
						Type: clientv3.EventTypePut,
						Kv: &mvccpb.KeyValue{
							Key:            []byte("index/key"),
							Value:          []byte("test/entity1"),
							CreateRevision: 100,
							ModRevision:    101,
						},
						PrevKv: &mvccpb.KeyValue{
							Value:       []byte("test/entity1"),
							ModRevision: 100,
						},
					},
				},
			}
		}()

		return ch, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := controller.Start(ctx)
	require.NoError(t, err)

	// Collect events
	var allEvents []Event
	timeout := time.After(200 * time.Millisecond)
collectLoop:
	for {
		select {
		case event := <-processedEvents:
			allEvents = append(allEvents, event)
		case <-timeout:
			break collectLoop
		}
	}

	controller.Stop()
	time.Sleep(50 * time.Millisecond)

	// Filter to watch events (UPDATED)
	var watchEvents []Event
	for _, ev := range allEvents {
		if ev.Type != EventAdded {
			watchEvents = append(watchEvents, ev)
		}
	}

	// Should have processed both watch events since the failed write didn't record a revision
	assert.GreaterOrEqual(t, len(watchEvents), 2, "Should process both watch events since failed write didn't record revision")

	// Verify the ring doesn't contain revision 1 (the "failed" write)
	assert.False(t, controller.recentWrites.Contains(1), "Failed write should not be recorded in ring")
}
