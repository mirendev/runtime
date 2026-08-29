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
	"miren.dev/runtime/pkg/boot"
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
	component *boot.Component
	inputs    entityAccessBootInputs
	output    boot.Output[entityAccessBootOutput]
}

func entityAccessInputs(options StartOptions) entityAccessBootInputs {
	return entityAccessBootInputs{
		group:           options.Group,
		servicePrefixes: serviceNetworkPrefixes(),
	}
}

func newEntityAccessBoot(inputs entityAccessBootInputs, coordinator boot.Output[coordinatorBootOutput], observability boot.Output[observabilityBootOutput]) *entityAccessBoot {
	b := &entityAccessBoot{inputs: inputs}
	b.component, b.output = boot.Provide2("entity-access", coordinator, observability, b.start)
	return b
}

func (b *entityAccessBoot) start(ctx context.Context, coordinator coordinatorBootOutput, observability observabilityBootOutput) (entityAccessBootOutput, error) {
	config, err := coordinator.coordinator.ServiceConfig()
	if err != nil {
		return entityAccessBootOutput{}, fmt.Errorf("getting service config: %w", err)
	}
	state, err := config.State(ctx, rpc.WithLogger(observability.log))
	if err != nil {
		return entityAccessBootOutput{}, fmt.Errorf("creating coordinator RPC client: %w", err)
	}
	client, err := state.Client("entities")
	if err != nil {
		return entityAccessBootOutput{}, fmt.Errorf("connecting to entity RPC service: %w", err)
	}

	result := entityAccessBootOutput{}
	result.access = entityserver_v1alpha.NewEntityAccessClient(client)
	result.client = entityserver.NewClient(observability.log, result.access)
	result.netService = network.NewServiceManager(observability.log, result.access)

	allocator := ipalloc.NewAllocator(observability.log, b.inputs.servicePrefixes)
	b.inputs.group.Go(func() error {
		return allocator.Watch(ctx, result.access)
	})
	return result, nil
}
