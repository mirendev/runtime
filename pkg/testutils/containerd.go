package testutils

import (
	"context"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
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
