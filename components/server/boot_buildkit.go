//go:build linux

package server

import (
	"context"
	"path/filepath"

	"miren.dev/runtime/components/buildkit"
	"miren.dev/runtime/components/ocireg"
	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/pkg/serverconfig"
	"miren.dev/runtime/pkg/units"
)

type buildkitBootInputs struct {
	config   serverconfig.BuildkitConfig
	dataPath string
}

type buildkitBootOutput struct {
	component *buildkit.Component
}

type buildkitBoot struct {
	component     *boot.Component
	inputs        buildkitBootInputs
	observability observabilityBootOutput
	result        buildkitBootOutput
	output        boot.Output[buildkitBootOutput]
}

func buildkitInputs(options StartOptions) buildkitBootInputs {
	return buildkitBootInputs{config: options.Config.Buildkit, dataPath: options.Config.Server.GetDataPath()}
}

func newBuildkitBoot(inputs buildkitBootInputs, containerd boot.Output[containerdBootOutput], network boot.Output[networkBootOutput], registryHostMapping boot.Output[registryHostMappingBootOutput], observability boot.Output[observabilityBootOutput]) *buildkitBoot {
	b := &buildkitBoot{inputs: inputs}
	stop := boot.WithStop(b.stop, componentStopTimeout)
	switch {
	case inputs.config.GetStartEmbedded():
		b.component, b.output = boot.Provide4("buildkit", containerd, network, registryHostMapping, observability, b.startEmbedded, stop)
	case inputs.config.GetSocketPath() != "":
		b.component, b.output = boot.Provide1("buildkit", observability, b.start, stop)
	default:
		b.component, b.output = boot.Provide0("buildkit", b.startDisabled, stop)
	}
	return b
}

func (b *buildkitBoot) start(ctx context.Context, observability observabilityBootOutput) (buildkitBootOutput, error) {
	b.observability = observability
	log := observability.log
	log.Info("using external buildkit daemon", "socket", b.inputs.config.GetSocketPath())
	b.result.component = buildkit.NewExternalComponent(log, b.inputs.config.GetSocketPath())
	if err := b.result.component.Start(ctx, buildkit.Config{}); err != nil {
		return buildkitBootOutput{}, err
	}
	log.Info("connected to external buildkit", "socket-path", b.result.component.SocketPath())
	return b.result, nil
}

func (b *buildkitBoot) startDisabled(context.Context) (buildkitBootOutput, error) {
	return buildkitBootOutput{}, nil
}

func (b *buildkitBoot) startEmbedded(ctx context.Context, containerd containerdBootOutput, network networkBootOutput, hostMapping registryHostMappingBootOutput, observability observabilityBootOutput) (buildkitBootOutput, error) {
	b.observability = observability
	log := observability.log
	log.Info("starting embedded buildkit daemon", "socket-dir", b.inputs.config.GetSocketDir())
	b.result.component = buildkit.NewComponent(log, containerd.client, containerd.namespace, b.inputs.dataPath)
	gcStorage, err := units.ParseData(b.inputs.config.GetGcKeepStorage())
	if err != nil {
		log.Warn("invalid buildkit.gc_keep_storage, falling back to default",
			"value", b.inputs.config.GetGcKeepStorage(), "error", err)
	}
	gcDuration, err := units.ParseDuration(b.inputs.config.GetGcKeepDuration())
	if err != nil {
		log.Warn("invalid buildkit.gc_keep_duration, falling back to default",
			"value", b.inputs.config.GetGcKeepDuration(), "error", err)
	}
	socketDir := b.inputs.config.GetSocketDir()
	if socketDir == "" {
		socketDir = filepath.Join(b.inputs.dataPath, "buildkit", "socket")
	}
	if err := b.result.component.Start(ctx, buildkit.Config{
		SocketDir:      socketDir,
		RegistryIP:     hostMapping.registryIP.String(),
		GCKeepStorage:  int64(gcStorage.Bytes()),
		GCKeepDuration: int64(gcDuration.Seconds()),
		RegistryHost:   ocireg.Host,
		DNSServer:      network.routerAddress.String(),
	}); err != nil {
		return buildkitBootOutput{}, err
	}
	log.Info("embedded buildkit started", "socket-path", b.result.component.SocketPath())
	return b.result, nil
}

func buildkitEnabled(config serverconfig.BuildkitConfig) bool {
	return config.GetStartEmbedded() || config.GetSocketPath() != ""
}

func (b *buildkitBoot) stop(ctx context.Context) error {
	if b.result.component == nil || !b.inputs.config.GetStartEmbedded() {
		return nil
	}
	b.observability.log.Info("stopping embedded buildkit")
	return b.result.component.Stop(ctx)
}
