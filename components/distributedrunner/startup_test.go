//go:build linux

package distributedrunner

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
	"miren.dev/runtime/components/runner"
	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/pkg/runnerconfig"
	"miren.dev/runtime/pkg/workloadidentity"
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

func TestSandboxHostDoesNotStartAfterContainerdFailure(t *testing.T) {
	containerd, containerdOutput := boot.Provide0("containerd", func(context.Context) (containerdBootOutput, error) {
		return containerdBootOutput{}, errors.New("containerd unavailable")
	})

	host := newSandboxHostBoot(
		sandboxHostBootInputs{log: testLogger()},
		boot.ResolvedOutput(clusterAccessBootOutput{}),
		boot.ResolvedOutput((*runner.NodeStorage)(nil)),
		containerdOutput,
		boot.ResolvedOutput(telemetryBootOutput{}),
	)

	graph := boot.NewGraph()
	require.NoError(t, graph.Add(containerd))
	require.NoError(t, graph.Add(host.component))
	require.ErrorContains(t, graph.Start(t.Context()), "containerd unavailable")
	require.Nil(t, host.value)
}

func TestTelemetryDoesNotStartAfterClusterAccessFailure(t *testing.T) {
	access, accessOutput := boot.Provide0("cluster-access", func(context.Context) (clusterAccessBootOutput, error) {
		return clusterAccessBootOutput{}, errors.New("cluster access unavailable")
	})
	telemetry := newTelemetryBoot(telemetryBootInputs{
		log:                    testLogger(),
		victoriaMetricsAddress: "configured",
	}, accessOutput)

	graph := boot.NewGraph()
	require.NoError(t, graph.Add(access))
	require.NoError(t, graph.Add(telemetry.component))
	require.ErrorContains(t, graph.Start(t.Context()), "cluster access unavailable")
	require.Nil(t, telemetry.tokenSource)
}

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
