package lifecyclesync

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"miren.dev/runtime/pkg/serverlifecycle"
)

const (
	// idleInterval bounds how stale the watcher can be when the directory
	// watch misses something. The executor's own writes wake it immediately.
	idleInterval = 30 * time.Second
	// activeInterval is the poll cadence while an operation is running. Phase
	// changes arrive through the watch; this catches progress rewrites the
	// watch coalesced.
	activeInterval = time.Second
)

// Watcher follows the file ledger and hands each changed record to its
// subscribers, which is how the uplink learns what to tell cloud. The files
// stay the source of truth; the watcher never writes them.
type Watcher struct {
	log   *slog.Logger
	store *serverlifecycle.Store

	kick chan struct{}

	mu   sync.Mutex
	seen map[string]stamp
	subs map[*Subscription]struct{}
}

// stamp is what a record looks like from the outside; a rewrite that changes
// none of it is not a change worth announcing.
type stamp struct {
	phase    serverlifecycle.Phase
	progress string
	err      string
	updated  time.Time
}

func stampOf(op *serverlifecycle.Operation) stamp {
	return stamp{phase: op.Phase, progress: op.Progress, err: op.Error, updated: op.UpdatedAt}
}

// Subscription hands a subscriber every record the watcher sees change. It
// holds the latest state per operation rather than a queue of events, so a
// slow reader never loses a change: two updates to one record before it is
// read collapse into the newer one, and a terminal phase always survives.
type Subscription struct {
	wake chan struct{}

	mu      sync.Mutex
	pending map[string]*serverlifecycle.Operation
	order   []string
}

func newSubscription() *Subscription {
	return &Subscription{wake: make(chan struct{}, 1), pending: make(map[string]*serverlifecycle.Operation)}
}

// Wake is signalled when there is something to Drain.
func (s *Subscription) Wake() <-chan struct{} {
	return s.wake
}

// Drain returns every changed record since the last Drain, oldest change
// first, and clears them.
func (s *Subscription) Drain() []*serverlifecycle.Operation {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*serverlifecycle.Operation, 0, len(s.order))
	for _, id := range s.order {
		out = append(out, s.pending[id])
	}
	s.pending = make(map[string]*serverlifecycle.Operation)
	s.order = s.order[:0]
	return out
}

func (s *Subscription) offer(op *serverlifecycle.Operation) {
	s.mu.Lock()
	if _, queued := s.pending[op.ID]; !queued {
		s.order = append(s.order, op.ID)
	}
	s.pending[op.ID] = op
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func NewWatcher(log *slog.Logger, store *serverlifecycle.Store) *Watcher {
	return &Watcher{
		log:   log,
		store: store,
		kick:  make(chan struct{}, 1),
		seen:  make(map[string]stamp),
		subs:  make(map[*Subscription]struct{}),
	}
}

// Kick asks for a pass now rather than at the next tick, for a caller that
// just wrote a record and wants it announced.
func (m *Watcher) Kick() {
	select {
	case m.kick <- struct{}{}:
	default:
	}
}

// Subscribe returns a subscription that sees every record change from now
// on, and a function to stop it. Delivery never blocks the watcher and never
// drops: the subscription keeps the latest state per record until read.
func (m *Watcher) Subscribe() (*Subscription, func()) {
	sub := newSubscription()
	m.mu.Lock()
	m.subs[sub] = struct{}{}
	m.mu.Unlock()
	return sub, func() {
		m.mu.Lock()
		delete(m.subs, sub)
		m.mu.Unlock()
	}
}

// Run reads the ledger once, so later passes only announce changes, and then
// follows it until ctx ends.
func (m *Watcher) Run(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err == nil {
		if err := watcher.Add(m.store.Dir()); err != nil {
			m.log.Warn("lifecycle ledger watch unavailable; polling only", "dir", m.store.Dir(), "error", err)
			watcher.Close()
			watcher = nil
		} else {
			defer watcher.Close()
		}
	} else {
		m.log.Warn("lifecycle ledger watch unavailable; polling only", "error", err)
		watcher = nil
	}

	var events <-chan fsnotify.Event
	var watchErrors <-chan error
	if watcher != nil {
		events = watcher.Events
		watchErrors = watcher.Errors
	}

	active, count, err := m.sync(ctx)
	if err != nil {
		m.log.Warn("lifecycle ledger read incomplete", "error", err)
	}
	m.log.Info("lifecycle ledger loaded", "operations", count, "active", active)

	timer := time.NewTimer(m.interval(active))
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		case <-m.kick:
		case <-events:
			// The executor writes a temp file and renames it into place, so
			// one logical change is several events. Draining briefly lets the
			// pass see the final file rather than the temp one.
			m.drain(events, 50*time.Millisecond)
		case err := <-watchErrors:
			m.log.Warn("lifecycle ledger watch error", "error", err)
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		active, _, err = m.sync(ctx)
		if err != nil && ctx.Err() == nil {
			m.log.Warn("lifecycle ledger sync failed", "error", err)
		}
		timer.Reset(m.interval(active))
	}
}

func (m *Watcher) interval(active bool) time.Duration {
	if active {
		return activeInterval
	}
	return idleInterval
}

func (m *Watcher) drain(events <-chan fsnotify.Event, window time.Duration) {
	deadline := time.NewTimer(window)
	defer deadline.Stop()
	for {
		select {
		case <-events:
		case <-deadline.C:
			return
		}
	}
}

// sync is one pass over the ledger. It reports whether any operation is still
// running and how many records it saw.
func (m *Watcher) sync(_ context.Context) (active bool, count int, err error) {
	ops, err := m.store.List()
	if err != nil {
		return false, 0, err
	}
	for _, op := range ops {
		if !op.Done() {
			active = true
		}
		st := stampOf(op)
		m.mu.Lock()
		prev, known := m.seen[op.ID]
		if known && prev == st {
			m.mu.Unlock()
			continue
		}
		m.seen[op.ID] = st
		subs := make([]*Subscription, 0, len(m.subs))
		for sub := range m.subs {
			subs = append(subs, sub)
		}
		m.mu.Unlock()
		for _, sub := range subs {
			sub.offer(op)
		}
	}
	return active, len(ops), nil
}
