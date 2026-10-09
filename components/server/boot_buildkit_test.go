//go:build linux

package server

import (
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/components/buildkit"
	"miren.dev/runtime/pkg/otlpexport"
	"miren.dev/runtime/pkg/workloadidentity"
)

func TestBuildkitWithoutConfiguredDaemonPublishesEmptyOutput(t *testing.T) {
	b := &buildkitBoot{}

	output, err := b.startDisabled(t.Context())
	if err != nil {
		t.Fatalf("start() error = %v", err)
	}
	if output.component != nil {
		t.Fatal("buildkit published a component without an embedded or external daemon")
	}
}

func TestBuildkitTraces(t *testing.T) {
	token := workloadidentity.NewSystemTokenSource(workloadidentity.SystemWorkloadTelemetryWriter, "aud")

	require.Equal(t, buildkit.TracesExport{}, buildkitTraces(tracingBootOutput{}),
		"no destination leaves buildkitd's env as the server inherited it")

	require.Equal(t,
		buildkit.TracesExport{Endpoint: "https://c.example"},
		buildkitTraces(tracingBootOutput{destination: &otlpexport.Destination{Endpoint: "https://c.example"}}))

	require.Equal(t,
		buildkit.TracesExport{Endpoint: "http://127.0.0.1:14318", Relayed: true, RelaySecret: "RELAYSECRET"},
		buildkitTraces(tracingBootOutput{
			destination: &otlpexport.Destination{Endpoint: "https://c.example", Token: token},
			relayURL:    "http://127.0.0.1:14318",
			relaySecret: "RELAYSECRET",
		}))

	require.Equal(t,
		buildkit.TracesExport{Disabled: true},
		buildkitTraces(tracingBootOutput{destination: &otlpexport.Destination{Endpoint: "https://c.example", Token: token}}),
		"identity without a relay must not fall back to a static credential")
}
