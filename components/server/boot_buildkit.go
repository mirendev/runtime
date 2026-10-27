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
	b.handle, b.output = boot.Provide2("buildkit-handle", containerd, observability, b.newEmbedded)
	b.component = boot.Run4("buildkit", b.output, registryHostMapping, network, tracing, b.startEmbedded, stop)
	return b
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

func (b *buildkitBoot) stop(ctx context.Context) error {
	if b.result.component == nil {
		return nil
	}
	b.observability.log.Info("stopping embedded buildkit")
	return b.result.component.Stop(ctx)
}
