//go:build linux

package server

import (
	"context"
	"net/netip"

	"miren.dev/runtime/components/netresolve"
	"miren.dev/runtime/pkg/boot"
)

type registryHostMappingBootInputs struct {
	hostMapper netresolve.HostMapper
}

type registryHostMappingBootOutput struct {
	registryIP netip.Addr
}

type registryHostMappingBoot struct {
	component *boot.Component
	inputs    registryHostMappingBootInputs
	output    boot.Output[registryHostMappingBootOutput]
}

func registryHostMappingInputs(hostMapper netresolve.HostMapper) registryHostMappingBootInputs {
	return registryHostMappingBootInputs{hostMapper: hostMapper}
}

func newRegistryHostMappingBoot(inputs registryHostMappingBootInputs, network boot.Output[networkBootOutput]) *registryHostMappingBoot {
	b := &registryHostMappingBoot{inputs: inputs}
	b.component, b.output = boot.Provide1("registry-host-mapping", network, b.start)
	return b
}

func (b *registryHostMappingBoot) start(_ context.Context, network networkBootOutput) (registryHostMappingBootOutput, error) {
	routerAddress := network.routerAddress
	if err := b.inputs.hostMapper.SetHost("cluster.local", routerAddress); err != nil {
		return registryHostMappingBootOutput{}, err
	}
	return registryHostMappingBootOutput{registryIP: routerAddress}, nil
}
