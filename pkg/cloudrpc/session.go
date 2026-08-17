package cloudrpc

import (
	"context"
	"io"
	"sync"
)

// session presents one relayed conversation as an rpc.MessageConn: a reliable,
// ordered, bidirectional pipe of discrete byte messages. Ordering comes from the
// hops underneath — one WebSocket, one queue, one WebSocket — and each frame
// arrives whole because the envelope carries it whole.
type session struct {
	id  string
	srv *Server

	// ctx is the uplink connection's context, so the session dies with the link
	// rather than lingering as a pipe to nowhere.
	ctx context.Context

	inbound chan []byte

	// pending is the total size of the frames queued on inbound. The channel's
	// depth bounds how many frames may wait; this bounds how much memory they
	// may hold, which is the quantity that actually matters — the far end
	// chooses the frame size, so a count says nothing about the cost.
	pendingMu sync.Mutex
	pending   int

	closeOnce sync.Once
	closed    chan struct{}
}

// reserve accounts for a frame about to be queued, and reports whether the
// session has room for it.
func (s *session) reserve(n int) bool {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()

	if s.pending+n > maxPendingBytes {
		return false
	}
	s.pending += n
	return true
}

// release accounts for a frame the reader has taken.
func (s *session) release(n int) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	s.pending -= n
}

// Send wraps a frame and waits for room on the uplink. Blocking here is how
// backpressure reaches the layer above: msgmux serialises Send, so a congested
// link slows the writer instead of piling up unsent frames.
func (s *session) Send(b []byte) error {
	return s.srv.send(s.ctx, TypeData, Data{SessionID: s.id, Payload: b})
}

// Recv returns the next frame, or io.EOF once the far end is gone and nothing
// buffered remains.
func (s *session) Recv() ([]byte, error) {
	// Buffered frames outrank a close that has already been signalled: the far
	// end may have sent its last reply and then hung up, and returning EOF while
	// that reply sits in the channel would lose it.
	select {
	case b := <-s.inbound:
		s.release(len(b))
		return b, nil
	default:
	}

	select {
	case b := <-s.inbound:
		s.release(len(b))
		return b, nil
	case <-s.closed:
		return nil, io.EOF
	case <-s.ctx.Done():
		return nil, io.EOF
	}
}

// Close ends the session locally. The far end is told by whoever tore it down —
// see Server.serve — rather than from here, because rpc closes the conn it was
// handed as part of ordinary teardown and a notice per close would be noise.
func (s *session) Close() error {
	s.shutdown()
	return nil
}

func (s *session) shutdown() {
	s.closeOnce.Do(func() { close(s.closed) })
}

// Remote names this session's far end for the audit trail.
//
// There is no address to give: the caller reached us through cloud, over a link
// this cluster dialed outbound, so the only thing the socket knows about is
// cloud. The session id is what actually distinguishes one caller from another
// here, and it is the identifier cloud logs on its side too, which is what makes
// the two halves of a relayed call joinable after the fact.
//
// Deliberately not address-shaped. An audit line that looked like a peer address
// would be claiming knowledge this transport does not have.
func (s *session) Remote() string {
	return "cloud-relay/" + s.id
}
