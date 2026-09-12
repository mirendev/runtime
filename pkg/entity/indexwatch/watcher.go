// Package indexwatch provides a robust, self-healing abstraction around the
// entity server's WatchIndex API.
//
// WatchIndex streams entity changes (create/update/delete) for an attribute
// based index query, but the underlying watch can stop at any time (etcd
// errors, RPC disconnects, server restarts). Consumers that use WatchIndex
// directly must re-establish the watch on failure, and even when they do, any
// changes that occur while the watch is down are silently lost.
//
// A Watcher closes both gaps while keeping only O(1) state — a single etcd
// revision cursor. It owns one goroutine that:
//
//  1. Takes an initial snapshot via List, delivering it as a single EventSync
//     carrying the complete current set of entities and the revision it was read
//     at. The cursor is set to that revision.
//  2. Watches from cursor+1 (etcd WithRev), forwarding live create/update/delete
//     events and advancing the cursor from each event's revision and from idle
//     progress watermarks.
//  3. On a transient disconnect, re-watches from cursor+1 — etcd replays every
//     put and delete since then, so nothing is missed and no re-list is needed.
//  4. Only if the cursor has been compacted away does it take a fresh snapshot
//     (another EventSync) and reset the cursor.
//
// Because the watcher holds no per-entity state, deletes that happen during a
// snapshot gap (initial sync or post-compaction) are not emitted as individual
// EventDeleted events. Instead an EventSync hands the consumer the full current
// set, and the consumer reconciles it against its own state in its own business
// domain (replace its cache; treat any id no longer present as removed) — work
// these consumers already do today. Live deletes (the common case) arrive as
// ordinary EventDeleted events.
package indexwatch

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc/stream"
)

// EventType describes the kind of change an Event represents.
type EventType int

const (
	// EventSync delivers a full snapshot of the index: Entities holds the
	// complete current set and Rev is the revision it was read at. Emitted on
	// initial sync and again after a compaction or resync. Consumers reconcile
	// their own state against Entities (replace their cache; treat any id no
	// longer present as removed).
	EventSync EventType = iota
	// EventAdded indicates a live create.
	EventAdded
	// EventUpdated indicates a watched entity changed (live).
	EventUpdated
	// EventDeleted indicates an entity left the watched index (live). Entity is
	// the last available value when the server can recover it; rely on Id because
	// the value may be unavailable.
	EventDeleted
)

// String renders the EventType for logging.
func (t EventType) String() string {
	switch t {
	case EventSync:
		return "sync"
	case EventAdded:
		return "added"
	case EventUpdated:
		return "updated"
	case EventDeleted:
		return "deleted"
	default:
		return "unknown"
	}
}

// Event is a single change delivered on the Updates channel.
type Event struct {
	// Type is the kind of change.
	Type EventType
	// Id is the entity's id. Set for Added/Updated/Deleted; empty for EventSync.
	Id entity.Id
	// Entity is the full entity for Added/Updated events and, when available, the
	// last value for Deleted events. It is nil for EventSync.
	Entity *entity.Entity
	// Entities is the complete current set for EventSync; nil for live events.
	Entities []*entity.Entity
	// Rev is the entity's revision for Added/Updated/Deleted, and the snapshot
	// revision for EventSync.
	Rev int64
}

// Options configures a Watcher. The zero value is usable; New applies defaults
// for any unset field.
type Options struct {
	// Logger receives operational logs. Defaults to slog.Default().
	Logger *slog.Logger

	// ResyncPeriod, when > 0, forces a periodic fresh snapshot even while the
	// watch is healthy, as belt-and-suspenders drift correction. Defaults to 0
	// (disabled); revision-resume makes it unnecessary in normal operation.
	ResyncPeriod time.Duration

	// MinBackoff is the initial delay between reconnect attempts. Defaults to
	// 1 second.
	MinBackoff time.Duration

	// MaxBackoff caps the exponential reconnect delay. Defaults to 5 minutes.
	MaxBackoff time.Duration

	// BufferSize is the capacity of the Updates channel. When the buffer is
	// full the watch goroutine blocks (applying backpressure to the entity
	// server) rather than dropping events, so no change is ever lost. Defaults
	// to 1000.
	BufferSize int
}

const (
	defaultMinBackoff = time.Second
	defaultMaxBackoff = 5 * time.Minute
	defaultBufferSize = 1000
)

func (o *Options) applyDefaults() {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.MinBackoff <= 0 {
		o.MinBackoff = defaultMinBackoff
	}
	if o.MaxBackoff <= 0 {
		o.MaxBackoff = defaultMaxBackoff
	}
	if o.MaxBackoff < o.MinBackoff {
		o.MaxBackoff = o.MinBackoff
	}
	if o.BufferSize <= 0 {
		o.BufferSize = defaultBufferSize
	}
}

// Watcher delivers gap-free change events for a single attribute based index
// query while holding only a revision cursor. Create one with New, then call
// Start. Consume events from Updates and optionally wait for the first snapshot
// via Synced. Call Stop to shut down.
type Watcher struct {
	esc   *entityserver_v1alpha.EntityAccessClient
	index entity.Attr
	opts  Options
	log   *slog.Logger

	updates chan Event
	synced  chan struct{}

	// cursor is the last etcd revision the watcher has fully observed. Owned
	// exclusively by the watch goroutine; no locking required.
	cursor int64

	// mu guards the updates channel against the send/close race. Events reach
	// updates from two unsynchronized senders: the run goroutine (snapshots) and
	// the RPC dispatch goroutine that invokes our WatchIndex callback. The latter
	// can still be mid-send when run returns and closes the channel, so both
	// send and close take mu and consult closed.
	mu     sync.Mutex
	closed bool

	startOnce sync.Once
	stopOnce  sync.Once
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	syncOnce  sync.Once
}

// New creates a Watcher for the given index query against the entity server.
// It does not start watching until Start is called.
func New(esc *entityserver_v1alpha.EntityAccessClient, index entity.Attr, opts Options) *Watcher {
	opts.applyDefaults()

	return &Watcher{
		esc:     esc,
		index:   index,
		opts:    opts,
		log:     opts.Logger.With("module", "indexwatch", "index", index.ID),
		updates: make(chan Event, opts.BufferSize),
		synced:  make(chan struct{}),
	}
}

// Updates returns the channel on which change events are delivered. The channel
// is closed after Stop completes and the watch goroutine has exited.
func (w *Watcher) Updates() <-chan Event {
	return w.updates
}

// Synced returns a channel that is closed once the first snapshot has been
// delivered (the first EventSync). Consumers that build an in-memory cache from
// the stream can wait on it to know when their cache reflects the current state
// of the index.
func (w *Watcher) Synced() <-chan struct{} {
	return w.synced
}

// Start launches the background watch goroutine. It is safe to call once; later
// calls are no-ops. The watcher runs until ctx is cancelled or Stop is called.
func (w *Watcher) Start(ctx context.Context) error {
	w.startOnce.Do(func() {
		runCtx, cancel := context.WithCancel(ctx)
		w.cancel = cancel

		w.wg.Go(func() {
			defer w.closeUpdates()
			w.run(runCtx)
		})
	})

	return nil
}

// Stop cancels the watch goroutine and waits for it to exit. It is safe to call
// multiple times and from multiple goroutines. After Stop returns, the Updates
// channel is closed.
func (w *Watcher) Stop() {
	w.stopOnce.Do(func() {
		if w.cancel != nil {
			w.cancel()
		}
		w.wg.Wait()
	})
}

// markSynced closes the synced channel exactly once.
func (w *Watcher) markSynced() {
	w.syncOnce.Do(func() {
		close(w.synced)
	})
}

// decodeEntity converts an entity server wire entity into an entity.Entity,
// restoring the timestamps and revision.
func decodeEntity(aen *entityserver_v1alpha.Entity) *entity.Entity {
	en := entity.New(aen.Attrs())
	en.SetCreatedAt(time.UnixMilli(aen.CreatedAt()))
	en.SetUpdatedAt(time.UnixMilli(aen.UpdatedAt()))
	en.SetRevision(aen.Revision())
	return en
}

// send delivers an event on the Updates channel, blocking until there is room
// (backpressure) or the context is cancelled. It reports whether the event was
// delivered; a false return means the watcher is shutting down.
//
// It holds mu across the send so a late callback delivery from the RPC dispatch
// goroutine can never race closeUpdates onto a closed channel. A plain
// select-with-ctx.Done guard is not enough on its own: once updates is closed,
// the send case is permanently ready (it panics rather than blocks) and select
// picks randomly among ready cases, so it would still hit the closed channel
// roughly half the time even with ctx already cancelled.
func (w *Watcher) send(ctx context.Context, ev Event) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return false
	}
	select {
	case <-ctx.Done():
		return false
	case w.updates <- ev:
		return true
	}
}

// closeUpdates closes the Updates channel exactly once, serialized with send via
// mu so an in-flight send can never observe the channel mid-close. Called from
// the run goroutine's deferred teardown after run returns.
func (w *Watcher) closeUpdates() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	close(w.updates)
}

// run is the main loop. It maintains the revision cursor, snapshots when needed
// (initially and after a compaction or resync), and otherwise resumes the watch
// from the cursor on every reconnect.
func (w *Watcher) run(ctx context.Context) {
	w.log.Info("starting index watch", "value", w.index.Value)
	defer w.log.Info("index watch stopped")

	backoff := w.opts.MinBackoff
	needSnapshot := true

	for {
		if ctx.Err() != nil {
			return
		}

		if needSnapshot {
			rev, err := w.snapshot(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				w.log.Error("snapshot failed, will retry", "error", err, "backoff", backoff)
				if !w.sleep(ctx, &backoff) {
					return
				}
				continue
			}
			w.cursor = rev
			needSnapshot = false
			w.markSynced()
			backoff = w.opts.MinBackoff
		}

		resnapshot, err := w.watch(ctx)
		if ctx.Err() != nil {
			return
		}
		if resnapshot {
			// Compaction or periodic resync: take a fresh snapshot, no backoff.
			needSnapshot = true
			backoff = w.opts.MinBackoff
			continue
		}
		// A transient error or a clean stream end both drop us here; back off
		// before resuming either way so an unexpected run of clean ends can't
		// become a tight reconnect loop.
		if err != nil {
			w.log.Error("watch disconnected, will resume", "error", err, "cursor", w.cursor, "backoff", backoff)
		} else {
			w.log.Info("watch ended cleanly, will resume", "cursor", w.cursor, "backoff", backoff)
		}
		if !w.sleep(ctx, &backoff) {
			return
		}
	}
}

// snapshot lists the index and delivers it as a single EventSync carrying the
// complete current set. It returns the revision the snapshot was read at.
func (w *Watcher) snapshot(ctx context.Context) (int64, error) {
	resp, err := w.esc.List(ctx, w.index)
	if err != nil {
		return 0, err
	}

	vals := resp.Values()
	entities := make([]*entity.Entity, 0, len(vals))
	for _, aen := range vals {
		entities = append(entities, decodeEntity(aen))
	}

	rev := resp.Revision()
	if !w.send(ctx, Event{Type: EventSync, Entities: entities, Rev: rev}) {
		return 0, ctx.Err()
	}

	return rev, nil
}

// watch establishes a single WatchIndex stream resuming from cursor+1 and
// forwards live events until it ends. It returns resnapshot=true when the caller
// should take a fresh snapshot (compaction, or the resync timer firing), and an
// error for a transient failure the caller should resume from after backoff.
func (w *Watcher) watch(ctx context.Context) (resnapshot bool, err error) {
	watchCtx := ctx
	if w.opts.ResyncPeriod > 0 {
		var cancel context.CancelFunc
		watchCtx, cancel = context.WithTimeout(ctx, w.opts.ResyncPeriod)
		defer cancel()
	}

	var compacted bool

	_, werr := w.esc.WatchIndex(watchCtx, w.index, w.cursor+1, stream.Callback(func(op *entityserver_v1alpha.EntityOp) error {
		switch op.OperationType() {
		case entityserver_v1alpha.EntityOperationCompacted:
			// Cursor too old; end the watch and re-snapshot.
			compacted = true
			return nil
		case entityserver_v1alpha.EntityOperationProgress:
			if op.Revision() > w.cursor {
				w.cursor = op.Revision()
			}
			return nil
		case entityserver_v1alpha.EntityOperationCreate, entityserver_v1alpha.EntityOperationUpdate, entityserver_v1alpha.EntityOperationDelete:
			// Entity mutations; converted to events by eventFromOp below.
		}

		ev, ok := w.eventFromOp(op)
		if !ok {
			return nil
		}
		if !w.send(ctx, ev) {
			return ctx.Err()
		}
		return nil
	}))

	if compacted {
		return true, nil
	}

	// Resync timer fired while the watcher is still running: force a fresh
	// snapshot rather than a plain resume.
	if w.opts.ResyncPeriod > 0 && ctx.Err() == nil && watchCtx.Err() != nil {
		return true, nil
	}

	return false, werr
}

// eventFromOp converts a live watch operation into an Event and advances the
// cursor. It reports whether the op produced a deliverable event (unknown
// operation types are ignored).
func (w *Watcher) eventFromOp(op *entityserver_v1alpha.EntityOp) (Event, bool) {
	var typ EventType

	switch op.OperationType() {
	case entityserver_v1alpha.EntityOperationCreate:
		typ = EventAdded
	case entityserver_v1alpha.EntityOperationUpdate:
		typ = EventUpdated
	case entityserver_v1alpha.EntityOperationDelete:
		typ = EventDeleted
	case entityserver_v1alpha.EntityOperationProgress, entityserver_v1alpha.EntityOperationCompacted:
		// Not entity mutations; no deliverable event.
		fallthrough
	default:
		return Event{}, false
	}

	rev := op.Revision()
	if rev > w.cursor {
		w.cursor = rev
	}

	var en *entity.Entity
	if op.HasEntity() {
		en = decodeEntity(op.Entity())
	}

	return Event{Type: typ, Id: entity.Id(op.EntityId()), Entity: en, Rev: rev}, true
}

// sleep waits for the current backoff duration, then doubles it up to MaxBackoff
// for the next attempt. It returns false if the context was cancelled while
// waiting.
func (w *Watcher) sleep(ctx context.Context, backoff *time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(*backoff):
		next := min(*backoff*2, w.opts.MaxBackoff)
		*backoff = next
		return true
	}
}
