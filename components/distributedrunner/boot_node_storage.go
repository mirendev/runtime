//go:build linux

package distributedrunner

import (
	"context"

	"miren.dev/runtime/components/runner"
	"miren.dev/runtime/pkg/boot"
)

type nodeStorageBoot struct {
	component *boot.Component
	value     *runner.NodeStorage
	output    boot.Output[*runner.NodeStorage]
}

func newNodeStorageBoot(access boot.Output[clusterAccessBootOutput], telemetry boot.Output[telemetryBootOutput], containerd boot.Output[containerdBootOutput], networkDeps boot.Output[runner.RunnerDeps]) *nodeStorageBoot {
	b := &nodeStorageBoot{}
	b.component, b.output = boot.Provide4("node-storage", access, telemetry, containerd, networkDeps, b.start,
		boot.WithStop(b.stop, 0))
	return b
}

func (b *nodeStorageBoot) start(ctx context.Context, access clusterAccessBootOutput, telemetry telemetryBootOutput, containerd containerdBootOutput, networkDeps runner.RunnerDeps) (*runner.NodeStorage, error) {
	var err error
	b.value, err = runner.NewNodeStorage(access.access, runner.RunnerDeps{
		MetricsWriter: telemetry.metricsWriter,
		CC:            containerd.Client,
		Resolver:      networkDeps.Resolver,
	}, access.config)
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
