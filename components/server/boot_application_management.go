//go:build linux

package server

import (
	"context"

	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/pkg/entitysync"
)

type applicationManagementBootOutput struct {
	applications *coordinate.ApplicationManagement
}

type applicationManagementBoot struct {
	component   *boot.Component
	value       *coordinate.ApplicationManagement
	output      boot.Output[applicationManagementBootOutput]
	diagnostics *entitysync.Diagnostics
}

func newApplicationManagementBoot(foundation boot.Output[foundationBootOutput], secrets boot.Output[secretStoreBootOutput], appData *boot.Component, diagnostics *entitysync.Diagnostics) *applicationManagementBoot {
	b := &applicationManagementBoot{diagnostics: diagnostics}
	b.component, b.output = boot.Provide2(
		"application-management", foundation, secrets, b.start,
		boot.DependsOn(appData),
		boot.WithStop(b.stop, componentStopTimeout),
	)
	return b
}

func (b *applicationManagementBoot) start(ctx context.Context, foundation foundationBootOutput, secrets secretStoreBootOutput) (applicationManagementBootOutput, error) {
	b.value = coordinate.NewApplicationManagement(foundation.foundation, secrets.secretStore, b.diagnostics)
	if err := b.value.Start(ctx); err != nil {
		return applicationManagementBootOutput{}, err
	}
	return applicationManagementBootOutput{applications: b.value}, nil
}

func (b *applicationManagementBoot) stop(context.Context) error {
	if b.value != nil {
		b.value.Stop()
	}
	return nil
}
