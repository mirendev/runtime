//go:build linux

package server

import (
	"context"

	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/pkg/entitysync"
)

type cloudControlBootOutput struct {
	cloud *coordinate.CloudControl
}

type cloudControlBoot struct {
	component   *boot.Component
	output      boot.Output[cloudControlBootOutput]
	value       *coordinate.CloudControl
	diagnostics *entitysync.Diagnostics
}

func newCloudControlBoot(foundation boot.Output[foundationBootOutput], applications, maintenance, workloads *boot.Component, diagnostics *entitysync.Diagnostics) *cloudControlBoot {
	b := &cloudControlBoot{diagnostics: diagnostics}
	b.component, b.output = boot.Provide1(
		"cloud-control", foundation, b.start,
		// Cloud startup status means the management, maintenance, and workload
		// control planes are available. It does not promise ingress or build and
		// deployment admission, which have their own readiness boundaries.
		boot.DependsOn(applications, maintenance, workloads),
		boot.WithStop(b.stop, componentStopTimeout),
	)
	return b
}

func (b *cloudControlBoot) start(ctx context.Context, foundation foundationBootOutput) (cloudControlBootOutput, error) {
	cloud := coordinate.NewCloudControl(foundation.foundation, b.diagnostics)
	if err := cloud.Start(ctx); err != nil {
		return cloudControlBootOutput{}, err
	}
	b.value = cloud
	return cloudControlBootOutput{cloud: cloud}, nil
}

func (b *cloudControlBoot) stop(context.Context) error {
	if b.value != nil {
		b.value.Stop()
	}
	return nil
}
