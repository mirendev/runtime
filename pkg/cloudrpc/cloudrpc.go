package cloudrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/uplink"
)

const (
	// maxFrameData bounds the RPC payload we put in one uplink envelope.
	//
	// It is well under what the link would accept, on purpose. The uplink is
	// shared: a single envelope is written whole, so an oversized one delays
	// every other tenant behind it. Splitting into smaller frames costs
	// throughput and nothing else, which is a good trade for a link that also
	// carries the cluster's coordination traffic.
	//
	// It bounds only our own writes. What the far end sends is framed by the
	// far end, so the link's read limit — not this — is what has to tolerate a
	// peer using pkg/rpc's larger default.
	maxFrameData = 256 << 10

	// inboundDepth is how many frames may await a session's reader.
	//
	// It is a backlog, not a buffer: the layer above drains its side promptly,
	// so frames only pile up here when a session has genuinely stopped reading.
	// The depth is what separates an ordinary scheduling delay from that.
	inboundDepth = 64

	// maxPendingBytes bounds the memory those frames may hold.
	//
	// The depth alone bounds nothing useful, because the far end chooses the
	// frame size: sixty-four frames is a few kilobytes or most of a gigabyte
	// depending on who is sending. This is the limit that holds either way.
	maxPendingBytes = 8 << 20

	// maxSessions bounds how many relayed sessions run at once.
	//
	// One session is one command in flight, so this is generous for an
	// operator and still a ceiling on what a misbehaving cloud can make this
	// cluster allocate. Cloud is authenticated, which makes this a guard
	// against a bug there rather than against an anonymous flood — but a
	// cluster with no limit of its own has no answer to either.
	maxSessions = 128
)

// Link is the part of the control-plane link this package uses: somewhere to
// register handlers, and a send that applies backpressure instead of dropping.
// *uplink.Client is the implementation; naming the two methods keeps the
// dependency honest and lets the relay be exercised without a socket.
type Link interface {
	Handle(msgType string, handler uplink.MessageHandler)
	SendContext(ctx context.Context, env *uplink.Envelope) error
}

// Config wires a Server to the link it rides and the RPC server it exposes.
type Config struct {
	// Uplink is the control-plane link. The Server registers handlers on it but
	// does not own its lifecycle; whoever created the link runs it.
	Uplink Link

	// State is the RPC state whose exposed objects relayed callers reach. It is
	// the same one the cluster serves directly, deliberately: a call arriving
	// this way must reach the same objects, under the same authorization, as one
	// that arrived over the network.
	State *rpc.State

	Log *slog.Logger
}

// Server serves relayed RPC sessions arriving over the uplink.
type Server struct {
	uplink Link
	state  *rpc.State
	log    *slog.Logger

	mu       sync.Mutex
	sessions map[string]*session
}

// New creates a Server and registers its handlers on the uplink. The caller is
// responsible for running the uplink itself.
func New(cfg Config) *Server {
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}

	s := &Server{
		uplink:   cfg.Uplink,
		state:    cfg.State,
		log:      log,
		sessions: make(map[string]*session),
	}

	cfg.Uplink.Handle(TypeOpen, s.handleOpen)
	cfg.Uplink.Handle(TypeData, s.handleData)
	cfg.Uplink.Handle(TypeClose, s.handleClose)

	// Startup confirmation. Like Miren Anywhere, this is otherwise silent until
	// a caller shows up, which may be never — leaving an operator unable to tell
	// a wired relay from one that failed to come up.
	log.Info("cloud RPC relay ready")

	return s
}

// handleOpen starts serving RPC on a new session.
//
// The context is the uplink's connection-scoped one, so a session cannot outlive
// the link that carries it. That is the right lifetime: a dropped uplink loses
// the frames in flight, and there is no way to resume from that. The caller sees
// a broken connection and retries, which is honest.
func (s *Server) handleOpen(ctx context.Context, data json.RawMessage) error {
	var req Open
	if err := json.Unmarshal(data, &req); err != nil {
		return err
	}
	if req.SessionID == "" {
		return fmt.Errorf("rpc.open with no session id")
	}

	sess := &session{
		id:      req.SessionID,
		srv:     s,
		ctx:     ctx,
		inbound: make(chan []byte, inboundDepth),
		closed:  make(chan struct{}),
	}

	s.mu.Lock()
	if _, exists := s.sessions[req.SessionID]; exists {
		s.mu.Unlock()
		// Cloud mints these ids, so a collision is a bug there rather than
		// something to paper over. Refusing beats serving two callers one pipe.
		return fmt.Errorf("session %s already open", req.SessionID)
	}
	if len(s.sessions) >= maxSessions {
		s.mu.Unlock()
		// Told rather than dropped: the caller is waiting on a connection that
		// would otherwise just never answer.
		//nolint:errcheck // the refusal is best-effort; the caller times out either way
		s.send(ctx, TypeClose, Close{SessionID: req.SessionID, Reason: "too many open sessions"})
		s.log.Warn("refused a relayed RPC session, too many open", "session", req.SessionID, "open", maxSessions)
		return fmt.Errorf("too many relayed sessions open")
	}
	s.sessions[req.SessionID] = sess
	s.mu.Unlock()

	s.log.Info("relayed RPC session opened", "session", req.SessionID)

	go s.serve(ctx, sess)

	return nil
}

func (s *Server) serve(ctx context.Context, sess *session) {
	defer func() {
		s.forget(sess.id)
		sess.shutdown()
	}()

	err := s.state.ServeMessageConn(ctx, sess, rpc.WithMaxFrameSize(maxFrameData))

	// Tell the far end, best effort: if the uplink is what failed, this goes
	// nowhere, and the caller learns from its own broken connection instead.
	//nolint:errcheck // best-effort teardown notice
	s.send(ctx, TypeClose, Close{SessionID: sess.id, Reason: "session ended"})

	s.log.Info("relayed RPC session closed", "session", sess.id, "error", err)
}

// handleData delivers one frame to its session.
//
// Handlers run on the uplink's read loop, which every tenant shares, so this
// must not block. A session whose backlog is full has stopped reading, and
// waiting for it would stall app reporting and POP handshakes behind a stuck
// caller. Dropping the frame instead would be worse than useless — RPC cannot
// resynchronise after a hole — so the session is torn down and the caller finds
// out.
func (s *Server) handleData(_ context.Context, data json.RawMessage) error {
	var msg Data
	if err := json.Unmarshal(data, &msg); err != nil {
		return err
	}

	sess := s.lookup(msg.SessionID)
	if sess == nil {
		// Ordinary at the tail of a teardown: the far end had frames in flight
		// when we closed. Nothing to do about them, and nothing alarming.
		s.log.Debug("frame for unknown relayed RPC session", "session", msg.SessionID)
		return nil
	}

	if !sess.reserve(len(msg.Payload)) {
		s.log.Warn("relayed RPC session is holding too much, closing it",
			"session", msg.SessionID, "limit", maxPendingBytes)
		s.forget(sess.id)
		sess.shutdown()
		return fmt.Errorf("session %s backlog too large", msg.SessionID)
	}

	select {
	case sess.inbound <- msg.Payload:
		return nil
	case <-sess.closed:
		sess.release(len(msg.Payload))
		return nil
	default:
		sess.release(len(msg.Payload))
		s.log.Warn("relayed RPC session is not reading, closing it", "session", msg.SessionID)
		s.forget(sess.id)
		sess.shutdown()
		return fmt.Errorf("session %s backlog full", msg.SessionID)
	}
}

func (s *Server) handleClose(_ context.Context, data json.RawMessage) error {
	var msg Close
	if err := json.Unmarshal(data, &msg); err != nil {
		return err
	}

	sess := s.lookup(msg.SessionID)
	if sess == nil {
		return nil
	}

	s.log.Debug("far end closed a relayed RPC session", "session", msg.SessionID, "reason", msg.Reason)
	s.forget(sess.id)
	sess.shutdown()

	return nil
}

func (s *Server) lookup(id string) *session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[id]
}

func (s *Server) forget(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

// send marshals and queues an envelope, waiting for room rather than dropping.
// A relayed session has no way to recover from a lost frame, so the uplink's
// best-effort path is not an option here.
func (s *Server) send(ctx context.Context, msgType string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", msgType, err)
	}
	return s.uplink.SendContext(ctx, &uplink.Envelope{Type: msgType, Data: raw})
}
