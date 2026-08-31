//go:build linux

package server

import (
	"context"
	"time"

	deploymentattemptsctrl "miren.dev/runtime/controllers/deploymentattempts"
	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/pkg/labs"
)

// deploymentAttemptMigrationBoot owns the controller's background lifecycle.
type deploymentAttemptMigrationBoot struct {
	component  *boot.Component
	controller *deploymentattemptsctrl.Controller
	output     boot.Output[deploymentAttemptMigrationBootOutput]
}

type deploymentAttemptMigrationBootOutput struct {
	entitySyncReady <-chan struct{}
}

const markerBackfillRetryInterval = 10 * time.Second

func newDeploymentAttemptMigrationBoot(coordinator boot.Output[coordinatorBootOutput]) *deploymentAttemptMigrationBoot {
	b := &deploymentAttemptMigrationBoot{}
	b.component, b.output = boot.Provide1("deployment-attempt-migration", coordinator, b.start,
		boot.WithStop(b.stop, componentStopTimeout),
	)
	return b
}

func (b *deploymentAttemptMigrationBoot) start(ctx context.Context, coordinator coordinatorBootOutput) (deploymentAttemptMigrationBootOutput, error) {
	controller, err := coordinator.coordinator.NewDeploymentAttemptController()
	if err != nil {
		return deploymentAttemptMigrationBootOutput{}, err
	}
	b.controller = controller
	b.controller.Start(ctx)
	ready := make(chan struct{})
	if !labs.AppVisibility() {
		close(ready)
		return deploymentAttemptMigrationBootOutput{entitySyncReady: ready}, nil
	}
	go b.prepareEntitySync(ctx, coordinator, ready)
	return deploymentAttemptMigrationBootOutput{entitySyncReady: ready}, nil
}

func (b *deploymentAttemptMigrationBoot) prepareEntitySync(ctx context.Context, coordinator coordinatorBootOutput, ready chan<- struct{}) {
	select {
	case <-ctx.Done():
		return
	case <-b.controller.Ready():
	}

	for {
		if err := coordinator.coordinator.BackfillCloudExportMarker(ctx); err == nil {
			close(ready)
			coordinator.coordinator.Log.Info("entity sync source preparation complete")
			return
		} else if ctx.Err() == nil {
			coordinator.coordinator.Log.Warn("cloud export marker backfill failed; retrying", "error", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(markerBackfillRetryInterval):
		}
	}
}

func (b *deploymentAttemptMigrationBoot) stop(context.Context) error {
	if b.controller != nil {
		b.controller.Stop()
	}
	return nil
}
