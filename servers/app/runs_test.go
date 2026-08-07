package app

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	run_v1alpha "miren.dev/runtime/api/run/run_v1alpha"
)

// A canceled or timed-out run does produce an observed exit code -- the
// platform killed the process and the kernel reported something -- but that is
// teardown noise, not the task's outcome. Showing it beside a "canceled" status
// invites reading it as an application error, so it is not surfaced as the
// run's result.
func TestRunInfoOnlyReportsCommandExitCodes(t *testing.T) {
	base := func(status run_v1alpha.RunStatus) *run_v1alpha.Run {
		return &run_v1alpha.Run{
			ID:      "run/demo-x-1",
			Task:    "reindex",
			Trigger: run_v1alpha.MANUAL,
			Status:  status,
			Result:  run_v1alpha.Result{Code: 2, At: time.Now()},
		}
	}

	t.Run("succeeded reports it", func(t *testing.T) {
		info := runInfo(base(run_v1alpha.SUCCEEDED))
		assert.True(t, info.HasExitCode())
		assert.Equal(t, int32(2), info.ExitCode())
	})

	t.Run("failed reports it", func(t *testing.T) {
		assert.True(t, runInfo(base(run_v1alpha.FAILED)).HasExitCode())
	})

	for _, status := range []run_v1alpha.RunStatus{
		run_v1alpha.CANCELED, run_v1alpha.TIMED_OUT, run_v1alpha.SKIPPED,
	} {
		t.Run(string(status)+" does not", func(t *testing.T) {
			assert.False(t, runInfo(base(status)).HasExitCode(),
				"the status carries the meaning; the killed process's code is teardown noise")
		})
	}
}

// A run with no observed exit reports none, whatever its status -- a bare 0
// would read as a clean exit.
func TestRunInfoWithNoObservedExit(t *testing.T) {
	info := runInfo(&run_v1alpha.Run{
		ID:     "run/demo-x-1",
		Status: run_v1alpha.FAILED,
	})
	assert.False(t, info.HasExitCode())
}

// A legitimate zero has to survive, since it is the most common successful
// outcome and the one a naive presence check drops.
func TestRunInfoReportsAZeroExitCode(t *testing.T) {
	info := runInfo(&run_v1alpha.Run{
		ID:     "run/demo-x-1",
		Status: run_v1alpha.SUCCEEDED,
		Result: run_v1alpha.Result{Code: 0, At: time.Now()},
	})
	assert.True(t, info.HasExitCode())
	assert.Equal(t, int32(0), info.ExitCode())
}
