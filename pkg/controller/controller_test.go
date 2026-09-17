package controller

import (
	"context"
	"sync"
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

// TestReconcileController_Resync verifies that periodic reconciliation
// re-enqueues every entity on each tick without restarting the watch, and that
// the watch keeps delivering live adds and deletes across those ticks.
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

	store.AddEntity(entity.Id("test/entity1"), entity.New(
		entity.Ident, "test/entity1",
		entity.Type, "test/type",
	))

	var mu sync.Mutex
	var seen []Event
	handler := func(ctx context.Context, event Event) ([]entity.Attr, error) {
		mu.Lock()
		seen = append(seen, event)
		mu.Unlock()
		return nil, nil
	}
	count := func(typ EventType, id entity.Id) int {
		mu.Lock()
		defer mu.Unlock()
		n := 0
		for _, ev := range seen {
			if ev.Type == typ && ev.Id == id {
				n++
			}
		}
		return n
	}

	controller := NewReconcileController(
		"test-controller",
		log,
		testIndex,
		sc,
		handler,
		50*time.Millisecond, // short resync period for testing
		1,                   // single worker
	)

	ctx := t.Context()
	require.NoError(t, controller.Start(ctx))
	defer controller.Stop()

	require.NoError(t, store.WaitForIndexWatcher(ctx, testIndex))

	// The initial snapshot plus at least two resync ticks each reconcile entity1.
	require.Eventually(t, func() bool {
		return count(EventUpdated, "test/entity1") >= 3
	}, 5*time.Second, 10*time.Millisecond, "entity1 should be reconciled on each resync tick")

	// Resync ticks must not have torn down and re-established the watch.
	assert.Len(t, store.WatchFromRevsCopy(), 1, "resync should reuse the healthy watch")

	// The watch is still live: a create and a delete both arrive as events.
	_, err := store.CreateEntity(ctx, entity.New(
		entity.Ref(entity.DBId, "test/entity2"),
		entity.String(entity.Type, "test/type"),
	))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return count(EventAdded, "test/entity2") >= 1
	}, 5*time.Second, 10*time.Millisecond, "live add should arrive through the open watch")

	require.NoError(t, store.DeleteEntity(ctx, "test/entity1"))
	require.Eventually(t, func() bool {
		return count(EventDeleted, "test/entity1") >= 1
	}, 5*time.Second, 10*time.Millisecond, "live delete should arrive through the open watch")

	assert.Len(t, store.WatchFromRevsCopy(), 1, "watch should still be the original one")
}

// TestReconcileController_SlowResyncDoesNotResurrectDelete pins the ordering
// between the periodic resync and the watch. A List held open across an
// index-only removal (the entity still exists, its index entry is gone, as when
// a session lease expires) returns a snapshot that predates the removal. If
// that snapshot were enqueued after the removal had been processed, the worker
// would read the still-present entity and reconcile it back to life. The
// resync runs on the watch consumer goroutine so the removal cannot be
// processed ahead of it.
func TestReconcileController_SlowResyncDoesNotResurrectDelete(t *testing.T) {
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
	ctx := t.Context()

	store.AddEntity("test/entity1", entity.New(
		entity.Ref(entity.DBId, "test/entity1"),
		entity.String(entity.Type, "test/type"),
	))

	// The test drives the raw watch stream itself so it can deliver an index
	// removal for an entity the store still holds.
	watches := make(chan chan clientv3.WatchResponse, 1)
	store.OnWatchIndex = func(ctx context.Context, attr entity.Attr) (clientv3.WatchChan, error) {
		ch := make(chan clientv3.WatchResponse)
		watches <- ch
		return ch, nil
	}

	// Hold the resync List open until released. The ids are read when the
	// List starts and the mock reports the revision it started at, so the held
	// snapshot predates the removal delivered while it is open, and every List
	// after the removal correctly reports an empty index.
	listing := make(chan struct{}, 1)
	release := make(chan struct{})
	var holdList, removed atomic.Bool
	var listsAfterRemoval atomic.Int64
	store.OnListIndex = func(ctx context.Context, attr entity.Attr) ([]entity.Id, error) {
		var ids []entity.Id
		if removed.Load() {
			listsAfterRemoval.Add(1)
		} else {
			ids = []entity.Id{"test/entity1"}
		}
		if holdList.CompareAndSwap(true, false) {
			listing <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
		return ids, nil
	}

	var mu sync.Mutex
	var seen []Event
	handler := func(ctx context.Context, event Event) ([]entity.Attr, error) {
		mu.Lock()
		seen = append(seen, event)
		mu.Unlock()
		return nil, nil
	}
	snapshot := func() []Event {
		mu.Lock()
		defer mu.Unlock()
		return append([]Event(nil), seen...)
	}

	controller := NewReconcileController("test-controller", log, testIndex, sc, handler, 50*time.Millisecond, 1)
	require.NoError(t, controller.Start(ctx))
	defer controller.Stop()

	var watch chan clientv3.WatchResponse
	select {
	case watch = <-watches:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the watch to be established")
	}

	require.Eventually(t, func() bool {
		return len(snapshot()) >= 1
	}, 5*time.Second, 10*time.Millisecond, "initial snapshot should reconcile entity1")

	holdList.Store(true)
	select {
	case <-listing:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a resync List to start")
	}

	// Remove entity1 from the index under the held List, at a revision after
	// the List's, and wait for the watcher to deliver it before releasing so
	// the stale snapshot provably arrives after the removal. With the resync
	// sequenced behind the consumer the removal parks in the Updates buffer;
	// an unsequenced resync would let it straight through to the handler.
	removed.Store(true)
	watch <- clientv3.WatchResponse{Events: []*clientv3.Event{{
		Type:   clientv3.EventTypeDelete,
		Kv:     &mvccpb.KeyValue{Key: []byte("k/test/entity1"), ModRevision: 100},
		PrevKv: &mvccpb.KeyValue{Key: []byte("k/test/entity1"), Value: []byte("test/entity1"), ModRevision: 99},
	}}}
	sawDelete := func() bool {
		for _, ev := range snapshot() {
			if ev.Type == EventDeleted && ev.Id == "test/entity1" {
				return true
			}
		}
		return false
	}
	require.Eventually(t, func() bool {
		return len(controller.watcher.Updates()) == 1 || sawDelete()
	}, 5*time.Second, time.Millisecond, "the removal should be delivered while the List is held")
	close(release)

	require.Eventually(t, sawDelete, 5*time.Second, 10*time.Millisecond, "the index removal should reach the handler")

	// Let further resync ticks run and the queue drain. The entity is still in
	// the store, so an out-of-order snapshot would show up as an Update after
	// the Delete.
	require.Eventually(t, func() bool {
		stats := controller.queue.Stats()
		return listsAfterRemoval.Load() >= 2 && stats.depth == 0 && controller.counters.inFlight.Load() == 0
	}, 5*time.Second, 10*time.Millisecond, "later resyncs should run and the queue should drain")

	events := snapshot()
	lastDelete := -1
	for i, ev := range events {
		if ev.Type == EventDeleted && ev.Id == "test/entity1" {
			lastDelete = i
		}
	}
	require.GreaterOrEqual(t, lastDelete, 0)
	for _, ev := range events[lastDelete+1:] {
		if ev.Id == "test/entity1" {
			t.Fatalf("stale resync resurrected entity1 after its removal: %s", ev.Type)
		}
	}
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
