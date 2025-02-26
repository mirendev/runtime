package run

import (
	"context"
	"time"

	"github.com/containerd/containerd/v2/client"
)

func (c *ContainerRunner) ReconcileLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			err := c.reconcile(ctx)
			if err != nil {
				c.Log.Error("reconcile failed", "error", err)
			}
		}
	}
}

func (c *ContainerRunner) reconcile(ctx context.Context) error {
	configs, err := c.store.List()
	if err != nil {
		return err
	}

	var toRestart []*ContainerConfig

	for _, cfg := range configs {
		co, err := c.CC.LoadContainer(ctx, cfg.Id)
		if err != nil {
			c.Log.Error("load container failed, restarting", "id", cfg.Id, "error", err)
			toRestart = append(toRestart, cfg)
			continue
		}

		if co == nil {
			c.Log.Error("container not found, restarting", "id", cfg.Id)
			toRestart = append(toRestart, cfg)
			continue
		}

		task, err := co.Task(ctx, nil)
		if err != nil {
			c.Log.Error("task failed, restarting", "id", cfg.Id, "error", err)
			toRestart = append(toRestart, cfg)
			continue
		}

		status, err := task.Status(ctx)
		if err != nil {
			c.Log.Error("status failed, restarting", "id", cfg.Id, "error", err)
			toRestart = append(toRestart, cfg)
			continue
		}

		if status.Status != client.Running {
			c.Log.Error("container not running, restarting", "id", cfg.Id, "status", status)
			toRestart = append(toRestart, cfg)
			continue
		}
	}

	for _, cfg := range toRestart {
		err := c.RestartContainer(ctx, cfg)
		if err != nil {
			c.Log.Error("restart failed", "id", cfg.Id, "error", err)
		} else {
			c.Log.Info("restarted", "id", cfg.Id)
		}
	}

	return nil
}
