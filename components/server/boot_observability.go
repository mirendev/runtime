//go:build linux

package server

import (
	"context"
	"log/slog"
	"time"

	"miren.dev/runtime/controllers/sandbox"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/boot"
)

type observabilityBootInputs struct {
	log     *slog.Logger
	timeout time.Duration
}

type observabilityBootOutput struct {
	log                    *slog.Logger
	metricsWriter          *metrics.VictoriaMetricsWriter
	metricsReader          *metrics.VictoriaMetricsReader
	cpu                    *metrics.CPUUsage
	memory                 *metrics.MemoryUsage
	http                   *metrics.HTTPMetrics
	logWriter              observability.LogWriter
	logs                   *observability.LogReader
	logsMaintainer         *observability.LogsMaintainer
	statusMonitor          *observability.StatusMonitor
	sandboxMetrics         *sandbox.Metrics
	victoriaLogsAddress    string
	victoriaMetricsAddress string
}

type observabilityBoot struct {
	component   *boot.Component
	inputs      observabilityBootInputs
	batchWriter *observability.BatchLogWriter
	result      observabilityBootOutput
	output      boot.Output[observabilityBootOutput]
}

func observabilityInputs(options StartOptions) observabilityBootInputs {
	return observabilityBootInputs{
		log:     options.Log,
		timeout: 30 * time.Second,
	}
}

func newObservabilityBoot(inputs observabilityBootInputs, tracing *boot.Component, victoriaLogs boot.Output[victoriaLogsBootOutput], victoriaMetrics boot.Output[victoriaMetricsBootOutput]) *observabilityBoot {
	b := &observabilityBoot{inputs: inputs}
	b.component, b.output = boot.Provide2("observability", victoriaLogs, victoriaMetrics, b.start,
		boot.DependsOn(tracing),
		boot.WithStop(b.stop, componentStopTimeout))
	return b
}

func (b *observabilityBoot) start(ctx context.Context, victoriaLogs victoriaLogsBootOutput, victoriaMetrics victoriaMetricsBootOutput) (observabilityBootOutput, error) {
	writer := metrics.NewVictoriaMetricsWriter(b.inputs.log, victoriaMetrics.address, b.inputs.timeout)
	writer.Start()
	reader := metrics.NewVictoriaMetricsReader(b.inputs.log, victoriaMetrics.address, b.inputs.timeout)
	cpu := metrics.NewCPUUsage(b.inputs.log, writer, reader)
	memory := metrics.NewMemoryUsage(b.inputs.log, writer, reader)

	logWriter := observability.NewPersistentLogWriter(victoriaLogs.address, b.inputs.timeout)
	logs := observability.NewLogReader(victoriaLogs.address, b.inputs.timeout)
	b.batchWriter = observability.NewBatchLogWriter(logWriter)
	log := slog.New(observability.NewSystemLogHandler(b.inputs.log.Handler(), b.batchWriter))

	runtimeMemory := metrics.NewRuntimeMemory(log, writer)
	go runtimeMemory.Monitor(ctx)

	sandboxMetrics := sandbox.NewMetrics()
	sandboxMetrics.Log = log
	sandboxMetrics.CPUUsage = cpu
	sandboxMetrics.MemUsage = memory

	b.result = observabilityBootOutput{
		log:                    log,
		metricsWriter:          writer,
		metricsReader:          reader,
		cpu:                    cpu,
		memory:                 memory,
		http:                   metrics.NewHTTPMetrics(log, writer, reader),
		logWriter:              logWriter,
		logs:                   logs,
		logsMaintainer:         observability.NewLogsMaintainer(),
		statusMonitor:          observability.NewStatusMonitor(log),
		sandboxMetrics:         sandboxMetrics,
		victoriaLogsAddress:    victoriaLogs.address,
		victoriaMetricsAddress: victoriaMetrics.address,
	}
	return b.result, nil
}

func (b *observabilityBoot) stop(context.Context) error {
	if b.batchWriter != nil {
		b.batchWriter.Close()
	}
	if b.result.metricsWriter == nil {
		return nil
	}
	return b.result.metricsWriter.Close()
}
