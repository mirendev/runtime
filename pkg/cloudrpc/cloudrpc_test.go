package cloudrpc_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/cloudrpc"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/example"
	"miren.dev/runtime/pkg/uplink"
)

// fakeLink stands in for the uplink: it collects the handlers the relay
// registers and captures what the relay sends, so a test can play the part of
// cloud without a socket.
type fakeLink struct {
	mu       sync.Mutex
	handlers map[string]uplink.MessageHandler

	out chan *uplink.Envelope

	// stalled makes SendContext block, standing in for a congested link.
	stalled bool
}

func newFakeLink() *fakeLink {
	return &fakeLink{
		handlers: make(map[string]uplink.MessageHandler),
		// Deep enough that no test fills it by accident: a full link changes
		// what the relay does (best-effort sends start dropping), so a test
		// that overflows it is measuring the fake rather than the code.
		out: make(chan *uplink.Envelope, 4096),
	}
}

func (l *fakeLink) Handle(msgType string, h uplink.MessageHandler) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.handlers[msgType] = h
}

func (l *fakeLink) SendContext(ctx context.Context, env *uplink.Envelope) error {
	l.mu.Lock()
	stalled := l.stalled
	l.mu.Unlock()

	if stalled {
		<-ctx.Done()
		return ctx.Err()
	}

	select {
	case l.out <- env:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Send is the best-effort half: it drops rather than waiting, and — the part
// that matters for a stalled link — it never blocks at all.
func (l *fakeLink) Send(env *uplink.Envelope) {
	select {
	case l.out <- env:
	default:
	}
}

func (l *fakeLink) stall() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.stalled = true
}

// deliver plays a message from cloud down to the cluster, the way the uplink's
// read loop would.
func (l *fakeLink) deliver(ctx context.Context, msgType string, payload any) error {
	l.mu.Lock()
	h := l.handlers[msgType]
	l.mu.Unlock()

	if h == nil {
		return errors.New("no handler for " + msgType)
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return h(ctx, raw)
}

// relayConn is the caller's end of a relayed session: the miniature of what
// cloud does, wrapping outbound frames into rpc.data and unwrapping inbound
// ones. It reads every envelope the cluster sends, so a test using it must be
// the only reader of the link.
type relayConn struct {
	link      *fakeLink
	sessionID string
	ctx       context.Context
}

func (c *relayConn) Send(b []byte) error {
	return c.link.deliver(c.ctx, cloudrpc.TypeData,
		cloudrpc.Data{SessionID: c.sessionID, Payload: b})
}

func (c *relayConn) Recv() ([]byte, error) {
	for {
		select {
		case env := <-c.link.out:
			switch env.Type {
			case cloudrpc.TypeData:
				var msg cloudrpc.Data
				if err := json.Unmarshal(env.Data, &msg); err != nil {
					return nil, err
				}
				if msg.SessionID != c.sessionID {
					continue
				}
				return msg.Payload, nil
			case cloudrpc.TypeClose:
				var msg cloudrpc.Close
				if err := json.Unmarshal(env.Data, &msg); err != nil {
					return nil, err
				}
				if msg.SessionID != c.sessionID {
					continue
				}
				return nil, io.EOF
			default:
				continue
			}
		case <-c.ctx.Done():
			return nil, io.EOF
		}
	}
}

func (c *relayConn) Close() error { return nil }

// clusterWithRelay stands up an RPC server exposing iface, fronted by a relay on
// a fake link, and returns the link.
func clusterWithRelay(t *testing.T, ctx context.Context, name string, iface *rpc.Interface) *fakeLink {
	t.Helper()
	r := require.New(t)

	ss, err := rpc.NewState(ctx, rpc.WithSkipVerify)
	r.NoError(err)
	ss.Server().ExposeValue(name, iface)

	link := newFakeLink()
	cloudrpc.New(cloudrpc.Config{Uplink: link, State: ss, Log: slog.Default()})

	return link
}

// The whole point: a call placed through the relay reaches the same objects the
// cluster serves directly, and its results come back.
func TestRelayedCallsReachTheServer(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	link := clusterWithRelay(t, ctx, "meter", example.AdaptMeter(&testMeter{temp: 42}))

	r.NoError(link.deliver(ctx, cloudrpc.TypeOpen, cloudrpc.Open{SessionID: "s1"}))

	cs, err := rpc.NewState(ctx, rpc.WithSkipVerify)
	r.NoError(err)

	conn := &relayConn{link: link, sessionID: "s1", ctx: ctx}
	c, err := cs.ClientFromMessageConn(ctx, conn, "meter")
	r.NoError(err)

	mc := &example.MeterClient{Client: c}

	res, err := mc.ReadTemperature(ctx, "test")
	r.NoError(err)
	r.Equal("test", res.Reading().Meter())
	r.Equal(float32(42), res.Reading().Temperature())

	// A capability handed back by the first call is followed over the same
	// session, so the relay carries more than one round trip.
	set, err := mc.GetSetter(ctx, "test")
	r.NoError(err)

	got, err := set.Setter().SetTemp(ctx, 100)
	r.NoError(err)
	r.Equal(int32(100), got.Temp())
}

// Frames larger than one uplink message are split by the layer above and
// reassembled on arrival. Nothing here has to know that, which is the property
// worth pinning: the relay carries payloads far past its own frame cap.
func TestRelayedCallsCarryLargePayloads(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	link := clusterWithRelay(t, ctx, "meter", example.AdaptMeter(&testMeter{temp: 7}))

	r.NoError(link.deliver(ctx, cloudrpc.TypeOpen, cloudrpc.Open{SessionID: "s1"}))

	cs, err := rpc.NewState(ctx, rpc.WithSkipVerify)
	r.NoError(err)

	conn := &relayConn{link: link, sessionID: "s1", ctx: ctx}
	c, err := cs.ClientFromMessageConn(ctx, conn, "meter")
	r.NoError(err)

	// Comfortably past the 256 KiB the relay puts in one envelope.
	name := make([]byte, 700*1024)
	for i := range name {
		name[i] = 'a'
	}

	res, err := (&example.MeterClient{Client: c}).ReadTemperature(ctx, string(name))
	r.NoError(err)
	r.Equal(string(name), res.Reading().Meter())
}

// Cloud mints session ids, so a repeat is a bug there. Serving two callers one
// pipe would interleave their frames into nonsense, so the second is refused.
func TestDuplicateSessionRefused(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	link := clusterWithRelay(t, ctx, "meter", example.AdaptMeter(&testMeter{temp: 42}))

	r.NoError(link.deliver(ctx, cloudrpc.TypeOpen, cloudrpc.Open{SessionID: "s1"}))
	r.Error(link.deliver(ctx, cloudrpc.TypeOpen, cloudrpc.Open{SessionID: "s1"}))
}

// An open with no session id names nothing and cannot be answered.
func TestOpenWithoutSessionIDRefused(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	link := clusterWithRelay(t, ctx, "meter", example.AdaptMeter(&testMeter{temp: 42}))

	r.Error(link.deliver(ctx, cloudrpc.TypeOpen, cloudrpc.Open{}))
}

// Frames for a session that has already gone are ordinary at the tail of a
// teardown — the far end had some in flight. They must not be an error, because
// the uplink logs a warning for every handler that returns one.
func TestFramesForUnknownSessionAreIgnored(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	link := clusterWithRelay(t, ctx, "meter", example.AdaptMeter(&testMeter{temp: 42}))

	r.NoError(link.deliver(ctx, cloudrpc.TypeData,
		cloudrpc.Data{SessionID: "gone", Payload: []byte("x")}))
	r.NoError(link.deliver(ctx, cloudrpc.TypeClose, cloudrpc.Close{SessionID: "gone"}))
}

// Handlers run on the uplink's shared read loop, so a session drowning in
// frames must not stall it. What that costs the session is covered next to the
// accounting it uses; what matters here is that everyone else keeps going.
//
// Nothing asserts how the flood ends, deliberately. Whether the backlog fills
// before the session's reader gives up on the garbage being sent is a race, and
// pinning either outcome would make this a test of scheduling.
func TestAFloodedSessionDoesNotStallTheLink(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	link := clusterWithRelay(t, ctx, "meter", example.AdaptMeter(&testMeter{temp: 42}))

	r.NoError(link.deliver(ctx, cloudrpc.TypeOpen, cloudrpc.Open{SessionID: "stuck"}))

	for range 4096 {
		if err := link.deliver(ctx, cloudrpc.TypeData,
			cloudrpc.Data{SessionID: "stuck", Payload: []byte("frame")}); err != nil {
			break
		}
	}

	// The link is still usable: a fresh session opens and serves.
	r.NoError(link.deliver(ctx, cloudrpc.TypeOpen, cloudrpc.Open{SessionID: "healthy"}))

	cs, cerr := rpc.NewState(ctx, rpc.WithSkipVerify)
	r.NoError(cerr)

	conn := &relayConn{link: link, sessionID: "healthy", ctx: ctx}
	c, cerr := cs.ClientFromMessageConn(ctx, conn, "meter")
	r.NoError(cerr)

	res, cerr := (&example.MeterClient{Client: c}).ReadTemperature(ctx, "test")
	r.NoError(cerr)
	r.Equal(float32(42), res.Reading().Temperature())
}

// One session's memory is bounded, so the other half of the sum has to be too.
func TestSessionsAreCapped(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	link := clusterWithRelay(t, ctx, "meter", example.AdaptMeter(&testMeter{temp: 42}))

	var err error
	opened := 0
	for i := range 1024 {
		err = link.deliver(ctx, cloudrpc.TypeOpen,
			cloudrpc.Open{SessionID: fmt.Sprintf("s%d", i)})
		if err != nil {
			break
		}
		opened++
	}

	r.Error(err, "the relay opened %d sessions without a ceiling", opened)
	r.Positive(opened, "the cap should leave room for real callers")

	// The refusal is told, not dropped: a caller waiting on a connection that
	// silently never answers has nothing to act on.
	//
	// Drained rather than sampled, because the refusal is best-effort and would
	// genuinely be dropped if the link filled — which would make this assertion
	// a statement about the fake's buffer rather than about the relay.
	var sawClose bool
	for {
		select {
		case env := <-link.out:
			if env.Type == cloudrpc.TypeClose {
				sawClose = true
			}
			continue
		case <-time.After(200 * time.Millisecond):
		}
		break
	}
	r.True(sawClose, "a refused session was never told")
}

// Refusing a session must not wait on the link.
//
// Handlers run on the link's read loop, so anything that waits there stops
// every other tenant — and a congested outbox is exactly the condition under
// which a cap gets hit, so the two arrive together or not at all.
func TestRefusingASessionDoesNotWaitOnTheLink(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	link := clusterWithRelay(t, ctx, "meter", example.AdaptMeter(&testMeter{temp: 42}))

	for i := range 1024 {
		if err := link.deliver(ctx, cloudrpc.TypeOpen,
			cloudrpc.Open{SessionID: fmt.Sprintf("s%d", i)}); err != nil {
			break
		}
	}

	// Nothing can reach the far end now, in either direction.
	link.stall()

	refused := make(chan error, 1)
	go func() {
		refused <- link.deliver(ctx, cloudrpc.TypeOpen, cloudrpc.Open{SessionID: "one-more"})
	}()

	select {
	case err := <-refused:
		r.Error(err, "the session past the cap should have been refused")
	case <-time.After(2 * time.Second):
		r.Fail("refusing a session blocked the link's read loop")
	}
}

// A session must not outlive the connection carrying it. There is no resuming a
// relayed session across a reconnect — the frames in flight are gone — so the
// honest outcome is that the caller's connection breaks and it retries.
func TestSessionEndsWithTheConnection(t *testing.T) {
	r := require.New(t)

	connCtx, dropConn := context.WithCancel(t.Context())
	defer dropConn()

	link := clusterWithRelay(t, t.Context(), "meter", example.AdaptMeter(&testMeter{temp: 42}))

	r.NoError(link.deliver(connCtx, cloudrpc.TypeOpen, cloudrpc.Open{SessionID: "s1"}))

	cs, err := rpc.NewState(t.Context(), rpc.WithSkipVerify)
	r.NoError(err)

	conn := &relayConn{link: link, sessionID: "s1", ctx: t.Context()}
	c, err := cs.ClientFromMessageConn(t.Context(), conn, "meter")
	r.NoError(err)

	mc := &example.MeterClient{Client: c}
	_, err = mc.ReadTemperature(t.Context(), "test")
	r.NoError(err)

	dropConn()

	// The session stops serving. Each attempt is bounded rather than left to
	// run: the close notice travels on the link that just died, so whether the
	// caller is told or simply left waiting is not something this end decides —
	// what it owes is to stop answering.
	deadline := time.After(3 * time.Second)
	for {
		callCtx, cancelCall := context.WithTimeout(t.Context(), 200*time.Millisecond)
		_, err = mc.ReadTemperature(callCtx, "test")
		cancelCall()

		if err != nil {
			return
		}

		select {
		case <-deadline:
			r.Fail("session outlived the connection that carried it")
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// Introspection has to answer the same over the relay as it does over a direct
// dial, because clients use it to decide what the far end supports.
//
// The message transport used to report method names alone. A missing Params map
// does not read as "these methods take no parameters" — it reads as "this
// server is too old to say," which is what a pre-introspection cluster looks
// like. So a current cluster reached through cloud was mistaken for an old one,
// and callers quietly dropped to older behaviour: `miren logs --until` refused
// it, and deploy fell back to the deprecated client-owned deployment path.
func TestRelayedIntrospectionReportsMethodParameters(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	link := clusterWithRelay(t, ctx, "meter", example.AdaptMeter(&testMeter{temp: 42}))

	r.NoError(link.deliver(ctx, cloudrpc.TypeOpen, cloudrpc.Open{SessionID: "s1"}))

	cs, err := rpc.NewState(ctx, rpc.WithSkipVerify)
	r.NoError(err)

	conn := &relayConn{link: link, sessionID: "s1", ctx: ctx}
	c, err := cs.ClientFromMessageConn(ctx, conn, "meter")
	r.NoError(err)

	r.True(c.HasMethod(ctx, "readTemperature"),
		"the relayed client cannot see a method the cluster exposes")
	r.True(c.HasMethodParam(ctx, "readTemperature", "name"),
		"the relayed client sees the method but not its parameters, "+
			"which reads to a caller as a cluster too old to ask")
	r.False(c.HasMethodParam(ctx, "readTemperature", "nonesuch"),
		"a parameter the method does not take must still report false")
}

// Ending a session must end the work dispatched from it.
//
// Handlers used to run on the uplink's context, which outlives any one session,
// so a build or a log stream kept going after its caller was gone. It did so
// uncounted, too: teardown removes the session from the map first, so the work
// no longer counted against maxSessions and repeated open-and-disconnect was a
// way around that ceiling.
func TestTeardownCancelsDispatchedHandlers(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	meter := &blockingMeter{
		entered:  make(chan struct{}),
		released: make(chan error, 1),
	}
	link := clusterWithRelay(t, ctx, "meter", example.AdaptMeter(meter))

	r.NoError(link.deliver(ctx, cloudrpc.TypeOpen, cloudrpc.Open{SessionID: "s1"}))

	cs, err := rpc.NewState(ctx, rpc.WithSkipVerify)
	r.NoError(err)

	conn := &relayConn{link: link, sessionID: "s1", ctx: ctx}
	c, err := cs.ClientFromMessageConn(ctx, conn, "meter")
	r.NoError(err)

	mc := &example.MeterClient{Client: c}

	// The caller's own context stays live throughout, so nothing here can end
	// the handler except the session teardown under test.
	go func() { _, _ = mc.ReadTemperature(ctx, "test") }()

	select {
	case <-meter.entered:
	case <-time.After(3 * time.Second):
		r.Fail("the handler never ran, so there is nothing to observe")
	}

	// Cloud ends the session while the handler is parked inside it.
	r.NoError(link.deliver(ctx, cloudrpc.TypeClose,
		cloudrpc.Close{SessionID: "s1", Reason: "caller went away"}))

	select {
	case err := <-meter.released:
		r.ErrorIs(err, context.Canceled)
	case <-time.After(3 * time.Second):
		r.Fail("the handler outlived the session it was dispatched from")
	}
}

// A congested link must slow the writer, not silently lose a frame. RPC cannot
// resynchronise after a hole, so Send has to block until the frame is queued.
func TestSendWaitsOnACongestedLink(t *testing.T) {
	r := require.New(t)

	connCtx, dropConn := context.WithCancel(t.Context())
	defer dropConn()

	link := clusterWithRelay(t, t.Context(), "meter", example.AdaptMeter(&testMeter{temp: 42}))
	link.stall()

	r.NoError(link.deliver(connCtx, cloudrpc.TypeOpen, cloudrpc.Open{SessionID: "s1"}))

	cs, err := rpc.NewState(t.Context(), rpc.WithSkipVerify)
	r.NoError(err)

	// The caller's end is scoped to the connection too, mirroring cloud closing
	// the caller's socket when the link under it goes away.
	done := make(chan error, 1)
	go func() {
		conn := &relayConn{link: link, sessionID: "s1", ctx: connCtx}
		_, cerr := cs.ClientFromMessageConn(t.Context(), conn, "meter")
		done <- cerr
	}()

	select {
	case <-done:
		r.Fail("a send on a congested link returned instead of waiting")
	case <-time.After(200 * time.Millisecond):
	}

	// Dropping the connection is what unblocks it, and the caller learns.
	dropConn()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		r.Fail("a blocked send did not give up when the connection went away")
	}
}
