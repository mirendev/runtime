//go:build linux

package server

import (
	"context"

	deploymentattemptsctrl "miren.dev/runtime/controllers/deploymentattempts"
	"miren.dev/runtime/pkg/boot"
)

// deploymentAttemptMigrationBoot owns the controller's background lifecycle.
type deploymentAttemptMigrationBoot struct {
	component  *boot.Component
	controller *deploymentattemptsctrl.Controller
}

func newDeploymentAttemptMigrationBoot(coordinator boot.Output[coordinatorBootOutput]) *deploymentAttemptMigrationBoot {
	b := &deploymentAttemptMigrationBoot{}
	b.component = boot.Run1("deployment-attempt-migration", coordinator, b.start,
		boot.WithStop(b.stop, componentStopTimeout),
	)
	return b
}

func (b *deploymentAttemptMigrationBoot) start(ctx context.Context, coordinator coordinatorBootOutput) error {
	controller, err := coordinator.coordinator.NewDeploymentAttemptController()
	if err != nil {
		return err
	}
	b.controller = controller
	b.controller.Start(ctx)
	return nil
}

func (b *deploymentAttemptMigrationBoot) stop(context.Context) error {
	if b.controller != nil {
		b.controller.Stop()
	}
	return nil
}
