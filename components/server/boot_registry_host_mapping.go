//go:build linux

package server

import (
	"context"
	"net/netip"

	"miren.dev/runtime/components/netresolve"
	"miren.dev/runtime/pkg/readiness"
)

type registryHostMappingBootInputs struct {
	hostMapper netresolve.HostMapper
}

type registryHostMappingBootOutput struct {
	registryIP netip.Addr
}

type registryHostMappingBoot struct {
	component *readiness.Component
	inputs    registryHostMappingBootInputs
	network   *networkBoot
	result    registryHostMappingBootOutput
}

func registryHostMappingInputs(hostMapper netresolve.HostMapper) registryHostMappingBootInputs {
	return registryHostMappingBootInputs{hostMapper: hostMapper}
}

func newRegistryHostMappingBoot(inputs registryHostMappingBootInputs, network *networkBoot) *registryHostMappingBoot {
	b := &registryHostMappingBoot{inputs: inputs, network: network}
	b.component = readiness.NewComponent("registry-host-mapping", readiness.Spec{
		Dependencies: []readiness.Dependency{readiness.ReadyDep(network.component)},
		Start:        b.start,
	})
	return b
}

func (b *registryHostMappingBoot) output() registryHostMappingBootOutput {
	return b.result
}

func (b *registryHostMappingBoot) start(context.Context, readiness.Reporter) error {
	routerAddress := b.network.output().routerAddress
	if err := b.inputs.hostMapper.SetHost("cluster.local", routerAddress); err != nil {
		return err
	}
	b.result.registryIP = routerAddress
	return nil
}
