package indexwatch

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"miren.dev/runtime/pkg/entity"
)

// TestSendAfterCloseIsSafe pins the fix for MIR-1307: a late event delivery
// (as happens when the RPC dispatch goroutine invokes our WatchIndex callback
// after run has torn down and closed the Updates channel) must not send on the
// closed channel. send has to report shutdown, not panic.
func TestSendAfterCloseIsSafe(t *testing.T) {
	w := New(nil, entity.Attr{}, Options{BufferSize: 1})

	w.closeUpdates()

	if w.send(context.Background(), Event{Type: EventAdded}) {
		t.Fatal("send reported delivery on a closed channel")
	}
}

// TestConcurrentSendCloseNoPanic hammers the send/close race directly under the
// race detector: many senders racing a single close must never panic with
// "send on closed channel" nor deliver after the close. Run with -race for the
// strongest signal; the closed-flag guard makes it deterministic regardless.
func TestConcurrentSendCloseNoPanic(t *testing.T) {
	const senders = 32

	// A cancelled context so every send that loses the race to the close
	// unblocks promptly via the ctx.Done arm rather than parking on the buffer.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	w := New(nil, entity.Attr{}, Options{BufferSize: 4})

	var wg sync.WaitGroup
	wg.Add(senders + 1)

	for range senders {
		go func() {
			defer wg.Done()
			// A send may succeed or report shutdown; it must never panic.
			w.send(ctx, Event{Type: EventAdded})
		}()
	}

	go func() {
		defer wg.Done()
		w.closeUpdates()
	}()

	wg.Wait()

	// The channel must end up closed exactly once (a second close would panic).
	if !w.closed {
		t.Fatal("closed flag not set after closeUpdates")
	}
}

func TestRetryLevelEscalatesPersistentFailures(t *testing.T) {
	start := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	if got := retryLevel(start, start.Add(90*time.Second)); got != slog.LevelWarn {
		t.Errorf("a coordinator restart's worth of failure logged at %v, want Warn", got)
	}
	if got := retryLevel(start, start.Add(persistentFailure)); got != slog.LevelError {
		t.Errorf("failing for %v logged at %v, want Error", persistentFailure, got)
	}
}
