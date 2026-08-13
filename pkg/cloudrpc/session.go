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

	closeOnce sync.Once
	closed    chan struct{}
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
		return b, nil
	default:
	}

	select {
	case b := <-s.inbound:
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
