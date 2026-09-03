//go:build linux

package server

import (
	"context"
	"fmt"
	"time"

	deploymentattemptsctrl "miren.dev/runtime/controllers/deploymentattempts"
	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/pkg/entitysync"
	"miren.dev/runtime/pkg/labs"
)

// deploymentAttemptMigrationBoot owns the controller's background lifecycle.
type deploymentAttemptMigrationBoot struct {
	component   *boot.Component
	controller  *deploymentattemptsctrl.Controller
	output      boot.Output[deploymentAttemptMigrationBootOutput]
	diagnostics *entitysync.Diagnostics
}

type deploymentAttemptMigrationBootOutput struct {
	entitySyncReady <-chan struct{}
}

const markerBackfillRetryInterval = 10 * time.Second

func newDeploymentAttemptMigrationBoot(foundation boot.Output[foundationBootOutput], appData *boot.Component, diagnostics *entitysync.Diagnostics) *deploymentAttemptMigrationBoot {
	b := &deploymentAttemptMigrationBoot{diagnostics: diagnostics}
	// Both startup migrations update AppVersions. The order-only edge prevents
	// this controller's patches from racing the older whole-entity rewrite.
	b.component, b.output = boot.Provide1("deployment-attempt-migration", foundation, b.start,
		boot.DependsOn(appData),
		boot.WithStop(b.stop, componentStopTimeout),
	)
	return b
}

func (b *deploymentAttemptMigrationBoot) start(ctx context.Context, foundation foundationBootOutput) (deploymentAttemptMigrationBootOutput, error) {
	controller, err := foundation.foundation.NewDeploymentAttemptController()
	if err != nil {
		return deploymentAttemptMigrationBootOutput{}, err
	}
	b.controller = controller
	diagnostics := b.diagnostics
	if labs.AppVisibility() {
		diagnostics.SetPreparation("deployment-migration", "waiting for the first clean migration and reconciliation sweep")
		b.controller.SetProgressReporter(func(progress deploymentattemptsctrl.Progress) {
			if progress.Ready {
				return
			}
			state := "deployment-migration"
			detail := fmt.Sprintf("phase %s", progress.Phase)
			if progress.Cursor != "" {
				detail += ", cursor " + progress.Cursor
			}
			if progress.PassFailed {
				detail += ", current sweep has failures"
			}
			if progress.LastError != "" {
				state = "deployment-migration-retrying"
				detail += ": " + progress.LastError
				diagnostics.SetPreparationFailure(state, detail)
				return
			}
			diagnostics.SetPreparation(state, detail)
		})
	}
	b.controller.Start(ctx)
	ready := make(chan struct{})
	if !labs.AppVisibility() {
		diagnostics.SetPreparation("disabled", "app visibility is disabled")
		close(ready)
		return deploymentAttemptMigrationBootOutput{entitySyncReady: ready}, nil
	}
	go b.prepareEntitySync(ctx, foundation, ready)
	return deploymentAttemptMigrationBootOutput{entitySyncReady: ready}, nil
}

func (b *deploymentAttemptMigrationBoot) prepareEntitySync(ctx context.Context, foundation foundationBootOutput, ready chan<- struct{}) {
	select {
	case <-ctx.Done():
		return
	case <-b.controller.Ready():
	}
	diagnostics := b.diagnostics
	diagnostics.SetPreparation("marker-backfill", "backfilling the cloud export marker")

	for {
		if stats, err := foundation.foundation.BackfillCloudExportMarker(ctx); err == nil {
			diagnostics.SetPreparation("ready", fmt.Sprintf(
				"marker backfill scanned %d entities, marked %d, already marked %d",
				stats.Scanned, stats.Marked, stats.AlreadyMarked,
			))
			close(ready)
			foundation.foundation.Log.Info("entity sync source preparation complete")
			return
		} else if ctx.Err() == nil {
			diagnostics.SetPreparationFailure("marker-backfill-retrying", err.Error())
			foundation.foundation.Log.Warn("cloud export marker backfill failed; retrying", "error", err)
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
