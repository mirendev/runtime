package packet

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"miren.dev/runtime/pkg/idgen"
)

type multiplexAddr struct {
	id string
}

func (a multiplexAddr) Network() string {
	return "multiplex"
}

func (a multiplexAddr) String() string {
	return a.id
}

// connection wraps an individual io.ReadWriteCloser with its metadata
type connection struct {
	rw     io.ReadWriteCloser
	raddr  net.Addr
	closed atomic.Int32

	wmu sync.Mutex
}

// PacketConnMultiplex implements net.PacketConn by managing multiple io.ReadWriteClosers
// and routing packets based on remote addresses.
type PacketConnMultiplex struct {
	ctx      context.Context
	mu       sync.RWMutex
	conns    map[string]*connection
	readChan chan packet
	closed   atomic.Int32

	bufpool sync.Pool
}

type bufPE struct {
	buf []byte
}

// packet represents a received packet and its metadata
type packet struct {
	data []byte
	addr net.Addr

	// currently unused because we emulate the behavior of a UDP based PacketConn
	err error
}

// NewPacketConnMultiplex creates a new PacketConnMultiplex.
func NewPacketConnMultiplex(ctx context.Context) *PacketConnMultiplex {
	p := &PacketConnMultiplex{
		ctx:      ctx,
		conns:    make(map[string]*connection),
		readChan: make(chan packet, 32), // Buffered channel for received packets
	}
	return p
}

// AddConn adds a new connection to the adapter.
// Returns an error if a connection with the same remote address already exists.
func (p *PacketConnMultiplex) AddConn(rw io.ReadWriteCloser) (net.Addr, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed.Load() == 1 {
		return nil, net.ErrClosed
	}

	addr := &multiplexAddr{id: idgen.Gen("c") + ":0"}

	key := addr.String()

	if _, exists := p.conns[key]; exists {
		return nil, fmt.Errorf("connection already exists for address: %s", key)
	}

	conn := &connection{
		rw:    rw,
		raddr: addr,
	}
	p.conns[key] = conn

	// Start a goroutine to read packets from this connection
	go p.readLoop(conn)

	return addr, nil
}

// RemoveConn removes and closes a connection.
func (p *PacketConnMultiplex) RemoveConn(remoteAddr net.Addr) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := remoteAddr.String()
	conn, exists := p.conns[key]
	if !exists {
		return fmt.Errorf("no connection exists for address: %s", key)
	}

	conn.closed.Store(1)
	delete(p.conns, key)
	return conn.rw.Close()
}

const MaxPacketSize = 64 * 1024

// readLoop continuously reads packets from a connection and sends them to readChan
func (p *PacketConnMultiplex) readLoop(conn *connection) {
	rb := make([]byte, 4)

	for {
		var next packet

		_, err := io.ReadFull(conn.rw, rb)
		if err != nil {
			if conn.closed.Load() == 1 {
				return
			}

			// We emulate the behavior of a UDP based PacketConn here, where
			// there are no read errors, just silence.
			p.RemoveConn(conn.raddr)

			return
		}

		length := binary.BigEndian.Uint32(rb)

		if length > MaxPacketSize {
			// Even with jumbo frames, etc, there is no reason we'd see
			// a packed this big, considering we're emulating UDP style
			// diagrams here. So we assume it's someone attempting to
			// DoS us.
			p.RemoveConn(conn.raddr)
			return
		}

		var data []byte
		v := p.bufpool.Get()

		if v == nil {
			data = make([]byte, max(length, 2048))
		} else {
			data = v.(*bufPE).buf
			if len(data) < int(length) {
				data = make([]byte, max(length, 2048))
			}
		}

		buf := data[:length]

		_, err = io.ReadFull(conn.rw, buf)
		if err != nil {
			if conn.closed.Load() == 1 {
				return
			}

			// We emulate the behavior of a UDP based PacketConn here, where
			// there are no read errors, just silence.

			p.RemoveConn(conn.raddr)

			return
		}

		next = packet{buf, conn.raddr, nil}

		select {
		case p.readChan <- next:
		// ok
		case <-p.ctx.Done():
			return
		}
	}
}

// ReadFrom implements net.PacketConn.ReadFrom.
func (p *PacketConnMultiplex) ReadFrom(b []byte) (n int, addr net.Addr, err error) {
	if p.closed.Load() == 1 {
		return 0, nil, net.ErrClosed
	}

	// Wait for a packet from any connection
	select {
	case <-p.ctx.Done():
		return 0, nil, p.ctx.Err()
	case pkt := <-p.readChan:
		if pkt.err != nil {
			return 0, pkt.addr, pkt.err
		}

		if len(pkt.data) > len(b) {
			return 0, pkt.addr, io.ErrShortBuffer
		}

		n = copy(b, pkt.data)

		p.bufpool.Put(&bufPE{
			buf: pkt.data[:cap(pkt.data)],
		})

		return n, pkt.addr, nil
	}
}

// WriteTo implements net.PacketConn.WriteTo.
func (p *PacketConnMultiplex) WriteTo(b []byte, addr net.Addr) (n int, err error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed.Load() == 1 {
		return 0, net.ErrClosed
	}

	conn, exists := p.conns[addr.String()]
	if !exists {
		// We emulate the behavior of a UDP based PacketConn here, which has no idea
		// if the remote side is alive or not.
		return len(b), nil // fmt.Errorf("no connection exists for address: %s", addr.String())
	}

	var wb [4]byte

	binary.BigEndian.PutUint32(wb[:], uint32(len(b)))

	conn.wmu.Lock()
	defer conn.wmu.Unlock()

	if _, err := conn.rw.Write(wb[:]); err != nil {
		return 0, err
	}

	return conn.rw.Write(b)
}

// Close implements net.PacketConn.Close.
func (p *PacketConnMultiplex) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed.Load() == 1 {
		return net.ErrClosed
	}

	p.closed.Store(1)
	close(p.readChan)

	var lastErr error
	for _, conn := range p.conns {
		if err := conn.rw.Close(); err != nil {
			lastErr = err
		}
		conn.closed.Store(1)
	}
	p.conns = nil

	return lastErr
}

// LocalAddr implements net.PacketConn.LocalAddr.
func (p *PacketConnMultiplex) LocalAddr() net.Addr {
	return &multiplexAddr{id: "multiplex:0"}
}

// SetDeadline implements net.PacketConn.SetDeadline.
func (p *PacketConnMultiplex) SetDeadline(t time.Time) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var lastErr error
	for _, conn := range p.conns {
		if setter, ok := conn.rw.(interface{ SetDeadline(time.Time) error }); ok {
			if err := setter.SetDeadline(t); err != nil {
				lastErr = err
			}
		}
	}
	return lastErr
}

// SetReadDeadline implements net.PacketConn.SetReadDeadline.
func (p *PacketConnMultiplex) SetReadDeadline(t time.Time) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var lastErr error
	for _, conn := range p.conns {
		if setter, ok := conn.rw.(interface{ SetReadDeadline(time.Time) error }); ok {
			if err := setter.SetReadDeadline(t); err != nil {
				lastErr = err
			}
		}
	}
	return lastErr
}

// SetWriteDeadline implements net.PacketConn.SetWriteDeadline.
func (p *PacketConnMultiplex) SetWriteDeadline(t time.Time) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var lastErr error
	for _, conn := range p.conns {
		if setter, ok := conn.rw.(interface{ SetWriteDeadline(time.Time) error }); ok {
			if err := setter.SetWriteDeadline(t); err != nil {
				lastErr = err
			}
		}
	}
	return lastErr
}
