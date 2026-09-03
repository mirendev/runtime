package sandbox

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/observability"
)

// newPortTestController builds the minimal SandboxController state needed to
// exercise WaitForPort / SetPortStatus in isolation, without containerd or any
// other dependency. Mirrors the construction used by the "waitForPort respects
// timeout" subtest in sandbox_test.go.
func newPortTestController() *SandboxController {
	c := &SandboxController{
		Log:     slog.Default(),
		portMap: make(map[string]*containerPorts),
	}
	c.portMu = sync.Mutex{}
	c.portCond = sync.NewCond(&c.portMu)
	return c
}

func boundPort(port int) observability.BoundPort {
	return observability.BoundPort{Port: port}
}

// TestWaitForPortNoSpuriousTimeoutWhenPortBound is the deterministic regression
// guard for the WaitForPort deadline race (controllers/sandbox/sandbox.go).
//
// Before the fix, WaitForPort's outer select raced its success signal — the
// wait goroutine closing `done` under portMu once it observes the configured
// port bound — against a `time.After(time.Until(deadline))` timer on the caller
// goroutine. When the timer wins that race (which can happen even though `done`
// is also ready, since Go's select chooses uniformly at random among ready
// cases), a port that is in fact bound is reported as a port-health-check
// timeout. With the fix, the timeout branch re-checks the bound set under
// portMu before declaring a timeout.
//
// This test reproduces the racing scenario deterministically: the port is
// already recorded bound in portMap, but portMu is held across the deadline so
// the wait goroutine cannot observe it and close `done` in time — guaranteeing
// the timer branch wins the select. The bind observation thus "lags the
// deadline" as it does in production when the port monitor's polling tick lands
// just before the deadline yet the goroutine has not yet been rescheduled. Under
// the bug the timer branch returns a spurious timeout while the port is bound;
// under the fix the re-check observes the bound port and returns nil.
func TestWaitForPortNoSpuriousTimeoutWhenPortBound(t *testing.T) {
	const iterations = 100
	const id = "race-id"
	const port = 8080
	const waitTimeout = 5 * time.Millisecond

	falseTimeouts := 0
	for i := 0; i < iterations; i++ {
		c := newPortTestController()
		// Pre-record the configured port as bound, as a port-monitor tick that
		// landed just before the deadline would.
		c.portMap[id] = &containerPorts{Ports: []observability.BoundPort{boundPort(port)}}

		// Hold portMu so the wait goroutine inside WaitForPort cannot observe
		// the bound port and close `done` before the deadline timer fires; the
		// timer branch therefore wins the select.
		c.portMu.Lock()
		errCh := make(chan error, 1)
		go func() {
			errCh <- c.WaitForPort(context.Background(), id, port, waitTimeout)
		}()
		// Wait past the deadline so the timer fires before we release the lock.
		time.Sleep(waitTimeout + 10*time.Millisecond)
		c.portMu.Unlock()

		err := <-errCh
		if err != nil {
			falseTimeouts++
			t.Logf("iteration %d: WaitForPort returned %q while the port was recorded bound", i, err)
		}
	}

	require.Equalf(t, 0, falseTimeouts,
		"WaitForPort returned a spurious timeout on %d/%d iterations even though the port was recorded bound; "+
			"the timeout branch must re-check the bound set under portMu before declaring a timeout",
		falseTimeouts, iterations)
}

// TestWaitForPortBothReadyConcurrently forces `done` and the deadline timer to
// become ready at (close to) the same instant on every iteration — the port is
// pre-bound (so the wait goroutine closes `done` immediately) and the timeout
// is negative (so the timer is ready immediately). A bound port must never be
// reported as a timeout regardless of which case the select happens to pick.
func TestWaitForPortBothReadyConcurrently(t *testing.T) {
	const iterations = 500
	const id = "ready-id"
	const port = 8080

	falseTimeouts := 0
	for i := 0; i < iterations; i++ {
		c := newPortTestController()
		c.portMap[id] = &containerPorts{Ports: []observability.BoundPort{boundPort(port)}}

		// Negative timeout ⇒ deadline in the past ⇒ timer ready immediately,
		// contending with `done` also closing immediately.
		err := c.WaitForPort(context.Background(), id, port, -1*time.Millisecond)
		if err != nil {
			falseTimeouts++
			t.Logf("iteration %d: WaitForPort returned %q while the port was bound", i, err)
		}
	}

	require.Equalf(t, 0, falseTimeouts,
		"WaitForPort returned a spurious timeout on %d/%d iterations even though the port was bound",
		falseTimeouts, iterations)
}
