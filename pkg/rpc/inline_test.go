package rpc

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/stretchr/testify/require"
)

// The peer accepts the callback stream and then stalls without replying. A
// caller with a deadline must come back with the context error instead of
// parking in Decode until the peer or the session gives up.
func TestInlineClientCallHonorsContext(t *testing.T) {
	r := require.New(t)

	ca, cb := newMemPipe()
	client := newMsgSession(ca, true, 0, 0)
	peer := newMsgSession(cb, false, 0, 0)
	t.Cleanup(func() {
		_ = client.Close()
		_ = peer.Close()
	})

	accepted := make(chan struct{})
	go func() {
		st, err := peer.AcceptStream(context.Background())
		if err != nil {
			return
		}
		close(accepted)
		// Drain the request so the client's writes complete, then never
		// answer. Stop reading once the stream is reset from the far side.
		_, _ = io.Copy(io.Discard, st)
	}()

	ic := &inlineClient{
		log:     slog.Default(),
		oid:     OID("cap"),
		session: client,
	}

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	var out struct{}
	err := ic.Call(ctx, "stall", struct{}{}, &out)
	elapsed := time.Since(start)

	r.Error(err)
	r.True(errors.Is(err, context.DeadlineExceeded), "want context error, got %v", err)
	r.Less(elapsed, 2*time.Second, "Call hung past context cancellation")

	<-accepted

	// The half-read stream must not be handed to the next caller.
	ic.poolMu.Lock()
	r.Equal(0, ic.activeCount)
	r.Len(ic.pool, 0)
	ic.poolMu.Unlock()
}

// serveInlineOK answers every inline request on every stream the peer accepts
// with an "ok" response, so the client side can be exercised without a real
// capability behind it.
func serveInlineOK(t *testing.T, peer rpcSession) {
	t.Helper()
	go func() {
		for {
			st, err := peer.AcceptStream(context.Background())
			if err != nil {
				return
			}
			go func() {
				dec := cbor.NewDecoder(st)
				enc := cbor.NewEncoder(st)
				for {
					var req streamRequest
					if err := dec.Decode(&req); err != nil {
						return
					}
					var args cbor.RawMessage
					if err := dec.Decode(&args); err != nil {
						return
					}
					if err := enc.Encode(refResponse{Status: "ok"}); err != nil {
						return
					}
					if err := enc.Encode(struct{}{}); err != nil {
						return
					}
				}
			}()
		}
	}()
}

// A call that completes normally must leave the stream reusable: the ctx
// watcher is stopped on return, so cancelling afterwards must not reset a
// stream that is back in the pool.
func TestInlineClientCallStopsWatcherOnReturn(t *testing.T) {
	r := require.New(t)

	ca, cb := newMemPipe()
	client := newMsgSession(ca, true, 0, 0)
	peer := newMsgSession(cb, false, 0, 0)
	t.Cleanup(func() {
		_ = client.Close()
		_ = peer.Close()
	})
	serveInlineOK(t, peer)

	ic := &inlineClient{
		log:     slog.Default(),
		oid:     OID("cap"),
		session: client,
	}

	ctx, cancel := context.WithCancel(t.Context())
	var out struct{}
	r.NoError(ic.Call(ctx, "ok", struct{}{}, &out))
	cancel()

	// Second call reuses the pooled stream; a leaked watcher from the first
	// call would have reset it on cancel() and this would fail.
	r.NoError(ic.Call(t.Context(), "ok", struct{}{}, &out))
}

// hookedSession wraps a real session so the first stream it opens can run a
// hook at a chosen point in its Read stream. Later streams pass through.
type hookedSession struct {
	rpcSession
	once   sync.Once
	stream *hookedStream
}

func (h *hookedSession) OpenStreamSync(ctx context.Context) (rpcStream, error) {
	st, err := h.rpcSession.OpenStreamSync(ctx)
	if err != nil {
		return nil, err
	}
	wrapped := st
	h.once.Do(func() {
		h.stream.rpcStream = st
		wrapped = h.stream
	})
	return wrapped, nil
}

// hookedStream calls afterRead once, on the Read that brings the total bytes
// read to at least afterBytes, before handing those bytes to the caller. It
// also records that CancelRead was called.
type hookedStream struct {
	rpcStream
	afterBytes int
	afterRead  func()

	read      int
	fired     bool
	cancelled chan struct{}
	cancelOne sync.Once
}

func (h *hookedStream) Read(p []byte) (int, error) {
	n, err := h.rpcStream.Read(p)
	h.read += n
	if !h.fired && h.read >= h.afterBytes {
		h.fired = true
		h.afterRead()
	}
	return n, err
}

func (h *hookedStream) CancelRead(code uint64) {
	h.rpcStream.CancelRead(code)
	h.cancelOne.Do(func() { close(h.cancelled) })
}

// Cancellation that lands after the response has been read but before cleanup
// runs must not put the stream back in the pool. Signalling the watcher is not
// enough: it has already taken ctx.Done and cancelled the stream, and a
// cancelled msgStream stays cancelled, so the next borrower would fail.
//
// The interleaving is forced rather than raced. The hooked stream cancels the
// ctx as it delivers the last response byte and then holds that Read until
// CancelRead has actually landed, so by the time Decode returns the watcher
// has provably fired and cleanup is deciding what to do with a poisoned
// stream. The next borrower's call is the assertion.
func TestInlineClientCancelRacingCleanupDoesNotPoisonPool(t *testing.T) {
	r := require.New(t)

	ca, cb := newMemPipe()
	client := newMsgSession(ca, true, 0, 0)
	peer := newMsgSession(cb, false, 0, 0)
	t.Cleanup(func() {
		_ = client.Close()
		_ = peer.Close()
	})
	serveInlineOK(t, peer)

	// The exact bytes serveInlineOK will send back, so the hook fires on the
	// Read that completes the response and not one before.
	var reply bytes.Buffer
	enc := cbor.NewEncoder(&reply)
	r.NoError(enc.Encode(refResponse{Status: "ok"}))
	r.NoError(enc.Encode(struct{}{}))

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	hs := &hookedStream{
		afterBytes: reply.Len(),
		cancelled:  make(chan struct{}),
	}
	hs.afterRead = func() {
		cancel()
		<-hs.cancelled
	}

	ic := &inlineClient{
		log:     slog.Default(),
		oid:     OID("cap"),
		session: &hookedSession{rpcSession: client, stream: hs},
	}

	var out struct{}
	r.NoError(ic.Call(ctx, "ok", struct{}{}, &out))
	r.True(hs.fired, "hook never ran; the response arrived in a shape the test did not expect")

	// The cancelled stream must have been closed, not pooled.
	ic.poolMu.Lock()
	r.Len(ic.pool, 0, "cancelled stream was returned to the pool")
	r.Equal(0, ic.activeCount)
	ic.poolMu.Unlock()

	// And the next borrower gets a fresh stream that works.
	r.NoError(ic.Call(t.Context(), "ok", struct{}{}, &out),
		"a stale watcher cancelled the pooled stream")
}
