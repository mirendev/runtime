//go:build linux

package server

import (
	"context"

	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/components/netresolve"
	"miren.dev/runtime/components/runner"
	"miren.dev/runtime/pkg/boot"
)

type nodeStorageBoot struct {
	component *boot.Component
	value     *runner.NodeStorage
	output    boot.Output[*runner.NodeStorage]
	resolver  netresolve.Resolver
}

func newNodeStorageBoot(resolver netresolve.Resolver, mapping *boot.Component, access boot.Output[clusterAccessBootOutput], registration boot.Output[registrationBootOutput], observability boot.Output[observabilityBootOutput], containerd boot.Output[containerdBootOutput]) *nodeStorageBoot {
	b := &nodeStorageBoot{resolver: resolver}
	b.component, b.output = boot.Provide4(
		"node-storage", access, registration, observability, containerd, b.start,
		boot.DependsOn(mapping),
		boot.WithStop(b.stop, runnerComponentStopTimeout),
	)
	return b
}

func (b *nodeStorageBoot) start(ctx context.Context, access clusterAccessBootOutput, registration registrationBootOutput, observability observabilityBootOutput, containerd containerdBootOutput) (*runner.NodeStorage, error) {
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
		MetricsWriter: observability.operationalMetrics,
		CC:            containerd.Client,
		Resolver:      b.resolver,
	}, config)
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
