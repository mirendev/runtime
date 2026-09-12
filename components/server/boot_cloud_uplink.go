//go:build linux

package server

import (
	"context"

	"miren.dev/runtime/pkg/boot"
)

type cloudUplinkBoot struct {
	component *boot.Component
}

func newCloudUplinkBoot(cloud boot.Output[cloudControlBootOutput], deploymentAttempts boot.Output[deploymentAttemptMigrationBootOutput], ingress boot.Output[ingressBootOutput]) *cloudUplinkBoot {
	b := &cloudUplinkBoot{}
	b.component = boot.Run3("cloud-uplink", cloud, deploymentAttempts, ingress, b.start)
	return b
}

func (b *cloudUplinkBoot) start(ctx context.Context, cloud cloudControlBootOutput, deploymentAttempts deploymentAttemptMigrationBootOutput, ingress ingressBootOutput) error {
	go func() {
		if err := cloud.cloud.RunCloudUplink(ctx, ingress.server, deploymentAttempts.entitySyncReady); err != nil && ctx.Err() == nil {
			cloud.cloud.Log.Error("cloud uplink exited with error", "error", err)
		}
	}()
	return nil
}
