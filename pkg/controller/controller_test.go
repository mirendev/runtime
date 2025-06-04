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

	store.Entities[entity.Id("test/entity1")] = &entity.Entity{
		ID: entity.Id("test/entity1"),
		Attrs: entity.Attrs(
			entity.Ident, "test/entity1",
			entity.Type, "test/type",
		),
	}

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
	store.Entities[entity.Id("test/entity1")] = &entity.Entity{
		ID: entity.Id("test/entity1"),
		Attrs: entity.Attrs(
			entity.Ident, "test/entity1",
			entity.Type, "test/type",
		),
	}

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
	e.ID = entity.MustGet(getter, entity.Ident).Value.String()
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
	entity1 := &entity.Entity{
		ID: "test1",
		Attrs: entity.Attrs(
			entity.Ident, "test1",
			NameAttr, "Test Entity 1",
		),
	}

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
	assert.Equal(t, []string{"test1", "test1"}, basicController.CreateCalls)
	assert.Empty(t, basicController.DeleteCalls)
}

func TestAdaptController_WithUpdateMethod(t *testing.T) {
	updatingController := &UpdatingControllerImpl{
		BasicController: &BasicController{},
	}
	handler := AdaptController[TestEntity](updatingController)

	// Test EventAdded - should call Create
	entity1 := &entity.Entity{
		ID: "test1",
		Attrs: entity.Attrs(
			entity.Ident, "test1",
			NameAttr, "Test Entity 1",
		),
	}

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
	assert.Equal(t, []string{"test1"}, updatingController.BasicController.CreateCalls)
	assert.Equal(t, []string{"test1"}, updatingController.UpdateCalls)
	assert.Empty(t, updatingController.BasicController.DeleteCalls)
}
