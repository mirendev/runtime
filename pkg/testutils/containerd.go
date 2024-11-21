package testutils

import (
	"context"
	"fmt"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/typeurl/v2"
	"github.com/davecgh/go-spew/spew"
)

func ClearContainers(cl *containerd.Client, ns string) error {
	ctx := namespaces.WithNamespace(context.Background(), ns)
	containers, err := cl.Containers(ctx)
	if err != nil {
		return err
	}

	for _, container := range containers {
		task, _ := container.Task(ctx, nil)
		if task != nil {
			task.Delete(ctx, containerd.WithProcessKill)
		}

		if err := container.Delete(ctx, containerd.WithSnapshotCleanup); err != nil {
			return err
		}
	}

	return nil
}

func ClearContainer(ctx context.Context, cont containerd.Container) {
	task, _ := cont.Task(ctx, nil)
	if task != nil {
		task.Delete(ctx, containerd.WithProcessKill)
	}

	cont.Delete(ctx, containerd.WithSnapshotCleanup)
}

func ListContainers(cl *containerd.Client, ns string) ([]string, error) {
	ctx := namespaces.WithNamespace(context.Background(), ns)
	containers, err := cl.Containers(ctx)
	if err != nil {
		return nil, err
	}

	var ret []string

	for _, container := range containers {
		ret = append(ret, container.ID())
	}

	return ret, nil
}

func MonitorContainers(ctx context.Context, cl *containerd.Client, ns string) error {
	ctx = namespaces.WithNamespace(ctx, ns)

	eventsCh, errs := cl.EventService().Subscribe(ctx)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev := <-eventsCh:
			v, err := typeurl.UnmarshalAny(ev.Event)
			spew.Dump(ev, v, err)
		case err := <-errs:
			return err
		}
	}
}

func WaitForContainerReady(ctx context.Context, cl *containerd.Client, ns, id string) error {
	ctx = namespaces.WithNamespace(ctx, ns)

	var err error

	for i := 0; i < 10; i++ {
		cont, err := cl.LoadContainer(ctx, id)
		if err == nil {
			task, err := cont.Task(ctx, nil)
			if err == nil {
				status, err := task.Status(ctx)
				if err == nil {
					if status.Status == containerd.Running {
						return nil
					}
				}
			}
		}

		time.Sleep(time.Second)
	}

	if err == nil {
		err = fmt.Errorf("timed out waiting for container %s", id)
	}
	return err
}
