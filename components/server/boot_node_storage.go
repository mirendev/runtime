//go:build linux

package server

import (
	"context"

	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/components/runner"
	"miren.dev/runtime/pkg/boot"
)

type nodeStorageBoot struct {
	component *boot.Component
	value     *runner.NodeStorage
	output    boot.Output[*runner.NodeStorage]
}

func newNodeStorageBoot(access boot.Output[clusterAccessBootOutput], registration boot.Output[registrationBootOutput]) *nodeStorageBoot {
	b := &nodeStorageBoot{}
	b.component, b.output = boot.Provide2(
		"node-storage", access, registration, b.start,
		boot.WithStop(b.stop, runnerComponentStopTimeout),
	)
	return b
}

func (b *nodeStorageBoot) start(ctx context.Context, access clusterAccessBootOutput, registration registrationBootOutput) (*runner.NodeStorage, error) {
	config := access.config
	cloudAuth := registration.cloudAuth
	if cloudAuth.Enabled {
		config.CloudAuth = &cloudAuth
	} else {
		config.CloudAuth = &coordinate.CloudAuthConfig{}
	}
	var err error
	b.value, err = runner.NewNodeStorage(access.access, runner.RunnerDeps{IsCoordinator: true}, config)
	if err != nil {
		return nil, err
	}
	if err := b.value.Start(ctx); err != nil {
		return nil, err
	}
	return b.value, nil
}

func (b *nodeStorageBoot) stop(context.Context) error {
	if b.value == nil {
		return nil
	}
	return b.value.Close()
}
