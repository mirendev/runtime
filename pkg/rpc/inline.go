package rpc

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/fxamacker/cbor/v2"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/webtransport"
)

const inlineStreamPoolSize = 10

type streamConn struct {
	stream webtransport.Stream
	enc    *cbor.Encoder
	dec    *cbor.Decoder
}

type inlineClient struct {
	log     *slog.Logger
	oid     OID
	ctrl    *controlStream
	session *webtransport.Session

	// Stream pool
	poolMu      sync.Mutex
	pool        chan *streamConn
	activeCount int // tracks total active streams (in use + in pool)
	closed      bool
}

// initPool initializes the stream pool
func (c *inlineClient) initPool() {
	c.poolMu.Lock()
	defer c.poolMu.Unlock()

	if c.pool != nil {
		return
	}

	c.pool = make(chan *streamConn, inlineStreamPoolSize)
}

// getStream gets a stream from the pool or creates a new one if pool is not full
func (c *inlineClient) getStream(ctx context.Context) (*streamConn, error) {
	// Ensure pool is initialized
	c.initPool()

	// Try to get an existing stream from the pool
	select {
	case conn := <-c.pool:
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

			return &streamConn{
				stream: str,
				enc:    cbor.NewEncoder(str),
				dec:    cbor.NewDecoder(str),
			}, nil
		}

		// Pool is at capacity, wait for a stream to become available
		select {
		case conn := <-c.pool:
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
	conn, err := c.getStream(ctx)
	if err != nil {
		return err
	}

	// Return stream to pool when done (unless there's an error)
	shouldReturn := true
	defer func() {
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
		return err
	}

	err = conn.enc.Encode(args)
	if err != nil {
		shouldReturn = false
		return err
	}

	var rr refResponse

	// Read response without timeout loop - let QUIC handle flow control
	if err := ctx.Err(); err != nil {
		shouldReturn = false
		return err
	}
	err = conn.dec.Decode(&rr)
	if err != nil {
		shouldReturn = false
		return err
	}

	switch rr.Status {
	case "error":
		return cond.RemoteError(rr.Category, rr.Code, rr.Error)
	case "ok":
		return conn.dec.Decode(ret)
	default:
		if err := ctx.Err(); err != nil {
			return err
		}

		return fmt.Errorf("unknown response status to %s/%s: %s", c.oid, method, rr.Status)
	}
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
	close(c.pool)
	for conn := range c.pool {
		_ = conn.stream.Close()
		c.activeCount--
	}

	return nil
}
