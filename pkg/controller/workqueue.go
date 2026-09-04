package controller

import (
	"context"
	"math/rand/v2"
	"sync"
	"time"

	"miren.dev/runtime/pkg/entity"
)

type workPriority uint8

const (
	workRepair workPriority = iota
	workUrgent
)

type enqueueResult uint8

const (
	enqueueQueued enqueueResult = iota
	enqueueCoalesced
	enqueueDropped
)

type workSignal struct {
	id        entity.Id
	priority  workPriority
	present   bool
	created   bool
	tombstone *entity.Entity
	queuedAt  time.Time
}

type workItem struct {
	id        entity.Id
	present   bool
	created   bool
	tombstone *entity.Entity
}

type workToken struct {
	id       entity.Id
	entry    *workEntry
	priority workPriority
}

type workEntry struct {
	priority workPriority
	queued   bool
	inFlight bool
	dirty    bool
	retrying bool

	present   bool
	created   bool
	tombstone *entity.Entity
	queuedAt  time.Time
	dirtyAt   time.Time
	attempts  int
	timer     *time.Timer
}

type workQueueStats struct {
	depth     int
	oldestAge time.Duration
}

// dirtyQueue is a keyed, two-lane work queue. An entity has one entry across
// queued, in-flight, and retrying states. Urgent watch/manual work always runs
// before anti-entropy and retry work.
type dirtyQueue struct {
	mu      sync.Mutex
	entries map[entity.Id]*workEntry
	urgent  []workToken
	repair  []workToken
	wake    chan struct{}
	closed  bool
	now     func() time.Time
	backoff func(int) time.Duration
}

func newDirtyQueue() *dirtyQueue {
	return &dirtyQueue{
		entries: make(map[entity.Id]*workEntry),
		wake:    make(chan struct{}, 1),
		now:     time.Now,
		backoff: retryDelay,
	}
}

// Add marks a key dirty and reports whether it queued, coalesced, or dropped
// the signal.
func (q *dirtyQueue) Add(signal workSignal) enqueueResult {
	if signal.id == "" {
		return enqueueDropped
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return enqueueDropped
	}

	now := signal.queuedAt
	if now.IsZero() {
		now = q.now()
	}
	entry, ok := q.entries[signal.id]
	if !ok {
		entry = &workEntry{
			priority:  signal.priority,
			queued:    true,
			present:   signal.present,
			created:   signal.created,
			tombstone: signal.tombstone,
			queuedAt:  now,
		}
		q.entries[signal.id] = entry
		q.appendLocked(signal.id, signal.priority)
		q.notifyLocked()
		return enqueueQueued
	}

	// Later observations replace the lifecycle hint but never replace a live
	// entity snapshot, because the worker will read that from the store.
	entry.present = signal.present
	if signal.created {
		entry.created = true
	}
	if signal.present {
		entry.tombstone = nil
	} else if signal.tombstone != nil {
		entry.tombstone = signal.tombstone
	}

	if entry.retrying && signal.priority == workUrgent {
		if entry.timer != nil {
			entry.timer.Stop()
		}
		entry.retrying = false
		entry.attempts = 0
		entry.priority = workUrgent
		entry.queued = true
		entry.queuedAt = now
		q.appendLocked(signal.id, workUrgent)
		q.notifyLocked()
		return enqueueCoalesced
	}

	if entry.inFlight {
		if !entry.dirty {
			entry.dirtyAt = now
			entry.priority = signal.priority
		} else if signal.priority > entry.priority {
			entry.priority = signal.priority
		}
		entry.dirty = true
		if signal.priority == workUrgent {
			entry.attempts = 0
		}
		return enqueueCoalesced
	}

	if entry.queued && signal.priority > entry.priority {
		// Promotion leaves a stale token in the repair lane. popLocked validates
		// tokens against the entry's current lane before returning them.
		entry.priority = signal.priority
		q.appendLocked(signal.id, signal.priority)
		q.notifyLocked()
	}
	return enqueueCoalesced
}

func (q *dirtyQueue) Get(ctx context.Context) (workItem, bool) {
	for {
		q.mu.Lock()
		if q.closed {
			q.mu.Unlock()
			return workItem{}, false
		}
		if id, ok := q.popLocked(workUrgent); ok {
			item := q.startLocked(id)
			q.notifyLocked()
			q.mu.Unlock()
			return item, true
		}
		if id, ok := q.popLocked(workRepair); ok {
			item := q.startLocked(id)
			q.notifyLocked()
			q.mu.Unlock()
			return item, true
		}
		q.mu.Unlock()

		select {
		case <-ctx.Done():
			return workItem{}, false
		case <-q.wake:
		}
	}
}

func (q *dirtyQueue) Done(item workItem, processErr error) (retrying bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	entry := q.entries[item.id]
	if entry == nil || q.closed {
		return false
	}
	entry.inFlight = false

	if processErr == nil {
		entry.attempts = 0
		if !entry.dirty {
			delete(q.entries, item.id)
			return false
		}
		entry.dirty = false
		entry.queued = true
		entry.queuedAt = entry.dirtyAt
		entry.dirtyAt = time.Time{}
		q.appendLocked(item.id, entry.priority)
		q.notifyLocked()
		return false
	}

	// A failed create/delete still needs the same lifecycle callback on retry.
	entry.created = entry.created || item.created
	if !item.present && !entry.present && entry.tombstone == nil {
		entry.tombstone = item.tombstone
	}
	if entry.dirty {
		entry.dirty = false
		entry.queued = true
		entry.queuedAt = entry.dirtyAt
		entry.dirtyAt = time.Time{}
		q.appendLocked(item.id, entry.priority)
		q.notifyLocked()
		return false
	}

	entry.attempts++
	delay := q.backoff(entry.attempts)
	entry.retrying = true
	entry.priority = workRepair
	entry.timer = time.AfterFunc(delay, func() { q.retry(item.id, entry) })
	return true
}

func retryDelay(attempt int) time.Duration {
	ceiling := retryBackoff(attempt)
	floor := ceiling / 2
	return floor + time.Duration(rand.Int64N(int64(ceiling-floor)+1))
}

func retryBackoff(attempt int) time.Duration {
	delay := time.Second
	for range attempt - 1 {
		delay = min(delay*2, time.Minute)
	}
	return delay
}

func (q *dirtyQueue) retry(id entity.Id, expected *workEntry) {
	q.mu.Lock()
	defer q.mu.Unlock()
	entry := q.entries[id]
	if q.closed || entry != expected || !entry.retrying {
		return
	}
	entry.retrying = false
	entry.queued = true
	entry.queuedAt = q.now()
	q.appendLocked(id, workRepair)
	q.notifyLocked()
}

func (q *dirtyQueue) Stats() workQueueStats {
	q.mu.Lock()
	defer q.mu.Unlock()
	now := q.now()
	stats := workQueueStats{}
	for _, entry := range q.entries {
		if entry.inFlight && !entry.dirty {
			continue
		}
		stats.depth++
		queuedAt := entry.queuedAt
		if entry.dirty && !entry.dirtyAt.IsZero() {
			queuedAt = entry.dirtyAt
		}
		if age := now.Sub(queuedAt); age > stats.oldestAge {
			stats.oldestAge = age
		}
	}
	return stats
}

func (q *dirtyQueue) Close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	q.closed = true
	for _, entry := range q.entries {
		if entry.timer != nil {
			entry.timer.Stop()
		}
	}
	close(q.wake)
}

func (q *dirtyQueue) startLocked(id entity.Id) workItem {
	entry := q.entries[id]
	entry.queued = false
	entry.inFlight = true
	item := workItem{
		id:        id,
		present:   entry.present,
		created:   entry.created,
		tombstone: entry.tombstone,
	}
	entry.created = false
	entry.tombstone = nil
	entry.dirty = false
	entry.dirtyAt = time.Time{}
	return item
}

func (q *dirtyQueue) popLocked(priority workPriority) (entity.Id, bool) {
	lane := &q.repair
	if priority == workUrgent {
		lane = &q.urgent
	}
	for len(*lane) > 0 {
		token := (*lane)[0]
		*lane = (*lane)[1:]
		entry := q.entries[token.id]
		if entry == token.entry && entry.queued && entry.priority == token.priority {
			return token.id, true
		}
	}
	return "", false
}

func (q *dirtyQueue) appendLocked(id entity.Id, priority workPriority) {
	token := workToken{id: id, entry: q.entries[id], priority: priority}
	if priority == workUrgent {
		q.urgent = append(q.urgent, token)
	} else {
		q.repair = append(q.repair, token)
	}
}

func (q *dirtyQueue) notifyLocked() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}
