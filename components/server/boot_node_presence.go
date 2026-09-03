//go:build linux

package server

import (
	"context"

	"miren.dev/runtime/components/runner"
	"miren.dev/runtime/pkg/boot"
)

type nodePresenceBootOutput struct {
	presence *runner.NodePresence
}

type nodePresenceBoot struct {
	component *boot.Component
	value     *runner.NodePresence
	output    boot.Output[nodePresenceBootOutput]
}

func newNodePresenceBoot(host boot.Output[sandboxHostBootOutput], storageAgent, sandboxAgent *boot.Component) *nodePresenceBoot {
	b := &nodePresenceBoot{}
	b.component, b.output = boot.Provide1(
		"node-presence", host, b.start,
		boot.DependsOn(storageAgent, sandboxAgent),
		boot.WithStop(b.stop, componentStopTimeout),
	)
	return b
}

func (b *nodePresenceBoot) start(ctx context.Context, host sandboxHostBootOutput) (nodePresenceBootOutput, error) {
	b.value = runner.NewNodePresence(host.host)
	if err := b.value.Start(ctx); err != nil {
		return nodePresenceBootOutput{}, err
	}
	return nodePresenceBootOutput{presence: b.value}, nil
}

func (b *nodePresenceBoot) stop(context.Context) error {
	if b.value == nil {
		return nil
	}
	return b.value.Close()
}
