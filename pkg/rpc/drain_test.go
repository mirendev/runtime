package rpc

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// blockingShutdown stands in for quic-go's graceful shutdown after it has lost
// the connCount race: it will wait forever unless its context ends.
func blockingShutdown(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestDrainQUICStopsOnceListenerIsIdle(t *testing.T) {
	ln := &countingListener{}
	ln.open.Store(1)

	// The last connection closes shortly after the drain begins, which is the
	// transition quic-go can miss.
	go func() {
		time.Sleep(2 * drainPollInterval)
		ln.open.Store(0)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	start := time.Now()
	err := drainQUIC(ctx, blockingShutdown, ln)
	took := time.Since(start)

	if err != nil {
		t.Fatalf("drain of an idle listener returned %v", err)
	}
	if took > time.Second {
		t.Fatalf("drain waited %s after the listener went idle", took)
	}
	if ctx.Err() != nil {
		t.Fatal("drain consumed the caller's deadline instead of finishing early")
	}
}

func TestDrainQUICReportsStallWhileConnectionsRemain(t *testing.T) {
	ln := &countingListener{}
	ln.open.Store(1)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := drainQUIC(ctx, blockingShutdown, ln)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a drain that stalled with a connection open returned %v, want DeadlineExceeded", err)
	}
}

func TestDrainQUICPassesThroughAnOrdinaryDrain(t *testing.T) {
	ln := &countingListener{}
	want := errors.New("listener exploded")

	err := drainQUIC(context.Background(), func(context.Context) error { return want }, ln)
	if !errors.Is(err, want) {
		t.Fatalf("drain returned %v, want the underlying error", err)
	}
}

func TestKeepGoroutineSelectsTheRelevantStacks(t *testing.T) {
	cases := []struct {
		name  string
		stack string
		keep  bool
	}{
		{
			name:  "quic-go frame",
			stack: "goroutine 12 [select]:\ngithub.com/quic-go/quic-go.(*Transport).listen(...)",
			keep:  true,
		},
		{
			name:  "rpc frame",
			stack: "goroutine 13 [chan receive]:\nmiren.dev/runtime/pkg/rpc.(*State).Shutdown(...)",
			keep:  true,
		},
		{
			name:  "parked long enough for the runtime to time it",
			stack: "goroutine 14 [select, 10 minutes]:\nsomewhere/else.Loop(...)",
			keep:  true,
		},
		{
			name:  "unrelated and not parked",
			stack: "goroutine 15 [chan receive]:\nsomewhere/else.Loop(...)",
			keep:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := keepGoroutine(tc.stack); got != tc.keep {
				t.Fatalf("keepGoroutine = %v, want %v", got, tc.keep)
			}
		})
	}
}

func TestDrainStacksReportsWhatItElided(t *testing.T) {
	out := drainStacks()
	if !strings.Contains(out, "elided") {
		t.Fatalf("dump does not account for elided goroutines: %q", out)
	}
	// The calling goroutine is itself in pkg/rpc, so it must survive the filter.
	if !strings.Contains(out, "miren.dev/runtime/pkg/rpc.drainStacks") {
		t.Fatal("dump filtered out its own caller")
	}
}

func TestCountingListenerDescribesItsOwnConnections(t *testing.T) {
	// A stalled drain reports the census of the surface that stalled. Reporting
	// another listener's numbers would be worse than reporting none, since the
	// census is the only evidence available at that moment.
	primary := &countingListener{}
	primary.accepted.Store(19)
	primary.open.Store(0)

	local := &countingListener{}
	local.accepted.Store(2)
	local.open.Store(1)

	if got, want := primary.describe(), "19 accepted, 0 still open"; got != want {
		t.Fatalf("primary census = %q, want %q", got, want)
	}
	if got, want := local.describe(), "2 accepted, 1 still open"; got != want {
		t.Fatalf("local census = %q, want %q", got, want)
	}
	if primary.idle() == local.idle() {
		t.Fatal("an idle listener and a busy one report the same idleness")
	}
	if got, want := (*countingListener)(nil).describe(), "no listener"; got != want {
		t.Fatalf("nil census = %q, want %q", got, want)
	}
}

// reportStalledDrainSpy is the part of State.Shutdown's error handling under
// test: which drain failures are allowed to spend the one-shot goroutine dump.
type reportStalledDrainSpy struct {
	once  sync.Once
	dumps int
}

func (r *reportStalledDrainSpy) report(err error) {
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		return
	}
	r.once.Do(func() { r.dumps++ })
}

func TestOnlyATimedOutDrainSpendsTheStackDump(t *testing.T) {
	// A REST or WebSocket surface failing fast must not consume the dump that a
	// genuinely stalled HTTP/3 drain will need later in the same shutdown.
	var r reportStalledDrainSpy

	r.report(errors.New("rest listener already closed"))
	if r.dumps != 0 {
		t.Fatalf("an ordinary drain failure spent the dump (%d)", r.dumps)
	}

	r.report(context.DeadlineExceeded)
	if r.dumps != 1 {
		t.Fatalf("a stalled drain did not produce a dump (%d)", r.dumps)
	}

	r.report(context.DeadlineExceeded)
	if r.dumps != 1 {
		t.Fatalf("a second stall dumped again (%d)", r.dumps)
	}
}
