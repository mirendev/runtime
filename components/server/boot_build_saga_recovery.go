//go:build linux

package server

import (
	"context"

	"miren.dev/runtime/pkg/readiness"
)

type buildSagaRecoveryBootInputs struct {
	enabled bool
}

type buildSagaRecoveryBoot struct {
	component   *readiness.Component
	inputs      buildSagaRecoveryBootInputs
	coordinator *coordinatorBoot
	buildkit    *buildkitBoot
	ociRegistry *ociRegistryBoot
	hostMapping *registryHostMappingBoot
}

func buildSagaRecoveryInputs(options StartOptions) buildSagaRecoveryBootInputs {
	return buildSagaRecoveryBootInputs{enabled: buildkitEnabled(options.Config.Buildkit)}
}

func newBuildSagaRecoveryBoot(inputs buildSagaRecoveryBootInputs, coordinator *coordinatorBoot, buildkit *buildkitBoot, ociRegistry *ociRegistryBoot, hostMapping *registryHostMappingBoot) *buildSagaRecoveryBoot {
	b := &buildSagaRecoveryBoot{
		inputs:      inputs,
		coordinator: coordinator,
		buildkit:    buildkit,
		ociRegistry: ociRegistry,
		hostMapping: hostMapping,
	}
	dependencies := []readiness.Dependency{
		readiness.ReadyDep(coordinator.component),
	}
	if inputs.enabled {
		dependencies = append(dependencies,
			readiness.ReadyDep(buildkit.component),
			readiness.ReadyDep(ociRegistry.component),
			readiness.ReadyDep(hostMapping.component),
		)
	}
	b.component = readiness.NewComponent("build-saga-recovery", readiness.Spec{
		Dependencies: dependencies,
		Start:        b.start,
	})
	return b
}

func (b *buildSagaRecoveryBoot) start(ctx context.Context, _ readiness.Reporter) error {
	if !b.inputs.enabled {
		return nil
	}
	go b.coordinator.output().coordinator.RecoverBuildSagas(ctx)
	return nil
}
