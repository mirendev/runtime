package controller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/cond"
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

// WriteTracker provides a way to track entity write revisions to skip self-generated watch events.
// Controllers that make manual entity writes outside the reconciliation framework can use this
// to record their writes and avoid unnecessary re-reconciliation.
type WriteTracker interface {
	RecordWrite(revision int64)
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

// inFlightEntry tracks an entity being processed and queues additional events
type inFlightEntry struct {
	revision      int64
	pendingEvents []Event
}

// ReconcileController implements the Controller interface
type ReconcileController struct {
	Log *slog.Logger

	cancel func()
	top    context.Context

	esc          *entityserver_v1alpha.EntityAccessClient
	name         string
	index        entity.Attr
	handler      HandlerFunc
	resyncPeriod time.Duration
	workers      int
	workQueue    chan Event
	wg           sync.WaitGroup

	// In-flight tracking to prevent concurrent processing of the same entity
	// Maps entity ID to an entry containing the revision being processed and pending events
	inFlight   map[entity.Id]*inFlightEntry
	inFlightMu sync.Mutex

	// Recent writes tracking to skip self-generated watch events
	// Controllers record revisions from their writes to reduce reconciliation noise
	recentWrites *RingSet

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
		inFlight:     make(map[entity.Id]*inFlightEntry),
		recentWrites: NewRingSet(1000), // Track last 1000 revisions written by this controller
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
	c.top = top

	c.Log.Info("Starting controller", "name", c.name)

	// Start workers
	for i := 0; i < c.workers; i++ {
		c.wg.Go(func() {
			c.runWorker(ctx)
		})
	}

	// Start event processor
	c.wg.Go(func() {
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

					createdAt := time.UnixMilli(aen.CreatedAt())
					updatedAt := time.UnixMilli(aen.UpdatedAt())

					ev.Entity = entity.New(aen.Attrs())

					ev.Entity.SetCreatedAt(createdAt)
					ev.Entity.SetUpdatedAt(updatedAt)
					ev.Entity.SetRevision(aen.Revision())
					ev.Rev = aen.Revision()
				}

				// Skip watch events for revisions we recently wrote to reduce reconciliation noise
				if ev.Rev > 0 && c.recentWrites.Contains(ev.Rev) {
					return nil
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
	})

	// Start periodic resync
	if c.resyncPeriod > 0 {
		c.wg.Go(func() {
			c.periodicResync(ctx)
		})
	}

	// Start periodic callback if set
	if c.periodic != nil {
		c.wg.Go(func() {
			c.runPeriodic(ctx)
		})
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

// RecordWrite records a revision that was written by this controller.
// Subsequent watch events for this revision will be skipped to reduce
// unnecessary reconciliation noise from self-generated updates.
func (c *ReconcileController) RecordWrite(revision int64) {
	if revision > 0 {
		c.recentWrites.Add(revision)
	}
}

// WriteTracker returns a WriteTracker interface that can be used by controllers
// to record manual entity writes outside the reconciliation framework.
func (c *ReconcileController) WriteTracker() WriteTracker {
	return c
}

// Enqueue adds an event to the work queue for processing
func (c *ReconcileController) Enqueue(event Event) {
	select {
	case <-c.top.Done():
		// Controller is stopping, do not enqueue
		return
	case c.workQueue <- event:
		// Successfully enqueued
	default:
		// Queue is full, log and drop
		c.Log.Warn("Work queue full, dropping enqueued event", "entity", event.Id, "eventType", event.Type, "queueSize", len(c.workQueue))
	}
}

// runWorker processes items from the work queue
func (c *ReconcileController) runWorker(ctx context.Context) {
	id := idgen.Gen("worker")

	c.Log.Info("Starting worker", "id", id)

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

			// Check if this entity is already being processed
			c.inFlightMu.Lock()
			if entry, inFlight := c.inFlight[event.Id]; inFlight {
				// Entity is already in-flight, queue event to the in-flight entry
				entry.pendingEvents = append(entry.pendingEvents, event)
				c.Log.Debug("Entity already in-flight, queuing to pending events",
					"entity", event.Id,
					"worker", WorkerId(ctx),
					"inFlightRev", entry.revision,
					"eventRev", event.Rev,
					"pendingCount", len(entry.pendingEvents))
				c.inFlightMu.Unlock()
				continue
			}

			// Mark entity as in-flight with a new entry
			c.inFlight[event.Id] = &inFlightEntry{
				revision:      event.Rev,
				pendingEvents: nil,
			}
			c.inFlightMu.Unlock()

			c.Log.Info("Processing event", "entity", event.Id, "worker", WorkerId(ctx), "rev", event.Rev)

			// Process the event
			updates, err := c.processItem(ctx, event)
			if err != nil {
				c.Log.Error("error processing item", "event", event, "error", err)
				// we still try to process updates even if there is an error.
			}

			c.applyUpdates(ctx, event, updates)

			// Process any pending events before removing from in-flight
			for {
				c.inFlightMu.Lock()
				entry := c.inFlight[event.Id]
				if entry == nil || len(entry.pendingEvents) == 0 {
					// No pending events, remove from in-flight and break
					delete(c.inFlight, event.Id)
					c.inFlightMu.Unlock()
					break
				}

				// Pop the first pending event
				event = entry.pendingEvents[0]
				entry.pendingEvents = entry.pendingEvents[1:]
				entry.revision = event.Rev
				c.inFlightMu.Unlock()

				c.Log.Info("Processing pending event", "entity", event.Id, "worker", WorkerId(ctx), "rev", event.Rev, "remainingPending", len(entry.pendingEvents))

				// Process the pending event
				updates, err := c.processItem(ctx, event)
				if err != nil {
					c.Log.Error("error processing pending item", "event", event, "error", err)
				}

				c.applyUpdates(ctx, event, updates)
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

// applyUpdates applies the given updates to an entity using Patch
func (c *ReconcileController) applyUpdates(ctx context.Context, event Event, updates []entity.Attr) {
	if len(updates) == 0 {
		return
	}

	if event.Id == "" {
		c.Log.Error("entity id is empty but there are updates", "event", event)
		return
	}

	c.Log.Info("updating entity with updates produced by controller", "event", event, "updates", len(updates))

	// Add entity ID to attrs for Patch
	attrs := append([]entity.Attr{entity.Ref(entity.DBId, event.Id)}, updates...)

	// Use Patch without OCC (revision 0) to avoid breaking unforeseen code that may depend
	// on this working without conflict detection. Can revisit with more holistic pass.
	result, err := c.esc.Patch(ctx, attrs, 0)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			c.Log.Warn("entity not found during update", "entity", event.Id)
			return
		}
		c.Log.Error("error updating entity", "entity", event.Id, "error", err)
	} else {
		c.Log.Info("updated entity", "entity", event.Id)
		// Record the revision we just wrote so we can skip the watch event
		if result.HasRevision() {
			c.RecordWrite(result.Revision())
		}
	}
}

// periodicResync periodically resyncs all entities
func (c *ReconcileController) periodicResync(ctx context.Context) {
	c.Log.Info("Starting resync")
	defer c.Log.Info("Stopping resync")

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
			createdAt := time.UnixMilli(aen.CreatedAt())
			updatedAt := time.UnixMilli(aen.UpdatedAt())

			en := entity.New(aen.Attrs())

			en.SetCreatedAt(createdAt)
			en.SetUpdatedAt(updatedAt)
			en.SetRevision(aen.Revision())

			ev := Event{
				Type:   EventUpdated,
				Id:     entity.Id(aen.Id()),
				Entity: en,
				Rev:    aen.Revision(),
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

// ReconcileControllerI is for controllers that maintain aggregate state
// across multiple entities. Unlike GenericController which maps 1:1 between
// an entity and a resource, ReconcileControllerI handles controllers where
// one entity drives reconciliation of N resources.
type ReconcileControllerI[P ControllerEntity] interface {
	Init(context.Context) error
	Reconcile(ctx context.Context, obj P, meta *entity.Meta) error
}

// DeletingReconcileController is an optional interface that reconcile controllers
// can implement to handle deletion of their managed entities.
type DeletingReconcileController interface {
	Delete(ctx context.Context, e entity.Id) error
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
				Revision: e.GetRevision(),
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

// AdaptReconcileController adapts a ReconcileControllerI into a HandlerFunc.
// It calls Reconcile() for both Add and Update events.
// If the controller implements DeletingReconcileController, Delete() is called for Delete events.
func AdaptReconcileController[
	T any,
	P interface {
		*T
		ControllerEntity
	},
	C ReconcileControllerI[P],
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
				Revision: e.GetRevision(),
				Previous: event.PrevRev,
			}

			err := cont.Reconcile(ctx, obj, meta)
			if err != nil {
				err = fmt.Errorf("failed to reconcile entity: %w", err)
			}

			return entity.Diff(meta.Entity, orig), err

		case EventDeleted:
			// Check if the controller implements DeletingReconcileController
			if deleter, ok := any(cont).(DeletingReconcileController); ok {
				if err := deleter.Delete(ctx, event.Id); err != nil {
					return nil, fmt.Errorf("failed to delete entity: %w", err)
				}
			}
			return nil, nil
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
