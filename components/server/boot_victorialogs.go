//go:build linux

package server

import (
	"context"
	"fmt"

	"miren.dev/runtime/components/victorialogs"
	"miren.dev/runtime/pkg/readiness"
	"miren.dev/runtime/pkg/serverconfig"
)

type victoriaLogsBootInputs struct {
	config   serverconfig.VictoriaLogsConfig
	dataPath string
}

type victoriaLogsBoot struct {
	component     *readiness.Component
	inputs        victoriaLogsBootInputs
	containerd    *containerdBoot
	observability *observabilityBoot
	server        *victorialogs.VictoriaLogsComponent
}

func victoriaLogsInputs(options StartOptions) victoriaLogsBootInputs {
	return victoriaLogsBootInputs{config: options.Config.Victorialogs, dataPath: options.Config.Server.GetDataPath()}
}

func newVictoriaLogsBoot(inputs victoriaLogsBootInputs, containerd *containerdBoot, observability *observabilityBoot) *victoriaLogsBoot {
	b := &victoriaLogsBoot{inputs: inputs, containerd: containerd, observability: observability}
	b.component = readiness.NewComponent("victorialogs", readiness.Spec{
		Dependencies: []readiness.Dependency{
			readiness.ReadyDep(containerd.component),
			readiness.ReadyDep(observability.component),
		},
		Start:       b.start,
		Stop:        b.stop,
		StopTimeout: componentStopTimeout,
	})
	return b
}

func (b *victoriaLogsBoot) start(ctx context.Context, _ readiness.Reporter) error {
	log := b.observability.output().log
	if !b.inputs.config.GetStartEmbedded() {
		if b.inputs.config.GetAddress() == "" {
			return fmt.Errorf("victorialogs address not specified and embedded victorialogs not started")
		}
		log.Info("using external victorialogs", "address", b.inputs.config.GetAddress())
		return nil
	}

	log.Info("starting embedded victorialogs server", "http-port", b.inputs.config.GetHTTPPort())
	containerd := b.containerd.output()
	b.server = victorialogs.NewVictoriaLogsComponent(log, containerd.client, containerd.namespace, b.inputs.dataPath)
	if err := b.server.Start(ctx, victorialogs.VictoriaLogsConfig{
		HTTPPort:        b.inputs.config.GetHTTPPort(),
		RetentionPeriod: b.inputs.config.GetRetentionPeriod(),
	}); err != nil {
		return err
	}
	log.Info("embedded victorialogs started", "http-endpoint", b.server.HTTPEndpoint())
	return nil
}

func (b *victoriaLogsBoot) stop(ctx context.Context) error {
	if b.server == nil {
		return nil
	}
	b.observability.output().log.Info("stopping embedded victorialogs")
	return b.server.Stop(ctx)
}
