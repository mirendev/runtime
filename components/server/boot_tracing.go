//go:build linux

package server

import (
	"context"
	"log/slog"
	"os"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"miren.dev/runtime/pkg/readiness"
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
	component    *readiness.Component
	inputs       tracingBootInputs
	registration *registrationBoot
	shutdown     func(context.Context) error
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

func newTracingBoot(inputs tracingBootInputs, registration *registrationBoot) *tracingBoot {
	b := &tracingBoot{inputs: inputs, registration: registration}
	b.component = readiness.NewComponent("tracing", readiness.Spec{
		Dependencies: []readiness.Dependency{readiness.ReadyDep(registration.component)},
		Start:        b.start,
		Stop:         b.stop,
		StopTimeout:  componentStopTimeout,
	})
	return b
}

func (b *tracingBoot) start(ctx context.Context, _ readiness.Reporter) error {
	if b.inputs.endpoint == "" {
		return nil
	}
	clusterName := b.inputs.clusterName
	registrationOutput := b.registration.output()
	if registrationOutput.cloudAuth.Enabled {
		if cloudName := registrationOutput.cloudAuth.Tags["cluster_name"]; cloudName != "" {
			clusterName = cloudName
		}
	} else if len(b.inputs.additionalNames) > 0 {
		clusterName = b.inputs.additionalNames[0]
	}

	shutdown, err := rpc.SetupTracing(ctx, attribute.String("miren.cluster.name", clusterName))
	if err != nil {
		return err
	}
	b.shutdown = shutdown
	b.inputs.log.Info("OTel tracing enabled", "endpoint", b.inputs.endpoint, "cluster", clusterName)
	return nil
}

func (b *tracingBoot) stop(ctx context.Context) error {
	if b.shutdown == nil {
		return nil
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, b.inputs.shutdownTimeout)
	defer cancel()
	return b.shutdown(shutdownCtx)
}
