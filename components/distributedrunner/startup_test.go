//go:build linux

package distributedrunner

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
	"miren.dev/runtime/components/runner"
	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/pkg/runnerconfig"
	"miren.dev/runtime/pkg/workloadidentity"
	"miren.dev/runtime/servers/runnertelemetry"
)

func TestStartupGraphValidates(t *testing.T) {
	group, groupCtx := errgroup.WithContext(t.Context())
	runtime := &Runtime{graph: boot.NewGraph()}
	components := newStartup(runtime, StartOptions{
		Log:     testLogger(),
		Context: groupCtx,
		Group:   group,
		Config: &runnerconfig.Config{
			CoordinatorAddress: "127.0.0.1:8443",
		},
	})
	require.NoError(t, components.addComponents())
	require.NoError(t, runtime.graph.Validate())
}

func TestRunnerDoesNotStartAfterContainerdFailure(t *testing.T) {
	containerd, containerdOutput := boot.Provide0("containerd", func(context.Context) (containerdBootOutput, error) {
		return containerdBootOutput{}, errors.New("containerd unavailable")
	})

	var constructed atomic.Bool
	inputs := runnerBootInputs{
		log:   testLogger(),
		group: new(errgroup.Group),
		newRunner: func(*slog.Logger, runner.RunnerDeps, runner.RunnerConfig) (runnerRuntime, error) {
			constructed.Store(true)
			return &stubRunner{}, nil
		},
	}
	runnerBoot := newRunnerBoot(
		inputs,
		containerdOutput,
		boot.ResolvedOutput(telemetryBootOutput{}),
	)

	graph := boot.NewGraph()
	require.NoError(t, graph.Add(containerd))
	require.NoError(t, graph.Add(runnerBoot.component))
	require.ErrorContains(t, graph.Start(t.Context()), "containerd unavailable")
	require.False(t, constructed.Load())
}

func TestRunnerActivatesTelemetryIssuerAfterStart(t *testing.T) {
	tokenSource := runnertelemetry.NewIssuerTokenSource()
	inputs := runnerBootInputs{
		log:      testLogger(),
		group:    new(errgroup.Group),
		dataPath: t.TempDir(),
		newRunner: func(*slog.Logger, runner.RunnerDeps, runner.RunnerConfig) (runnerRuntime, error) {
			return &stubRunner{issuer: stubIssuer{}}, nil
		},
	}
	runnerBoot := newRunnerBoot(
		inputs,
		boot.ResolvedOutput(containerdBootOutput{}),
		boot.ResolvedOutput(telemetryBootOutput{tokenSource: tokenSource}),
	)

	graph := boot.NewGraph()
	require.NoError(t, graph.Add(runnerBoot.component))

	_, err := tokenSource.Token()
	require.ErrorIs(t, err, runnertelemetry.ErrIssuerUnavailable)
	require.NoError(t, graph.Start(t.Context()))
	token, err := tokenSource.Token()
	require.NoError(t, err)
	require.Equal(t, "telemetry-token", token)
	require.NoError(t, graph.Stop(t.Context()))
}

type stubRunner struct {
	issuer workloadidentity.TokenIssuer
}

func (*stubRunner) Start(context.Context, ...*errgroup.Group) error { return nil }
func (*stubRunner) Close() error                                    { return nil }
func (r *stubRunner) WorkloadIssuer() workloadidentity.TokenIssuer  { return r.issuer }

type stubIssuer struct{}

func (stubIssuer) IssueToken(string, string) (string, error) { return "", nil }
func (stubIssuer) IssueTokenWithOptions(string, string, workloadidentity.TokenOptions) (string, error) {
	return "", nil
}
func (stubIssuer) IssueSystemWorkloadToken(workloadidentity.SystemWorkload, workloadidentity.TokenOptions) (string, error) {
	return "telemetry-token", nil
}
func (stubIssuer) IssuerURL() string { return "https://issuer.example" }

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
