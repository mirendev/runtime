//go:build linux

package distributedrunner

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/components/runner"
	"miren.dev/runtime/pkg/boot"
)

type clusterAccessBootInputs struct {
	log           *slog.Logger
	clientConfig  *clientconfig.Config
	runnerID      string
	name          string
	listenAddress string
	dataPath      string
	diskMode      string
}

type clusterAccessBootOutput struct {
	access *runner.ClusterAccess
	config runner.RunnerConfig
}

type clusterAccessBoot struct {
	component *boot.Component
	inputs    clusterAccessBootInputs
	value     *runner.ClusterAccess
	output    boot.Output[clusterAccessBootOutput]
}

func clusterAccessInputs(options StartOptions) clusterAccessBootInputs {
	return clusterAccessBootInputs{
		log:           options.Log,
		clientConfig:  options.ClientConfig,
		runnerID:      options.Config.RunnerID,
		name:          options.Config.Name,
		listenAddress: options.ListenAddr,
		dataPath:      options.DataPath,
		diskMode:      options.Config.DiskMode,
	}
}

func newClusterAccessBoot(inputs clusterAccessBootInputs) *clusterAccessBoot {
	b := &clusterAccessBoot{inputs: inputs}
	b.component, b.output = boot.Provide0("cluster-access", b.start,
		boot.WithStop(b.stop, 0))
	return b
}

func (b *clusterAccessBoot) start(ctx context.Context) (clusterAccessBootOutput, error) {
	if err := os.MkdirAll(b.inputs.dataPath, 0755); err != nil {
		return clusterAccessBootOutput{}, fmt.Errorf("creating data directory: %w", err)
	}
	config := runner.RunnerConfig{
		Id:            b.inputs.runnerID,
		Name:          b.inputs.name,
		ListenAddress: b.inputs.listenAddress,
		Workers:       runner.DefaulWorkers,
		DataPath:      b.inputs.dataPath,
		Config:        b.inputs.clientConfig,
		DiskMode:      b.inputs.diskMode,
	}
	var err error
	b.value, err = runner.NewClusterAccess(b.inputs.log, runner.RunnerDeps{}, config)
	if err != nil {
		return clusterAccessBootOutput{}, fmt.Errorf("creating cluster access: %w", err)
	}
	if err := b.value.Start(ctx); err != nil {
		return clusterAccessBootOutput{}, fmt.Errorf("starting cluster access: %w", err)
	}
	return clusterAccessBootOutput{access: b.value, config: config}, nil
}

func (b *clusterAccessBoot) stop(context.Context) error {
	if b.value == nil {
		return nil
	}
	return b.value.Close()
}
