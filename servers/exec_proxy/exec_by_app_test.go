package execproxy

import (
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"

	"miren.dev/runtime/api/exec/exec_v1alpha"
	run_v1alpha "miren.dev/runtime/api/run/run_v1alpha"
	"miren.dev/runtime/pkg/cond"
)

// A v0.13 client asks for a terminal by carrying an initial WinSize, never by
// setting Terminal (its setupExecIO leaves Terminal unset and the old exec
// server keyed the pty off WinSize). Reading only Terminal, as the first cut
// did, gives every interactive v0.13 run a non-tty: `test -t 0` reports false
// and `stty size` fails. execTTY has to honor the WinSize signal.
func TestExecTTY(t *testing.T) {
	assert.False(t, execTTY(nil), "no options means no terminal")
	assert.False(t, execTTY(&exec_v1alpha.ShellOptions{}), "neither signal set")

	term := &exec_v1alpha.ShellOptions{}
	term.SetTerminal(true)
	assert.True(t, execTTY(term), "an explicit Terminal flag still allocates a pty")

	withSize := &exec_v1alpha.ShellOptions{}
	ws := &exec_v1alpha.WindowSize{}
	ws.SetWidth(80)
	ws.SetHeight(24)
	withSize.SetWinSize(ws)
	assert.False(t, withSize.Terminal(), "the v0.13 client leaves Terminal unset")
	assert.True(t, execTTY(withSize), "a WinSize is how a v0.13 client asks for a pty")
}

// The legacy exec-by-app path retries an attach while the run's sandbox is
// coming up. attachNotReady decides what is worth waiting on: the node's three
// "not yet" phrasings and a missing entity, but nothing else -- a denial or a
// transport failure must surface at once, not be retried into the 2m deadline.
// The typed error does not survive the RPC hop to the node, so the match is by
// text, mirroring the client's attachTargetMissing.
func TestAttachNotReady(t *testing.T) {
	assert.True(t, attachNotReady(cond.ErrNotFound{}), "a missing sandbox is worth waiting on")
	assert.True(t, attachNotReady(errors.New(`attach: container "app" of sandbox/run-x-a1 is not running yet`)),
		"the node's NotReadyError text must be recognized across the RPC boundary")
	assert.True(t, attachNotReady(errors.New("failed to find sandbox foo")))
	assert.True(t, attachNotReady(errors.New("sandbox is not scheduled to a node yet")))
	// Wrapped forms still match by substring and by errors.Is.
	assert.True(t, attachNotReady(fmt.Errorf("attaching: %w", cond.ErrNotFound{})))

	assert.False(t, attachNotReady(errors.New("access denied for app foo")),
		"a denial must not be retried into the deadline")
	assert.False(t, attachNotReady(errors.New("connection reset by peer")),
		"a transport failure is not a not-ready condition")
}

// The exit code a v0.13 client reads back must describe the command, not its
// teardown: a killed (canceled/timed-out) run's kernel-reported code is noise,
// and reporting it as the command's result would tell a script a failing run
// succeeded, or vice versa.
func TestExitCodeReportingRules(t *testing.T) {
	assert.True(t, reportsExitCode(run_v1alpha.SUCCEEDED))
	assert.True(t, reportsExitCode(run_v1alpha.FAILED))
	for _, s := range []run_v1alpha.RunStatus{
		run_v1alpha.PENDING, run_v1alpha.RUNNING, run_v1alpha.CANCELED,
		run_v1alpha.TIMED_OUT, run_v1alpha.SKIPPED,
	} {
		assert.False(t, reportsExitCode(s), "%s carries meaning in its status, not an exit code", s)
	}
}

func TestIsTerminalRunStatus(t *testing.T) {
	for _, s := range []run_v1alpha.RunStatus{
		run_v1alpha.SUCCEEDED, run_v1alpha.FAILED, run_v1alpha.TIMED_OUT,
		run_v1alpha.CANCELED, run_v1alpha.SKIPPED,
	} {
		assert.True(t, isTerminalRunStatus(s), "%s is terminal", s)
	}
	assert.False(t, isTerminalRunStatus(run_v1alpha.PENDING))
	assert.False(t, isTerminalRunStatus(run_v1alpha.RUNNING))
}

// Real exit codes are a byte, so clamping only matters for a corrupt value --
// but wrapping one into a small plausible-looking number is the worst outcome,
// since nothing downstream can tell it from a genuine result.
func TestClampInt32Saturates(t *testing.T) {
	assert.Equal(t, int32(0), clampInt32(0))
	assert.Equal(t, int32(42), clampInt32(42))
	assert.Equal(t, int32(math.MaxInt32), clampInt32(math.MaxInt32+1))
	assert.Equal(t, int32(math.MinInt32), clampInt32(math.MinInt32-1))
}
