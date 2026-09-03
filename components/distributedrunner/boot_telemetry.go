//go:build linux

package distributedrunner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"miren.dev/runtime/controllers/sandbox"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/servers/runnertelemetry"
)

type telemetryBootInputs struct {
	log                    *slog.Logger
	coordinatorAddress     string
	victoriaMetricsAddress string
	victoriaLogsAddress    string
	clientCert             []byte
	clientKey              []byte
	caCert                 []byte
	timeout                time.Duration
}

type telemetryBootOutput struct {
	sandboxMetrics *sandbox.Metrics
	logWriter      observability.LogWriter
}

type telemetryBoot struct {
	component   *boot.Component
	inputs      telemetryBootInputs
	client      *runnertelemetry.Client
	metrics     *metrics.VictoriaMetricsWriter
	batch       *observability.BatchLogWriter
	tokenSource *runnertelemetry.IssuerTokenSource
	output      boot.Output[telemetryBootOutput]
}

func telemetryInputs(options StartOptions) telemetryBootInputs {
	return telemetryBootInputs{
		log:                    options.Log,
		coordinatorAddress:     options.Config.CoordinatorAddress,
		victoriaMetricsAddress: options.Config.VictoriametricsAddress,
		victoriaLogsAddress:    options.Config.VictorialogsAddress,
		clientCert:             []byte(options.Config.ClientCert),
		clientKey:              []byte(options.Config.ClientKey),
		caCert:                 []byte(options.Config.CACert),
		timeout:                30 * time.Second,
	}
}

func newTelemetryBoot(inputs telemetryBootInputs, access boot.Output[clusterAccessBootOutput]) *telemetryBoot {
	b := &telemetryBoot{inputs: inputs}
	b.component, b.output = boot.Provide1("telemetry", access, b.start,
		boot.WithStop(b.stop, 0))
	return b
}

func (b *telemetryBoot) start(_ context.Context, access clusterAccessBootOutput) (telemetryBootOutput, error) {
	result := telemetryBootOutput{}
	// Distributed runners ship telemetry to the coordinator's ingest endpoints,
	// not to VictoriaMetrics or VictoriaLogs directly. The joined addresses are
	// only signals that the cluster records each kind of telemetry.
	if b.inputs.victoriaMetricsAddress != "" || b.inputs.victoriaLogsAddress != "" {
		issuer := access.access.WorkloadIssuer()
		b.tokenSource = runnertelemetry.NewIssuerTokenSource()
		b.tokenSource.SetIssuer(issuer)
		if issuer == nil {
			b.inputs.log.Error("no workload identity issuer available; telemetry cannot be shipped")
		}
		client, err := runnertelemetry.NewClient(runnertelemetry.ClientConfig{
			ClientCertPEM: b.inputs.clientCert,
			ClientKeyPEM:  b.inputs.clientKey,
			CACertPEM:     b.inputs.caCert,
			TokenSource:   b.tokenSource,
			Timeout:       b.inputs.timeout,
		})
		if err != nil {
			return telemetryBootOutput{}, fmt.Errorf("building telemetry client: %w", err)
		}
		b.client = client
	}

	if b.inputs.victoriaMetricsAddress != "" {
		endpoint := runnertelemetry.MetricsURL(b.inputs.coordinatorAddress)
		b.metrics = metrics.NewVictoriaMetricsWriter(b.inputs.log, endpoint, b.inputs.timeout,
			metrics.WithHTTPClient(b.client.HTTP))
		b.metrics.Start()
		b.inputs.log.Info("metrics writer started", "endpoint", endpoint)
	} else {
		b.inputs.log.Warn("no VictoriaMetrics address configured, sandbox metrics will not be recorded")
	}
	result.sandboxMetrics = sandbox.NewMetrics()
	result.sandboxMetrics.Log = b.inputs.log
	result.sandboxMetrics.CPUUsage = metrics.NewCPUUsage(b.inputs.log, b.metrics, nil)
	result.sandboxMetrics.MemUsage = metrics.NewMemoryUsage(b.inputs.log, b.metrics, nil)

	if b.inputs.victoriaLogsAddress != "" {
		endpoint := runnertelemetry.LogsURL(b.inputs.coordinatorAddress)
		persistent := observability.NewPersistentLogWriter(endpoint, b.inputs.timeout,
			observability.WithHTTPClient(b.client.HTTP))
		// Batch here because one HTTP/3 round trip per log line is much more
		// expensive than the old unbatched POST to local VictoriaLogs. Reporting
		// failures through the runner's system log cannot feed back into this
		// buffer, which only carries sandbox logs.
		b.batch = observability.NewBatchLogWriter(persistent,
			observability.WithBatchErrorHandler(func(err error, dropped int) {
				b.inputs.log.Error("failed to ship sandbox logs", "error", err, "dropped", dropped)
			}))
		result.logWriter = b.batch
		b.inputs.log.Info("log writer started", "endpoint", endpoint)
	} else {
		result.logWriter = observability.NewDebugLogWriter(b.inputs.log)
		b.inputs.log.Warn("no VictoriaLogs address configured, sandbox logs will only be written to debug output")
	}
	return result, nil
}

func (b *telemetryBoot) stop(context.Context) error {
	var errs []error
	if b.metrics != nil {
		if err := b.metrics.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if b.batch != nil {
		b.batch.Close()
	}
	if b.client != nil {
		if err := b.client.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
