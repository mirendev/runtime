package base

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/errdefs"
	"github.com/stretchr/testify/require"
)

// fakeTask only needs an identity; ReplaceTask hands it straight to the evictor.
type fakeTask struct {
	containerd.Task
	id string
}

// scriptedHost answers each Task() lookup from a queue, mirroring the sequence
// of states containerd reports across a retry loop. Once the script runs out
// it keeps answering NotFound.
type scriptedHost struct {
	lookups []func() (containerd.Task, error)
	calls   int
}

func (h *scriptedHost) ID() string { return "test-container" }

func (h *scriptedHost) Task(context.Context, cio.Attach) (containerd.Task, error) {
	h.calls++
	if h.calls > len(h.lookups) {
		return notFound()
	}
	return h.lookups[h.calls-1]()
}

func found(id string) func() (containerd.Task, error) {
	return func() (containerd.Task, error) { return &fakeTask{id: id}, nil }
}

func notFound() (containerd.Task, error) {
	return nil, fmt.Errorf("no running task found: %w", errdefs.ErrNotFound)
}

var testLog = slog.New(slog.NewTextHandler(io.Discard, nil))

func noEvict(t *testing.T) TaskEvictor {
	return func(context.Context, containerd.Task) error {
		t.Fatal("evict called with no task present")
		return nil
	}
}

func noReap(t *testing.T) TaskReaper {
	return func(context.Context, string) error {
		t.Fatal("reap called without an AlreadyExists")
		return nil
	}
}

func TestReplaceTaskEvictsExistingTaskBeforeCreating(t *testing.T) {
	host := &scriptedHost{lookups: []func() (containerd.Task, error){found("old")}}
	var evicted []string
	created := &fakeTask{id: "new"}

	task, err := ReplaceTask(context.Background(), testLog, "test", host,
		func(_ context.Context, task containerd.Task) error {
			evicted = append(evicted, task.(*fakeTask).id)
			return nil
		},
		noReap(t),
		func(context.Context) (containerd.Task, error) { return created, nil })
	require.NoError(t, err)
	require.Same(t, created, task)
	require.Equal(t, []string{"old"}, evicted)
}

func TestReplaceTaskCreatesWhenNothingToEvict(t *testing.T) {
	created := &fakeTask{id: "new"}

	task, err := ReplaceTask(context.Background(), testLog, "test", &scriptedHost{},
		noEvict(t), noReap(t),
		func(context.Context) (containerd.Task, error) { return created, nil })
	require.NoError(t, err)
	require.Same(t, created, task)
}

// The MIR-1856 state: containerd says NotFound on lookup but AlreadyExists on
// create because a killed process left a task record behind. The record is
// reaped by container ID and the create goes through.
func TestReplaceTaskReapsLeakedRecord(t *testing.T) {
	host := &scriptedHost{}
	created := &fakeTask{id: "new"}
	var reaped []string
	attempts := 0

	task, err := ReplaceTask(context.Background(), testLog, "test", host,
		noEvict(t),
		func(_ context.Context, id string) error {
			reaped = append(reaped, id)
			return nil
		},
		func(context.Context) (containerd.Task, error) {
			attempts++
			if len(reaped) == 0 {
				return nil, fmt.Errorf("task test: %w", errdefs.ErrAlreadyExists)
			}
			return created, nil
		})
	require.NoError(t, err)
	require.Same(t, created, task)
	require.Equal(t, []string{"test-container"}, reaped)
	require.Equal(t, 2, attempts)
	require.Equal(t, 2, host.calls, "the retry should look the task up again after reaping")
}

// If the task turns up on a retry lookup it gets evicted like any other stale
// task rather than being left to block the create.
func TestReplaceTaskEvictsTaskThatAppearsOnRetry(t *testing.T) {
	host := &scriptedHost{lookups: []func() (containerd.Task, error){notFound, found("late")}}
	var evicted []string
	created := &fakeTask{id: "new"}
	attempts := 0

	task, err := ReplaceTask(context.Background(), testLog, "test", host,
		func(_ context.Context, task containerd.Task) error {
			evicted = append(evicted, task.(*fakeTask).id)
			return nil
		},
		func(context.Context, string) error { return nil },
		func(context.Context) (containerd.Task, error) {
			attempts++
			if attempts == 1 {
				return nil, errdefs.ErrAlreadyExists
			}
			return created, nil
		})
	require.NoError(t, err)
	require.Same(t, created, task)
	require.Equal(t, []string{"late"}, evicted)
}

func TestReplaceTaskGivesUpWhenRecordNeverClears(t *testing.T) {
	reaps := 0

	_, err := ReplaceTask(context.Background(), testLog, "test", &scriptedHost{},
		noEvict(t),
		func(context.Context, string) error { reaps++; return nil },
		func(context.Context) (containerd.Task, error) { return nil, errdefs.ErrAlreadyExists })
	require.Error(t, err)
	require.True(t, errdefs.IsAlreadyExists(err), "final error should keep the AlreadyExists cause: %v", err)
	require.Equal(t, replaceTaskMaxAttempts-1, reaps)
}

func TestReplaceTaskStopsWhenReapFails(t *testing.T) {
	boom := errors.New("boom")
	attempts := 0

	_, err := ReplaceTask(context.Background(), testLog, "test", &scriptedHost{},
		noEvict(t),
		func(context.Context, string) error { return boom },
		func(context.Context) (containerd.Task, error) {
			attempts++
			return nil, errdefs.ErrAlreadyExists
		})
	require.ErrorIs(t, err, boom)
	require.Equal(t, 1, attempts)
}

func TestReplaceTaskDoesNotRetryOtherCreateErrors(t *testing.T) {
	boom := errors.New("boom")
	attempts := 0

	_, err := ReplaceTask(context.Background(), testLog, "test", &scriptedHost{},
		noEvict(t), noReap(t),
		func(context.Context) (containerd.Task, error) {
			attempts++
			return nil, boom
		})
	require.ErrorIs(t, err, boom)
	require.Equal(t, 1, attempts)
}

func TestReplaceTaskStopsWhenEvictionFails(t *testing.T) {
	host := &scriptedHost{lookups: []func() (containerd.Task, error){found("stuck")}}
	boom := errors.New("boom")

	_, err := ReplaceTask(context.Background(), testLog, "test", host,
		func(context.Context, containerd.Task) error { return boom },
		noReap(t),
		func(context.Context) (containerd.Task, error) {
			t.Fatal("create called after eviction failed")
			return nil, nil
		})
	require.ErrorIs(t, err, boom)
}
