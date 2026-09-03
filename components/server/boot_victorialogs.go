//go:build linux

package server

import (
	"context"
	"fmt"
	"log/slog"

	"miren.dev/runtime/components/victorialogs"
	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/pkg/serverconfig"
)

type victoriaLogsBootInputs struct {
	log      *slog.Logger
	config   serverconfig.VictoriaLogsConfig
	dataPath string
}

type victoriaLogsBootOutput struct {
	address string
}

type victoriaLogsBoot struct {
	component *boot.Component
	inputs    victoriaLogsBootInputs
	log       *slog.Logger
	server    *victorialogs.VictoriaLogsComponent
	output    boot.Output[victoriaLogsBootOutput]
}

func victoriaLogsInputs(options StartOptions) victoriaLogsBootInputs {
	return victoriaLogsBootInputs{log: options.Log, config: options.Config.Victorialogs, dataPath: options.Config.Server.GetDataPath()}
}

func newVictoriaLogsBoot(inputs victoriaLogsBootInputs, containerd boot.Output[containerdBootOutput]) *victoriaLogsBoot {
	b := &victoriaLogsBoot{inputs: inputs}
	stop := boot.WithStop(b.stop, componentStopTimeout)
	if inputs.config.GetStartEmbedded() {
		b.component, b.output = boot.Provide1("victorialogs", containerd, b.startEmbedded, stop)
	} else {
		b.component, b.output = boot.Provide0("victorialogs", b.startExternal, stop)
	}
	return b
}

func (b *victoriaLogsBoot) startExternal(ctx context.Context) (victoriaLogsBootOutput, error) {
	b.log = b.inputs.log
	if b.inputs.config.GetAddress() == "" {
		return victoriaLogsBootOutput{}, fmt.Errorf("victorialogs address not specified and embedded victorialogs not started")
	}
	b.log.Info("using external victorialogs", "address", b.inputs.config.GetAddress())
	if err := waitForVictoriaHealth(ctx, "victorialogs", b.inputs.config.GetAddress()); err != nil {
		return victoriaLogsBootOutput{}, err
	}
	return victoriaLogsBootOutput{address: b.inputs.config.GetAddress()}, nil
}

func (b *victoriaLogsBoot) startEmbedded(ctx context.Context, containerd containerdBootOutput) (victoriaLogsBootOutput, error) {
	b.log = b.inputs.log
	log := b.log
	log.Info("starting embedded victorialogs server", "http-port", b.inputs.config.GetHTTPPort())
	b.server = victorialogs.NewVictoriaLogsComponent(log, containerd.Client, containerd.Namespace, b.inputs.dataPath)
	if err := b.server.Start(ctx, victorialogs.VictoriaLogsConfig{
		HTTPPort:        b.inputs.config.GetHTTPPort(),
		RetentionPeriod: b.inputs.config.GetRetentionPeriod(),
	}); err != nil {
		return victoriaLogsBootOutput{}, err
	}
	address := b.server.HTTPEndpoint()
	if err := waitForVictoriaHealth(ctx, "victorialogs", address); err != nil {
		return victoriaLogsBootOutput{}, err
	}
	log.Info("embedded victorialogs started", "http-endpoint", address)
	return victoriaLogsBootOutput{address: address}, nil
}

func (b *victoriaLogsBoot) stop(ctx context.Context) error {
	if b.server == nil {
		return nil
	}
	b.log.Info("stopping embedded victorialogs")
	return b.server.Stop(ctx)
}
