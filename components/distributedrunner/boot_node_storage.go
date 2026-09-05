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

func newNodeStorageBoot(access boot.Output[clusterAccessBootOutput]) *nodeStorageBoot {
	b := &nodeStorageBoot{}
	b.component, b.output = boot.Provide1("node-storage", access, b.start,
		boot.WithStop(b.stop, 0))
	return b
}

func (b *nodeStorageBoot) start(ctx context.Context, access clusterAccessBootOutput) (*runner.NodeStorage, error) {
	var err error
	b.value, err = runner.NewNodeStorage(access.access, runner.RunnerDeps{}, access.config)
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
