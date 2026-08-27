//go:build linux

package server

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/boot"
)

func TestObservabilityWaitsForVictoriaEndpoints(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracingComponent, tracingOutput := boot.Provide0("tracing", func(context.Context) (struct{}, error) {
		return struct{}{}, nil
	})

	logsStarted := make(chan struct{})
	releaseLogs := make(chan struct{})
	logsComponent, logsOutput := boot.Provide0("victorialogs", func(context.Context) (victoriaLogsBootOutput, error) {
		close(logsStarted)
		<-releaseLogs
		return victoriaLogsBootOutput{address: "logs.example:9428"}, nil
	})

	metricsStarted := make(chan struct{})
	releaseMetrics := make(chan struct{})
	metricsComponent, metricsOutput := boot.Provide0("victoriametrics", func(context.Context) (victoriaMetricsBootOutput, error) {
		close(metricsStarted)
		<-releaseMetrics
		return victoriaMetricsBootOutput{address: "metrics.example:8428"}, nil
	})

	observability := newObservabilityBoot(
		observabilityBootInputs{log: log, timeout: time.Second},
		tracingOutput,
		logsOutput,
		metricsOutput,
	)
	started := make(chan observabilityBootOutput, 1)
	consumer := boot.Run1("consumer", observability.output, func(_ context.Context, output observabilityBootOutput) error {
		started <- output
		return nil
	})

	graph := boot.NewGraph()
	for _, component := range []*boot.Component{
		tracingComponent,
		logsComponent,
		metricsComponent,
		observability.component,
		consumer,
	} {
		require.NoError(t, graph.Add(component))
	}

	startErr := make(chan error, 1)
	go func() { startErr <- graph.Start(t.Context()) }()
	<-logsStarted
	<-metricsStarted

	close(releaseLogs)
	select {
	case <-started:
		t.Fatal("observability started before VictoriaMetrics published its endpoint")
	default:
	}

	close(releaseMetrics)
	select {
	case output := <-started:
		require.Equal(t, "logs.example:9428", output.victoriaLogsAddress)
		require.Equal(t, "metrics.example:8428", output.victoriaMetricsAddress)
	case <-time.After(time.Second):
		t.Fatal("observability did not start after both Victoria endpoints were published")
	}
	require.NoError(t, <-startErr)
	require.NoError(t, graph.Stop(t.Context()))
}
