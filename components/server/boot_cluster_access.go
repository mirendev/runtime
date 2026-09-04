//go:build linux

package server

import (
	"context"

	"miren.dev/runtime/components/runner"
	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/pkg/serverconfig"
)

type clusterAccessBootInputs struct {
	config serverconfig.ServerConfig
}

type clusterAccessBootOutput struct {
	access *runner.ClusterAccess
	config runner.RunnerConfig
	caCert []byte
}

type clusterAccessBoot struct {
	component *boot.Component
	inputs    clusterAccessBootInputs
	value     *runner.ClusterAccess
	output    boot.Output[clusterAccessBootOutput]
}

func newClusterAccessBoot(inputs clusterAccessBootInputs, foundation boot.Output[foundationBootOutput], identity boot.Output[workloadIdentityBootOutput], secrets boot.Output[secretStoreBootOutput], observability boot.Output[observabilityBootOutput], runnerEndpoints *boot.Component) *clusterAccessBoot {
	b := &clusterAccessBoot{inputs: inputs}
	b.component, b.output = boot.Provide4(
		"cluster-access", foundation, identity, secrets, observability, b.start,
		boot.DependsOn(runnerEndpoints),
		boot.WithStop(b.stop, componentStopTimeout),
	)
	return b
}

func (b *clusterAccessBoot) start(ctx context.Context, foundationOutput foundationBootOutput, identity workloadIdentityBootOutput, secrets secretStoreBootOutput, observability observabilityBootOutput) (clusterAccessBootOutput, error) {
	foundation := foundationOutput.foundation
	config := runner.RunnerConfig{
		Id:            b.inputs.config.GetRunnerID(),
		ListenAddress: b.inputs.config.GetRunnerAddress(),
		Workers:       runner.DefaulWorkers,
		DataPath:      b.inputs.config.GetDataPath(),
		DiskMode:      b.inputs.config.GetDiskMode(),
	}
	var err error
	config.Config, err = foundation.RunnerConfig(config.ListenAddress)
	if err != nil {
		return clusterAccessBootOutput{}, err
	}
	deps := runner.RunnerDeps{Secrets: secrets.secretStore.Registry()}
	if identity.issuer != nil {
		deps.WorkloadIssuer = identity.issuer
	}
	b.value, err = runner.NewClusterAccess(observability.log, deps, config)
	if err != nil {
		return clusterAccessBootOutput{}, err
	}
	if err := b.value.Start(ctx); err != nil {
		return clusterAccessBootOutput{}, err
	}
	return clusterAccessBootOutput{access: b.value, config: config, caCert: foundation.CACertificate()}, nil
}

func (b *clusterAccessBoot) stop(context.Context) error {
	if b.value == nil {
		return nil
	}
	return b.value.Close()
}
