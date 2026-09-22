//go:build linux

package server

import (
	"context"

	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/pkg/entitysync"
)

type entityMaintenanceBootOutput struct {
	maintenance *coordinate.EntityMaintenance
}

type entityMaintenanceBoot struct {
	component   *boot.Component
	value       *coordinate.EntityMaintenance
	output      boot.Output[entityMaintenanceBootOutput]
	diagnostics *entitysync.Diagnostics
}

func newEntityMaintenanceBoot(foundation boot.Output[foundationBootOutput], deploymentAttempts boot.Output[deploymentAttemptMigrationBootOutput], appData *boot.Component, diagnostics *entitysync.Diagnostics) *entityMaintenanceBoot {
	b := &entityMaintenanceBoot{diagnostics: diagnostics}
	b.component, b.output = boot.Provide2(
		"entity-maintenance", foundation, deploymentAttempts, b.start,
		boot.DependsOn(appData),
		boot.WithStop(b.stop, componentStopTimeout),
	)
	return b
}

func (b *entityMaintenanceBoot) start(ctx context.Context, foundation foundationBootOutput, deploymentAttempts deploymentAttemptMigrationBootOutput) (entityMaintenanceBootOutput, error) {
	b.value = coordinate.NewEntityMaintenance(foundation.foundation)
	b.value.DeploymentHistoryReady = deploymentAttempts.entitySyncReady
	b.value.DeploymentExports = b.diagnostics
	if err := b.value.Start(ctx); err != nil {
		return entityMaintenanceBootOutput{}, err
	}
	return entityMaintenanceBootOutput{maintenance: b.value}, nil
}

func (b *entityMaintenanceBoot) stop(context.Context) error {
	if b.value != nil {
		b.value.Stop()
	}
	return nil
}
