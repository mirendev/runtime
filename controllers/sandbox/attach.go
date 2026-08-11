package sandbox

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"time"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/pkg/entity"
)

// subscriberQueue bounds how much output a single attached client may fall
// behind before it starts losing data.
//
// The bound exists because the alternative is worse. Hub.Write runs on
// containerd's stdout copy goroutine, so blocking there blocks the container's
// stdout — a stalled or half-dead client would freeze the workload it is
// watching. Dropping is safe here because the log stream, not the attached
// terminal, is the durable record: everything a dropped client missed is still
// readable with `miren logs`.
const subscriberQueue = 256

// replayBuffer bounds the recent output a Hub keeps for clients that have not
// attached yet.
//
// Without it, output written before a client subscribes is gone: the container
// starts as soon as its sandbox boots, while the client is still resolving the
// sandbox and opening a connection. A command that finishes in that window --
// `miren app run -- echo hi`, or any task that fails immediately -- would print
// nothing at all, which is what running a command interactively is for. The old
// exec path could not lose this, because it started the process with the
// client's streams already connected.
//
// Bounded because a Hub lives as long as its container and a detached run can
// produce output for hours. What a late client gets is the recent tail; the log
// stream remains the complete record.
const replayBuffer = 64 << 10

// Resizer is the part of containerd's task API a Hub needs to propagate window
// size changes. Narrowed to one method so tests don't need a real task.
type Resizer interface {
	Resize(ctx context.Context, w, h uint32) error
}

// Hub fans a container's output out to zero or more attached clients and
// funnels their input back in.
//
// It exists so that attaching to a running container is a subscription rather
// than a new process. Executing a second process to serve an attach — which is
// what `miren app run` did before — means the attached command dies with the
// client that started it, and its exit code dies with it. A Hub decouples the
// two: the command is the container's own primary process, clients come and go
// around it, and nothing about their comings and goings reaches the workload.
//
// The zero value is not usable; call NewHub.
type Hub struct {
	mu     sync.Mutex
	subs   map[uint64]*subscriber
	nextID uint64
	closed bool

	// done is closed when the container this Hub fronts is gone. It is the
	// only signal an attached client has that the thing it is watching has
	// ended: the client's own stdin says nothing about the workload, and
	// treating stdin's EOF as the end reports a run still going as finished.
	done chan struct{}

	// history is the recent output replayed to a client that attaches after the
	// container has already written something.
	history []byte

	stdinR *io.PipeReader
	stdinW *io.PipeWriter

	resizer atomic.Pointer[Resizer]
}

type subscriber struct {
	ch      chan []byte
	done    chan struct{}
	dropped atomic.Uint64

	// closeOnce guards done, which both Close and the subscriber's own
	// unsubscribe need to close. They do not otherwise coordinate: teardown can
	// close a Hub while a client is still attached, and that client's deferred
	// unsubscribe then runs afterwards. Without a shared guard the second close
	// panics and takes the runner process with it.
	closeOnce sync.Once
}

// stop releases the subscriber's goroutine. Safe to call from either path, and
// safe to call repeatedly.
func (s *subscriber) stop() {
	s.closeOnce.Do(func() { close(s.done) })
}

func NewHub() *Hub {
	r, w := io.Pipe()
	return &Hub{
		subs:   make(map[uint64]*subscriber),
		done:   make(chan struct{}),
		stdinR: r,
		stdinW: w,
	}
}

// Write fans one chunk of container output out to every attached client.
//
// It is an io.Writer so it can sit in the same cio.WithStreams call as the log
// consumer. It never returns an error: it shares an io.MultiWriter with the log
// consumer, and a MultiWriter abandons the remaining writers on the first
// error, so a failing Hub would silently stop the container's output reaching
// the logs.
func (h *Hub) Write(p []byte) (int, error) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return len(p), nil
	}

	// containerd copies through a pooled buffer it reuses immediately, so the
	// chunk has to be copied before it reaches another goroutine.
	chunk := make([]byte, len(p))
	copy(chunk, p)

	h.remember(chunk)

	if len(h.subs) == 0 {
		h.mu.Unlock()
		return len(p), nil
	}

	for _, s := range h.subs {
		select {
		case s.ch <- chunk:
		default:
			s.dropped.Add(1)
		}
	}
	h.mu.Unlock()

	return len(p), nil
}

// remember appends a chunk to the replay buffer, dropping the oldest bytes once
// it is full. Caller holds h.mu.
func (h *Hub) remember(chunk []byte) {
	if len(chunk) >= replayBuffer {
		// A single chunk larger than the whole buffer: keep only its tail, so
		// what a client replays is still the most recent output.
		h.history = append(h.history[:0], chunk[len(chunk)-replayBuffer:]...)
		return
	}

	if len(h.history)+len(chunk) > replayBuffer {
		drop := len(h.history) + len(chunk) - replayBuffer
		h.history = append(h.history[:0], h.history[drop:]...)
	}
	h.history = append(h.history, chunk...)
}

// Subscribe delivers container output to w until the returned function is
// called. Multiple clients may subscribe at once.
//
// A new subscriber first receives the recent output the container has already
// produced, then everything written from here on. The replay is what makes a
// command that finishes before the client connects still print something; the
// snapshot and the registration happen under one lock, so no chunk is delivered
// twice or skipped between the two.
//
// The returned function is idempotent and must be called to release the
// subscriber's goroutine.
func (h *Hub) Subscribe(w io.Writer) (unsubscribe func()) {
	s := &subscriber{
		ch:   make(chan []byte, subscriberQueue),
		done: make(chan struct{}),
	}

	h.mu.Lock()
	if h.closed {
		// Still replay: a container that has already exited is the case where
		// the output matters most, and there is nothing else left to read it
		// from at a terminal.
		history := make([]byte, len(h.history))
		copy(history, h.history)
		h.mu.Unlock()

		if len(history) > 0 {
			_, _ = w.Write(history)
		}
		return func() {}
	}
	id := h.nextID
	h.nextID++
	h.subs[id] = s

	history := make([]byte, len(h.history))
	copy(history, h.history)
	h.mu.Unlock()

	go func() {
		if len(history) > 0 {
			_, _ = w.Write(history)
		}
		for {
			select {
			case <-s.done:
				return
			case chunk := <-s.ch:
				// A write failure means this client is gone. Keep draining
				// rather than returning, so Hub.Write's non-blocking send
				// continues to succeed until the caller unsubscribes.
				_, _ = w.Write(chunk)
			}
		}
	}()

	return func() {
		h.mu.Lock()
		delete(h.subs, id)
		h.mu.Unlock()
		s.stop()
	}
}

// Stdin returns the reader handed to containerd as the container's stdin.
//
// It must never reach EOF while the container is alive. containerd's copyIO
// closes the container's stdin FIFO as soon as this reader returns EOF, which
// for an interactive shell means the shell exits. That is precisely the
// behavior a Hub exists to prevent, so detaching a client never closes this
// pipe — only Close does, at teardown.
func (h *Hub) Stdin() io.Reader { return h.stdinR }

// WriteStdin forwards input from an attached client to the container.
//
// It blocks until the container reads, which is the correct backpressure: it is
// the calling client's own goroutine that waits, and a client typing faster
// than the workload reads should be slowed rather than have its keystrokes
// dropped.
func (h *Hub) WriteStdin(p []byte) (int, error) {
	return h.stdinW.Write(p)
}

// SetResizer records the task whose terminal should follow attached clients'
// window sizes. It is called once the task exists, which is after the Hub is
// built and handed to containerd.
func (h *Hub) SetResizer(r Resizer) {
	if r == nil {
		return
	}
	h.resizer.Store(&r)
}

// Resize propagates a window size change to the container's terminal. It is a
// no-op before the task exists or on a container without a TTY.
func (h *Hub) Resize(ctx context.Context, w, height uint32) error {
	r := h.resizer.Load()
	if r == nil {
		return nil
	}
	return (*r).Resize(ctx, w, height)
}

// Dropped reports how many output chunks were discarded because attached
// clients could not keep up. Non-zero means some client saw a gap; the log
// stream still has everything.
func (h *Hub) Dropped() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()

	var total uint64
	for _, s := range h.subs {
		total += s.dropped.Load()
	}
	return total
}

// Done is closed once the container is gone.
//
// An attached client selects on this to learn that the workload ended. Without
// it the only event an attach can see is its own stdin reaching EOF, which is
// unrelated: stdin is /dev/null in any non-interactive caller, so an attach
// keyed on it returns immediately and reports a still-running task as finished.
func (h *Hub) Done() <-chan struct{} { return h.done }

// Close releases the Hub and closes the container's stdin. Only teardown may
// call it: closing stdin ends an interactive shell, so a client disconnecting
// must not.
func (h *Hub) Close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	close(h.done)
	subs := make([]*subscriber, 0, len(h.subs))
	for id, s := range h.subs {
		subs = append(subs, s)
		delete(h.subs, id)
	}
	h.mu.Unlock()

	for _, s := range subs {
		s.stop()
	}

	_ = h.stdinW.Close()
	_ = h.stdinR.Close()
}

// containerIsAttachable reports whether a sandbox's spec asked for the named
// container to keep stdin open. It is the same signal BootContainers used when
// the task was created, which is what matters on reattach: containerd can only
// reopen a stdin FIFO that was created in the first place.
func containerIsAttachable(sb *compute.Sandbox, containerName string) bool {
	for _, c := range sb.Spec.Container {
		if c.Name == containerName {
			return c.Stdin
		}
	}
	return false
}

// hubLinger is how long a torn-down Hub stays reachable.
//
// A short command races its own teardown. The container exits, the run
// controller observes it and stops the sandbox, and the Hub goes with it --
// possibly before the client that asked for the command has finished
// connecting. Dropping the Hub at that moment means the client finds nothing to
// attach to and prints no output at all, so `miren app run -- echo hi` says
// nothing about half the time, depending on which side wins.
//
// Lingering makes that deterministic instead: the Hub is closed, so an attach
// returns immediately, but its replay buffer is still there to hand over. The
// cost is bounded -- a closed Hub holds at most replayBuffer bytes and is swept
// on the next registry operation after it expires.
const hubLinger = 60 * time.Second

// HubRegistry holds the live Hubs for a runner's containers, so the exec server
// can find the Hub for a sandbox the sandbox controller booted. Both live in
// the runner process.
type HubRegistry struct {
	mu   sync.Mutex
	hubs map[string]*Hub

	// expiring holds Hubs whose containers are gone, kept briefly so a client
	// that arrives just after teardown still gets the output.
	expiring map[string]expiringHub

	// now is overridable so tests can advance the clock rather than sleep.
	now func() time.Time
}

type expiringHub struct {
	hub *Hub
	at  time.Time
}

func NewHubRegistry() *HubRegistry {
	return &HubRegistry{
		hubs:     make(map[string]*Hub),
		expiring: make(map[string]expiringHub),
		now:      time.Now,
	}
}

// retire closes a Hub and holds it for the linger window. Caller holds r.mu.
func (r *HubRegistry) retire(key string, h *Hub) {
	h.Close()
	r.expiring[key] = expiringHub{hub: h, at: r.now().Add(hubLinger)}
}

// sweepExpired drops lingering Hubs whose window has passed. Caller holds r.mu.
func (r *HubRegistry) sweepExpired() {
	now := r.now()
	for key, e := range r.expiring {
		if now.After(e.at) {
			delete(r.expiring, key)
		}
	}
}

func hubKey(sandboxID entity.Id, container string) string {
	return sandboxID.String() + "/" + container
}

// GetOrCreate returns the Hub for a container, creating it if the container is
// attachable. Callers hold the result for the life of the container.
func (r *HubRegistry) GetOrCreate(sandboxID entity.Id, container string) *Hub {
	if r == nil {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.sweepExpired()

	key := hubKey(sandboxID, container)
	if h, ok := r.hubs[key]; ok {
		return h
	}

	// A sandbox name is derived from the run and attempt, so a retired Hub
	// under the same key belongs to an earlier life of this container and must
	// not be revived: its buffer holds the previous attempt's output.
	delete(r.expiring, key)

	h := NewHub()
	r.hubs[key] = h
	return h
}

// Get returns the Hub for a container, or nil if the container is not
// attachable and never ran here.
//
// A Hub whose container has already gone is still returned for a short window,
// so a client that arrives just after teardown gets the output rather than
// nothing. It is closed, so the attach ends as soon as it has replayed.
func (r *HubRegistry) Get(sandboxID entity.Id, container string) *Hub {
	if r == nil {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.sweepExpired()

	key := hubKey(sandboxID, container)
	if h, ok := r.hubs[key]; ok {
		return h
	}
	if e, ok := r.expiring[key]; ok {
		return e.hub
	}
	return nil
}

// Remove tears down a container's Hub, leaving it reachable for the linger
// window.
func (r *HubRegistry) Remove(sandboxID entity.Id, container string) {
	if r == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.sweepExpired()

	key := hubKey(sandboxID, container)
	if h, ok := r.hubs[key]; ok {
		delete(r.hubs, key)
		r.retire(key, h)
	}
}

// RemoveAll tears down every Hub belonging to a sandbox, whatever its
// containers are called. Used when the sandbox itself goes away.
func (r *HubRegistry) RemoveAll(sandboxID entity.Id) {
	if r == nil {
		return
	}

	prefix := sandboxID.String() + "/"

	r.mu.Lock()
	defer r.mu.Unlock()

	r.sweepExpired()

	for key, h := range r.hubs {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			delete(r.hubs, key)
			r.retire(key, h)
		}
	}
}
