//go:build linux

package server

import (
	"context"

	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/pkg/boot"
)

type runnerEndpointsBoot struct {
	component *boot.Component
	output    boot.Output[runnerEndpointsBootOutput]
}

type runnerEndpointsBootOutput struct {
	endpoints *coordinate.RunnerEndpoints
}

func newRunnerEndpointsBoot(foundation boot.Output[foundationBootOutput], secrets *boot.Component) *runnerEndpointsBoot {
	b := &runnerEndpointsBoot{}
	b.component, b.output = boot.Provide1(
		"runner-endpoints", foundation, b.start,
		// A remote runner treats secret setup as mandatory. Do not publish the
		// registration endpoint until the secret endpoint it will receive is live.
		boot.DependsOn(secrets),
	)
	return b
}

func (b *runnerEndpointsBoot) start(ctx context.Context, foundation foundationBootOutput) (runnerEndpointsBootOutput, error) {
	endpoints := coordinate.NewRunnerEndpoints(foundation.foundation)
	if err := endpoints.Start(ctx); err != nil {
		return runnerEndpointsBootOutput{}, err
	}
	return runnerEndpointsBootOutput{endpoints: endpoints}, nil
}
