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

// buildkitBoot is two components. The handle constructs the buildkit component
// and is what the cluster foundation consumes; starting the daemon, which pulls
// its image on a version change and takes a couple of seconds even when it does
// not, is a separate component nothing on the serving path waits for. The
// build server holds the handle from the start, and work admission keeps builds
// and deployments from being accepted until the daemon is up.
type buildkitBoot struct {
	handle        *boot.Component
	component     *boot.Component
	inputs        buildkitBootInputs
	observability observabilityBootOutput
	result        buildkitBootOutput
	output        boot.Output[buildkitBootOutput]
}

func buildkitInputs(options StartOptions) buildkitBootInputs {
	return buildkitBootInputs{config: options.Config.Buildkit, dataPath: options.Config.Server.GetDataPath()}
}

func newBuildkitBoot(inputs buildkitBootInputs, containerd boot.Output[containerdBootOutput], registryHostMapping boot.Output[registryHostMappingBootOutput], network boot.Output[networkBootOutput], observability boot.Output[observabilityBootOutput], tracing boot.Output[tracingBootOutput]) *buildkitBoot {
	b := &buildkitBoot{inputs: inputs}
	stop := boot.WithStop(b.stop, componentStopTimeout)
	switch {
	case inputs.config.GetStartEmbedded():
		b.handle, b.output = boot.Provide2("buildkit-handle", containerd, observability, b.newEmbedded)
		b.component = boot.Run4("buildkit", b.output, registryHostMapping, network, tracing, b.startEmbedded, stop)
	case inputs.config.GetSocketPath() != "":
		b.handle, b.output = boot.Provide1("buildkit-handle", observability, b.newExternal)
		b.component = boot.Run1("buildkit", b.output, b.start, stop)
	default:
		b.handle, b.output = boot.Provide0("buildkit-handle", b.startDisabled)
		b.component = boot.Run1("buildkit", b.output, func(context.Context, buildkitBootOutput) error { return nil })
	}
	return b
}

func (b *buildkitBoot) newExternal(_ context.Context, observability observabilityBootOutput) (buildkitBootOutput, error) {
	b.observability = observability
	b.result.component = buildkit.NewExternalComponent(observability.log, b.inputs.config.GetSocketPath())
	return b.result, nil
}

func (b *buildkitBoot) start(ctx context.Context, handle buildkitBootOutput) error {
	log := b.observability.log
	log.Info("using external buildkit daemon", "socket", b.inputs.config.GetSocketPath())
	if err := handle.component.Start(ctx, buildkit.Config{}); err != nil {
		return err
	}
	log.Info("connected to external buildkit", "socket-path", handle.component.SocketPath())
	return nil
}

func (b *buildkitBoot) startDisabled(context.Context) (buildkitBootOutput, error) {
	return buildkitBootOutput{}, nil
}

func (b *buildkitBoot) newEmbedded(_ context.Context, containerd containerdBootOutput, observability observabilityBootOutput) (buildkitBootOutput, error) {
	b.observability = observability
	b.result.component = buildkit.NewComponent(observability.log, containerd.Client, containerd.Namespace, b.inputs.dataPath)
	return b.result, nil
}

func (b *buildkitBoot) startEmbedded(ctx context.Context, handle buildkitBootOutput, hostMapping registryHostMappingBootOutput, network networkBootOutput, tracing tracingBootOutput) error {
	log := b.observability.log
	log.Info("starting embedded buildkit daemon", "socket-dir", b.inputs.config.GetSocketDir())
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
	// Point build steps at miren's bridge DNS server so they resolve against the
	// host's real upstream (and *.app.miren) instead of the public DNS buildkit
	// would otherwise substitute for the host's loopback stub resolver.
	var dnsNameservers []string
	if network.routerAddress.IsValid() {
		dnsNameservers = []string{network.routerAddress.String()}
	}
	if err := handle.component.Start(ctx, buildkit.Config{
		SocketDir:      socketDir,
		RegistryIP:     hostMapping.registryIP.String(),
		GCKeepStorage:  int64(gcStorage.Bytes()),
		GCKeepDuration: int64(gcDuration.Seconds()),
		RegistryHost:   ocireg.Host,
		DNSNameservers: dnsNameservers,
		Traces:         buildkitTraces(tracing),
	}); err != nil {
		return err
	}
	log.Info("embedded buildkit started", "socket-path", handle.component.SocketPath())
	return nil
}

// buildkitTraces decides where buildkitd's spans go from where the server's go.
// When traces need a workload identity token, buildkitd exports through the
// server's loopback relay, since it cannot refresh a token itself.
func buildkitTraces(tracing tracingBootOutput) buildkit.TracesExport {
	switch {
	case tracing.destination == nil:
		return buildkit.TracesExport{}
	case tracing.destination.Token == nil:
		return buildkit.TracesExport{Endpoint: tracing.destination.Endpoint}
	case tracing.relayURL != "":
		return buildkit.TracesExport{Endpoint: tracing.relayURL, Relayed: true, RelaySecret: tracing.relaySecret}
	default:
		return buildkit.TracesExport{Disabled: true}
	}
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
