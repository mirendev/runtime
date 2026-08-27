//go:build linux

package server

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"miren.dev/runtime/controllers/sandbox"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/readiness"
)

type observabilityBootInputs struct {
	log                    *slog.Logger
	victoriaLogsAddress    string
	victoriaMetricsAddress string
	timeout                time.Duration
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
	component   *readiness.Component
	inputs      observabilityBootInputs
	tracing     *tracingBoot
	batchWriter *observability.BatchLogWriter
	result      observabilityBootOutput
}

func observabilityInputs(options StartOptions) observabilityBootInputs {
	logsAddress := options.Config.Victorialogs.GetAddress()
	if options.Config.Victorialogs.GetStartEmbedded() {
		logsAddress = localAddress(options.Config.Victorialogs.GetHTTPPort())
	}
	metricsAddress := options.Config.Victoriametrics.GetAddress()
	if options.Config.Victoriametrics.GetStartEmbedded() {
		metricsAddress = localAddress(options.Config.Victoriametrics.GetHTTPPort())
	}
	return observabilityBootInputs{
		log:                    options.Log,
		victoriaLogsAddress:    logsAddress,
		victoriaMetricsAddress: metricsAddress,
		timeout:                30 * time.Second,
	}
}

func newObservabilityBoot(inputs observabilityBootInputs, tracing *tracingBoot) *observabilityBoot {
	b := &observabilityBoot{inputs: inputs, tracing: tracing}
	b.component = readiness.NewComponent("observability", readiness.Spec{
		Dependencies: []readiness.Dependency{readiness.ReadyDep(tracing.component)},
		Start:        b.start,
		Stop:         b.stop,
		StopTimeout:  componentStopTimeout,
	})
	return b
}

func (b *observabilityBoot) output() observabilityBootOutput {
	return b.result
}

func (b *observabilityBoot) start(ctx context.Context, _ readiness.Reporter) error {
	writer := metrics.NewVictoriaMetricsWriter(b.inputs.log, b.inputs.victoriaMetricsAddress, b.inputs.timeout)
	writer.Start()
	reader := metrics.NewVictoriaMetricsReader(b.inputs.log, b.inputs.victoriaMetricsAddress, b.inputs.timeout)
	cpu := metrics.NewCPUUsage(b.inputs.log, writer, reader)
	memory := metrics.NewMemoryUsage(b.inputs.log, writer, reader)

	logWriter := observability.NewPersistentLogWriter(b.inputs.victoriaLogsAddress, b.inputs.timeout)
	logs := observability.NewLogReader(b.inputs.victoriaLogsAddress, b.inputs.timeout)
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
		victoriaLogsAddress:    b.inputs.victoriaLogsAddress,
		victoriaMetricsAddress: b.inputs.victoriaMetricsAddress,
	}
	return nil
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

func localAddress(port int) string {
	return fmt.Sprintf("127.0.0.1:%d", port)
}
