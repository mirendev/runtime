package rpc

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"
)

// Graceful HTTP/3 shutdown needs a second opinion.
//
// http3.Server.Shutdown decides it is finished from an internal connection
// count that it does not export. That count has a race: the channel a stalled
// Shutdown waits on is closed only when the count reaches zero *while* the
// server's grace context is already cancelled, so a final connection that
// closes just before Shutdown cancels that context leaves nobody to close it.
// Shutdown then waits out its whole deadline with no connections open and no
// requests running, and reports the timeout as a failure to drain.
//
// We saw this on a coordinator whose every peer had already disconnected: ten
// seconds of shutdown latency and a spurious error, reproducible about one run
// in five once the machine was short enough of cores to lose the race. The
// logic is identical in every quic-go release we have looked at, so this is not
// something an upgrade settles.
//
// countingListener below is the second opinion. An idle listener is a stronger
// signal than quic-go's count: a request can only exist on a connection, so
// zero open connections means nothing is left to drain, whatever the count
// says.

// drainPollInterval is how often a drain in progress re-checks whether the
// listener has gone idle. Short enough that a completed drain is not held open
// noticeably, long enough that the poll costs nothing over a real drain.
const drainPollInterval = 25 * time.Millisecond

// WebTransport's raw HTTP/3 connections are not managed by http3.Shutdown.
// Clients that understand /_rpc/drain retire themselves after reading their
// responses. Older and plain HTTP/3 clients have only the deadline fallback.
func (s *State) drainWebTransport(ctx context.Context) error {
	_ = s.li.Close()
	// Stabilize the census before observing zero: Accept may have returned a
	// connection just before listener closure and not counted it yet.
	<-s.acceptDone
	ticker := time.NewTicker(drainPollInterval)
	defer ticker.Stop()
	for !s.li.idle() {
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case <-ticker.C:
		}
	}
	return nil
}

// countingListener wraps the QUIC listener handed to an http3.Server so we can
// tell whether a drain still has work to do. It costs one atomic increment per
// accepted connection, on a path that has just finished a QUIC handshake.
type countingListener struct {
	*quic.EarlyListener

	accepted atomic.Int64
	open     atomic.Int64
}

func (l *countingListener) Accept(ctx context.Context) (*quic.Conn, error) {
	conn, err := l.EarlyListener.Accept(ctx)
	if err != nil {
		return nil, err
	}

	l.accepted.Add(1)
	l.open.Add(1)
	go func() {
		<-conn.Context().Done()
		l.open.Add(-1)
	}()

	return conn, nil
}

// idle reports that no accepted connection is still open, and therefore that no
// request handler can still be running on one.
func (l *countingListener) idle() bool {
	return l.open.Load() <= 0
}

func (l *countingListener) describe() string {
	if l == nil {
		return "no listener"
	}
	return fmt.Sprintf("%d accepted, %d still open", l.accepted.Load(), l.open.Load())
}

// drainQUIC gracefully shuts down an HTTP/3 server, stopping the wait as soon
// as its listener has no open connections left rather than trusting quic-go's
// own idea of when the drain is complete.
//
// Cancelling the drain early is not a shortcut past in-flight work: quic-go
// responds to the cancellation by closing the server, which is the same
// teardown it would perform on its own, and we only do it once the listener
// says there is nothing left to close. A drain that misses its deadline with
// connections still open is still reported as the failure it is.
//
// It takes the server's Shutdown as a function rather than the server itself so
// the policy can be exercised against a drain that stalls on demand, which a
// real QUIC stack only does by losing a race.
func drainQUIC(ctx context.Context, shutdown func(context.Context) error, ln *countingListener) error {
	if ln == nil {
		return shutdown(ctx)
	}

	drainCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// The poll checks before it waits, so a listener that is already idle on
	// entry cancels drainCtx before shutdown is even called. That is the
	// intended path and not a degenerate one: quic-go still cancels its grace
	// context and closes its listeners before it looks at the context, so the
	// teardown happens in full and only the waiting is skipped.
	go func() {
		ticker := time.NewTicker(drainPollInterval)
		defer ticker.Stop()
		for {
			if ln.idle() {
				cancel()
				return
			}
			select {
			case <-drainCtx.Done():
				return
			case <-ticker.C:
			}
		}
	}()

	err := shutdown(drainCtx)
	if ln.idle() && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		// Every connection is closed, so the drain finished even though
		// quic-go could not observe it. Reporting the cutoff here would be
		// reporting a failure that did not happen. Only a context-shaped error
		// is absorbed: a drain that failed for its own reasons still says so.
		return nil
	}
	return err
}

// drainStackPackages are the packages a stalled HTTP/3 drain lives in. A
// goroutine touching none of them is almost never the reason a handler has not
// returned, and printing every one of them would bury the few that matter.
var drainStackPackages = []string{
	"github.com/quic-go/",
	"miren.dev/runtime/pkg/rpc",
}

// drainStacks renders the goroutines worth reading when a graceful shutdown
// misses its deadline with connections still open: those running in the RPC and
// QUIC stacks, plus any the runtime has annotated with how long it has been
// parked, since a goroutine blocked for minutes is interesting wherever it
// lives. Everything else is counted rather than printed, so this stays a
// readable log line on a busy process instead of a wall of text.
//
// Nothing here runs on a healthy shutdown, which is what makes it affordable to
// keep: the one moment it costs anything is the moment its absence would leave
// an operator with an unattributable timeout and no way back to the cause.
func drainStacks() string {
	buf := make([]byte, 64<<10)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) || len(buf) >= 4<<20 {
			buf = buf[:n]
			break
		}
		buf = make([]byte, 2*len(buf))
	}

	var kept []string
	var elided int
	for g := range strings.SplitSeq(string(buf), "\n\n") {
		g = strings.TrimSpace(g)
		if g == "" {
			continue
		}
		if keepGoroutine(g) {
			kept = append(kept, g)
			continue
		}
		elided++
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%d goroutines shown, %d elided\n\n", len(kept), elided)
	b.WriteString(strings.Join(kept, "\n\n"))
	return b.String()
}

func keepGoroutine(stack string) bool {
	header, _, _ := strings.Cut(stack, "\n")
	// The runtime annotates a goroutine parked long enough as
	// "goroutine 12 [select, 10 minutes]:". Anything it thinks is worth timing
	// is worth reading during a shutdown that will not finish. It only annotates
	// waits over a minute, so on a drain with a short deadline this clause
	// matches nothing and the package list below does all the work; it earns its
	// place on the long production drains, which are the ones nobody can rerun.
	if strings.Contains(header, " minutes]:") {
		return true
	}
	for _, pkg := range drainStackPackages {
		if strings.Contains(stack, pkg) {
			return true
		}
	}
	return false
}
