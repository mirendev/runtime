//go:build linux

package server

import (
	"context"

	"miren.dev/runtime/components/ocireg"
	"miren.dev/runtime/pkg/readiness"
)

type ociRegistryBootInputs struct {
	dataPath string
}

type ociRegistryBoot struct {
	component     *readiness.Component
	inputs        ociRegistryBootInputs
	identity      *workloadIdentityBoot
	entityAccess  *entityAccessBoot
	hostMapping   *registryHostMappingBoot
	observability *observabilityBoot
	registry      *ocireg.Registry
}

func ociRegistryInputs(options StartOptions) ociRegistryBootInputs {
	return ociRegistryBootInputs{dataPath: options.Config.Server.GetDataPath()}
}

func newOCIRegistryBoot(inputs ociRegistryBootInputs, identity *workloadIdentityBoot, entityAccess *entityAccessBoot, hostMapping *registryHostMappingBoot, observability *observabilityBoot) *ociRegistryBoot {
	b := &ociRegistryBoot{inputs: inputs, identity: identity, entityAccess: entityAccess, hostMapping: hostMapping, observability: observability}
	b.component = readiness.NewComponent("oci-registry", readiness.Spec{
		Dependencies: []readiness.Dependency{
			readiness.ReadyDep(identity.component),
			readiness.ReadyDep(entityAccess.component),
			readiness.ReadyDep(hostMapping.component),
			readiness.ReadyDep(observability.component),
		},
		Start:       b.start,
		Stop:        b.stop,
		StopTimeout: componentStopTimeout,
	})
	return b
}

func (b *ociRegistryBoot) start(ctx context.Context, _ readiness.Reporter) error {
	b.registry = ocireg.NewRegistry(
		b.inputs.dataPath,
		b.observability.output().log,
		b.entityAccess.output().client,
		b.identity.output().issuer,
	)
	if err := b.registry.Start(ctx, ":5000"); err != nil {
		return err
	}
	b.observability.output().log.Info("OCI registry listening", "address", ocireg.Host)
	return nil
}

func (b *ociRegistryBoot) stop(ctx context.Context) error {
	if b.registry == nil {
		return nil
	}
	return b.registry.Shutdown(ctx)
}
