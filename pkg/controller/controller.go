package controller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/indexwatch"
	"miren.dev/runtime/pkg/idgen"
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
	queue        *dirtyQueue
	watcher      *indexwatch.Watcher
	wg           sync.WaitGroup

	metricWriter *metrics.VictoriaMetricsWriter
	metricLabels map[string]string
	counters     controllerCounters

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
		queue:        newDirtyQueue(),
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

	c.Log.Info("Starting controller", "name", c.name)

	// Start workers
	for i := 0; i < c.workers; i++ {
		c.wg.Go(func() {
			c.runWorker(ctx)
		})
	}

	c.watcher = indexwatch.New(c.esc, c.index, indexwatch.Options{
		Logger:       c.Log,
		ResyncPeriod: c.resyncPeriod,
	})
	if err := c.watcher.Start(ctx); err != nil {
		cancel()
		return err
	}
	c.wg.Go(func() {
		for event := range c.watcher.Updates() {
			c.acceptWatchEvent(event)
		}
	})

	if c.metricWriter != nil {
		c.wg.Go(func() { c.reportMetrics(ctx) })
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
	if c.watcher != nil {
		c.watcher.Stop()
	}
	c.queue.Close()
	c.wg.Wait()
}

// RecordWrite records a revision that was written by this controller.
// Subsequent watch events for this revision will be skipped to reduce
// unnecessary reconciliation noise from self-generated updates. Calls also
// contribute to the controller's write metric so writes performed outside the
// framework are included in its write velocity.
func (c *ReconcileController) RecordWrite(revision int64) {
	if revision > 0 {
		c.counters.writes.Add(1)
		c.recentWrites.Add(revision)
	}
}

// WriteTracker returns a WriteTracker interface that can be used by controllers
// to record manual entity writes outside the reconciliation framework.
func (c *ReconcileController) WriteTracker() WriteTracker {
	return c
}

// Enqueue marks an entity dirty. The supplied snapshot is ignored for live
// entities; workers always re-read current state. Delete snapshots are retained
// only as tombstones for cleanup handlers that need the removed entity.
func (c *ReconcileController) Enqueue(event Event) {
	c.enqueueSignal(workSignal{
		id:        event.Id,
		priority:  workUrgent,
		present:   event.Type != EventDeleted,
		created:   event.Type == EventAdded,
		tombstone: event.Entity,
	})
}

func (c *ReconcileController) acceptWatchEvent(event indexwatch.Event) {
	switch event.Type {
	case indexwatch.EventSync:
		// A snapshot describes current positive state, not a deletion log. Keeping
		// every previously seen id just to infer missed deletes would make each
		// controller retain its index's full cardinality and still would not cover
		// deletes missed across a process restart.
		for _, en := range event.Entities {
			c.enqueueSignal(workSignal{id: en.Id(), priority: workRepair, present: true})
		}

	case indexwatch.EventAdded, indexwatch.EventUpdated:
		if event.Rev > 0 && c.recentWrites.Contains(event.Rev) {
			return
		}
		c.enqueueSignal(workSignal{
			id:       event.Id,
			priority: workUrgent,
			present:  true,
			created:  event.Type == indexwatch.EventAdded,
		})

	case indexwatch.EventDeleted:
		c.enqueueSignal(workSignal{
			id:        event.Id,
			priority:  workUrgent,
			present:   false,
			tombstone: event.Entity,
		})
	}
}

func (c *ReconcileController) enqueueSignal(signal workSignal) {
	switch c.queue.Add(signal) {
	case enqueueQueued:
	case enqueueCoalesced:
		c.counters.coalesced.Add(1)
	case enqueueDropped:
		c.counters.dropped.Add(1)
		if signal.id != "" {
			c.Log.Warn("dropping reconcile signal after queue closed", "entity", signal.id)
		}
	}
}

// runWorker processes items from the work queue
func (c *ReconcileController) runWorker(ctx context.Context) {
	id := idgen.Gen("worker")

	c.Log.Info("Starting worker", "id", id)

	ctx = withWorkerId(ctx, id)

	for {
		item, ok := c.queue.Get(ctx)
		if !ok {
			c.Log.Info("Stopping worker", "id", id)
			return
		}

		c.counters.inFlight.Add(1)
		event, loadErr := c.currentEvent(ctx, item)
		var handlerErr, writeErr error
		if loadErr == nil {
			current := cloneEntity(event.Entity)
			var updates []entity.Attr
			updates, handlerErr = c.processItem(ctx, event)
			writeErr = c.applyUpdates(ctx, event, current, updates)
		}
		c.counters.inFlight.Add(-1)

		processErr := errors.Join(loadErr, handlerErr, writeErr)
		if processErr != nil && ctx.Err() == nil {
			c.counters.failures.Add(1)
			c.Log.Error("reconcile failed", "entity", item.id, "error", processErr)
		}
		if ctx.Err() != nil {
			processErr = nil
		}
		if retrying := c.queue.Done(item, processErr); retrying {
			c.counters.retries.Add(1)
		}
	}
}

func (c *ReconcileController) currentEvent(ctx context.Context, item workItem) (Event, error) {
	resp, err := c.esc.Get(ctx, item.id.String())
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			return Event{Type: EventDeleted, Id: item.id, Entity: item.tombstone}, nil
		}
		return Event{}, fmt.Errorf("reading current entity %s: %w", item.id, err)
	}

	aen := resp.Entity()
	en := entity.New(aen.Attrs())
	en.SetCreatedAt(time.UnixMilli(aen.CreatedAt()))
	en.SetUpdatedAt(time.UnixMilli(aen.UpdatedAt()))
	en.SetRevision(aen.Revision())

	if !item.present {
		// An index-entry deletion can leave the entity itself alive, notably when
		// a session lease expires. Preserve the index lifecycle signal in that
		// case. A following add/update will dirty the key again with current state.
		tombstone := item.tombstone
		if tombstone == nil {
			tombstone = en
		}
		return Event{Type: EventDeleted, Id: item.id, Entity: tombstone, Rev: aen.Revision()}, nil
	}

	typ := EventUpdated
	if item.created {
		typ = EventAdded
	}
	return Event{Type: typ, Id: item.id, Entity: en, Rev: aen.Revision()}, nil
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

// ProcessEventForTest processes a single event and applies any updates.
// This is exposed for testing controllers without starting the full controller loop.
func (c *ReconcileController) ProcessEventForTest(ctx context.Context, event Event) error {
	current, loadErr := c.currentEvent(ctx, workItem{
		id:        event.Id,
		present:   event.Type != EventDeleted,
		created:   event.Type == EventAdded,
		tombstone: event.Entity,
	})
	if loadErr != nil {
		return loadErr
	}
	before := cloneEntity(current.Entity)
	updates, handlerErr := c.processItem(ctx, current)
	return errors.Join(handlerErr, c.applyUpdates(ctx, current, before, updates))
}

// applyUpdates applies the given updates to an entity using Patch
func (c *ReconcileController) applyUpdates(ctx context.Context, event Event, current *entity.Entity, updates []entity.Attr) error {
	if len(updates) == 0 {
		return nil
	}

	if event.Id == "" {
		return errors.New("entity id is empty but handler returned updates")
	}

	if current != nil {
		updates = entity.Diff(entity.New(updates), current)
		if len(updates) == 0 {
			return nil
		}
	}

	// Debug rather than Info: every controller write passes through here, so at
	// Info this and the confirmation below reported each routine reconcile twice
	// in the operator's stream. Failures still surface at Warn/Error.
	c.Log.Debug("updating entity with updates produced by controller", "event", event, "updates", len(updates))

	// Add entity ID to attrs for Patch
	attrs := append([]entity.Attr{entity.Ref(entity.DBId, event.Id)}, updates...)

	// Use Patch without OCC (revision 0) to avoid breaking unforeseen code that may depend
	// on this working without conflict detection. Can revisit with more holistic pass.
	result, err := c.esc.Patch(ctx, attrs, 0)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			c.Log.Warn("entity not found during update", "entity", event.Id)
			return err
		}
		return fmt.Errorf("updating entity %s: %w", event.Id, err)
	}
	c.Log.Debug("updated entity", "entity", event.Id)
	// Record the revision we just wrote so we can skip the watch event
	if result.HasRevision() {
		c.RecordWrite(result.Revision())
	} else {
		c.counters.writes.Add(1)
	}
	return nil
}

func cloneEntity(en *entity.Entity) *entity.Entity {
	if en == nil {
		return nil
	}
	return en.Clone()
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
	// Delete is called when an entity is deleted. The obj parameter contains the
	// entity data from before deletion (available since delete watch events now
	// include the previous value). It may be nil if the entity data was unavailable
	// (e.g., watch reconnect after etcd compaction).
	Delete(ctx context.Context, id entity.Id, obj P) error
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
			var obj P
			if event.Entity != nil {
				obj = new(T)
				obj.Decode(event.Entity)
			}
			if err := cont.Delete(ctx, event.Id, obj); err != nil {
				return nil, fmt.Errorf("failed to delete entity: %w", err)
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
	metrics     *metrics.VictoriaMetricsWriter
	labels      map[string]string
}

// NewControllerManager creates a new controller manager
func NewControllerManager(opts ...ManagerOption) *ControllerManager {
	m := &ControllerManager{
		controllers: make([]Controller, 0),
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// AddController adds a controller to the manager
func (m *ControllerManager) AddController(controller Controller) {
	if reconcile, ok := controller.(*ReconcileController); ok {
		reconcile.metricWriter = m.metrics
		reconcile.metricLabels = m.labels
	}
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
