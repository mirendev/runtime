//go:build linux

package server

import (
	"context"

	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/pkg/boot"
)

type entityMaintenanceBootOutput struct {
	maintenance *coordinate.EntityMaintenance
}

type entityMaintenanceBoot struct {
	component *boot.Component
	value     *coordinate.EntityMaintenance
	output    boot.Output[entityMaintenanceBootOutput]
}

func newEntityMaintenanceBoot(foundation boot.Output[foundationBootOutput], appData *boot.Component) *entityMaintenanceBoot {
	b := &entityMaintenanceBoot{}
	b.component, b.output = boot.Provide1(
		"entity-maintenance", foundation, b.start,
		boot.DependsOn(appData),
		boot.WithStop(b.stop, componentStopTimeout),
	)
	return b
}

func (b *entityMaintenanceBoot) start(ctx context.Context, foundation foundationBootOutput) (entityMaintenanceBootOutput, error) {
	b.value = coordinate.NewEntityMaintenance(foundation.foundation)
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
