//go:build linux

package server

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/components/buildkit"
	"miren.dev/runtime/pkg/workloadidentity"
)

func tracingTestInputs(t *testing.T, endpoint, audience, relayAddr string) tracingBootInputs {
	return tracingBootInputs{
		log:             slog.Default(),
		endpoint:        endpoint,
		configEndpoint:  true,
		audience:        audience,
		relayAddr:       relayAddr,
		relaySecretPath: filepath.Join(t.TempDir(), "otlp-relay", "secret"),
		shutdownTimeout: 5 * time.Second,
	}
}

func tracingTestIssuer(t *testing.T) *workloadidentity.Issuer {
	t.Helper()
	iss, err := workloadidentity.NewIssuer(workloadidentity.IssuerConfig{
		DataPath:  t.TempDir(),
		IssuerURL: "https://cluster.example",
	})
	require.NoError(t, err)
	return iss
}

func TestTracingBootWithoutEndpointExportsNothing(t *testing.T) {
	b := &tracingBoot{inputs: tracingTestInputs(t, "", "traces.example", "127.0.0.1:0")}

	out, err := b.start(t.Context(), registrationBootOutput{}, workloadIdentityBootOutput{})
	require.NoError(t, err)
	require.Nil(t, out.destination)
	require.Empty(t, out.relayURL)
	require.NoError(t, b.stop(t.Context()))
}

func TestTracingBootStaticAuthRunsNoRelay(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "")
	b := &tracingBoot{inputs: tracingTestInputs(t, "https://collector.invalid", "", "127.0.0.1:0")}

	out, err := b.start(t.Context(), registrationBootOutput{}, workloadIdentityBootOutput{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, b.stop(context.Background())) })

	require.NotNil(t, out.destination)
	require.Nil(t, out.destination.Token)
	require.Empty(t, out.relayURL, "static credentials reach buildkitd directly, so there is nothing to relay")
}

// With identity on, the relay comes up and buildkitd's exports through it
// reach the collector carrying the cluster's token; stopping the boot takes the
// relay down with it.
func TestTracingBootIdentityRunsRelay(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "")
	got := make(chan string, 1)
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/traces" {
			select {
			case got <- r.Header.Get("Authorization"):
			default:
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(collector.Close)
	iss := tracingTestIssuer(t)

	b := &tracingBoot{inputs: tracingTestInputs(t, collector.URL, "traces.example", "127.0.0.1:0")}
	out, err := b.start(t.Context(), registrationBootOutput{}, workloadIdentityBootOutput{issuer: iss})
	require.NoError(t, err)
	require.NotNil(t, out.destination.Token)
	require.NotEmpty(t, out.relayURL)
	require.NotEmpty(t, out.relaySecret)

	resp, err := http.Post(out.relayURL+"/v1/traces", "application/x-protobuf", strings.NewReader("spans"))
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode, "a local caller without the relay secret is refused")

	req, err := http.NewRequest(http.MethodPost, out.relayURL+"/v1/traces", strings.NewReader("spans"))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("Authorization", "Bearer "+out.relaySecret)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	bearer, ok := strings.CutPrefix(<-got, "Bearer ")
	require.True(t, ok)
	_, err = iss.VerifySystemWorkloadToken(bearer, "traces.example", workloadidentity.SystemWorkloadTelemetryWriter)
	require.NoError(t, err)

	require.NoError(t, b.stop(t.Context()))
	_, err = http.Post(out.relayURL+"/v1/traces", "application/x-protobuf", strings.NewReader("spans"))
	require.Error(t, err, "the relay should stop listening when tracing stops")
}

// A relay that cannot bind leaves buildkitd without a destination rather than
// failing the boot or handing it a static credential.
func TestTracingBootRelayBindFailureDisablesBuildkitTraces(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "")
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { taken.Close() })

	b := &tracingBoot{inputs: tracingTestInputs(t, "https://collector.invalid", "traces.example", taken.Addr().String())}
	out, err := b.start(t.Context(), registrationBootOutput{}, workloadIdentityBootOutput{issuer: tracingTestIssuer(t)})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, b.stop(context.Background())) })

	require.Empty(t, out.relayURL)
	require.Equal(t, buildkit.TracesExport{Disabled: true}, buildkitTraces(out))
}

// Without its secret the relay would have to take spans from any local
// process, so buildkitd goes without traces instead.
func TestTracingBootRelayWithoutSecretDisablesBuildkitTraces(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "")
	inputs := tracingTestInputs(t, "https://collector.invalid", "traces.example", "127.0.0.1:0")
	// A regular file where the secret's directory should be.
	blocker := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blocker, nil, 0600))
	inputs.relaySecretPath = filepath.Join(blocker, "secret")

	b := &tracingBoot{inputs: inputs}
	out, err := b.start(t.Context(), registrationBootOutput{}, workloadIdentityBootOutput{issuer: tracingTestIssuer(t)})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, b.stop(context.Background())) })

	require.Empty(t, out.relayURL)
	require.Equal(t, buildkit.TracesExport{Disabled: true}, buildkitTraces(out))
}
