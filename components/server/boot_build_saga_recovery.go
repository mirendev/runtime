//go:build linux

package server

import (
	"context"

	"miren.dev/runtime/pkg/boot"
)

type buildSagaRecoveryBootInputs struct {
	enabled bool
}

type buildSagaRecoveryBoot struct {
	component *boot.Component
}

func buildSagaRecoveryInputs(options StartOptions) buildSagaRecoveryBootInputs {
	return buildSagaRecoveryBootInputs{enabled: buildkitEnabled(options.Config.Buildkit)}
}

func newBuildSagaRecoveryBoot(inputs buildSagaRecoveryBootInputs, coordinator boot.Output[coordinatorBootOutput], buildkit boot.Output[buildkitBootOutput], ociRegistry boot.Output[struct{}], hostMapping boot.Output[registryHostMappingBootOutput]) *buildSagaRecoveryBoot {
	b := &buildSagaRecoveryBoot{}
	if inputs.enabled {
		b.component = boot.Run4("build-saga-recovery", coordinator, buildkit, ociRegistry, hostMapping, b.start)
	} else {
		b.component = boot.Run1("build-saga-recovery", coordinator, func(context.Context, coordinatorBootOutput) error { return nil })
	}
	return b
}

func (b *buildSagaRecoveryBoot) start(ctx context.Context, coordinator coordinatorBootOutput, _ buildkitBootOutput, _ struct{}, _ registryHostMappingBootOutput) error {
	go coordinator.coordinator.RecoverBuildSagas(ctx)
	return nil
}
