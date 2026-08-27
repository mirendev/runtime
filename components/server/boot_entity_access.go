//go:build linux

package server

import (
	"context"
	"fmt"
	"net/netip"

	"golang.org/x/sync/errgroup"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/components/ipalloc"
	"miren.dev/runtime/network"
	"miren.dev/runtime/pkg/readiness"
	"miren.dev/runtime/pkg/rpc"
)

type entityAccessBootInputs struct {
	group           *errgroup.Group
	servicePrefixes []netip.Prefix
}

type entityAccessBootOutput struct {
	access     *entityserver_v1alpha.EntityAccessClient
	client     *entityserver.Client
	netService *network.ServiceManager
}

type entityAccessBoot struct {
	component     *readiness.Component
	inputs        entityAccessBootInputs
	coordinator   *coordinatorBoot
	observability *observabilityBoot
	result        entityAccessBootOutput
}

func entityAccessInputs(options StartOptions) entityAccessBootInputs {
	return entityAccessBootInputs{
		group: options.Group,
		servicePrefixes: []netip.Prefix{
			netip.MustParsePrefix("10.10.0.0/16"),
			netip.MustParsePrefix("fd47:cafe:d00d::/64"),
		},
	}
}

func newEntityAccessBoot(inputs entityAccessBootInputs, coordinator *coordinatorBoot, observability *observabilityBoot) *entityAccessBoot {
	b := &entityAccessBoot{inputs: inputs, coordinator: coordinator, observability: observability}
	b.component = readiness.NewComponent("entity-access", readiness.Spec{
		Dependencies: []readiness.Dependency{
			readiness.ReadyDep(coordinator.component),
			readiness.ReadyDep(observability.component),
		},
		Start: b.start,
	})
	return b
}

func (b *entityAccessBoot) output() entityAccessBootOutput {
	return b.result
}

func (b *entityAccessBoot) start(ctx context.Context, _ readiness.Reporter) error {
	coordinator := b.coordinator.output().coordinator
	config, err := coordinator.ServiceConfig()
	if err != nil {
		return fmt.Errorf("getting service config: %w", err)
	}
	state, err := config.State(ctx, rpc.WithLogger(b.observability.output().log))
	if err != nil {
		return fmt.Errorf("creating coordinator RPC client: %w", err)
	}
	client, err := state.Client("entities")
	if err != nil {
		return fmt.Errorf("connecting to entity RPC service: %w", err)
	}

	b.result.access = entityserver_v1alpha.NewEntityAccessClient(client)
	b.result.client = entityserver.NewClient(b.observability.output().log, b.result.access)
	b.result.netService = network.NewServiceManager(b.observability.output().log, b.result.access)

	allocator := ipalloc.NewAllocator(b.observability.output().log, b.inputs.servicePrefixes)
	b.inputs.group.Go(func() error {
		return allocator.Watch(ctx, b.result.access)
	})
	return nil
}
