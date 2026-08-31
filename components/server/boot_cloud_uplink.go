//go:build linux

package server

import (
	"context"

	"miren.dev/runtime/pkg/boot"
)

type cloudUplinkBoot struct {
	component *boot.Component
}

func newCloudUplinkBoot(coordinator boot.Output[coordinatorBootOutput], deploymentAttempts boot.Output[deploymentAttemptMigrationBootOutput]) *cloudUplinkBoot {
	b := &cloudUplinkBoot{}
	b.component = boot.Run2("cloud-uplink", coordinator, deploymentAttempts, b.start)
	return b
}

func (b *cloudUplinkBoot) start(ctx context.Context, coordinator coordinatorBootOutput, deploymentAttempts deploymentAttemptMigrationBootOutput) error {
	go func() {
		if err := coordinator.coordinator.RunCloudUplink(ctx, deploymentAttempts.entitySyncReady); err != nil && ctx.Err() == nil {
			coordinator.coordinator.Log.Error("cloud uplink exited with error", "error", err)
		}
	}()
	return nil
}
