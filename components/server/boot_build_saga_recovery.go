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

func newBuildSagaRecoveryBoot(inputs buildSagaRecoveryBootInputs, applications boot.Output[applicationManagementBootOutput], workAdmission *boot.Component) *buildSagaRecoveryBoot {
	b := &buildSagaRecoveryBoot{}
	if inputs.enabled {
		// Recovery remains background work. The edge guarantees only that the
		// runtime dependencies collected by work admission are ready before it
		// launches; it does not serialize recovered and newly admitted builds.
		b.component = boot.Run1(
			"build-saga-recovery", applications, b.start,
			boot.DependsOn(workAdmission),
		)
	} else {
		b.component = boot.Run1("build-saga-recovery", applications, func(context.Context, applicationManagementBootOutput) error { return nil })
	}
	return b
}

func (b *buildSagaRecoveryBoot) start(ctx context.Context, applications applicationManagementBootOutput) error {
	go applications.applications.RecoverBuildSagas(ctx)
	return nil
}
