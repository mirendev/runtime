//go:build linux

package server

import (
	"context"

	"miren.dev/runtime/components/runner"
	"miren.dev/runtime/pkg/boot"
)

type sandboxAgentBootOutput struct {
	agent *runner.SandboxAgent
}

type sandboxAgentBoot struct {
	component *boot.Component
	value     *runner.SandboxAgent
	output    boot.Output[sandboxAgentBootOutput]
}

func newSandboxAgentBoot(host boot.Output[sandboxHostBootOutput], workloads *boot.Component) *sandboxAgentBoot {
	b := &sandboxAgentBoot{}
	b.component, b.output = boot.Provide1(
		"sandbox-agent", host, b.start,
		// Let cluster-level workload owners recover before the local watcher
		// begins acting on their desired state.
		boot.DependsOn(workloads),
		boot.WithStop(b.stop, componentStopTimeout),
	)
	return b
}

func (b *sandboxAgentBoot) start(ctx context.Context, host sandboxHostBootOutput) (sandboxAgentBootOutput, error) {
	b.value = runner.NewSandboxAgent(host.host)
	if err := b.value.Start(ctx); err != nil {
		return sandboxAgentBootOutput{}, err
	}
	return sandboxAgentBootOutput{agent: b.value}, nil
}

func (b *sandboxAgentBoot) stop(context.Context) error {
	if b.value == nil {
		return nil
	}
	return b.value.Close()
}
