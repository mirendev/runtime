package rpc

import (
	"context"
	"errors"
	"io"
	"log/slog"
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

// Cancellation landing in the same instant as cleanup must not reach a stream
// that has already gone back to the pool. Signalling the watcher is not
// enough: with ctx.Done and stop both ready, select may take ctx.Done after
// the stream is pooled, and the CancelRead lands on whoever borrows it next.
// Cancelling right after Call returns puts the watcher in exactly that state,
// so the next borrower's call is the assertion.
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

	ic := &inlineClient{
		log:     slog.Default(),
		oid:     OID("cap"),
		session: client,
	}

	var out struct{}
	for i := range 2000 {
		ctx, cancel := context.WithCancel(t.Context())
		r.NoError(ic.Call(ctx, "ok", struct{}{}, &out), "iteration %d", i)
		cancel()

		r.NoError(ic.Call(t.Context(), "ok", struct{}{}, &out),
			"iteration %d: a stale watcher cancelled the pooled stream", i)
	}
}
