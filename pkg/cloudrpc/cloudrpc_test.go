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
		out:      make(chan *uplink.Envelope, 256),
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
	var sawClose bool
	for len(link.out) > 0 {
		if env := <-link.out; env.Type == cloudrpc.TypeClose {
			sawClose = true
		}
	}
	r.True(sawClose, "a refused session was never told")
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
