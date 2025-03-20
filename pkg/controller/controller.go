package controller

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"miren.dev/runtime/pkg/entity"
)

// EventType represents the type of event that occurred on an entity
type EventType string

const (
	EventAdded   EventType = "ADDED"
	EventUpdated EventType = "UPDATED"
	EventDeleted EventType = "DELETED"
)

// Event represents a change to an entity
type Event struct {
	Type EventType
	Id   entity.Id
}

// Controller processes entities of a specific kind
type Controller interface {
	Start(ctx context.Context) error
	Stop()
}

// HandlerFunc is a function that processes an entity
type HandlerFunc func(ctx context.Context, event Event) error

// ReconcileController implements the Controller interface
type ReconcileController struct {
	Log *slog.Logger

	name         string
	index        entity.Attr
	store        *entity.EtcdStore
	handler      HandlerFunc
	resyncPeriod time.Duration
	workers      int
	workQueue    chan Event
	stopCh       chan struct{}
	wg           sync.WaitGroup
}

// NewReconcileController creates a new controller
func NewReconcileController(name string, index entity.Attr, store *entity.EtcdStore, handler HandlerFunc, resyncPeriod time.Duration, workers int) *ReconcileController {
	return &ReconcileController{
		name:         name,
		index:        index,
		store:        store,
		handler:      handler,
		resyncPeriod: resyncPeriod,
		workers:      workers,
		workQueue:    make(chan Event, 1000),
		stopCh:       make(chan struct{}),
	}
}

func convertEvent(event *clientv3.Event) (Event, error) {
	var eventType EventType

	switch {
	case event.IsCreate():
		eventType = EventAdded
	case event.IsModify():
		eventType = EventUpdated
	case event.Type == clientv3.EventTypeDelete:
		eventType = EventDeleted
	default:
		return Event{}, nil
	}

	id := entity.Id(event.Kv.Value)

	if eventType == EventDeleted {
		return Event{
			Type: eventType,
			Id:   id,
		}, nil
	}

	return Event{
		Type: eventType,
		Id:   id,
	}, nil
}

// Start starts the controller
func (c *ReconcileController) Start(ctx context.Context) error {
	c.Log.Info("Starting controller", "name", c.name, "id", c.index.ID, "value", c.index.Value)

	// Start watching for events
	eventChan, err := c.store.WatchIndex(ctx, c.index)
	if err != nil {
		return fmt.Errorf("failed to watch for events: %w", err)
	}

	// Start workers
	for i := 0; i < c.workers; i++ {
		c.wg.Add(1)
		go c.runWorker(ctx)
	}

	// Start event processor
	c.wg.Add(1)
	go c.processEvents(ctx, eventChan)

	// Start periodic resync
	if c.resyncPeriod > 0 {
		c.wg.Add(1)
		go c.periodicResync(ctx)
	}

	return nil
}

// Stop stops the controller
func (c *ReconcileController) Stop() {
	c.Log.Info("Stopping controller", "name", c.name)
	close(c.stopCh)
	c.wg.Wait()
	close(c.workQueue)
}

// processEvents processes events from the watch channel
func (c *ReconcileController) processEvents(ctx context.Context, eventChan clientv3.WatchChan) {
	defer c.wg.Done()

	for {
		select {
		case <-c.stopCh:
			return
		case resp, ok := <-eventChan:
			if !ok {
				return
			}

			for _, xevent := range resp.Events {
				event, err := convertEvent(xevent)
				if err != nil {
					c.Log.Error("error converting etcd event", "error", err)
					continue
				}

				// Queue the event
				select {
				case c.workQueue <- event:
					// Event added to queue
				case <-c.stopCh:
					return
				}
			}
		}
	}
}

// runWorker processes items from the work queue
func (c *ReconcileController) runWorker(ctx context.Context) {
	defer c.wg.Done()

	for {
		select {
		case <-c.stopCh:
			return
		case event, ok := <-c.workQueue:
			if !ok {
				return
			}

			// Process the event
			if err := c.processItem(ctx, event); err != nil {
				c.Log.Error("error processing item", "event", event, "error", err)
			}
		}
	}
}

// processItem processes a single item from the work queue
func (c *ReconcileController) processItem(ctx context.Context, event Event) error {
	// Handle different event types
	switch event.Type {
	case EventAdded, EventUpdated, EventDeleted:
		return c.handler(ctx, event)
	default:
		return fmt.Errorf("unknown event type: %s", event.Type)
	}
}

// periodicResync periodically resyncs all entities
func (c *ReconcileController) periodicResync(ctx context.Context) {
	defer c.wg.Done()

	ticker := time.NewTicker(c.resyncPeriod)
	defer ticker.Stop()

	for {
		// List all entities and queue them for processing
		entities, err := c.store.ListIndex(ctx, c.index)
		if err != nil {
			c.Log.Error("error listing entities for resync", "error", err)
			continue
		}

		for _, entity := range entities {
			select {
			case c.workQueue <- Event{Type: EventUpdated, Id: entity}:
				// Event added to queue
			case <-c.stopCh:
				return
			}
		}

		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			break
		}
	}
}

type ControllerEntity interface {
	Decode(getter entity.AttrGetter)
	Encode() []entity.Attr
}

type GenericController[P ControllerEntity] interface {
	Init(context.Context) error
	Create(ctx context.Context, obj P) error
	Delete(ctx context.Context, e *entity.Entity) error
}

func AdaptController[
	T any,
	P interface {
		*T
		ControllerEntity
	},
	C GenericController[P],
](cont C, eg entity.EntityGetter) HandlerFunc {
	return func(ctx context.Context, event Event) error {
		switch event.Type {
		case EventAdded, EventUpdated:
			entity, err := eg.GetEntity(ctx, event.Id)
			if err != nil {
				return fmt.Errorf("failed to get entity: %w", err)
			}

			if entity == nil {
				return fmt.Errorf("entity not found: %s", event.Id)
			}

			// Decode the entity into the controller entity type
			var obj P = new(T)
			obj.Decode(entity)

			if err := cont.Create(ctx, obj); err != nil {
				return fmt.Errorf("failed to create entity: %w", err)
			}

		case EventDeleted:
			entity, err := eg.GetEntity(ctx, event.Id)
			if err != nil {
				return fmt.Errorf("failed to get entity: %w", err)
			}

			if entity == nil {
				return fmt.Errorf("entity not found: %s", event.Id)
			}

			if err := cont.Delete(ctx, entity); err != nil {
				return fmt.Errorf("failed to create entity: %w", err)
			}
		}

		return nil
	}
}

// ControllerManager manages multiple controllers
type ControllerManager struct {
	controllers []Controller
}

// NewControllerManager creates a new controller manager
func NewControllerManager() *ControllerManager {
	return &ControllerManager{
		controllers: make([]Controller, 0),
	}
}

// AddController adds a controller to the manager
func (m *ControllerManager) AddController(controller Controller) {
	m.controllers = append(m.controllers, controller)
}

// Start starts all controllers
func (m *ControllerManager) Start(ctx context.Context) error {
	for _, controller := range m.controllers {
		if err := controller.Start(ctx); err != nil {
			return err
		}
	}

	return nil
}

// Stop stops all controllers
func (m *ControllerManager) Stop() {
	for _, controller := range m.controllers {
		controller.Stop()
	}
}
