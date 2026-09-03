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
	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/pkg/rpc"
)

type entityAccessBootInputs struct {
	group           *errgroup.Group
	servicePrefixes []netip.Prefix
}

type entityAccessBootOutput struct {
	access    *entityserver_v1alpha.EntityAccessClient
	client    *entityserver.Client
	rpcClient rpc.Client
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

func newEntityAccessBoot(inputs entityAccessBootInputs, foundation boot.Output[foundationBootOutput], observability boot.Output[observabilityBootOutput]) *entityAccessBoot {
	b := &entityAccessBoot{inputs: inputs}
	b.component, b.output = boot.Provide2("entity-access", foundation, observability, b.start)
	return b
}

func (b *entityAccessBoot) start(ctx context.Context, foundation foundationBootOutput, observability observabilityBootOutput) (entityAccessBootOutput, error) {
	config, err := foundation.foundation.ServiceConfig()
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
	result.rpcClient = client
	result.access = entityserver_v1alpha.NewEntityAccessClient(client)
	result.client = entityserver.NewClient(observability.log, result.access)

	allocator := ipalloc.NewAllocator(observability.log, b.inputs.servicePrefixes)
	b.inputs.group.Go(func() error {
		return allocator.Watch(ctx, result.access)
	})
	return result, nil
}
