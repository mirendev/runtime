//go:build linux

package server

import (
	"context"

	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/pkg/serverinfo"
	"miren.dev/runtime/pkg/serverlifecycle"
)

type serverLifecycleBootOutput struct {
	lifecycle *coordinate.ServerLifecycle
}

type serverLifecycleBoot struct {
	component *boot.Component
	output    boot.Output[serverLifecycleBootOutput]
	value     *coordinate.ServerLifecycle
	instance  *serverinfo.Source
}

// The ledger is on disk and the RPC only reads it, so this needs nothing but
// the foundation. The cloud uplink takes its output so the capability is
// registered before the link connects.
func newServerLifecycleBoot(instance *serverinfo.Source, foundation boot.Output[foundationBootOutput]) *serverLifecycleBoot {
	b := &serverLifecycleBoot{instance: instance}
	b.component, b.output = boot.Provide1(
		"server-lifecycle", foundation, b.start,
		boot.WithStop(b.stop, componentStopTimeout),
	)
	return b
}

func (b *serverLifecycleBoot) start(ctx context.Context, foundation foundationBootOutput) (serverLifecycleBootOutput, error) {
	lifecycle := coordinate.NewServerLifecycle(foundation.foundation, b.instance, serverlifecycle.DefaultDir)
	if err := lifecycle.Start(ctx); err != nil {
		return serverLifecycleBootOutput{}, err
	}
	b.value = lifecycle
	return serverLifecycleBootOutput{lifecycle: lifecycle}, nil
}

func (b *serverLifecycleBoot) stop(context.Context) error {
	if b.value != nil {
		b.value.Stop()
	}
	return nil
}
