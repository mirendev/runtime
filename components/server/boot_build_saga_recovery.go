//go:build linux

package server

import (
	"context"

	"miren.dev/runtime/pkg/boot"
)

type buildSagaRecoveryBoot struct {
	component *boot.Component
}

func newBuildSagaRecoveryBoot(applications boot.Output[applicationManagementBootOutput], workAdmission *boot.Component) *buildSagaRecoveryBoot {
	b := &buildSagaRecoveryBoot{}
	// Recovery remains background work. The edge guarantees only that the
	// runtime dependencies collected by work admission are ready before it
	// launches; it does not serialize recovered and newly admitted builds.
	b.component = boot.Run1(
		"build-saga-recovery", applications, b.start,
		boot.DependsOn(workAdmission),
	)
	return b
}

func (b *buildSagaRecoveryBoot) start(ctx context.Context, applications applicationManagementBootOutput) error {
	go applications.applications.RecoverBuildSagas(ctx)
	return nil
}
