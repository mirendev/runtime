//go:build blackbox

package blackbox

import (
	"testing"

	"miren.dev/runtime/blackbox/harness"
)

// TestDebugSagaShowUnknownID checks that `debug saga` is wired up and fails
// cleanly on a miss. It deliberately does not provision anything: producing a
// real saga means a full app build plus an addon provision, and what that would
// verify beyond this is already covered by the decode and index tests in
// cli/commands. If real-record coverage is wanted later, the cheap place to put
// it is TestAddonCreateListDestroy, which already runs a provisioning saga.
func TestDebugSagaShowUnknownID(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	r := m.Run("debug", "saga", "show", "sg-DoesNotExist")
	if r.Success() {
		t.Fatalf("expected failure for an unknown saga, got exit 0:\n%s", r.Stdout)
	}
	if !r.OutputContains("not found") {
		t.Errorf("expected a not-found message, got:\nstdout: %s\nstderr: %s", r.Stdout, r.Stderr)
	}
}
