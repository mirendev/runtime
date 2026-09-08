//go:build linux

package server

import (
	"context"

	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/pkg/boot"
)

type workloadControlBootOutput struct {
	workloadControl *coordinate.WorkloadControl
}

type workloadControlBoot struct {
	component *boot.Component
	value     *coordinate.WorkloadControl
	output    boot.Output[workloadControlBootOutput]
}

func newWorkloadControlBoot(foundation boot.Output[foundationBootOutput], applications boot.Output[applicationManagementBootOutput], host *boot.Component) *workloadControlBoot {
	b := &workloadControlBoot{}
	b.component, b.output = boot.Provide2(
		"workload-control", foundation, applications, b.start,
		// After a crash the coordinator's old session-scoped READY status can
		// remain schedulable until its lease expires. Restore the local host before
		// starting placement so new stateful work cannot land on an unready host.
		boot.DependsOn(host),
		boot.WithStop(b.stop, componentStopTimeout),
	)
	return b
}

func (b *workloadControlBoot) start(ctx context.Context, foundation foundationBootOutput, applications applicationManagementBootOutput) (workloadControlBootOutput, error) {
	b.value = coordinate.NewWorkloadControl(foundation.foundation, applications.applications)
	if err := b.value.Start(ctx); err != nil {
		return workloadControlBootOutput{}, err
	}
	return workloadControlBootOutput{workloadControl: b.value}, nil
}

func (b *workloadControlBoot) stop(context.Context) error {
	if b.value != nil {
		b.value.Stop()
	}
	return nil
}
