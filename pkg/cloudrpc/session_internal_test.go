package cloudrpc

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
)

// idleSession registers a session with no reader behind it, so what the
// delivery path does when frames pile up is a property of the code rather than
// of which goroutine got scheduled.
func idleSession(t *testing.T, id string, depth int) (*Server, *session) {
	t.Helper()

	srv := &Server{
		log:      slog.Default(),
		sessions: make(map[string]*session),
	}
	sess := &session{
		id:      id,
		srv:     srv,
		ctx:     t.Context(),
		inbound: make(chan []byte, depth),
		closed:  make(chan struct{}),
	}
	srv.sessions[id] = sess

	return srv, sess
}

func frameFor(t *testing.T, id string, payload []byte) json.RawMessage {
	t.Helper()

	raw, err := json.Marshal(Data{SessionID: id, Payload: payload})
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}
	return raw
}

// A session that has stopped reading is closed rather than waited on. Waiting
// would stall the uplink's read loop, which every tenant shares; dropping the
// frame instead would leave the session a hole it cannot resynchronise past.
func TestFullBacklogClosesTheSession(t *testing.T) {
	srv, sess := idleSession(t, "stuck", 2)

	ctx := context.Background()
	for range 2 {
		if err := srv.handleData(ctx, frameFor(t, "stuck", []byte("frame"))); err != nil {
			t.Fatalf("refused a frame the backlog had room for: %v", err)
		}
	}

	if err := srv.handleData(ctx, frameFor(t, "stuck", []byte("frame"))); err == nil {
		t.Fatal("accepted a frame past the backlog depth")
	}

	if srv.lookup("stuck") != nil {
		t.Error("the session was left in the map after being closed")
	}
	select {
	case <-sess.closed:
	default:
		t.Error("the session was not closed")
	}
}

// The same, reached by size rather than by count: a few frames big enough to
// matter close the session even though the depth is nowhere near full.
func TestOversizedBacklogClosesTheSession(t *testing.T) {
	srv, _ := idleSession(t, "greedy", 1024)

	ctx := context.Background()
	frame := make([]byte, 1<<20)

	var err error
	sent := 0
	for range 1024 {
		if err = srv.handleData(ctx, frameFor(t, "greedy", frame)); err != nil {
			break
		}
		sent++
	}

	if err == nil {
		t.Fatalf("accepted %d MiB without complaint", sent)
	}
	if sent >= 1024 {
		t.Fatal("the byte limit never bit")
	}
}

// The backlog is bounded by what it holds, not by how many frames hold it. The
// far end chooses the frame size, so a count says nothing about the memory: the
// same sixty-four frames are a few kilobytes or most of a gigabyte depending on
// who is sending them.
func TestSessionReservesByBytes(t *testing.T) {
	s := &session{}

	const frame = 1 << 20

	for i := range maxPendingBytes / frame {
		if !s.reserve(frame) {
			t.Fatalf("refused frame %d, well inside the limit", i)
		}
	}

	if s.reserve(frame) {
		t.Fatal("accepted a frame past the limit")
	}

	// What the reader takes becomes available again, or a long-lived session
	// would ratchet itself shut.
	s.release(frame)
	if !s.reserve(frame) {
		t.Fatal("released space was not reusable")
	}
}

// A single frame larger than the whole allowance is refused rather than
// admitted on the grounds that nothing else is queued. Accepting it would make
// the limit a suggestion, since the far end picks the size.
func TestSessionRefusesAnOversizedFrame(t *testing.T) {
	s := &session{}

	if s.reserve(maxPendingBytes + 1) {
		t.Fatal("accepted a frame larger than the entire allowance")
	}
}
