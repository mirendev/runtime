package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/pkg/entity"
)

// syncBuffer is a bytes.Buffer safe to read while a subscriber goroutine writes.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func eventuallyEquals(t *testing.T, want string, b *syncBuffer, msg string) {
	t.Helper()
	assert.Eventually(t, func() bool { return b.String() == want }, 2*time.Second, 5*time.Millisecond, msg)
}

func TestHubFansOutToEverySubscriber(t *testing.T) {
	h := NewHub()
	defer h.Close()

	var a, b syncBuffer
	stopA := h.Subscribe(&a)
	defer stopA()
	stopB := h.Subscribe(&b)
	defer stopB()

	_, err := h.Write([]byte("hello "))
	require.NoError(t, err)
	_, err = h.Write([]byte("world"))
	require.NoError(t, err)

	eventuallyEquals(t, "hello world", &a, "first subscriber sees all output")
	eventuallyEquals(t, "hello world", &b, "second subscriber sees the same output")
}

// One client leaving must not disturb the others, and must not disturb the
// container: that is the entire point of a subscription model.
func TestHubUnsubscribeLeavesOthersRunning(t *testing.T) {
	h := NewHub()
	defer h.Close()

	var stays, leaves syncBuffer
	stopStays := h.Subscribe(&stays)
	defer stopStays()
	stopLeaves := h.Subscribe(&leaves)

	_, err := h.Write([]byte("before"))
	require.NoError(t, err)
	eventuallyEquals(t, "before", &leaves, "departing subscriber got the first chunk")

	stopLeaves()

	_, err = h.Write([]byte("|after"))
	require.NoError(t, err)

	eventuallyEquals(t, "before|after", &stays, "remaining subscriber keeps receiving")
	assert.Equal(t, "before", leaves.String(), "departed subscriber receives nothing further")
}

func TestHubUnsubscribeIsIdempotent(t *testing.T) {
	h := NewHub()
	defer h.Close()

	stop := h.Subscribe(&syncBuffer{})
	stop()
	assert.NotPanics(t, stop, "double unsubscribe must not close a closed channel")
}

// Writing with nobody attached is the normal case for a detached run, and must
// not block or error.
func TestHubWriteWithNoSubscribers(t *testing.T) {
	h := NewHub()
	defer h.Close()

	n, err := h.Write([]byte("nobody is listening"))
	require.NoError(t, err)
	assert.Equal(t, len("nobody is listening"), n)
}

// containerd reuses a pooled buffer for every copy, so the Hub must not retain
// the caller's slice. If it did, a subscriber would observe later output
// retroactively overwriting earlier output.
func TestHubCopiesTheCallersBuffer(t *testing.T) {
	h := NewHub()
	defer h.Close()

	var got syncBuffer
	stop := h.Subscribe(&got)
	defer stop()

	shared := []byte("first ")
	_, err := h.Write(shared)
	require.NoError(t, err)

	eventuallyEquals(t, "first ", &got, "subscriber received the first chunk")

	// Reuse the same backing array, exactly as containerd's buffer pool does.
	copy(shared, []byte("second"))

	assert.Equal(t, "first ", got.String(), "already-delivered output must not mutate")
}

// A stalled client must lose output rather than block the container's stdout.
// Hub.Write runs on containerd's copy goroutine: blocking there freezes the
// workload being watched.
func TestHubDropsRatherThanBlockingOnASlowSubscriber(t *testing.T) {
	h := NewHub()
	defer h.Close()

	blocked := make(chan struct{})
	stop := h.Subscribe(writerFunc(func(p []byte) (int, error) {
		<-blocked
		return len(p), nil
	}))
	defer stop()
	defer close(blocked)

	// Far more than the queue depth. Every write must return promptly.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range subscriberQueue * 4 {
			_, _ = h.Write([]byte("x"))
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Hub.Write blocked on a stalled subscriber; this would freeze the container's stdout")
	}

	assert.NotZero(t, h.Dropped(), "output outrunning a stalled client should be reported as dropped")
}

// A subscriber whose client has gone away errors on every write. The Hub must
// keep draining it, or its queue fills and Hub.Write starts reporting drops for
// every other client too.
func TestHubToleratesAFailingSubscriber(t *testing.T) {
	h := NewHub()
	defer h.Close()

	stopBad := h.Subscribe(writerFunc(func(_ []byte) (int, error) {
		return 0, errors.New("client is gone")
	}))
	defer stopBad()

	var good syncBuffer
	stopGood := h.Subscribe(&good)
	defer stopGood()

	// Stay under the queue depth so nothing is dropped for capacity reasons and
	// the only variable under test is the broken subscriber's influence.
	const chunks = subscriberQueue / 2
	for i := range chunks {
		_, err := h.Write(fmt.Appendf(nil, "%d", i%10))
		require.NoError(t, err)
	}

	assert.Eventually(t, func() bool {
		return len(good.String()) == chunks
	}, 2*time.Second, 5*time.Millisecond, "a healthy client keeps receiving alongside a broken one")
	assert.Zero(t, h.Dropped(), "a subscriber that errors must still be drained, not left to fill its queue")
}

// The container's stdin must survive every client disconnecting. containerd
// closes the container's stdin FIFO the moment this reader EOFs, which for an
// interactive shell means the shell exits -- so a detach that closed it would
// make disconnect equivalent to cancellation.
func TestHubStdinSurvivesEveryClientLeaving(t *testing.T) {
	h := NewHub()
	defer h.Close()

	read := make(chan string, 2)
	go func() {
		buf := make([]byte, 16)
		for {
			n, err := h.Stdin().Read(buf)
			if n > 0 {
				read <- string(buf[:n])
			}
			if err != nil {
				close(read)
				return
			}
		}
	}()

	stop := h.Subscribe(&syncBuffer{})
	_, err := h.WriteStdin([]byte("one"))
	require.NoError(t, err)
	assert.Equal(t, "one", <-read)

	// Every client detaches.
	stop()

	// stdin must still be open for the next client to arrive.
	_, err = h.WriteStdin([]byte("two"))
	require.NoError(t, err, "stdin must stay open after the last client detaches")
	assert.Equal(t, "two", <-read)
}

// Teardown is the one caller allowed to close stdin: the container is going
// away, so there is no shell left for an EOF to kill.
func TestHubCloseEndsStdin(t *testing.T) {
	h := NewHub()

	readErr := make(chan error, 1)
	go func() {
		_, err := h.Stdin().Read(make([]byte, 8))
		readErr <- err
	}()

	h.Close()

	select {
	case err := <-readErr:
		assert.Error(t, err, "stdin reader should end once the Hub is closed")
	case <-time.After(2 * time.Second):
		t.Fatal("closing the Hub did not release the stdin reader")
	}
}

func TestHubCloseIsIdempotent(t *testing.T) {
	h := NewHub()
	h.Close()
	assert.NotPanics(t, h.Close)
}

func TestHubSubscribeAfterCloseIsInert(t *testing.T) {
	h := NewHub()
	h.Close()

	var got syncBuffer
	stop := h.Subscribe(&got)
	defer stop()

	_, err := h.Write([]byte("ignored"))
	require.NoError(t, err)
	assert.Empty(t, got.String())
}

type fakeResizer struct {
	mu   sync.Mutex
	w, h uint32
	err  error
}

func (f *fakeResizer) Resize(_ context.Context, w, h uint32) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.w, f.h = w, h
	return f.err
}

func TestHubResize(t *testing.T) {
	h := NewHub()
	defer h.Close()

	// Before the task exists, resizing is a no-op rather than an error: clients
	// send an initial window size as soon as they attach.
	require.NoError(t, h.Resize(context.Background(), 80, 24))

	r := &fakeResizer{}
	h.SetResizer(r)
	require.NoError(t, h.Resize(context.Background(), 120, 40))

	r.mu.Lock()
	defer r.mu.Unlock()
	assert.Equal(t, uint32(120), r.w)
	assert.Equal(t, uint32(40), r.h)
}

func TestHubSetResizerIgnoresNil(t *testing.T) {
	h := NewHub()
	defer h.Close()

	h.SetResizer(nil)
	assert.NoError(t, h.Resize(context.Background(), 80, 24))
}

func TestHubRegistry(t *testing.T) {
	r := NewHubRegistry()
	id := entity.Id("sandbox/sb-1")

	assert.Nil(t, r.Get(id, "app"), "unknown container has no hub")

	h := r.GetOrCreate(id, "app")
	require.NotNil(t, h)
	assert.Same(t, h, r.GetOrCreate(id, "app"), "the same container gets the same hub")
	assert.Same(t, h, r.Get(id, "app"))

	other := r.GetOrCreate(id, "sidecar")
	assert.NotSame(t, h, other, "containers within a sandbox get their own hubs")

	r.Remove(id, "app")
	removed := r.Get(id, "app")
	require.NotNil(t, removed, "a removed hub lingers so a late client still gets its output")
	assert.True(t, removed.closed, "but it is closed, so an attach ends at once")
	assert.NotNil(t, r.Get(id, "sidecar"), "removing one container leaves its siblings")

	expireHubs(r)
	assert.Nil(t, r.Get(id, "app"), "and it is gone once the linger window passes")
}

// expireHubs advances the registry's clock past the linger window, so tests can
// assert on expiry without sleeping a minute.
func expireHubs(r *HubRegistry) {
	r.mu.Lock()
	base := time.Now().Add(2 * hubLinger)
	r.now = func() time.Time { return base }
	r.mu.Unlock()

	// Any registry operation sweeps; use one that observes nothing.
	r.Get(entity.Id("sandbox/nonexistent"), "app")
}

// A sandbox going away must take every one of its containers' hubs with it, or
// the runner leaks a goroutine and an open pipe per container.
func TestHubRegistryRemoveAll(t *testing.T) {
	r := NewHubRegistry()
	doomed := entity.Id("sandbox/sb-1")
	survivor := entity.Id("sandbox/sb-2")

	r.GetOrCreate(doomed, "app")
	r.GetOrCreate(doomed, "sidecar")
	kept := r.GetOrCreate(survivor, "app")

	r.RemoveAll(doomed)

	assert.Same(t, kept, r.Get(survivor, "app"), "another sandbox's hubs are untouched")

	expireHubs(r)
	assert.Nil(t, r.Get(doomed, "app"))
	assert.Nil(t, r.Get(doomed, "sidecar"))
	assert.Same(t, kept, r.Get(survivor, "app"), "and a live sandbox's hub is not swept with them")
}

// RemoveAll must key on the sandbox boundary, not a bare string prefix, or
// sandbox/sb-1 would take sandbox/sb-10's hubs down with it.
func TestHubRegistryRemoveAllRespectsSandboxBoundary(t *testing.T) {
	r := NewHubRegistry()
	short := entity.Id("sandbox/sb-1")
	longer := entity.Id("sandbox/sb-10")

	r.GetOrCreate(short, "app")
	kept := r.GetOrCreate(longer, "app")

	r.RemoveAll(short)

	expireHubs(r)
	assert.Nil(t, r.Get(short, "app"))
	assert.Same(t, kept, r.Get(longer, "app"), "a sandbox whose id merely shares a prefix is unaffected")
}

// A sandbox name is derived from its run and attempt, so the same key can come
// back for a retry. Reviving the retired Hub would replay the failed attempt's
// output into the new attempt's terminal.
func TestHubRegistryDoesNotReviveARetiredHub(t *testing.T) {
	r := NewHubRegistry()
	id := entity.Id("sandbox/sb-1")

	first := r.GetOrCreate(id, "app")
	_, _ = first.Write([]byte("output from the first attempt"))
	r.RemoveAll(id)

	second := r.GetOrCreate(id, "app")
	assert.NotSame(t, first, second, "a new container gets a new hub")

	var got syncBuffer
	unsubscribe := second.Subscribe(&got)
	defer unsubscribe()
	assert.Empty(t, got.String(), "the retired hub's output must not replay into the new one")
}

// A nil registry is what a controller built without one has; every method must
// tolerate it rather than making callers guard.
func TestNilHubRegistryIsSafe(t *testing.T) {
	var r *HubRegistry
	id := entity.Id("sandbox/sb-1")

	assert.Nil(t, r.Get(id, "app"))
	assert.Nil(t, r.GetOrCreate(id, "app"))
	assert.NotPanics(t, func() { r.Remove(id, "app") })
	assert.NotPanics(t, func() { r.RemoveAll(id) })
}

func TestContainerIsAttachable(t *testing.T) {
	sb := &compute.Sandbox{}
	sb.Spec.Container = []compute.SandboxSpecContainer{
		{Name: "app", Stdin: true},
		{Name: "sidecar"},
	}

	assert.True(t, containerIsAttachable(sb, "app"))
	assert.False(t, containerIsAttachable(sb, "sidecar"), "a container that never had a stdin FIFO cannot get one on reattach")
	assert.False(t, containerIsAttachable(sb, "absent"))
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

var _ io.Writer = writerFunc(nil)

// Teardown can close a Hub while a client is still attached, and that client's
// deferred unsubscribe runs afterwards. Both paths close the subscriber's done
// channel, so without a shared guard the second close panics and takes the
// runner process with it.
func TestHubCloseThenUnsubscribeDoesNotPanic(t *testing.T) {
	h := NewHub()

	stop := h.Subscribe(&syncBuffer{})

	// The sandbox goes away first...
	h.Close()

	// ...then the attach handler's deferred cleanup runs.
	assert.NotPanics(t, stop, "unsubscribing after Close must be safe")
	assert.NotPanics(t, stop, "and must stay safe when repeated")
}

// The reverse order has to be safe too: a client detaches, then the sandbox is
// torn down.
func TestHubUnsubscribeThenCloseDoesNotPanic(t *testing.T) {
	h := NewHub()

	stop := h.Subscribe(&syncBuffer{})
	stop()

	assert.NotPanics(t, h.Close)
}

// Several attached clients, torn down underneath all of them at once.
func TestHubCloseWithManySubscribersThenUnsubscribeAll(t *testing.T) {
	h := NewHub()

	var stops []func()
	for range 5 {
		stops = append(stops, h.Subscribe(&syncBuffer{}))
	}

	h.Close()

	assert.NotPanics(t, func() {
		for _, stop := range stops {
			stop()
		}
	})
}

// Done is the signal an attached client waits on. Without it the only event an
// attach can observe is its own stdin ending, which says nothing about the
// workload: a non-interactive caller's stdin is /dev/null and EOFs at once, so
// the attach would return while the task was still running and report it as
// finished.
func TestHubDoneIsOpenUntilClose(t *testing.T) {
	h := NewHub()

	select {
	case <-h.Done():
		t.Fatal("Done must not be closed while the container is alive")
	default:
	}

	h.Close()

	select {
	case <-h.Done():
	case <-time.After(time.Second):
		t.Fatal("Done must be closed once the Hub is torn down")
	}
}

// Teardown races the client's own unsubscribe, and both paths close channels.
func TestHubDoneSurvivesRepeatedClose(t *testing.T) {
	h := NewHub()
	h.Close()
	h.Close()

	select {
	case <-h.Done():
	default:
		t.Fatal("Done must stay closed")
	}
}

// A Hub reached through the registry is the one a client attaches to, so
// removal has to close it -- otherwise the client waits on a container that is
// already gone.
func TestHubRegistryRemoveClosesDone(t *testing.T) {
	r := NewHubRegistry()
	h := r.GetOrCreate(entity.Id("sandbox/one"), "app")

	r.RemoveAll(entity.Id("sandbox/one"))

	select {
	case <-h.Done():
	case <-time.After(time.Second):
		t.Fatal("RemoveAll must close the Hub it drops")
	}
}

// A container starts as soon as its sandbox boots, while the client is still
// resolving the sandbox and opening a connection. Anything written in that
// window used to be dropped, so `miren app run -- echo hi` printed nothing --
// the one thing running a command interactively is for.
func TestHubReplaysOutputWrittenBeforeAnyoneAttached(t *testing.T) {
	h := NewHub()
	defer h.Close()

	_, _ = h.Write([]byte("printed before the client arrived\n"))

	var got syncBuffer
	unsubscribe := h.Subscribe(&got)
	defer unsubscribe()

	assert.Eventually(t, func() bool {
		return strings.Contains(got.String(), "printed before the client arrived")
	}, time.Second, 10*time.Millisecond)
}

// The replay must not duplicate or drop the chunk that lands while a subscriber
// is being registered, which is why the snapshot and the registration share a
// lock.
func TestHubReplayThenLiveOutputArrivesOnceInOrder(t *testing.T) {
	h := NewHub()
	defer h.Close()

	_, _ = h.Write([]byte("first\n"))

	var got syncBuffer
	unsubscribe := h.Subscribe(&got)
	defer unsubscribe()

	_, _ = h.Write([]byte("second\n"))

	assert.Eventually(t, func() bool {
		return got.String() == "first\nsecond\n"
	}, time.Second, 10*time.Millisecond, "got %q", got.String())
}

// The buffer is bounded because a Hub lives as long as its container and a
// detached run can produce output for hours. A late client gets the tail.
func TestHubReplayIsBounded(t *testing.T) {
	h := NewHub()
	defer h.Close()

	chunk := bytes.Repeat([]byte("x"), 8<<10)
	for range 16 {
		_, _ = h.Write(chunk)
	}
	_, _ = h.Write([]byte("TAIL"))

	var got syncBuffer
	unsubscribe := h.Subscribe(&got)
	defer unsubscribe()

	assert.Eventually(t, func() bool {
		s := got.String()
		return len(s) <= replayBuffer && strings.HasSuffix(s, "TAIL")
	}, time.Second, 10*time.Millisecond)
}

// A single write larger than the whole buffer keeps its tail rather than
// overflowing or being discarded outright.
func TestHubReplayHandlesAnOversizedWrite(t *testing.T) {
	h := NewHub()
	defer h.Close()

	_, _ = h.Write(append(bytes.Repeat([]byte("y"), replayBuffer+1024), []byte("END")...))

	var got syncBuffer
	unsubscribe := h.Subscribe(&got)
	defer unsubscribe()

	assert.Eventually(t, func() bool {
		s := got.String()
		return len(s) <= replayBuffer && strings.HasSuffix(s, "END")
	}, time.Second, 10*time.Millisecond)
}

// A command that finished before the client connected is exactly when its
// output matters most, and a terminal has nowhere else to read it from.
func TestHubClosedStillReplays(t *testing.T) {
	h := NewHub()
	_, _ = h.Write([]byte("ran and exited\n"))
	h.Close()

	var got syncBuffer
	unsubscribe := h.Subscribe(&got)
	defer unsubscribe()

	assert.Contains(t, got.String(), "ran and exited")
}
