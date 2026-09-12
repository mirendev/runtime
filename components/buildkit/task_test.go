package buildkit

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"syscall"
	"testing"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

type forcedStopTask struct {
	graceExit      chan containerd.ExitStatus
	killExit       chan containerd.ExitStatus
	waitCalls      int
	signals        []syscall.Signal
	contextErrs    []error
	deleteOptCount int
	deleteErr      error
}

func newForcedStopTask() *forcedStopTask {
	return &forcedStopTask{
		graceExit: make(chan containerd.ExitStatus, 1),
		killExit:  make(chan containerd.ExitStatus, 1),
	}
}

func (t *forcedStopTask) Wait(ctx context.Context) (<-chan containerd.ExitStatus, error) {
	t.contextErrs = append(t.contextErrs, ctx.Err())
	t.waitCalls++
	if t.waitCalls == 1 {
		return t.graceExit, nil
	}
	return t.killExit, nil
}

func (t *forcedStopTask) Kill(ctx context.Context, signal syscall.Signal, _ ...containerd.KillOpts) error {
	t.contextErrs = append(t.contextErrs, ctx.Err())
	t.signals = append(t.signals, signal)
	if signal == unix.SIGKILL {
		t.killExit <- *containerd.NewExitStatus(137, time.Now(), nil)
	}
	return nil
}

func (t *forcedStopTask) Delete(ctx context.Context, opts ...containerd.ProcessDeleteOpts) (*containerd.ExitStatus, error) {
	t.contextErrs = append(t.contextErrs, ctx.Err())
	t.deleteOptCount = len(opts)
	return nil, t.deleteErr
}

func TestStopTaskForceDeletesAfterGracePeriod(t *testing.T) {
	task := newForcedStopTask()
	component := &Component{
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Namespace: "miren-test",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.NoError(t, component.stopTaskWithGrace(ctx, task, time.Millisecond))
	require.Equal(t, []syscall.Signal{unix.SIGTERM, unix.SIGKILL}, task.signals)
	require.Equal(t, 2, task.waitCalls)
	require.Empty(t, task.killExit, "task exit must be observed before deletion")
	require.Equal(t, 1, task.deleteOptCount)
	for _, contextErr := range task.contextErrs {
		require.NoError(t, contextErr, "cleanup must detach from caller cancellation")
	}
}

func TestStopTaskReturnsDeleteFailure(t *testing.T) {
	task := newForcedStopTask()
	task.graceExit <- *containerd.NewExitStatus(0, time.Now(), nil)
	task.deleteErr = errors.New("task still exists")
	component := &Component{
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Namespace: "miren-test",
	}

	err := component.stopTaskWithGrace(context.Background(), task, time.Second)
	require.ErrorContains(t, err, "delete buildkit task: task still exists")
	require.Equal(t, 1, task.deleteOptCount)
}
