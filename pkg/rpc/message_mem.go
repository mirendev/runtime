package rpc

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
)

// memConn is an in-process MessageConn: two cross-wired buffered channels. It
// is reliable and ordered, satisfying msgmux's requirements with no network.
// Closing either end tears down both.
type memConn struct {
	send chan []byte
	recv chan []byte

	done    chan struct{}
	closeFn func()
}

func newMemPipe() (*memConn, *memConn) {
	ab := make(chan []byte, 64)
	ba := make(chan []byte, 64)
	done := make(chan struct{})
	var once sync.Once
	closeFn := func() { once.Do(func() { close(done) }) }

	a := &memConn{send: ab, recv: ba, done: done, closeFn: closeFn}
	b := &memConn{send: ba, recv: ab, done: done, closeFn: closeFn}
	return a, b
}

func (c *memConn) Send(b []byte) error {
	cp := make([]byte, len(b))
	copy(cp, b)
	select {
	case c.send <- cp:
		return nil
	case <-c.done:
		return net.ErrClosed
	}
}

func (c *memConn) Recv() ([]byte, error) {
	select {
	case b := <-c.recv:
		return b, nil
	case <-c.done:
		// Drain anything already buffered before reporting EOF.
		select {
		case b := <-c.recv:
			return b, nil
		default:
			return nil, io.EOF
		}
	}
}

func (c *memConn) Close() error {
	c.closeFn()
	return nil
}

// memRegistry maps a process-local name to a server State, mirroring the
// LocalActorRegistry pattern (actor.go). Dialing a name creates a memConn pair,
// spawns the server's session loop on one end, and returns the other end.
type memRegistry struct {
	mu      sync.Mutex
	servers map[string]*memServerEntry
}

type memServerEntry struct {
	state *State
	ctx   context.Context
}

var defaultMemRegistry = &memRegistry{servers: make(map[string]*memServerEntry)}

func (r *memRegistry) register(ctx context.Context, name string, s *State) {
	r.mu.Lock()
	r.servers[name] = &memServerEntry{state: s, ctx: ctx}
	r.mu.Unlock()

	go func() {
		<-ctx.Done()
		r.mu.Lock()
		if e := r.servers[name]; e != nil && e.state == s {
			delete(r.servers, name)
		}
		r.mu.Unlock()
	}()
}

func (r *memRegistry) dial(name string) (MessageConn, error) {
	r.mu.Lock()
	entry := r.servers[name]
	r.mu.Unlock()

	if entry == nil {
		return nil, fmt.Errorf("rpc: no mem server registered as %q", name)
	}

	clientEnd, serverEnd := newMemPipe()
	serverSess := newMsgSession(serverEnd, false, 0)

	router := &sessionRouter{state: entry.state, server: entry.state.server}
	go router.run(entry.ctx, serverSess) //nolint:errcheck // loop ends with the session

	return clientEnd, nil
}
