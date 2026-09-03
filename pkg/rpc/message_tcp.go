package rpc

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

// tcpFrameHeader is the 4-byte big-endian length prefix that turns a TCP byte
// stream into the discrete messages msgmux consumes.
const tcpFrameHeader = 4

// tcpConn is a MessageConn over a byte stream, framing each message with a
// length prefix. TLS is applied by the caller; this type only frames.
type tcpConn struct {
	conn net.Conn
	r    *bufio.Reader

	// maxFrame bounds an inbound frame. A peer announcing more than this is
	// rejected before anything is allocated, so a bad length cannot exhaust
	// memory.
	maxFrame int

	wmu sync.Mutex
	hdr [tcpFrameHeader]byte
}

func newTCPConn(conn net.Conn, maxFrame int) *tcpConn {
	if maxFrame <= 0 {
		maxFrame = defaultMaxFrameData
	}

	return &tcpConn{
		conn: conn,
		r:    bufio.NewReader(conn),
		// A frame carries a payload plus msgmux's own CBOR envelope, so allow
		// headroom over the payload bound rather than rejecting valid frames.
		maxFrame: maxFrame + frameReadHeadroom,
	}
}

// tcpDialTimeout bounds a TCP message-transport dial (connect plus TLS
// handshake). The dial runs under the transport mutex, so an unbounded one on
// an unresponsive peer would stall every other operation on the client.
const tcpDialTimeout = 30 * time.Second

// tcpHandshakeTimeout bounds the server-side TLS handshake for an accepted
// connection, so an idle peer can't pin resources before the session starts.
const tcpHandshakeTimeout = 10 * time.Second

func (t *tcpConn) Send(b []byte) error {
	t.wmu.Lock()
	defer t.wmu.Unlock()

	binary.BigEndian.PutUint32(t.hdr[:], uint32(len(b)))

	if _, err := t.conn.Write(t.hdr[:]); err != nil {
		return err
	}
	_, err := t.conn.Write(b)
	return err
}

func (t *tcpConn) Recv() ([]byte, error) {
	var hdr [tcpFrameHeader]byte
	if _, err := io.ReadFull(t.r, hdr[:]); err != nil {
		return nil, err
	}

	n := binary.BigEndian.Uint32(hdr[:])
	if int(n) > t.maxFrame {
		return nil, fmt.Errorf("rpc: peer announced a %d byte frame, over the %d byte limit", n, t.maxFrame)
	}

	buf := make([]byte, n)
	if _, err := io.ReadFull(t.r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func (t *tcpConn) Close() error {
	return t.conn.Close()
}

// startTCPListener serves RPC over raw TLS-over-TCP. There is no HTTP layer:
// each accepted connection is framed into messages and handed straight to the
// session router.
func (s *State) startTCPListener(ctx context.Context, addr string) error {
	tlsCfg := s.serverTlsCfg.Clone()
	tlsCfg.NextProtos = nil // no ALPN: nothing here speaks HTTP

	ln, err := tls.Listen("tcp", addr, tlsCfg)
	if err != nil {
		return err
	}

	s.msgLn = ln

	go func() {
		if s.contextOwnsShutdown(ctx) {
			_ = ln.Close()
		}
	}()

	s.log.Info("starting tcp message listener", "addr", ln.Addr().String())

	go func() {
		for {
			conn, aerr := ln.Accept()
			if aerr != nil {
				if ctx.Err() == nil {
					s.log.Error("tcp message listener stopped", "error", aerr)
				}
				return
			}

			go func() {
				// Bound the TLS handshake before starting the session. The
				// handshake is otherwise lazy, run inside the first Recv with no
				// deadline, so a peer that connects and sends nothing would pin a
				// goroutine and fd indefinitely (slowloris). Clear the deadline
				// once the handshake completes: msgmux has no per-operation
				// deadline and the session is long-lived.
				if tc, ok := conn.(*tls.Conn); ok {
					_ = tc.SetDeadline(time.Now().Add(tcpHandshakeTimeout))
					if herr := tc.HandshakeContext(ctx); herr != nil {
						s.log.Debug("rpc: tcp tls handshake failed", "error", herr)
						_ = conn.Close()
						return
					}
					_ = tc.SetDeadline(time.Time{})
				}

				mc := newTCPConn(conn, 0)
				if serr := s.ServeMessageConn(ctx, mc); serr != nil {
					s.log.Debug("rpc: tcp session ended", "error", serr)
				}
				_ = mc.Close()
			}()
		}
	}()

	return nil
}

// TCPListenAddr returns the address the raw TCP message listener is bound to,
// or an empty string if no such listener is running.
func (s *State) TCPListenAddr() string {
	if s.msgLn == nil {
		return ""
	}
	return s.msgLn.Addr().String()
}

// setupTCPTransport wires a client for a "tcp://host:port" remote.
func (c *NetworkClient) setupTCPTransport() {
	remote := strings.TrimPrefix(c.remote, "tcp://")

	tlsCfg := c.tlsCfg
	if tlsCfg == nil {
		tlsCfg = c.State.clientTlsCfg
	}

	cfg := tlsCfg.Clone()
	cfg.NextProtos = nil

	dialer := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: tcpDialTimeout},
		Config:    cfg,
	}

	c.ops = &msgOpTransport{
		owner: c,
		dial: func() (MessageConn, error) {
			ctx, cancel := context.WithTimeout(c.State.top, tcpDialTimeout)
			defer cancel()

			conn, err := dialer.DialContext(ctx, "tcp", remote)
			if err != nil {
				return nil, err
			}
			return newTCPConn(conn, 0), nil
		},
	}
}
