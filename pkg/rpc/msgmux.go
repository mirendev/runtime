package rpc

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"

	"github.com/fxamacker/cbor/v2"
)

// msgmux is a minimal stream multiplexer that implements rpcSession/rpcStream
// over a MessageConn. It is to a MessageConn what yamux is to a byte conn, but
// because a MessageConn already delivers reliable, ordered, discrete messages,
// msgmux needs no windowing: each frame is one message, and the backend's own
// backpressure governs throughput.
//
// It does need a ceiling, which is a different thing. The backend's
// backpressure stops at the point a frame is handed over; after that the bytes
// wait in a stream's read buffer for a handler to read them. A peer that sends
// faster than its handler consumes — or goes on sending after the handler has
// stopped reading — grows that buffer with nothing to stop it, and a transport
// that reclaims its own accounting when it hands a frame over would be
// advertising a limit it no longer enforces. maxBuffered is that ceiling:
// exceed it and the session is torn down, because there is no way to apply
// backpressure retroactively to bytes already delivered.
//
// Frame flags. A stream is opened with an eager SYN (so AcceptStream fires even
// if the opener reads before writing); data frames carry DATA; a clean close
// sends FIN; CancelRead/abort sends RST.
const (
	flagSYN  uint8 = 1 << 0
	flagDATA uint8 = 1 << 1
	flagFIN  uint8 = 1 << 2
	flagRST  uint8 = 1 << 3
)

type msgFrame struct {
	Stream uint32 `cbor:"0,keyasint"`
	Flags  uint8  `cbor:"1,keyasint"`
	Data   []byte `cbor:"2,keyasint,omitempty"`
}

var (
	errSessionClosed = errors.New("rpc: message session closed")
	errStreamReset   = errors.New("rpc: message stream reset")
	errStreamCancel  = errors.New("rpc: message stream read canceled")

	// errSessionOverBuffered ends a session holding more delivered-but-unread
	// data than it is allowed to. Named rather than generic because it is the
	// one teardown here that says something about the peer's behaviour instead
	// of the link's.
	errSessionOverBuffered = errors.New("rpc: message session buffered too much unread data")
)

type msgSession struct {
	conn MessageConn

	// maxFrame bounds the payload in one frame, so a backend that caps message
	// size can still carry arbitrarily large stream writes.
	maxFrame int

	sendMu sync.Mutex

	// bufMu guards buffered, the total delivered-but-unread bytes held across
	// every stream on this session. Its own lock rather than mu: it is touched
	// on each frame in and each read out, and those paths already hold a
	// stream's readMu, so sharing the session lock would put the frame path
	// behind stream table work.
	bufMu       sync.Mutex
	buffered    int
	maxBuffered int

	mu       sync.Mutex
	streams  map[uint32]*msgStream
	nextID   uint32
	idStep   uint32
	closed   bool
	closeErr error

	accept chan *msgStream
	done   chan struct{}
}

// newMsgSession wraps a MessageConn. Exactly one side must pass client=true
// (the dialer); it uses odd stream ids while the accepter uses even ids, so ids
// never collide. A maxFrame of zero uses defaultMaxFrameData, and a maxBuffered
// of zero uses defaultMaxBufferedData.
func newMsgSession(conn MessageConn, client bool, maxFrame, maxBuffered int) *msgSession {
	var start uint32 = 2
	if client {
		start = 1
	}
	if maxFrame <= 0 {
		maxFrame = defaultMaxFrameData
	}
	if maxBuffered <= 0 {
		maxBuffered = defaultMaxBufferedData
	}
	s := &msgSession{
		conn:        conn,
		maxFrame:    maxFrame,
		maxBuffered: maxBuffered,
		streams:     make(map[uint32]*msgStream),
		nextID:      start,
		idStep:      2,
		accept:      make(chan *msgStream, 16),
		done:        make(chan struct{}),
	}
	go s.readPump()
	return s
}

func (s *msgSession) newStream(id uint32) *msgStream {
	st := &msgStream{sess: s, id: id}
	st.readCond = sync.NewCond(&st.readMu)
	return st
}

func (s *msgSession) sendFrame(f msgFrame) error {
	data, err := cbor.Marshal(f)
	if err != nil {
		return err
	}
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	return s.conn.Send(data)
}

func (s *msgSession) OpenStreamSync(ctx context.Context) (rpcStream, error) {
	// A already-cancelled caller shouldn't open a stream. The subsequent
	// sendFrame still blocks on an unresponsive conn.Send (a MessageConn has no
	// context), so this bounds the common case, not a mid-send stall.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.Lock()
	if s.closed {
		err := s.closeErr
		s.mu.Unlock()
		return nil, err
	}
	id := s.nextID
	s.nextID += s.idStep
	st := s.newStream(id)
	s.streams[id] = st
	s.mu.Unlock()

	if err := s.sendFrame(msgFrame{Stream: id, Flags: flagSYN}); err != nil {
		s.removeStream(id)
		return nil, err
	}
	st.synSent = true
	return st, nil
}

func (s *msgSession) AcceptStream(ctx context.Context) (rpcStream, error) {
	select {
	case st := <-s.accept:
		return st, nil
	case <-s.done:
		return nil, s.closeErr
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *msgSession) Close() error {
	// Mark the session closed before closing the conn, rather than waiting for
	// readPump to notice the conn error asynchronously. Otherwise OpenStreamSync
	// could register a stream after Close, and AcceptStream or blocked readers
	// could stay parked until the conn error propagates. shutdown is idempotent,
	// so readPump calling it again on the conn error is harmless.
	s.shutdown(errSessionClosed)
	return s.conn.Close()
}

// reserveBuffered accounts for bytes about to be buffered for a reader, and
// reports whether the session has room. A refusal is fatal to the session:
// there is no way to push back on a frame already delivered, and dropping it
// silently would leave the stream with a hole RPC cannot resynchronise after.
func (s *msgSession) reserveBuffered(n int) bool {
	s.bufMu.Lock()
	defer s.bufMu.Unlock()

	if s.buffered+n > s.maxBuffered {
		return false
	}
	s.buffered += n
	return true
}

// releaseBuffered gives back what a reader took, or what died with a stream.
func (s *msgSession) releaseBuffered(n int) {
	if n == 0 {
		return
	}
	s.bufMu.Lock()
	defer s.bufMu.Unlock()
	s.buffered -= n
}

func (s *msgSession) removeStream(id uint32) {
	s.mu.Lock()
	st := s.streams[id]
	delete(s.streams, id)
	s.mu.Unlock()

	// Whatever this stream still held is gone with it. Without this the
	// session's total only ever climbs, and a long-lived connection running
	// many short operations would eventually trip a limit while holding
	// nothing at all.
	if st != nil {
		st.discardBuffered()
	}
}

func (s *msgSession) readPump() {
	for {
		data, err := s.conn.Recv()
		if err != nil {
			s.shutdown(err)
			return
		}

		var f msgFrame
		if err := cbor.Unmarshal(data, &f); err != nil {
			s.shutdown(err)
			return
		}

		s.handleFrame(f)
	}
}

func (s *msgSession) handleFrame(f msgFrame) {
	s.mu.Lock()
	st := s.streams[f.Stream]
	newStream := false
	if st == nil && f.Flags&flagSYN != 0 && !s.closed {
		st = s.newStream(f.Stream)
		st.synSent = true // peer opened it; we never send a SYN for it
		s.streams[f.Stream] = st
		newStream = true
	}
	s.mu.Unlock()

	if st == nil {
		return // data for an unknown, un-SYN'd stream: drop
	}

	if newStream {
		select {
		case s.accept <- st:
		case <-s.done:
			return
		}
	}

	if f.Flags&flagRST != 0 {
		st.fail(errStreamReset)
		s.removeStream(f.Stream)
		return
	}

	if f.Flags&(flagDATA|flagFIN) != 0 {
		if !st.feed(f.Data, f.Flags&flagFIN != 0) {
			// The whole session goes, not just this stream. The peer has put
			// more unread data on us than the session may hold, and the only
			// lever left is to stop reading from it entirely.
			s.shutdown(errSessionOverBuffered)
			return
		}
	}

	if f.Flags&flagFIN != 0 {
		s.maybeRemove(st)
	}
}

func (s *msgSession) shutdown(err error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.closeErr = err
	streams := make([]*msgStream, 0, len(s.streams))
	for _, st := range s.streams {
		streams = append(streams, st)
	}
	s.streams = map[uint32]*msgStream{}
	close(s.done)
	s.mu.Unlock()

	for _, st := range streams {
		st.fail(err)
	}
}

// maybeRemove drops a stream from the table once both directions are finished,
// so a long-lived session multiplexing many short ops does not leak entries.
func (s *msgSession) maybeRemove(st *msgStream) {
	st.readMu.Lock()
	remoteDone := st.remoteClosed
	st.readMu.Unlock()

	st.wmu.Lock()
	localDone := st.localClosed
	st.wmu.Unlock()

	if remoteDone && localDone {
		s.removeStream(st.id)
	}
}

type msgStream struct {
	sess *msgSession
	id   uint32

	readMu       sync.Mutex
	readCond     *sync.Cond
	buf          bytes.Buffer
	remoteClosed bool
	readErr      error
	canceled     bool
	// buffered is what this stream currently accounts for in the session's
	// total. Tracked per stream so releasing is idempotent by construction:
	// bytes leave through a read or through teardown, and either way the count
	// they are subtracted from is the same one they were added to.
	buffered int

	wmu         sync.Mutex
	synSent     bool
	localClosed bool
	// writeErr, once set, fails all further writes. It is set when the stream is
	// reset or cancelled from either side, so a peer RST or a local CancelRead
	// stops us writing into a stream the peer has already dropped.
	writeErr error
}

// feed hands delivered bytes to the stream's reader, and reports whether the
// session had room for them. A false return means the session is over its
// buffered-data ceiling; the caller tears it down rather than proceeding,
// because a frame that cannot be buffered also cannot be refused after the
// fact.
func (st *msgStream) feed(data []byte, fin bool) bool {
	if len(data) > 0 && !st.sess.reserveBuffered(len(data)) {
		return false
	}

	st.readMu.Lock()
	if len(data) > 0 {
		st.buf.Write(data)
		st.buffered += len(data)
	}
	if fin {
		st.remoteClosed = true
	}
	st.readCond.Broadcast()
	st.readMu.Unlock()

	return true
}

// discardBuffered releases everything this stream still accounts for, for a
// stream being dropped with data its reader will never take.
func (st *msgStream) discardBuffered() {
	st.readMu.Lock()
	n := st.buffered
	st.buffered = 0
	st.buf.Reset()
	st.readMu.Unlock()

	st.sess.releaseBuffered(n)
}

func (st *msgStream) fail(err error) {
	st.readMu.Lock()
	if st.readErr == nil {
		st.readErr = err
	}
	st.readCond.Broadcast()
	st.readMu.Unlock()

	// A reset or a dead session must stop writes too, not just reads: the peer
	// has dropped the stream, so further frames would be silently discarded.
	st.failWrite(err)
}

func (st *msgStream) failWrite(err error) {
	st.wmu.Lock()
	if st.writeErr == nil {
		st.writeErr = err
	}
	st.wmu.Unlock()
}

func (st *msgStream) Read(p []byte) (int, error) {
	st.readMu.Lock()

	for st.buf.Len() == 0 {
		switch {
		case st.canceled:
			st.readMu.Unlock()
			return 0, errStreamCancel
		case st.buf.Len() == 0 && st.readErr != nil:
			err := st.readErr
			st.readMu.Unlock()
			return 0, err
		case st.remoteClosed:
			st.readMu.Unlock()
			return 0, io.EOF
		}
		st.readCond.Wait()
	}

	n, err := st.buf.Read(p)
	st.buffered -= n
	st.readMu.Unlock()

	// Released outside readMu: the session's own lock is the inner one on the
	// feed path too, and taking them in one order everywhere is what keeps
	// that true.
	st.sess.releaseBuffered(n)

	return n, err
}

func (st *msgStream) Write(p []byte) (int, error) {
	st.wmu.Lock()
	defer st.wmu.Unlock()

	if st.writeErr != nil {
		return 0, st.writeErr
	}
	if st.localClosed {
		return 0, errSessionClosed
	}

	total := 0
	for len(p) > 0 {
		chunk := p
		if len(chunk) > st.sess.maxFrame {
			chunk = chunk[:st.sess.maxFrame]
		}

		flags := flagDATA
		if !st.synSent {
			flags |= flagSYN
			st.synSent = true
		}

		if err := st.sess.sendFrame(msgFrame{Stream: st.id, Flags: flags, Data: chunk}); err != nil {
			return total, err
		}

		total += len(chunk)
		p = p[len(chunk):]
	}

	return total, nil
}

func (st *msgStream) Close() error {
	st.wmu.Lock()
	if st.localClosed {
		st.wmu.Unlock()
		return nil
	}
	st.localClosed = true
	flags := flagFIN
	if !st.synSent {
		flags |= flagSYN
		st.synSent = true
	}
	st.wmu.Unlock()

	err := st.sess.sendFrame(msgFrame{Stream: st.id, Flags: flags})
	st.sess.maybeRemove(st)
	return err
}

// CancelRead aborts a Read currently blocked on this stream and signals the
// peer (RST). The code is unused: msgmux has no per-stream error code, so
// callers detect that the cancellation was caller-initiated by inspecting their
// context.
func (st *msgStream) CancelRead(code uint64) {
	st.readMu.Lock()
	st.canceled = true
	st.readCond.Broadcast()
	st.readMu.Unlock()

	st.failWrite(errStreamCancel)

	_ = st.sess.sendFrame(msgFrame{Stream: st.id, Flags: flagRST})

	// A local RST is a hard teardown: the peer drops its side and never sends a
	// FIN back, so maybeRemove (which waits for both directions) would never
	// fire. Remove the entry here to avoid leaking it for the session's life.
	st.sess.removeStream(st.id)
}
