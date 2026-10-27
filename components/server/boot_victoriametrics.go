//go:build linux

package server

import (
	"context"
	"log/slog"

	"miren.dev/runtime/components/victoriametrics"
	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/pkg/serverconfig"
)

type victoriaMetricsBootInputs struct {
	log      *slog.Logger
	config   serverconfig.VictoriaMetricsConfig
	dataPath string
}

type victoriaMetricsBootOutput struct {
	address string
}

type victoriaMetricsBoot struct {
	component *boot.Component
	inputs    victoriaMetricsBootInputs
	log       *slog.Logger
	server    *victoriametrics.VictoriaMetricsComponent
	output    boot.Output[victoriaMetricsBootOutput]
}

func victoriaMetricsInputs(options StartOptions) victoriaMetricsBootInputs {
	return victoriaMetricsBootInputs{log: options.Log, config: options.Config.Victoriametrics, dataPath: options.Config.Server.GetDataPath()}
}

func newVictoriaMetricsBoot(inputs victoriaMetricsBootInputs, containerd boot.Output[containerdBootOutput]) *victoriaMetricsBoot {
	b := &victoriaMetricsBoot{inputs: inputs}
	stop := boot.WithStop(b.stop, componentStopTimeout)
	b.component, b.output = boot.Provide1("victoriametrics", containerd, b.startEmbedded, stop)
	return b
}

func (b *victoriaMetricsBoot) startEmbedded(ctx context.Context, containerd containerdBootOutput) (victoriaMetricsBootOutput, error) {
	b.log = b.inputs.log
	log := b.log
	log.Info("starting embedded victoriametrics server", "http-port", b.inputs.config.GetHTTPPort())
	b.server = victoriametrics.NewVictoriaMetricsComponent(log, containerd.Client, containerd.Namespace, b.inputs.dataPath)
	if err := b.server.Start(ctx, victoriametrics.VictoriaMetricsConfig{
		HTTPPort:        b.inputs.config.GetHTTPPort(),
		RetentionPeriod: b.inputs.config.GetRetentionPeriod(),
	}); err != nil {
		return victoriaMetricsBootOutput{}, err
	}
	address := b.server.HTTPEndpoint()
	if err := waitForVictoriaHealth(ctx, "victoriametrics", address); err != nil {
		return victoriaMetricsBootOutput{}, err
	}
	log.Info("embedded victoriametrics started", "http-endpoint", address)
	return victoriaMetricsBootOutput{address: address}, nil
}

func (b *victoriaMetricsBoot) stop(ctx context.Context) error {
	if b.server == nil {
		return nil
	}
	b.log.Info("stopping embedded victoriametrics")
	return b.server.Stop(ctx)
}
