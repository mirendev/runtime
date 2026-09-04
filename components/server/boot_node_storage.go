//go:build linux

package server

import (
	"context"

	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/components/runner"
	"miren.dev/runtime/pkg/boot"
)

type nodeStorageBootOutput struct {
	storage *runner.NodeStorage
}

type nodeStorageBoot struct {
	component *boot.Component
	value     *runner.NodeStorage
	output    boot.Output[nodeStorageBootOutput]
}

func newNodeStorageBoot(access boot.Output[clusterAccessBootOutput], registration boot.Output[registrationBootOutput], observability boot.Output[observabilityBootOutput]) *nodeStorageBoot {
	b := &nodeStorageBoot{}
	b.component, b.output = boot.Provide3(
		"node-storage", access, registration, observability, b.start,
		boot.WithStop(b.stop, runnerComponentStopTimeout),
	)
	return b
}

func (b *nodeStorageBoot) start(ctx context.Context, access clusterAccessBootOutput, registration registrationBootOutput, observability observabilityBootOutput) (nodeStorageBootOutput, error) {
	config := access.config
	cloudAuth := registration.cloudAuth
	if cloudAuth.Enabled {
		config.CloudAuth = &cloudAuth
	} else {
		config.CloudAuth = &coordinate.CloudAuthConfig{}
	}
	var err error
	b.value, err = runner.NewNodeStorage(access.access, runner.RunnerDeps{
		IsCoordinator: true,
		MetricsWriter: observability.metricsWriter,
	}, config)
	if err != nil {
		return nodeStorageBootOutput{}, err
	}
	if err := b.value.Start(ctx); err != nil {
		return nodeStorageBootOutput{}, err
	}
	return nodeStorageBootOutput{storage: b.value}, nil
}

func (b *nodeStorageBoot) stop(context.Context) error {
	if b.value == nil {
		return nil
	}
	return b.value.Close()
}
