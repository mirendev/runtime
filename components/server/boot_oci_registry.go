//go:build linux

package server

import (
	"context"
	"net"

	"miren.dev/runtime/components/ocireg"
	"miren.dev/runtime/network"
	"miren.dev/runtime/pkg/boot"
)

type ociRegistryBootInputs struct {
	dataPath string
}

type ociRegistryBoot struct {
	component *boot.Component
	inputs    ociRegistryBootInputs
	registry  *ocireg.Registry
	output    boot.Output[struct{}]
}

func ociRegistryInputs(options StartOptions) ociRegistryBootInputs {
	return ociRegistryBootInputs{dataPath: options.Config.Server.GetDataPath()}
}

func newOCIRegistryBoot(inputs ociRegistryBootInputs, identity boot.Output[workloadIdentityBootOutput], entityAccess boot.Output[entityAccessBootOutput], hostMapping boot.Output[registryHostMappingBootOutput], sandboxHost *boot.Component, observability boot.Output[observabilityBootOutput]) *ociRegistryBoot {
	b := &ociRegistryBoot{inputs: inputs}
	b.component, b.output = boot.Provide4("oci-registry", identity, entityAccess, hostMapping, observability, b.start,
		boot.DependsOn(sandboxHost),
		boot.WithStop(b.stop, componentStopTimeout),
	)
	return b
}

func (b *ociRegistryBoot) start(ctx context.Context, identity workloadIdentityBootOutput, entityAccess entityAccessBootOutput, hostMapping registryHostMappingBootOutput, observability observabilityBootOutput) (struct{}, error) {
	if err := network.AllowRegistryFromWireGuard(); err != nil {
		return struct{}{}, err
	}
	listenAddress := net.JoinHostPort(hostMapping.registryIP.String(), "5000")
	b.registry = ocireg.NewRegistry(
		b.inputs.dataPath,
		observability.log,
		entityAccess.client,
		identity.issuer,
	)
	if err := b.registry.Start(ctx, listenAddress); err != nil {
		return struct{}{}, err
	}
	observability.log.Info("OCI registry listening", "listen-address", listenAddress, "service-address", ocireg.Host)
	return struct{}{}, nil
}

func (b *ociRegistryBoot) stop(ctx context.Context) error {
	if b.registry == nil {
		return nil
	}
	return b.registry.Shutdown(ctx)
}
