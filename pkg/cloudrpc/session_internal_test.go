package cloudrpc

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/rpc"
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
	sess := newSession(t.Context(), id, srv)
	// A shallower backlog than production's, so a test can fill it without
	// queueing inboundDepth frames to get there.
	sess.inbound = make(chan []byte, depth)
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

// A relayed session has no peer address to report — the socket only knows about
// cloud — so it reports the session id, which is what distinguishes one caller
// from another and is the same identifier cloud records on its side.
func TestSessionReportsARemoteForAuditing(t *testing.T) {
	sess := &session{id: "cluster-abc.xyz"}

	var conn rpc.MessageConn = sess
	remote, ok := conn.(rpc.MessageRemote)
	require.True(t, ok, "a relayed session must be able to name its far end")
	require.Equal(t, "cloud-relay/cluster-abc.xyz", remote.Remote())
}

// A frame that lands in the same instant as the close is still delivered.
// Recv's contract is EOF only once nothing buffered remains, and readPump
// treats EOF as final, so a frame left behind is a frame lost.
//
// The window is the gap between Recv's two selects: a reader that is already
// parked gets the frame handed to it directly, so the loss only shows when
// the frame and the close both land while the reader is between the two.
// That is narrow (a handful of hits per hundred thousand tries on the old
// code), so this runs enough iterations to make a clean pass meaningful.
func TestRecvDrainsFrameRacingClose(t *testing.T) {
	for i := range 200_000 {
		sess := newSession(t.Context(), "race", nil)

		got := make(chan []byte, 1)
		errs := make(chan error, 1)
		go func() {
			b, err := sess.Recv()
			got <- b
			errs <- err
		}()

		sess.inbound <- []byte("last")
		sess.shutdown()

		b, err := <-got, <-errs
		require.NoError(t, err, "iteration %d: frame lost behind EOF", i)
		require.Equal(t, []byte("last"), b)

		// Once drained, the closed session reports EOF.
		_, err = sess.Recv()
		require.ErrorIs(t, err, io.EOF)
	}
}
