package controller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/rpc/stream"
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

	Entity *entity.Entity // The entity that was changed

	Rev, PrevRev int64 // Revision and previous revision for the entity
}

// Controller processes entities of a specific kind
type Controller interface {
	Start(ctx context.Context) error
	Stop()
}

type workerIdKey struct{}

func withWorkerId(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, workerIdKey{}, id)
}

func WorkerId(ctx context.Context) string {
	if id, ok := ctx.Value(workerIdKey{}).(string); ok {
		return id
	}
	return "unknown"
}

// HandlerFunc is a function that processes an entity
type HandlerFunc func(ctx context.Context, event Event) ([]entity.Attr, error)

// ReconcileController implements the Controller interface
type ReconcileController struct {
	Log *slog.Logger

	cancel func()

	esc          *entityserver_v1alpha.EntityAccessClient
	name         string
	index        entity.Attr
	handler      HandlerFunc
	resyncPeriod time.Duration
	workers      int
	workQueue    chan Event
	wg           sync.WaitGroup

	// periodic is an optional periodic callback
	periodic     func(ctx context.Context) error
	periodicTime time.Duration
}

// NewReconcileController creates a new controller
func NewReconcileController(name string, log *slog.Logger, index entity.Attr, esc *entityserver_v1alpha.EntityAccessClient, handler HandlerFunc, resyncPeriod time.Duration, workers int) *ReconcileController {
	return &ReconcileController{
		Log:          log.With("module", fmt.Sprintf("reconcile.%s", name)),
		name:         name,
		index:        index,
		esc:          esc,
		handler:      handler,
		resyncPeriod: resyncPeriod,
		workers:      workers,
		workQueue:    make(chan Event, 1000),
	}
}

// SetPeriodic sets the periodic callback function
func (c *ReconcileController) SetPeriodic(often time.Duration, fn func(ctx context.Context) error) {
	c.periodic = fn
	c.periodicTime = often
}

// Start starts the controller
func (c *ReconcileController) Start(top context.Context) error {
	ctx, cancel := context.WithCancel(top)
	c.cancel = cancel

	c.Log.Info("Starting controller", "name", c.name, "id", c.index.ID, "value", c.index.Value)

	// Start workers
	for i := 0; i < c.workers; i++ {
		c.wg.Add(1)
		go c.runWorker(ctx)
	}

	// Start event processor
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.Log.Info("Starting index watch")
		defer c.Log.Info("Index watch stopped")

		// Retry logic with exponential backoff
		retryDelay := time.Second
		maxRetryDelay := time.Minute * 5
		retryCount := 0

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			if retryCount > 0 {
				c.Log.Info("Attempting to reconnect watch", "index", c.index.ID, "value", c.index.Value, "attempt", retryCount)
			} else {
				c.Log.Debug("Attempting to establish watch connection", "index", c.index.ID, "value", c.index.Value)
			}

			_, err := c.esc.WatchIndex(ctx, c.index, stream.Callback(func(op *entityserver_v1alpha.EntityOp) error {
				// Log successful reconnection after retry
				if retryCount > 0 {
					c.Log.Info("Watch successfully reconnected", "index", c.index.ID, "value", c.index.Value, "afterAttempts", retryCount)
					retryCount = 0
					retryDelay = time.Second // Reset delay for future disconnects
				}
				var eventType EventType

				switch op.OperationType() {
				case entityserver_v1alpha.EntityOperationCreate:
					eventType = EventAdded
				case entityserver_v1alpha.EntityOperationUpdate:
					eventType = EventUpdated
				case entityserver_v1alpha.EntityOperationDelete:
					eventType = EventDeleted
				default:
					return nil
				}

				ev := Event{
					Type:    eventType,
					Id:      entity.Id(op.EntityId()),
					PrevRev: op.Previous(),
				}

				if op.HasEntity() {
					aen := op.Entity()

					ev.Entity = &entity.Entity{
						ID:        entity.Id(aen.Id()),
						CreatedAt: aen.CreatedAt(),
						UpdatedAt: aen.UpdatedAt(),
						Attrs:     aen.Attrs(),
					}
				}

				select {
				case <-ctx.Done():
					return nil
				case c.workQueue <- ev:
					//ok
				default:
					// Queue is full, log and continue
					c.Log.Warn("Work queue full, dropping watch event", "entity", ev.Id, "eventType", ev.Type, "queueSize", len(c.workQueue))
				}

				return nil
			}))

			if err != nil {
				// Check if context was cancelled
				if ctx.Err() != nil {
					c.Log.Debug("Watch context cancelled, stopping watch")
					return
				}

				retryCount++
				c.Log.Error("Watch disconnected, will retry", "error", err, "retryDelay", retryDelay, "attempt", retryCount)

				// Wait before retrying with exponential backoff
				select {
				case <-ctx.Done():
					return
				case <-time.After(retryDelay):
					// Exponential backoff with max delay
					retryDelay = retryDelay * 2
					if retryDelay > maxRetryDelay {
						retryDelay = maxRetryDelay
					}
				}
			} else {
				// Watch completed normally (shouldn't happen unless context cancelled)
				c.Log.Debug("Watch completed normally")
				return
			}
		}
	}()

	// Start periodic resync
	if c.resyncPeriod > 0 {
		c.wg.Add(1)
		go c.periodicResync(ctx)
	}

	// Start periodic callback if set
	if c.periodic != nil {
		c.wg.Add(1)
		go c.runPeriodic(ctx)
	}

	return nil
}

// Stop stops the controller
func (c *ReconcileController) Stop() {
	c.Log.Info("Stopping controller", "name", c.name)
	if c.cancel != nil {
		c.cancel()
	}
	c.wg.Wait()
	close(c.workQueue)
}

// runWorker processes items from the work queue
func (c *ReconcileController) runWorker(ctx context.Context) {
	id := idgen.Gen("worker")

	c.Log.Info("Starting worker", "id", id)

	defer c.wg.Done()

	ctx = withWorkerId(ctx, id)

	for {
		select {
		case <-ctx.Done():
			c.Log.Info("Stopping worker", "id", id)
			return
		case event, ok := <-c.workQueue:
			if !ok {
				c.Log.Info("Stopping worker", "id", id)
				return
			}

			c.Log.Info("Processing event", "entity", event.Id, "worker", WorkerId(ctx))

			// Process the event
			updates, err := c.processItem(ctx, event)
			if err != nil {
				c.Log.Error("error processing item", "event", event, "error", err)
				// we still try to process updates even if there is an error.
			}

			if len(updates) > 0 {
				if event.Id == "" {
					c.Log.Error("entity id is empty update there are updates", "event", event)
				} else {
					c.Log.Info("updating entity with updates produced by controller", "event", event, "updates", len(updates))

					var rpcE entityserver_v1alpha.Entity
					rpcE.SetId(string(event.Id))
					rpcE.SetAttrs(updates)

					_, err := c.esc.Put(ctx, &rpcE)
					if err != nil {
						c.Log.Error("error updating entity", "entity", event.Id, "error", err)
					} else {
						c.Log.Info("updated entity", "entity", event.Id)
					}
				}
			}
		}
	}
}

// processItem processes a single item from the work queue
func (c *ReconcileController) processItem(ctx context.Context, event Event) ([]entity.Attr, error) {
	// Handle different event types
	switch event.Type {
	case EventAdded, EventUpdated, EventDeleted:
		return c.handler(ctx, event)
	default:
		return nil, fmt.Errorf("unknown event type: %s", event.Type)
	}
}

// periodicResync periodically resyncs all entities
func (c *ReconcileController) periodicResync(ctx context.Context) {
	c.Log.Info("Starting resync")
	defer c.Log.Info("Stopping resync")

	defer c.wg.Done()

	ticker := time.NewTicker(c.resyncPeriod)
	defer ticker.Stop()

	for {
		// List all entities and queue them for processing
		resp, err := c.esc.List(ctx, c.index)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}

			c.Log.Error("error listing entities for resync", "error", err)
			continue
		}

		entities := resp.Values()

		for _, aen := range entities {
			en := &entity.Entity{
				ID:        entity.Id(aen.Id()),
				CreatedAt: aen.CreatedAt(),
				UpdatedAt: aen.UpdatedAt(),
				Attrs:     aen.Attrs(),
			}

			ev := Event{
				Type:   EventUpdated,
				Id:     entity.Id(aen.Id()),
				Entity: en,
			}

			select {
			case <-ctx.Done():
				return
			case c.workQueue <- ev:
				// Event added to queue
			default:
				// Queue is full, log and skip this event
				c.Log.Warn("Work queue full during resync, dropping event", "entity", ev.Id, "queueSize", len(c.workQueue))
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Continue to the next tick
		}
	}
}

// runPeriodic runs the periodic callback every 10 minutes
func (c *ReconcileController) runPeriodic(ctx context.Context) {
	c.Log.Info("Starting periodic callback")
	defer c.Log.Info("Stopping periodic callback")

	defer c.wg.Done()

	dur := c.periodicTime
	if dur == 0 {
		dur = 10 * time.Minute // Default to 10 minutes if not set
	}

	// Run every 10 minutes
	ticker := time.NewTicker(dur)
	defer ticker.Stop()

	// Run once immediately
	if err := c.periodic(ctx); err != nil {
		c.Log.Error("error running periodic callback", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.periodic(ctx); err != nil {
				c.Log.Error("error running periodic callback", "error", err)
			}
		}
	}
}

type ControllerEntity interface {
	Decode(getter entity.AttrGetter)
	Encode() []entity.Attr
}

type GenericController[P ControllerEntity] interface {
	Init(context.Context) error
	Create(ctx context.Context, obj P, meta *entity.Meta) error
	Delete(ctx context.Context, e entity.Id) error
}

// UpdatingController is an optional interface that controllers can implement
// to handle updates differently from creates
type UpdatingController[P ControllerEntity] interface {
	Update(ctx context.Context, obj P, meta *entity.Meta) error
}

func AdaptController[
	T any,
	P interface {
		*T
		ControllerEntity
	},
	C GenericController[P],
](cont C) HandlerFunc {
	return func(ctx context.Context, event Event) ([]entity.Attr, error) {
		switch event.Type {
		case EventAdded, EventUpdated:
			e := event.Entity

			if e == nil {
				return nil, fmt.Errorf("entity not found: %s", event.Id)
			}

			// Decode the entity into the controller entity type
			var obj P = new(T)
			obj.Decode(e)

			orig := e.Clone()

			meta := &entity.Meta{
				Entity:   e,
				Revision: e.Revision,
				Previous: event.PrevRev,
			}

			var err error
			if event.Type == EventUpdated {
				// Check if the controller implements UpdatingController
				if updater, ok := any(cont).(UpdatingController[P]); ok {
					err = updater.Update(ctx, obj, meta)
				} else {
					err = cont.Create(ctx, obj, meta)
				}
			} else {
				err = cont.Create(ctx, obj, meta)
			}

			if err != nil {
				err = fmt.Errorf("failed to process entity: %w", err)
			}

			return entity.Diff(meta.Entity, orig), err

		case EventDeleted:
			if err := cont.Delete(ctx, event.Id); err != nil {
				return nil, fmt.Errorf("failed to create entity: %w", err)
			}
		}

		return nil, nil
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
