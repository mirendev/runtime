//go:build linux

package server

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/pkg/otlpexport"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/workloadidentity"
)

type tracingBootInputs struct {
	log *slog.Logger
	// endpoint is the resolved OTLP base URL; configEndpoint says whether it
	// came from telemetry.traces.endpoint rather than the OTel env, in which
	// case the SDK exporter has to be told about it explicitly.
	endpoint        string
	configEndpoint  bool
	audience        string
	relayAddr       string
	relaySecretPath string
	clusterName     string
	additionalNames []string
	shutdownTimeout time.Duration
}

type tracingBootOutput struct {
	// destination is nil when the server exports no traces.
	destination *otlpexport.Destination
	// relayURL is where processes that cannot hold a token of their own export
	// through. Empty unless traces authenticate with workload identity and the
	// relay is listening.
	relayURL string
	// relaySecret is the bearer token the relay requires. Set with relayURL.
	relaySecret string
}

type tracingBoot struct {
	component *boot.Component
	inputs    tracingBootInputs
	output    boot.Output[tracingBootOutput]
	shutdown  func(context.Context) error
	relay     *http.Server
}

func tracingInputs(options StartOptions) tracingBootInputs {
	traces := options.Config.Telemetry.Traces
	return tracingBootInputs{
		log:             options.Log,
		endpoint:        traces.ResolvedEndpoint(),
		configEndpoint:  traces.GetEndpoint() != "",
		audience:        traces.GetWorkloadIdentityAudience(),
		relayAddr:       otlpexport.RelayAddr,
		relaySecretPath: filepath.Join(options.Config.Server.GetDataPath(), "otlp-relay", "secret"),
		clusterName:     options.Config.Server.GetConfigClusterName(),
		additionalNames: append([]string(nil), options.Config.TLS.AdditionalNames...),
		shutdownTimeout: 5 * time.Second,
	}
}

func newTracingBoot(inputs tracingBootInputs, registration boot.Output[registrationBootOutput], identity boot.Output[workloadIdentityBootOutput]) *tracingBoot {
	b := &tracingBoot{inputs: inputs}
	b.component, b.output = boot.Provide2("tracing", registration, identity, b.start,
		boot.WithStop(b.stop, componentStopTimeout))
	return b
}

func (b *tracingBoot) start(ctx context.Context, registrationOutput registrationBootOutput, identity workloadIdentityBootOutput) (tracingBootOutput, error) {
	if b.inputs.endpoint == "" {
		return tracingBootOutput{}, nil
	}
	log := b.inputs.log
	clusterName := b.inputs.clusterName
	if registrationOutput.cloudAuth.Enabled {
		if cloudName := registrationOutput.cloudAuth.Tags["cluster_name"]; cloudName != "" {
			clusterName = cloudName
		}
	} else if len(b.inputs.additionalNames) > 0 {
		clusterName = b.inputs.additionalNames[0]
	}

	var token workloadidentity.TokenSource
	auth := "static"
	if b.inputs.audience != "" {
		source := workloadidentity.NewSystemTokenSource(workloadidentity.SystemWorkloadTelemetryWriter, b.inputs.audience)
		// A nil *Issuer stored in the interface would panic on first use
		// instead of failing as ErrIssuerUnavailable.
		if identity.issuer != nil {
			source.SetIssuer(identity.issuer)
		} else {
			log.Error("traces are set to authenticate with workload identity but no issuer is available; every trace export will fail",
				"audience", b.inputs.audience)
		}
		token = source
		auth = "workload-identity"
		if identity.issuer != nil && identity.issuer.IssuerURL() == workloadidentity.LocalIssuerURL {
			log.Warn("traces authenticate with workload identity, but the issuer is cluster-local, "+
				"so no external collector can fetch its keys; give the cluster a hostname or register it",
				"audience", b.inputs.audience)
		}
	}
	destination := otlpexport.Resolve(log, b.inputs.endpoint, token)

	var exporterOpts []otlptracehttp.Option
	if b.inputs.configEndpoint {
		exporterOpts = append(exporterOpts, otlptracehttp.WithEndpointURL(destination.TracesURL()))
	}
	if token != nil {
		// A custom client makes the SDK ignore OTEL_EXPORTER_OTLP_CERTIFICATE
		// and OTEL_EXPORTER_OTLP_TIMEOUT. The timeout is restored by Client;
		// a private CA for the collector is not supported alongside identity.
		exporterOpts = append(exporterOpts, otlptracehttp.WithHTTPClient(destination.Client()))
	}

	shutdown, err := rpc.SetupTracing(ctx, exporterOpts, attribute.String("miren.cluster.name", clusterName))
	if err != nil {
		return tracingBootOutput{}, err
	}
	b.shutdown = shutdown
	log.Info("OTel tracing enabled", "endpoint", b.inputs.endpoint, "cluster", clusterName, "auth", auth)

	out := tracingBootOutput{destination: &destination}
	if token != nil {
		out.relayURL, out.relaySecret = b.startRelay(destination)
	}
	return out, nil
}

// startRelay listens for trace exports from processes that cannot refresh a
// token themselves. A relay that cannot bind is not fatal: the server's own
// spans still export, and buildkitd is left without a destination rather than
// with a credential that would expire under it. The same goes for a relay
// without its secret, since it would have to take spans from anyone.
func (b *tracingBoot) startRelay(destination otlpexport.Destination) (string, string) {
	log := b.inputs.log
	secret, err := otlpexport.LoadRelaySecret(b.inputs.relaySecretPath)
	if err != nil {
		log.Warn("OTLP relay for buildkit traces has no secret; build spans will not be exported",
			"path", b.inputs.relaySecretPath, "error", err)
		return "", ""
	}
	listener, err := net.Listen("tcp", b.inputs.relayAddr)
	if err != nil {
		log.Warn("OTLP relay for buildkit traces could not listen; build spans will not be exported",
			"addr", b.inputs.relayAddr, "error", err)
		return "", ""
	}
	b.relay = &http.Server{
		Handler:           otlpexport.NewRelay(log, destination, secret),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := b.relay.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("OTLP relay stopped", "error", err)
		}
	}()
	relayURL := "http://" + listener.Addr().String()
	log.Info("OTLP relay listening for local trace exporters", "url", relayURL)
	return relayURL, secret
}

func (b *tracingBoot) stop(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, b.inputs.shutdownTimeout)
	defer cancel()

	var err error
	if b.relay != nil {
		err = b.relay.Shutdown(shutdownCtx)
	}
	if b.shutdown != nil {
		err = errors.Join(err, b.shutdown(shutdownCtx))
	}
	return err
}
