//go:build linux

package server

import (
	"context"

	"miren.dev/runtime/pkg/boot"
)

// workAdmissionBoot exposes build and deployment only after every capability
// those request paths may use has finished booting.
type workAdmissionBoot struct {
	component *boot.Component
}

func newWorkAdmissionBoot(coordinator boot.Output[coordinatorBootOutput], runner boot.Output[runnerBootOutput], buildkit boot.Output[buildkitBootOutput], ociRegistry boot.Output[struct{}], hostMapping boot.Output[registryHostMappingBootOutput]) *workAdmissionBoot {
	b := &workAdmissionBoot{}
	b.component = boot.Run5("work-admission", coordinator, runner, buildkit, ociRegistry, hostMapping, b.start)
	return b
}

func (b *workAdmissionBoot) start(_ context.Context, coordinator coordinatorBootOutput, _ runnerBootOutput, _ buildkitBootOutput, _ struct{}, _ registryHostMappingBootOutput) error {
	return coordinator.coordinator.ExposeWorkServices()
}
