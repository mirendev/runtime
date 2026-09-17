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

	go func() {
		st, err := peer.AcceptStream(context.Background())
		if err != nil {
			return
		}
		go func() { _, _ = io.Copy(io.Discard, st) }()
		enc := cbor.NewEncoder(st)
		for range 2 {
			_ = enc.Encode(refResponse{Status: "ok"})
			_ = enc.Encode(struct{}{})
		}
	}()

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
