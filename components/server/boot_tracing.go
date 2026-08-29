//go:build linux

package server

import (
	"context"
	"log/slog"
	"os"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/pkg/rpc"
)

type tracingBootInputs struct {
	log             *slog.Logger
	endpoint        string
	clusterName     string
	additionalNames []string
	shutdownTimeout time.Duration
}

type tracingBoot struct {
	component *boot.Component
	inputs    tracingBootInputs
	output    boot.Output[struct{}]
	shutdown  func(context.Context) error
}

func tracingInputs(options StartOptions) tracingBootInputs {
	return tracingBootInputs{
		log:             options.Log,
		endpoint:        os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		clusterName:     options.Config.Server.GetConfigClusterName(),
		additionalNames: append([]string(nil), options.Config.TLS.AdditionalNames...),
		shutdownTimeout: 5 * time.Second,
	}
}

func newTracingBoot(inputs tracingBootInputs, registration boot.Output[registrationBootOutput]) *tracingBoot {
	b := &tracingBoot{inputs: inputs}
	b.component, b.output = boot.Provide1("tracing", registration, b.start,
		boot.WithStop(b.stop, componentStopTimeout))
	return b
}

func (b *tracingBoot) start(ctx context.Context, registrationOutput registrationBootOutput) (struct{}, error) {
	if b.inputs.endpoint == "" {
		return struct{}{}, nil
	}
	clusterName := b.inputs.clusterName
	if registrationOutput.cloudAuth.Enabled {
		if cloudName := registrationOutput.cloudAuth.Tags["cluster_name"]; cloudName != "" {
			clusterName = cloudName
		}
	} else if len(b.inputs.additionalNames) > 0 {
		clusterName = b.inputs.additionalNames[0]
	}

	shutdown, err := rpc.SetupTracing(ctx, attribute.String("miren.cluster.name", clusterName))
	if err != nil {
		return struct{}{}, err
	}
	b.shutdown = shutdown
	b.inputs.log.Info("OTel tracing enabled", "endpoint", b.inputs.endpoint, "cluster", clusterName)
	return struct{}{}, nil
}

func (b *tracingBoot) stop(ctx context.Context) error {
	if b.shutdown == nil {
		return nil
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, b.inputs.shutdownTimeout)
	defer cancel()
	return b.shutdown(shutdownCtx)
}
