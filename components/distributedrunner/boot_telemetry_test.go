//go:build linux

package distributedrunner

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/components/runner"
	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/pkg/caauth"
	"miren.dev/runtime/pkg/runnerconfig"
)

func TestTelemetryConfiguredAndMissing(t *testing.T) {
	ca, err := caauth.New(caauth.Options{
		CommonName:   "test-ca",
		Organization: "test",
		ValidFor:     time.Hour,
	})
	require.NoError(t, err)
	access, err := runner.NewClusterAccess(testLogger(), runner.RunnerDeps{
		WorkloadIssuer: stubIssuer{},
	}, runner.RunnerConfig{
		Id:       "runner-test",
		DataPath: t.TempDir(),
	})
	require.NoError(t, err)
	accessOutput := boot.ResolvedOutput(clusterAccessBootOutput{access: access})
	cert, err := ca.IssueCertificate(caauth.Options{
		CommonName:   "runner-test",
		Organization: "test",
		ValidFor:     time.Hour,
		IPs:          []net.IP{net.ParseIP("127.0.0.1")},
	})
	require.NoError(t, err)

	tests := []struct {
		name            string
		victoriaMetrics string
		victoriaLogs    string
		wantClient      bool
		wantMetrics     bool
		wantBatch       bool
	}{
		{name: "missing configuration"},
		{
			name:            "metrics only",
			victoriaMetrics: "configured",
			wantClient:      true,
			wantMetrics:     true,
		},
		{
			name:         "logs only",
			victoriaLogs: "configured",
			wantClient:   true,
			wantBatch:    true,
		},
		{
			name:            "metrics and logs",
			victoriaMetrics: "configured",
			victoriaLogs:    "configured",
			wantClient:      true,
			wantMetrics:     true,
			wantBatch:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := StartOptions{
				Log: testLogger(),
				Config: &runnerconfig.Config{
					CoordinatorAddress:     "127.0.0.1:8443",
					ClientCert:             string(cert.CertPEM),
					ClientKey:              string(cert.KeyPEM),
					CACert:                 string(ca.GetCACertificate()),
					VictoriametricsAddress: tt.victoriaMetrics,
					VictorialogsAddress:    tt.victoriaLogs,
				},
			}
			telemetry := newTelemetryBoot(telemetryInputs(options), accessOutput)
			graph := boot.NewGraph()
			require.NoError(t, graph.Add(telemetry.component))

			require.NoError(t, graph.Start(t.Context()))
			t.Cleanup(func() {
				require.NoError(t, graph.Stop(context.Background()))
			})
			require.Equal(t, tt.wantClient, telemetry.client != nil)
			require.Equal(t, tt.wantMetrics, telemetry.metrics != nil)
			require.Equal(t, tt.wantBatch, telemetry.batch != nil)
			require.Equal(t, tt.wantClient, telemetry.tokenSource != nil)
			require.NotNil(t, telemetry.output.Value().sandboxMetrics)
			require.NotNil(t, telemetry.output.Value().logWriter)
			if tt.wantClient {
				token, tokenErr := telemetry.tokenSource.Token()
				require.NoError(t, tokenErr)
				require.Equal(t, "telemetry-token", token)
			}
		})
	}
}
