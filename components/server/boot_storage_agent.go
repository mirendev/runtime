//go:build linux

package server

import (
	"context"

	"miren.dev/runtime/components/runner"
	"miren.dev/runtime/pkg/boot"
)

type storageAgentBootOutput struct {
	agent *runner.StorageAgent
}

type storageAgentBoot struct {
	component *boot.Component
	value     *runner.StorageAgent
	output    boot.Output[storageAgentBootOutput]
}

func newStorageAgentBoot(storage boot.Output[nodeStorageBootOutput], host *boot.Component) *storageAgentBoot {
	b := &storageAgentBoot{}
	b.component, b.output = boot.Provide1(
		"storage-agent", storage, b.start,
		boot.DependsOn(host),
		boot.WithStop(b.stop, componentStopTimeout),
	)
	return b
}

func (b *storageAgentBoot) start(ctx context.Context, storage nodeStorageBootOutput) (storageAgentBootOutput, error) {
	b.value = runner.NewStorageAgent(storage.storage)
	if err := b.value.Start(ctx); err != nil {
		return storageAgentBootOutput{}, err
	}
	return storageAgentBootOutput{agent: b.value}, nil
}

func (b *storageAgentBoot) stop(context.Context) error {
	if b.value == nil {
		return nil
	}
	return b.value.Close()
}
