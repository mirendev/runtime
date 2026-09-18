package rpc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/fxamacker/cbor/v2"
	"miren.dev/runtime/pkg/cond"
)

var errInlineClientClosed = errors.New("inline client closed")

const inlineStreamPoolSize = 10

type streamConn struct {
	stream rpcStream
	enc    *cbor.Encoder
	dec    *cbor.Decoder
}

type inlineClient struct {
	log     *slog.Logger
	oid     OID
	ctrl    *controlStream
	session rpcSession

	// prelude reports whether each newly opened stream must lead with an
	// opRequest, as message-transport sessions require so that one accept loop
	// can route both operations and callbacks.
	prelude bool

	// Stream pool
	poolMu      sync.Mutex
	pool        chan *streamConn
	activeCount int // tracks total active streams (in use + in pool)
	closed      bool
}

// initPool initializes the stream pool
func (c *inlineClient) initPool() error {
	c.poolMu.Lock()
	defer c.poolMu.Unlock()

	if c.closed {
		return errInlineClientClosed
	}

	if c.pool != nil {
		return nil
	}

	c.pool = make(chan *streamConn, inlineStreamPoolSize)
	return nil
}

// getStream gets a stream from the pool or creates a new one if pool is not full
func (c *inlineClient) getStream(ctx context.Context) (*streamConn, error) {
	// Ensure pool is initialized
	if err := c.initPool(); err != nil {
		return nil, err
	}

	// Try to get an existing stream from the pool
	select {
	case conn, ok := <-c.pool:
		if !ok {
			return nil, errInlineClientClosed
		}
		return conn, nil
	default:
		// Pool is empty, check if we can create a new stream
		c.poolMu.Lock()
		canCreate := c.activeCount < inlineStreamPoolSize
		if canCreate {
			c.activeCount++
		}
		c.poolMu.Unlock()

		if canCreate {
			// We can create a new stream
			str, err := c.session.OpenStreamSync(ctx)
			if err != nil {
				// Decrement counter on error
				c.poolMu.Lock()
				c.activeCount--
				c.poolMu.Unlock()
				return nil, err
			}

			conn := &streamConn{
				stream: str,
				enc:    cbor.NewEncoder(str),
				dec:    cbor.NewDecoder(str),
			}

			// The prelude is written once per stream, not once per call: the
			// stream is pooled and carries many calls against this capability.
			if c.prelude {
				prelude := opRequest{
					Op:      opInlineCall,
					OID:     c.oid,
					Version: currentProtocolVersion,
				}
				if err := conn.enc.Encode(prelude); err != nil {
					_ = str.Close()
					c.poolMu.Lock()
					c.activeCount--
					c.poolMu.Unlock()
					return nil, err
				}
			}

			return conn, nil
		}

		// Pool is at capacity, wait for a stream to become available
		select {
		case conn, ok := <-c.pool:
			if !ok {
				return nil, errInlineClientClosed
			}
			return conn, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// returnStream returns a stream to the pool for reuse
func (c *inlineClient) returnStream(conn *streamConn) {
	c.poolMu.Lock()
	defer c.poolMu.Unlock()

	if c.closed {
		_ = conn.stream.Close()
		c.activeCount--
		return
	}

	// Try to return to pool
	select {
	case c.pool <- conn:
		// Successfully returned to pool (activeCount stays the same)
	default:
		// Pool is full, close the stream
		_ = conn.stream.Close()
		c.activeCount--
	}
}

func (c *inlineClient) Call(ctx context.Context, method string, args any, ret any) error {
	// A ctx that is already done must not reach the peer at all. The pool
	// hands out a stream without consulting ctx, and the watcher below only
	// starts after that, so the request could be encoded and sent before
	// CancelRead fails the write side, and the peer would run a call whose
	// caller was told it never happened.
	if err := ctx.Err(); err != nil {
		return err
	}

	conn, err := c.getStream(ctx)
	if err != nil {
		return err
	}

	// Bridge the caller's ctx to the stream for the life of this call. A
	// blocked Read is only unblocked by transport teardown or CancelRead, so
	// without this a cancelled caller parks in dec.Decode until the peer
	// finally replies. Mirrors handleCallStream and msgOpTransport.roundTrip.
	//
	// The watcher must be fully retired before the stream can go back in the
	// pool. Signalling it is not enough: if ctx fires in the same instant as
	// cleanup, both cases are ready and select may still take ctx.Done after
	// the stream has been handed to another caller. So cleanup waits on done,
	// and a stream the watcher did cancel is closed rather than pooled, since
	// a cancelled msgStream stays cancelled.
	stop := make(chan struct{})
	done := make(chan struct{})
	fired := false
	if ctx.Done() != nil {
		go func() {
			defer close(done)
			select {
			case <-ctx.Done():
				fired = true
				conn.stream.CancelRead(cancelReadCode)
			case <-stop:
			}
		}()
	} else {
		close(done)
	}

	// Return stream to pool when done (unless there's an error)
	shouldReturn := true
	defer func() {
		close(stop)
		<-done
		if fired {
			shouldReturn = false
		}
		if shouldReturn {
			c.returnStream(conn)
		} else {
			_ = conn.stream.Close()
			c.poolMu.Lock()
			c.activeCount--
			c.poolMu.Unlock()
		}
	}()

	err = conn.enc.Encode(streamRequest{
		Kind:   "call",
		OID:    c.oid,
		Method: method,
	})
	if err != nil {
		shouldReturn = false
		return callErr(ctx, err)
	}

	err = conn.enc.Encode(args)
	if err != nil {
		shouldReturn = false
		return callErr(ctx, err)
	}

	var rr refResponse

	err = conn.dec.Decode(&rr)
	if err != nil {
		shouldReturn = false
		return callErr(ctx, err)
	}

	switch rr.Status {
	case "error":
		return cond.RemoteError(rr.Category, rr.Code, rr.Error)
	case "ok":
		if err := conn.dec.Decode(ret); err != nil {
			shouldReturn = false
			return callErr(ctx, err)
		}
		return nil
	default:
		if err := ctx.Err(); err != nil {
			return err
		}

		return fmt.Errorf("unknown response status to %s/%s: %s", c.oid, method, rr.Status)
	}
}

// callErr maps a failed Encode or Decode back to the caller's context error
// when our own CancelRead is what aborted it, so callers see a context error
// rather than a transport-specific stream-cancel error. Writes need it too:
// on msgStream, CancelRead fails the write side as well, so a cancellation
// that lands before the request is sent surfaces from Encode.
func callErr(ctx context.Context, err error) error {
	if cerr := ctx.Err(); cerr != nil {
		return cerr
	}
	return err
}

func (c *inlineClient) derefOID(ctx context.Context, oid OID) error {
	err := c.ctrl.NoReply(streamRequest{
		Kind: "deref",
		OID:  oid,
	}, nil)

	return err
}

// Close closes all streams in the pool
func (c *inlineClient) Close() error {
	c.poolMu.Lock()
	defer c.poolMu.Unlock()

	c.closed = true

	if c.pool == nil {
		return nil
	}

	// Close all streams in the pool
	ch := c.pool
	c.pool = nil
	close(ch)
	for conn := range ch {
		_ = conn.stream.Close()
		c.activeCount--
	}

	return nil
}
