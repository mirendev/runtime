//go:build linux

package server

import (
	"context"

	deploymentattemptsctrl "miren.dev/runtime/controllers/deploymentattempts"
	"miren.dev/runtime/pkg/boot"
)

// deploymentAttemptMigrationBoot owns the controller's background lifecycle.
// Its feature-local gate opens after one clean sweep without holding boot open.
type deploymentAttemptMigrationBoot struct {
	component  *boot.Component
	gate       *initialSweepGate
	controller *deploymentattemptsctrl.Controller
}

func newDeploymentAttemptMigrationBoot(coordinator boot.Output[coordinatorBootOutput], gate *initialSweepGate) *deploymentAttemptMigrationBoot {
	b := &deploymentAttemptMigrationBoot{gate: gate}
	b.component = boot.Run1("deployment-attempt-migration", coordinator, b.start,
		boot.WithStop(b.stop, componentStopTimeout),
	)
	return b
}

func (b *deploymentAttemptMigrationBoot) start(ctx context.Context, coordinator coordinatorBootOutput) error {
	controller, err := coordinator.coordinator.NewDeploymentAttemptController(b.gate.Complete)
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
