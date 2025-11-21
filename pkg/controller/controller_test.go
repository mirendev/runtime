package controller

import (
	"context"
	"fmt"
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

	store.AddEntity(entity.Id("test/entity1"), entity.New(
		entity.Ident, "test/entity1",
		entity.Type, "test/type",
	))

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
	store.AddEntity(entity.Id("test/entity1"), entity.New(
		entity.Ident, "test/entity1",
		entity.Type, "test/type",
	))
	store.AddEntity(entity.Id("test/entity2"), entity.New(
		entity.Ident, "test/entity2",
		entity.Type, "test/type",
	))

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
		store.AddEntity(entity.Id(id), entity.New(
			entity.Ident, id,
			entity.Type, "test/type",
		))
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

func TestReconcileController_InFlightQueueing(t *testing.T) {
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

	// Add test entity to store
	store.AddEntity(entity.Id("test/entity1"), entity.New(
		entity.Ident, "test/entity1",
		entity.Type, "test/type",
	))

	// Track processing with detailed information
	type processRecord struct {
		entityId entity.Id
		rev      int64
		workerId string
		started  time.Time
		finished time.Time
	}

	var recordsMu sync.Mutex
	records := []processRecord{}

	// Channel to control when handler completes
	blockChan := make(chan struct{})
	// Track how many events are currently being processed concurrently
	var concurrentCount atomic.Int32
	var maxConcurrent atomic.Int32

	handler := func(ctx context.Context, event Event) ([]entity.Attr, error) {
		workerId := WorkerId(ctx)
		started := time.Now()

		// Track concurrent processing
		current := concurrentCount.Add(1)
		// Update max if this is higher
		for {
			max := maxConcurrent.Load()
			if current <= max || maxConcurrent.CompareAndSwap(max, current) {
				break
			}
		}

		// Wait for signal to proceed
		<-blockChan

		finished := time.Now()
		concurrentCount.Add(-1)

		recordsMu.Lock()
		records = append(records, processRecord{
			entityId: event.Id,
			rev:      event.Rev,
			workerId: workerId,
			started:  started,
			finished: finished,
		})
		recordsMu.Unlock()

		return nil, nil
	}

	controller := NewReconcileController(
		"test-controller",
		log,
		testIndex,
		sc,
		handler,
		0, // disable resync
		2, // use 2 workers to test concurrent scenarios
	)

	// Setup mock watch that sends multiple events for the same entity
	eventsSent := make(chan int64, 10)
	store.OnWatchIndex = func(ctx context.Context, attr entity.Attr) (clientv3.WatchChan, error) {
		ch := make(chan clientv3.WatchResponse)

		go func() {
			// Send 5 rapid-fire events for the same entity with different revisions
			for rev := int64(1); rev <= 5; rev++ {
				select {
				case ch <- clientv3.WatchResponse{
					Events: []*clientv3.Event{
						{
							Type: clientv3.EventTypePut,
							Kv: &mvccpb.KeyValue{
								Key:            []byte("test-key"),
								Value:          []byte("test/entity1"),
								ModRevision:    rev,
								CreateRevision: 1,
							},
						},
					},
				}:
					eventsSent <- rev
				case <-ctx.Done():
					close(ch)
					return
				}
			}

			// Keep channel open
			<-ctx.Done()
			close(ch)
		}()

		return ch, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := controller.Start(ctx)
	require.NoError(t, err)

	// Wait for all 5 events to be sent
	for i := 0; i < 5; i++ {
		select {
		case <-eventsSent:
		case <-time.After(1 * time.Second):
			t.Fatal("Timeout waiting for events to be sent")
		}
	}

	// Give a bit of time for events to be queued
	time.Sleep(50 * time.Millisecond)

	// Now unblock the handler one at a time
	// Since all events are for the same entity, they should be processed sequentially
	// by the same worker, not concurrently
	for i := 0; i < 5; i++ {
		blockChan <- struct{}{}
		time.Sleep(10 * time.Millisecond) // Small delay between unblocks
	}

	// Wait for all processing to complete
	time.Sleep(100 * time.Millisecond)

	controller.Stop()
	close(blockChan)

	// Verify results
	recordsMu.Lock()
	defer recordsMu.Unlock()

	require.Equal(t, 5, len(records), "Should have processed all 5 events")

	// Verify all events were for the same entity
	for _, record := range records {
		assert.Equal(t, entity.Id("test/entity1"), record.entityId)
	}

	t.Logf("Processed %d events for entity %s", len(records), "test/entity1")

	// CRITICAL: Verify that the same worker processed all events for this entity
	// This proves the in-flight queueing is working
	firstWorker := records[0].workerId
	t.Logf("All events processed by worker: %s", firstWorker)
	for i, record := range records {
		assert.Equal(t, firstWorker, record.workerId,
			"Event %d should be processed by same worker %s, got %s",
			i, firstWorker, record.workerId)
	}

	// Verify no concurrent processing occurred for the same entity
	maxConcurrentVal := maxConcurrent.Load()
	assert.Equal(t, int32(1), maxConcurrentVal,
		"Should never have more than 1 concurrent processing of same entity, got %d", maxConcurrentVal)

	// Verify events were processed sequentially (no overlap in time)
	for i := 1; i < len(records); i++ {
		prev := records[i-1]
		curr := records[i]
		assert.True(t, curr.started.After(prev.finished) || curr.started.Equal(prev.finished),
			"Event %d should start after event %d finishes. "+
				"Event %d finished at %v, Event %d started at %v",
			i, i-1,
			i-1, prev.finished, i, curr.started)
	}

	t.Logf("✓ Verified in-flight queueing: all %d events for same entity processed sequentially by worker %s",
		len(records), firstWorker)
}

func TestReconcileController_InFlightWithMultipleEntities(t *testing.T) {
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

	// Add test entities to store
	store.AddEntity(entity.Id("test/entity1"), entity.New(
		entity.Ident, "test/entity1",
		entity.Type, "test/type",
	))
	store.AddEntity(entity.Id("test/entity2"), entity.New(
		entity.Ident, "test/entity2",
		entity.Type, "test/type",
	))

	// Track processing with detailed information
	type processRecord struct {
		entityId entity.Id
		rev      int64
		workerId string
		started  time.Time
		finished time.Time
	}

	var recordsMu sync.Mutex
	records := []processRecord{}

	// Channel to control when handler completes
	blockChan := make(chan struct{})

	handler := func(ctx context.Context, event Event) ([]entity.Attr, error) {
		workerId := WorkerId(ctx)
		started := time.Now()

		// Wait for signal to proceed
		<-blockChan

		finished := time.Now()

		recordsMu.Lock()
		records = append(records, processRecord{
			entityId: event.Id,
			rev:      event.Rev,
			workerId: workerId,
			started:  started,
			finished: finished,
		})
		recordsMu.Unlock()

		return nil, nil
	}

	controller := NewReconcileController(
		"test-controller",
		log,
		testIndex,
		sc,
		handler,
		0, // disable resync
		2, // use 2 workers
	)

	// Setup mock watch that sends events for TWO different entities
	eventsSent := make(chan string, 10)
	store.OnWatchIndex = func(ctx context.Context, attr entity.Attr) (clientv3.WatchChan, error) {
		ch := make(chan clientv3.WatchResponse)

		go func() {
			// Send 3 events for entity1
			for rev := int64(1); rev <= 3; rev++ {
				select {
				case ch <- clientv3.WatchResponse{
					Events: []*clientv3.Event{
						{
							Type: clientv3.EventTypePut,
							Kv: &mvccpb.KeyValue{
								Key:            []byte("test-key-1"),
								Value:          []byte("test/entity1"),
								ModRevision:    rev,
								CreateRevision: 1,
							},
						},
					},
				}:
					eventsSent <- fmt.Sprintf("entity1-rev%d", rev)
				case <-ctx.Done():
					close(ch)
					return
				}
			}

			// Send 3 events for entity2
			for rev := int64(1); rev <= 3; rev++ {
				select {
				case ch <- clientv3.WatchResponse{
					Events: []*clientv3.Event{
						{
							Type: clientv3.EventTypePut,
							Kv: &mvccpb.KeyValue{
								Key:            []byte("test-key-2"),
								Value:          []byte("test/entity2"),
								ModRevision:    rev,
								CreateRevision: 1,
							},
						},
					},
				}:
					eventsSent <- fmt.Sprintf("entity2-rev%d", rev)
				case <-ctx.Done():
					close(ch)
					return
				}
			}

			// Keep channel open
			<-ctx.Done()
			close(ch)
		}()

		return ch, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := controller.Start(ctx)
	require.NoError(t, err)

	// Wait for all 6 events to be sent (3 per entity)
	for i := 0; i < 6; i++ {
		select {
		case <-eventsSent:
		case <-time.After(1 * time.Second):
			t.Fatal("Timeout waiting for events to be sent")
		}
	}

	// Give a bit of time for events to be queued
	time.Sleep(50 * time.Millisecond)

	// Unblock all handlers
	for i := 0; i < 6; i++ {
		blockChan <- struct{}{}
		time.Sleep(10 * time.Millisecond)
	}

	// Wait for all processing to complete
	time.Sleep(100 * time.Millisecond)

	controller.Stop()
	close(blockChan)

	// Verify results
	recordsMu.Lock()
	defer recordsMu.Unlock()

	require.Equal(t, 6, len(records), "Should have processed all 6 events")

	// Group records by entity
	entity1Records := []processRecord{}
	entity2Records := []processRecord{}
	for _, record := range records {
		switch record.entityId {
		case "test/entity1":
			entity1Records = append(entity1Records, record)
		case "test/entity2":
			entity2Records = append(entity2Records, record)
		}
	}

	require.Equal(t, 3, len(entity1Records), "Should have 3 records for entity1")
	require.Equal(t, 3, len(entity2Records), "Should have 3 records for entity2")

	// CRITICAL: Verify each entity's events were processed by the same worker
	entity1Worker := entity1Records[0].workerId
	t.Logf("Entity1 events processed by worker: %s", entity1Worker)
	for i, record := range entity1Records {
		assert.Equal(t, entity1Worker, record.workerId,
			"Entity1 event %d should be processed by same worker", i)
	}

	entity2Worker := entity2Records[0].workerId
	t.Logf("Entity2 events processed by worker: %s", entity2Worker)
	for i, record := range entity2Records {
		assert.Equal(t, entity2Worker, record.workerId,
			"Entity2 event %d should be processed by same worker", i)
	}

	// Verify each entity's events were processed sequentially
	for i := 1; i < len(entity1Records); i++ {
		prev := entity1Records[i-1]
		curr := entity1Records[i]
		assert.True(t, curr.started.After(prev.finished) || curr.started.Equal(prev.finished),
			"Entity1: event %d should start after event %d finishes", i, i-1)
	}

	for i := 1; i < len(entity2Records); i++ {
		prev := entity2Records[i-1]
		curr := entity2Records[i]
		assert.True(t, curr.started.After(prev.finished) || curr.started.Equal(prev.finished),
			"Entity2: event %d should start after event %d finishes", i, i-1)
	}

	// With 2 workers and 2 entities, we should see different workers handling different entities
	// (though this isn't strictly guaranteed due to scheduling, it's likely)
	t.Logf("✓ Verified in-flight queueing with multiple entities:")
	t.Logf("  - Entity1: %d events by worker %s", len(entity1Records), entity1Worker)
	t.Logf("  - Entity2: %d events by worker %s", len(entity2Records), entity2Worker)

	if entity1Worker != entity2Worker {
		t.Logf("  - ✓ Different workers handled different entities (concurrent processing of different entities)")
	} else {
		t.Logf("  - Note: Same worker handled both entities (still correct, just less concurrent)")
	}
}

func TestReconcileController_SkipsSelfGeneratedWatchEvents(t *testing.T) {
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

	// Add entity to store - must set revisions manually for watch event simulation
	// We'll create entities with different revisions that watch events will reference
	entity1Rev1 := entity.New(
		entity.Ident, "test/entity1",
		entity.Type, "test/type",
	)
	entity1Rev1.SetRevision(1)
	store.AddEntity(entity.Id("test/entity1"), entity1Rev1)

	processedEvents := make(chan Event, 10)

	handler := func(ctx context.Context, event Event) ([]entity.Attr, error) {
		processedEvents <- event
		// Don't return any updates - we're just testing the skip logic
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

	// Pre-record revision 2 as if controller wrote it
	controller.RecordWrite(2)

	// Setup watch to send multiple events
	store.OnWatchIndex = func(ctx context.Context, attr entity.Attr) (clientv3.WatchChan, error) {
		ch := make(chan clientv3.WatchResponse)

		go func() {
			// Send event for external write (revision 1 - already in store - should be processed)
			// This is a MODIFY event (CreateRevision < ModRevision)
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

			// Update entity to revision 2 in store
			entity1Rev2 := entity.New(
				entity.Ident, "test/entity1",
				entity.Type, "test/type",
			)
			entity1Rev2.SetRevision(2)
			store.AddEntity(entity.Id("test/entity1"), entity1Rev2)

			// Send event for controller's own write (revision 2 - should be skipped)
			ch <- clientv3.WatchResponse{
				Events: []*clientv3.Event{
					{
						Type: clientv3.EventTypePut,
						Kv: &mvccpb.KeyValue{
							Key:            []byte("index/key"),
							Value:          []byte("test/entity1"),
							CreateRevision: 100,
							ModRevision:    102,
						},
						PrevKv: &mvccpb.KeyValue{
							Value:       []byte("test/entity1"),
							ModRevision: 101,
						},
					},
				},
			}

			time.Sleep(50 * time.Millisecond)

			// Update entity to revision 3 in store
			entity1Rev3 := entity.New(
				entity.Ident, "test/entity1",
				entity.Type, "test/type",
			)
			entity1Rev3.SetRevision(3)
			store.AddEntity(entity.Id("test/entity1"), entity1Rev3)

			// Send event for another external write (revision 3 - should be processed)
			ch <- clientv3.WatchResponse{
				Events: []*clientv3.Event{
					{
						Type: clientv3.EventTypePut,
						Kv: &mvccpb.KeyValue{
							Key:            []byte("index/key"),
							Value:          []byte("test/entity1"),
							CreateRevision: 100,
							ModRevision:    103,
						},
						PrevKv: &mvccpb.KeyValue{
							Value:       []byte("test/entity1"),
							ModRevision: 102,
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

	// Verify: We should see events for revisions 1 and 3, but NOT 2
	// (Also will see initial ADDED event with rev 0, which we ignore)
	receivedRevisions := []int64{}

	// Collect events until we have 2 watch events (ignoring ADDED)
	timeout := time.After(500 * time.Millisecond)
	for len(receivedRevisions) < 2 {
		select {
		case event := <-processedEvents:
			// Only count watch events (UPDATED), not initial ADDED
			if event.Type != EventAdded {
				receivedRevisions = append(receivedRevisions, event.Rev)
			}
		case <-timeout:
			t.Fatalf("timeout waiting for events, got: %v", receivedRevisions)
		}
	}

	controller.Stop()

	// Verify we got revisions 1 and 3, but not 2
	assert.Equal(t, []int64{1, 3}, receivedRevisions, "Should skip self-generated revision 2")
}

func TestReconcileController_RingWraparound(t *testing.T) {
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

	// Add entity to store with revision 1 (for first watch event)
	entity1Rev1 := entity.New(
		entity.Ident, "test/entity1",
		entity.Type, "test/type",
	)
	entity1Rev1.SetRevision(1)
	store.AddEntity(entity.Id("test/entity1"), entity1Rev1)

	processedEvents := make(chan Event, 10)
	handler := func(ctx context.Context, event Event) ([]entity.Attr, error) {
		processedEvents <- event
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

	// Replace the ring with a small one for testing wraparound
	controller.recentWrites = NewRingSet(3)
	// Record revisions 1, 2, 3
	controller.RecordWrite(1)
	controller.RecordWrite(2)
	controller.RecordWrite(3)
	// Verify all are in the ring
	assert.True(t, controller.recentWrites.Contains(1))
	assert.True(t, controller.recentWrites.Contains(2))
	assert.True(t, controller.recentWrites.Contains(3))
	// Add revision 4 - should evict revision 1
	controller.RecordWrite(4)
	// Verify wraparound
	assert.False(t, controller.recentWrites.Contains(1), "Revision 1 should be evicted")
	assert.True(t, controller.recentWrites.Contains(2))
	assert.True(t, controller.recentWrites.Contains(3))
	assert.True(t, controller.recentWrites.Contains(4))

	// Setup watch BEFORE starting controller
	store.OnWatchIndex = func(ctx context.Context, attr entity.Attr) (clientv3.WatchChan, error) {
		ch := make(chan clientv3.WatchResponse)

		go func() {
			// Wait for watch to be fully established
			time.Sleep(20 * time.Millisecond)

			// Event for evicted revision 1 (should be processed)
			// Entity already has revision 1 in store
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

			// Update entity to revision 4 in store
			entity1Rev4 := entity.New(
				entity.Ident, "test/entity1",
				entity.Type, "test/type",
			)
			entity1Rev4.SetRevision(4)
			store.AddEntity(entity.Id("test/entity1"), entity1Rev4)

			// Event for revision 4 still in ring (should be skipped)
			ch <- clientv3.WatchResponse{
				Events: []*clientv3.Event{
					{
						Type: clientv3.EventTypePut,
						Kv: &mvccpb.KeyValue{
							Key:            []byte("index/key"),
							Value:          []byte("test/entity1"),
							CreateRevision: 100,
							ModRevision:    104,
						},
						PrevKv: &mvccpb.KeyValue{
							Value:       []byte("test/entity1"),
							ModRevision: 101,
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

	// Collect all events to debug
	var allEvents []Event
	timeout := time.After(300 * time.Millisecond)
collectLoop:
	for {
		select {
		case event := <-processedEvents:
			allEvents = append(allEvents, event)
			t.Logf("Collected event: Type=%s, Rev=%d", event.Type, event.Rev)
		case <-timeout:
			break collectLoop
		}
	}

	controller.Stop()
	time.Sleep(50 * time.Millisecond) // Allow goroutines to fully clean up

	// Filter to just watch events (UPDATED)
	var watchEvents []Event
	for _, ev := range allEvents {
		if ev.Type != EventAdded {
			watchEvents = append(watchEvents, ev)
		}
	}

	// Should only receive event for revision 1 (evicted), not 4 (still in ring)
	require.Len(t, watchEvents, 1, "Should receive exactly one watch event (revision 1), got: %v", watchEvents)
	assert.Equal(t, int64(1), watchEvents[0].Rev, "Should process evicted revision 1")
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
