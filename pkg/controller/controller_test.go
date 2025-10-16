package controller

import (
	"context"
	"fmt"
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

	// Give a few millis for the test event to come through
	time.Sleep(10 * time.Millisecond)

	// Test Stop
	controller.Stop()

	// MockStore sends a fake Put event to /mock/entity, ensure it came through
	require.Equal(t, handlerCalls.Load(), uint64(1))
}

func TestReconcileController_EventProcessing(t *testing.T) {
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

	eventsChan := make(chan Event, 10)
	handler := func(ctx context.Context, event Event) ([]entity.Attr, error) {
		eventsChan <- event
		return nil, nil
	}

	controller := NewReconcileController(
		"test-controller",
		log,
		testIndex,
		sc,
		handler,
		0, // disable resync
		1, // single worker
	)

	store.AddEntity(entity.Id("test/entity1"), entity.NewEntity(entity.Attrs(
		entity.Ident, "test/entity1",
		entity.Type, "test/type",
	)))

	// Setup mock watch handler
	store.OnWatchIndex = func(ctx context.Context, attr entity.Attr) (clientv3.WatchChan, error) {
		ch := make(chan clientv3.WatchResponse)

		go func() {
			ch <- clientv3.WatchResponse{
				Events: []*clientv3.Event{
					{
						Type: clientv3.EventTypePut,
						Kv: &mvccpb.KeyValue{
							Key:            []byte("fobar"),
							Value:          []byte("test/entity1"),
							ModRevision:    1,
							CreateRevision: 1,
						},
					},
				},
			}

			ch <- clientv3.WatchResponse{
				Events: []*clientv3.Event{
					{
						Type: clientv3.EventTypePut,
						Kv: &mvccpb.KeyValue{
							Key:            []byte("fobar"),
							Value:          []byte("test/entity1"),
							ModRevision:    2,
							CreateRevision: 1,
						},
						PrevKv: &mvccpb.KeyValue{
							Key:            []byte("fobar"),
							Value:          []byte("test/entity1"),
							ModRevision:    1,
							CreateRevision: 1,
						},
					},
				},
			}

			ch <- clientv3.WatchResponse{
				Events: []*clientv3.Event{
					{
						Type: clientv3.EventTypeDelete,
						Kv: &mvccpb.KeyValue{
							Key:            []byte("fobar"),
							Value:          []byte("test/entity1"),
							ModRevision:    3,
							CreateRevision: 1,
						},
						PrevKv: &mvccpb.KeyValue{
							Key:            []byte("fobar"),
							Value:          []byte("test/entity1"),
							ModRevision:    2,
							CreateRevision: 1,
						},
					},
				},
			}
		}()

		return ch, nil
	}

	// Start controller
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := controller.Start(ctx)
	require.NoError(t, err)

	// Verify events are processed
	expectedEvents := 3
	receivedEvents := 0

	for receivedEvents < expectedEvents {
		select {
		case event := <-eventsChan:
			receivedEvents++
			switch receivedEvents {
			case 1:
				assert.Equal(t, EventAdded, event.Type)
			case 2:
				assert.Equal(t, EventUpdated, event.Type)
				assert.Equal(t, int64(1), event.PrevRev)
			case 3:
				assert.Equal(t, EventDeleted, event.Type)
			}
		case <-ctx.Done():
			t.Fatal("timeout waiting for events")
		}
	}

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
	store.AddEntity(entity.Id("test/entity1"), entity.NewEntity(entity.Attrs(
		entity.Ident, "test/entity1",
		entity.Type, "test/type",
	)))

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
	return entity.Attrs(
		entity.Ident, e.ID,
		NameAttr, e.Name,
	)
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

func (c *BasicController) Delete(ctx context.Context, id entity.Id) error {
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
	entity1 := entity.NewEntity(entity.Attrs(
		entity.Ident, "test1",
		NameAttr, "Test Entity 1",
	))

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
	entity1 := entity.NewEntity(entity.Attrs(
		entity.Ident, "test1",
		NameAttr, "Test Entity 1",
	))

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

func TestReconcileController_WatchReconnect(t *testing.T) {
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

	eventsChan := make(chan Event, 10)
	handler := func(ctx context.Context, event Event) ([]entity.Attr, error) {
		eventsChan <- event
		return nil, nil
	}

	controller := NewReconcileController(
		"test-controller",
		log,
		testIndex,
		sc,
		handler,
		0, // disable resync
		1, // single worker
	)

	// Track number of WatchIndex calls to verify reconnection
	watchCallCount := atomic.Int32{}
	connectionAttempts := make(chan int32, 10)

	// Control channel to simulate disconnection
	simulateDisconnect := make(chan struct{})

	// Setup mock watch handler that simulates disconnection and reconnection
	store.OnWatchIndex = func(ctx context.Context, attr entity.Attr) (clientv3.WatchChan, error) {
		attempt := watchCallCount.Add(1)
		connectionAttempts <- attempt

		// First connection - return error to simulate disconnection
		switch attempt {
		case 1:
			ch := make(chan clientv3.WatchResponse)
			go func() {
				// Send initial event
				select {
				case ch <- clientv3.WatchResponse{
					Events: []*clientv3.Event{
						{
							Type: clientv3.EventTypePut,
							Kv: &mvccpb.KeyValue{
								Key:            []byte("test-key-1"),
								Value:          []byte("test/entity1"),
								ModRevision:    1,
								CreateRevision: 1,
							},
						},
					},
				}:
				case <-ctx.Done():
					return
				}

				// Wait for signal to disconnect
				select {
				case <-simulateDisconnect:
					// Send canceled response to simulate disconnection
					ch <- clientv3.WatchResponse{
						Canceled: true,
					}
					close(ch)
				case <-ctx.Done():
					close(ch)
				}
			}()
			return ch, nil
		case 2:
			// Second connection - successful reconnection
			ch := make(chan clientv3.WatchResponse)
			go func() {
				// Send event after reconnection
				select {
				case ch <- clientv3.WatchResponse{
					Events: []*clientv3.Event{
						{
							Type: clientv3.EventTypePut,
							Kv: &mvccpb.KeyValue{
								Key:            []byte("test-key-2"),
								Value:          []byte("test/entity2"),
								ModRevision:    2,
								CreateRevision: 2,
							},
						},
					},
				}:
				case <-ctx.Done():
					return
				}

				// Keep channel open until context is done
				<-ctx.Done()
				close(ch)
			}()
			return ch, nil
		default:
			// Subsequent connections if any
			ch := make(chan clientv3.WatchResponse)
			go func() {
				<-ctx.Done()
				close(ch)
			}()
			return ch, nil
		}
	}

	// Add test entity to store
	store.AddEntity(entity.Id("test/entity1"), entity.NewEntity(entity.Attrs(
		entity.Ident, "test/entity1",
		entity.Type, "test/type",
	)))
	store.AddEntity(entity.Id("test/entity2"), entity.NewEntity(entity.Attrs(
		entity.Ident, "test/entity2",
		entity.Type, "test/type",
	)))

	// Start controller
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := controller.Start(ctx)
	require.NoError(t, err)

	// Wait for first connection
	select {
	case attempt := <-connectionAttempts:
		assert.Equal(t, int32(1), attempt, "First connection attempt")
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for first connection")
	}

	// Wait for first event
	select {
	case event := <-eventsChan:
		assert.Equal(t, EventAdded, event.Type)
		assert.Equal(t, entity.Id("test/entity1"), event.Id)
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for first event")
	}

	// Trigger disconnection
	close(simulateDisconnect)

	// Wait for reconnection after simulated disconnection
	select {
	case attempt := <-connectionAttempts:
		assert.Equal(t, int32(2), attempt, "Should reconnect after disconnection")
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for reconnection")
	}

	// Wait for event after reconnection
	select {
	case event := <-eventsChan:
		assert.Equal(t, EventAdded, event.Type)
		assert.Equal(t, entity.Id("test/entity2"), event.Id)
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for event after reconnection")
	}

	// Stop controller
	controller.Stop()

	// Verify we had exactly 2 connection attempts (initial + 1 reconnect)
	assert.Equal(t, int32(2), watchCallCount.Load(), "Should have exactly 2 watch connections")
}

func TestReconcileController_QueueOverflow(t *testing.T) {
	t.Skip("This test is designed to to check queue overflow handling, but it's got some races. TODO: rewrite with synctest")

	log := slog.New(slogfmt.NewTestHandler(t, &slog.HandlerOptions{Level: slog.LevelDebug}))

	store := entity.NewMockStore()

	// Pre-create all test entities before starting controller to avoid concurrent map access
	// We need more than the queue size (1000) to trigger overflow
	numEntities := 1100 // Just slightly more than queue size
	entityIds := make([]entity.Id, 0, numEntities)
	for i := range numEntities {
		id := fmt.Sprintf("test/entity%d", i)
		entityIds = append(entityIds, entity.Id(id))
		store.AddEntity(entity.Id(id), entity.NewEntity(entity.Attrs(
			entity.Ident, id,
			entity.Type, "test/type",
		)))
	}

	// Track resync completion attempts
	var resyncStarted atomic.Int32
	var resyncCompleted atomic.Int32

	// Custom ListIndex to track when resync starts and completes
	store.OnListIndex = func(ctx context.Context, attr entity.Attr) ([]entity.Id, error) {
		if attr.ID == entity.Type && attr.Value.String() == "test/type" {
			count := resyncStarted.Add(1)
			t.Logf("Resync #%d started - listing %d entities", count, len(entityIds))

			// Small delay to ensure deterministic behavior
			select {
			case <-time.After(1 * time.Millisecond):
			case <-ctx.Done():
				return nil, ctx.Err()
			}

			// Track completion
			resyncCompleted.Add(1)
			t.Logf("Resync #%d completed listing", count)

			return entityIds, nil
		}
		// For other queries, return empty
		return []entity.Id{}, nil
	}

	// Provide a custom WatchIndex to avoid modifying the entities map
	store.OnWatchIndex = func(ctx context.Context, attr entity.Attr) (clientv3.WatchChan, error) {
		ch := make(chan clientv3.WatchResponse)
		// Just keep the channel open until context is done
		go func() {
			<-ctx.Done()
			close(ch)
		}()
		return ch, nil
	}

	server := &entityserver.EntityServer{
		Log:   log,
		Store: store,
	}

	sc := &entityserver_v1alpha.EntityAccessClient{
		Client: rpc.LocalClient(entityserver_v1alpha.AdaptEntityAccess(server)),
	}

	testIndex := entity.Any(entity.Type, "test/type")

	// Track how many events were processed
	var eventsProcessed atomic.Int32
	processedChan := make(chan struct{}, 1)
	blockHandler := make(chan struct{})

	// Handler that processes first event then blocks until we signal
	handler := func(ctx context.Context, event Event) ([]entity.Attr, error) {
		count := eventsProcessed.Add(1)
		if count == 1 {
			// Signal first event was processed
			select {
			case processedChan <- struct{}{}:
			default:
			}
		}
		// Block until test signals us to continue
		select {
		case <-blockHandler:
		case <-ctx.Done():
		}
		return nil, nil
	}

	controller := NewReconcileController(
		"test-controller",
		log,
		testIndex,
		sc,
		handler,
		30*time.Millisecond, // resync period - short enough to trigger multiple times
		1,                   // single worker
	)

	// Start controller
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	defer close(blockHandler) // Ensure handler unblocks on cleanup

	err := controller.Start(ctx)
	require.NoError(t, err)

	// Wait for first event to be processed (proving worker started)
	select {
	case <-processedChan:
		t.Log("First event processed, queue should start filling")
	case <-time.After(50 * time.Millisecond):
		t.Fatal("First event was never processed")
	}

	// Now the worker is blocked and queue has 999 free slots (started with 1000, processed 1)
	// First resync should happen around 30ms and will try to queue 1100 events
	// It should queue 999 successfully, then drop the rest with warnings

	// Wait for multiple resync periods
	time.Sleep(70 * time.Millisecond) // Should be enough for 2+ resync attempts

	// The context will timeout and we'll stop the controller
	<-ctx.Done()
	controller.Stop()

	// Check the results
	started := resyncStarted.Load()
	completed := resyncCompleted.Load()
	processed := eventsProcessed.Load()

	t.Logf("Resync attempts started: %d", started)
	t.Logf("Resync attempts completed: %d", completed)
	t.Logf("Events processed: %d", processed)

	// With the bug (no default case):
	// - First resync starts and completes (lists entities)
	// - periodicResync tries to queue events, gets stuck at event 1000
	// - Second resync may start but won't complete because periodicResync is stuck
	//
	// With the fix (default case):
	// - Multiple resyncs should complete successfully
	// - Events get dropped when queue is full but resync continues

	if started < 2 {
		t.Errorf("Expected at least 2 resync attempts to start, got %d", started)
	}

	// This is the key test: with the bug, the second resync won't complete
	// because periodicResync gets stuck trying to queue event 1001
	if completed < 2 {
		t.Errorf("Expected at least 2 resync attempts to complete, got %d", completed)
		t.Log("This indicates the resync got stuck trying to queue events to a full queue")
		t.Log("The fix (default case) allows resync to drop events and continue")
	}

	// We expect at least 1 event to be processed (the first one before blocking)
	// Due to timing, a few more might sneak through before the handler blocks
	if processed < 1 {
		t.Errorf("Expected at least 1 event to be processed, got %d", processed)
	}
	if processed > 10 {
		// If too many were processed, the blocking isn't working
		t.Errorf("Too many events processed (%d), handler should have blocked", processed)
	}
}
