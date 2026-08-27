//go:build linux

package server

import (
	"context"
	"fmt"

	"miren.dev/runtime/components/victoriametrics"
	"miren.dev/runtime/pkg/readiness"
	"miren.dev/runtime/pkg/serverconfig"
)

type victoriaMetricsBootInputs struct {
	config   serverconfig.VictoriaMetricsConfig
	dataPath string
}

type victoriaMetricsBoot struct {
	component     *readiness.Component
	inputs        victoriaMetricsBootInputs
	containerd    *containerdBoot
	observability *observabilityBoot
	server        *victoriametrics.VictoriaMetricsComponent
}

func victoriaMetricsInputs(options StartOptions) victoriaMetricsBootInputs {
	return victoriaMetricsBootInputs{config: options.Config.Victoriametrics, dataPath: options.Config.Server.GetDataPath()}
}

func newVictoriaMetricsBoot(inputs victoriaMetricsBootInputs, containerd *containerdBoot, observability *observabilityBoot) *victoriaMetricsBoot {
	b := &victoriaMetricsBoot{inputs: inputs, containerd: containerd, observability: observability}
	b.component = readiness.NewComponent("victoriametrics", readiness.Spec{
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

func (b *victoriaMetricsBoot) start(ctx context.Context, _ readiness.Reporter) error {
	log := b.observability.output().log
	if !b.inputs.config.GetStartEmbedded() {
		if b.inputs.config.GetAddress() == "" {
			return fmt.Errorf("victoriametrics address not specified and embedded victoriametrics not started")
		}
		log.Info("using external victoriametrics", "address", b.inputs.config.GetAddress())
		return nil
	}

	log.Info("starting embedded victoriametrics server", "http-port", b.inputs.config.GetHTTPPort())
	containerd := b.containerd.output()
	b.server = victoriametrics.NewVictoriaMetricsComponent(log, containerd.client, containerd.namespace, b.inputs.dataPath)
	if err := b.server.Start(ctx, victoriametrics.VictoriaMetricsConfig{
		HTTPPort:        b.inputs.config.GetHTTPPort(),
		RetentionPeriod: b.inputs.config.GetRetentionPeriod(),
	}); err != nil {
		return err
	}
	log.Info("embedded victoriametrics started", "http-endpoint", b.server.HTTPEndpoint())
	return nil
}

func (b *victoriaMetricsBoot) stop(ctx context.Context) error {
	if b.server == nil {
		return nil
	}
	b.observability.output().log.Info("stopping embedded victoriametrics")
	return b.server.Stop(ctx)
}
