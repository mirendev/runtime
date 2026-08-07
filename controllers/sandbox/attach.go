package sandbox

import (
	"context"
	"io"
	"sync"
	"sync/atomic"

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

	stdinR *io.PipeReader
	stdinW *io.PipeWriter

	resizer atomic.Pointer[Resizer]
}

type subscriber struct {
	ch      chan []byte
	done    chan struct{}
	dropped atomic.Uint64
}

func NewHub() *Hub {
	r, w := io.Pipe()
	return &Hub{
		subs:   make(map[uint64]*subscriber),
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
	if h.closed || len(h.subs) == 0 {
		h.mu.Unlock()
		return len(p), nil
	}

	// containerd copies through a pooled buffer it reuses immediately, so the
	// chunk has to be copied before it reaches another goroutine.
	chunk := make([]byte, len(p))
	copy(chunk, p)

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

// Subscribe delivers subsequent container output to w until the returned
// function is called. Multiple clients may subscribe at once; each gets every
// chunk written after it subscribed.
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
		h.mu.Unlock()
		return func() {}
	}
	id := h.nextID
	h.nextID++
	h.subs[id] = s
	h.mu.Unlock()

	go func() {
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

	var once sync.Once
	return func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs, id)
			h.mu.Unlock()
			close(s.done)
		})
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
	subs := make([]*subscriber, 0, len(h.subs))
	for id, s := range h.subs {
		subs = append(subs, s)
		delete(h.subs, id)
	}
	h.mu.Unlock()

	for _, s := range subs {
		close(s.done)
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

// HubRegistry holds the live Hubs for a runner's containers, so the exec server
// can find the Hub for a sandbox the sandbox controller booted. Both live in
// the runner process.
type HubRegistry struct {
	mu   sync.Mutex
	hubs map[string]*Hub
}

func NewHubRegistry() *HubRegistry {
	return &HubRegistry{hubs: make(map[string]*Hub)}
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

	key := hubKey(sandboxID, container)
	if h, ok := r.hubs[key]; ok {
		return h
	}
	h := NewHub()
	r.hubs[key] = h
	return h
}

// Get returns the Hub for a container, or nil if the container is not
// attachable or is not running on this node.
func (r *HubRegistry) Get(sandboxID entity.Id, container string) *Hub {
	if r == nil {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	return r.hubs[hubKey(sandboxID, container)]
}

// Remove tears down and forgets a container's Hub.
func (r *HubRegistry) Remove(sandboxID entity.Id, container string) {
	if r == nil {
		return
	}

	r.mu.Lock()
	key := hubKey(sandboxID, container)
	h := r.hubs[key]
	delete(r.hubs, key)
	r.mu.Unlock()

	if h != nil {
		h.Close()
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
	var doomed []*Hub
	for key, h := range r.hubs {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			doomed = append(doomed, h)
			delete(r.hubs, key)
		}
	}
	r.mu.Unlock()

	for _, h := range doomed {
		h.Close()
	}
}
