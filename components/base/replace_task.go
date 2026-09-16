package base

import (
	"context"
	"fmt"
	"log/slog"

	tasks "github.com/containerd/containerd/api/services/tasks/v1"
	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/errdefs"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TaskEvictor stops and deletes a task that a previous process left registered
// on a container, so a fresh one can be created in its place.
type TaskEvictor func(ctx context.Context, task containerd.Task) error

// TaskFactory creates a new task on the container the caller has already bound
// it to.
type TaskFactory func(ctx context.Context) (containerd.Task, error)

// TaskReaper removes containerd's record of a task that can no longer be
// loaded through the normal client path. See ReapLeakedTask.
type TaskReaper func(ctx context.Context, containerID string) error

// taskHost is the slice of containerd.Container that ReplaceTask needs.
type taskHost interface {
	ID() string
	Task(ctx context.Context, attach cio.Attach) (containerd.Task, error)
}

// replaceTaskMaxAttempts bounds how many times ReplaceTask will reap and retry
// before giving up. One reap normally clears a leaked record; the rest is
// headroom for containerd racing its own cleanup.
const replaceTaskMaxAttempts = 3

// ReplaceTask creates a fresh task on a container, first evicting any task a
// previous miren process left behind.
//
// A previous process killed part-way through deleting its task can leave
// containerd in a state where the two normal client calls disagree: Task()
// reports NotFound, because the shim has already dropped the container and
// answers State() that way, while NewTask() reports AlreadyExists, because the
// cancelled delete never removed the record from containerd's task list. That
// record does not clear on its own for as long as that containerd keeps
// running, so ReplaceTask treats the combination as a leaked record, reaps it,
// and creates again.
func ReplaceTask(ctx context.Context, log *slog.Logger, componentName string, container taskHost, evict TaskEvictor, reap TaskReaper, create TaskFactory) (containerd.Task, error) {
	// evictIfPresent reports whether a loadable task was found (and evicted).
	evictIfPresent := func() (bool, error) {
		task, err := container.Task(ctx, nil)
		if errdefs.IsNotFound(err) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("inspect existing %s task: %w", componentName, err)
		}
		log.Info("evicting stale " + componentName + " task before creating a new one")
		if err := evict(ctx, task); err != nil {
			return true, fmt.Errorf("evict stale %s task: %w", componentName, err)
		}
		return true, nil
	}

	for attempt := 1; ; attempt++ {
		if _, err := evictIfPresent(); err != nil {
			return nil, err
		}

		task, err := create(ctx)
		if err == nil {
			return task, nil
		}
		if !errdefs.IsAlreadyExists(err) {
			return nil, err
		}
		if attempt >= replaceTaskMaxAttempts {
			return nil, fmt.Errorf("%s task record still registered after %d attempts: %w", componentName, attempt, err)
		}

		// Look again before reaching for the raw delete. A task that turned up
		// since the lookup above is a real one somebody just created, not a
		// leaked record, and it deserves the graceful eviction path.
		if found, err := evictIfPresent(); err != nil {
			return nil, err
		} else if found {
			continue
		}

		log.Warn("containerd holds a leaked "+componentName+" task record (lookup says not found, create says already exists); reaping it", "attempt", attempt, "error", err)
		if err := reap(ctx, container.ID()); err != nil {
			return nil, fmt.Errorf("reap leaked %s task: %w", componentName, err)
		}
	}
}

// ReapLeakedTask asks containerd to delete a task by container ID without first
// loading it. The loaded-task path (container.Task followed by task.Delete) is
// closed to a leaked record because the lookup itself fails, but the delete RPC
// goes straight to containerd's runtime, which tolerates the shim reporting the
// container gone, shuts the shim down, and drops the record. containerd still
// surfaces the shim's NotFound as the RPC result, so that is treated as success.
func ReapLeakedTask(ctx context.Context, cc *containerd.Client, containerID string) error {
	_, err := cc.TaskService().Delete(ctx, &tasks.DeleteTaskRequest{ContainerID: containerID})
	if err != nil && status.Code(err) != codes.NotFound {
		return err
	}
	return nil
}

// ReplaceTask is the BaseComponent form of the package-level ReplaceTask: the
// eviction goes through the component's own stopTask (SIGTERM, SIGKILL, delete),
// leaked records are reaped through the component's containerd client, and
// create is the component's TaskCreator bound to container.
func (b *BaseComponent) ReplaceTask(ctx context.Context, container containerd.Container, create TaskCreator) (containerd.Task, error) {
	return ReplaceTask(ctx, b.Log, b.ComponentName, container, b.stopTask,
		func(ctx context.Context, id string) error { return ReapLeakedTask(ctx, b.CC, id) },
		func(ctx context.Context) (containerd.Task, error) { return create(ctx, container) })
}
