//go:build linux

package server

import (
	"context"

	"miren.dev/runtime/pkg/boot"
)

type cloudUplinkBoot struct {
	component *boot.Component
}

func newCloudUplinkBoot(cloud boot.Output[cloudControlBootOutput], deploymentAttempts boot.Output[deploymentAttemptMigrationBootOutput], ingress boot.Output[ingressBootOutput], lifecycle boot.Output[serverLifecycleBootOutput]) *cloudUplinkBoot {
	b := &cloudUplinkBoot{}
	b.component = boot.Run4("cloud-uplink", cloud, deploymentAttempts, ingress, lifecycle, b.start)
	return b
}

func (b *cloudUplinkBoot) start(ctx context.Context, cloud cloudControlBootOutput, deploymentAttempts deploymentAttemptMigrationBootOutput, ingress ingressBootOutput, lifecycle serverLifecycleBootOutput) error {
	cloud.cloud.Lifecycle = lifecycle.lifecycle
	go func() {
		if err := cloud.cloud.RunCloudUplink(ctx, ingress.server, deploymentAttempts.entitySyncReady); err != nil && ctx.Err() == nil {
			cloud.cloud.Log.Error("cloud uplink exited with error", "error", err)
		}
	}()
	return nil
}
