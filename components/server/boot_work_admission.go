//go:build linux

package server

import (
	"context"

	"miren.dev/runtime/pkg/boot"
)

// workAdmissionBoot exposes build and deployment operations only after every
// capability those work-creating paths may use has finished booting.
type workAdmissionBoot struct {
	component *boot.Component
}

func newWorkAdmissionBoot(applications boot.Output[applicationManagementBootOutput], workloads, runner, buildkit, ociRegistry, hostMapping *boot.Component) *workAdmissionBoot {
	b := &workAdmissionBoot{}
	b.component = boot.Run1(
		"work-admission", applications, b.start,
		boot.DependsOn(workloads, runner, buildkit, ociRegistry, hostMapping),
	)
	return b
}

func (b *workAdmissionBoot) start(_ context.Context, applications applicationManagementBootOutput) error {
	return applications.applications.ExposeBuildAndDeployment()
}
