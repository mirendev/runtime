//go:build linux

package server

import (
	"context"
	"path/filepath"

	"miren.dev/runtime/components/buildkit"
	"miren.dev/runtime/components/ocireg"
	"miren.dev/runtime/pkg/readiness"
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
	component           *readiness.Component
	inputs              buildkitBootInputs
	containerd          *containerdBoot
	registryHostMapping *registryHostMappingBoot
	observability       *observabilityBoot
	result              buildkitBootOutput
}

func buildkitInputs(options StartOptions) buildkitBootInputs {
	return buildkitBootInputs{config: options.Config.Buildkit, dataPath: options.Config.Server.GetDataPath()}
}

func newBuildkitBoot(inputs buildkitBootInputs, containerd *containerdBoot, registryHostMapping *registryHostMappingBoot, observability *observabilityBoot) *buildkitBoot {
	b := &buildkitBoot{
		inputs:              inputs,
		containerd:          containerd,
		registryHostMapping: registryHostMapping,
		observability:       observability,
	}
	dependencies := []readiness.Dependency{
		readiness.ReadyDep(containerd.component),
		readiness.ReadyDep(observability.component),
	}
	if inputs.config.GetStartEmbedded() {
		dependencies = append(dependencies, readiness.ReadyDep(registryHostMapping.component))
	}
	b.component = readiness.NewComponent("buildkit", readiness.Spec{
		Dependencies: dependencies,
		Start:        b.start,
		Stop:         b.stop,
		StopTimeout:  componentStopTimeout,
	})
	return b
}

func (b *buildkitBoot) output() buildkitBootOutput {
	return b.result
}

func (b *buildkitBoot) start(ctx context.Context, report readiness.Reporter) error {
	log := b.observability.output().log
	containerd := b.containerd.output()
	switch {
	case b.inputs.config.GetStartEmbedded():
		log.Info("starting embedded buildkit daemon", "socket-dir", b.inputs.config.GetSocketDir())
		b.result.component = buildkit.NewComponent(log, containerd.client, containerd.namespace, b.inputs.dataPath)
		report.Started()
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
			RegistryIP:     b.registryHostMapping.output().registryIP.String(),
			GCKeepStorage:  int64(gcStorage.Bytes()),
			GCKeepDuration: int64(gcDuration.Seconds()),
			RegistryHost:   ocireg.Host,
		}); err != nil {
			return err
		}
		log.Info("embedded buildkit started", "socket-path", b.result.component.SocketPath())
	case b.inputs.config.GetSocketPath() != "":
		log.Info("using external buildkit daemon", "socket", b.inputs.config.GetSocketPath())
		b.result.component = buildkit.NewExternalComponent(log, b.inputs.config.GetSocketPath())
		report.Started()
		if err := b.result.component.Start(ctx, buildkit.Config{}); err != nil {
			return err
		}
		log.Info("connected to external buildkit", "socket-path", b.result.component.SocketPath())
	default:
		// The coordinator can still start without a builder, but BuildReady must
		// remain false so build RPCs wait for a configured build path.
		report.NotReady()
	}
	return nil
}

func buildkitEnabled(config serverconfig.BuildkitConfig) bool {
	return config.GetStartEmbedded() || config.GetSocketPath() != ""
}

func (b *buildkitBoot) stop(ctx context.Context) error {
	if b.result.component == nil || !b.inputs.config.GetStartEmbedded() {
		return nil
	}
	b.observability.output().log.Info("stopping embedded buildkit")
	return b.result.component.Stop(ctx)
}
