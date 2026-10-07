package buildkit

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOTELEnvForBuildkitd(t *testing.T) {
	getenv := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}

	t.Run("nothing forwarded when no endpoint is set", func(t *testing.T) {
		env := otelEnvForBuildkitd(getenv(map[string]string{
			"OTEL_SERVICE_NAME": "miren",
			"HOME":              "/root",
		}), TracesExport{})
		require.Equal(t, []string{"OTEL_SERVICE_NAME=miren"}, env)
	})

	t.Run("endpoint only pins http/protobuf and disables metrics", func(t *testing.T) {
		env := otelEnvForBuildkitd(getenv(map[string]string{
			"OTEL_EXPORTER_OTLP_ENDPOINT": "https://tempo.example",
			"OTEL_EXPORTER_OTLP_HEADERS":  "Authorization=Basic abc",
		}), TracesExport{})
		require.ElementsMatch(t, []string{
			"OTEL_EXPORTER_OTLP_ENDPOINT=https://tempo.example",
			"OTEL_EXPORTER_OTLP_HEADERS=Authorization=Basic abc",
			"OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf",
			"OTEL_METRICS_EXPORTER=none",
		}, env)
	})

	t.Run("traces-only endpoint counts as exporting", func(t *testing.T) {
		env := otelEnvForBuildkitd(getenv(map[string]string{
			"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": "https://tempo.example/v1/traces",
		}), TracesExport{})
		require.Contains(t, env, "OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf")
		require.Contains(t, env, "OTEL_METRICS_EXPORTER=none")
	})

	t.Run("operator protocol choice is forwarded untouched", func(t *testing.T) {
		env := otelEnvForBuildkitd(getenv(map[string]string{
			"OTEL_EXPORTER_OTLP_ENDPOINT": "https://otel.example",
			"OTEL_EXPORTER_OTLP_PROTOCOL": "grpc",
		}), TracesExport{})
		require.Contains(t, env, "OTEL_EXPORTER_OTLP_PROTOCOL=grpc")
		require.NotContains(t, env, "OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf")
	})

	t.Run("per-signal protocol still rides alongside the generic default", func(t *testing.T) {
		env := otelEnvForBuildkitd(getenv(map[string]string{
			"OTEL_EXPORTER_OTLP_ENDPOINT":        "https://otel.example",
			"OTEL_EXPORTER_OTLP_TRACES_PROTOCOL": "grpc",
		}), TracesExport{})
		require.Contains(t, env, "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL=grpc")
		require.Contains(t, env, "OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf")
	})

	t.Run("explicit metrics exporter is respected", func(t *testing.T) {
		env := otelEnvForBuildkitd(getenv(map[string]string{
			"OTEL_EXPORTER_OTLP_ENDPOINT": "https://otel.example",
			"OTEL_METRICS_EXPORTER":       "otlp",
		}), TracesExport{})
		require.Contains(t, env, "OTEL_METRICS_EXPORTER=otlp")
		require.NotContains(t, env, "OTEL_METRICS_EXPORTER=none")
	})

	t.Run("explicit metrics endpoint keeps metrics on", func(t *testing.T) {
		env := otelEnvForBuildkitd(getenv(map[string]string{
			"OTEL_EXPORTER_OTLP_ENDPOINT":         "https://tempo.example",
			"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT": "http://127.0.0.1:8429/opentelemetry/v1/metrics",
		}), TracesExport{})
		require.Contains(t, env, "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=http://127.0.0.1:8429/opentelemetry/v1/metrics")
		require.NotContains(t, env, "OTEL_METRICS_EXPORTER=none")
	})

	t.Run("metrics-only endpoint still gets the protocol pin", func(t *testing.T) {
		env := otelEnvForBuildkitd(getenv(map[string]string{
			"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT": "http://127.0.0.1:8429/opentelemetry/v1/metrics",
		}), TracesExport{})
		require.Contains(t, env, "OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf")
		require.NotContains(t, env, "OTEL_METRICS_EXPORTER=none")
	})

	t.Run("metrics exporter alone still gets the protocol pin", func(t *testing.T) {
		env := otelEnvForBuildkitd(getenv(map[string]string{
			"OTEL_METRICS_EXPORTER": "otlp",
		}), TracesExport{})
		require.Contains(t, env, "OTEL_METRICS_EXPORTER=otlp")
		require.Contains(t, env, "OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf")
		require.NotContains(t, env, "OTEL_METRICS_EXPORTER=none")
	})

	t.Run("per-signal headers are forwarded", func(t *testing.T) {
		env := otelEnvForBuildkitd(getenv(map[string]string{
			"OTEL_EXPORTER_OTLP_ENDPOINT":        "https://otel.example",
			"OTEL_EXPORTER_OTLP_TRACES_HEADERS":  "Authorization=Bearer traces",
			"OTEL_EXPORTER_OTLP_METRICS_HEADERS": "Authorization=Bearer metrics",
		}), TracesExport{})
		require.Contains(t, env, "OTEL_EXPORTER_OTLP_TRACES_HEADERS=Authorization=Bearer traces")
		require.Contains(t, env, "OTEL_EXPORTER_OTLP_METRICS_HEADERS=Authorization=Bearer metrics")
	})

	t.Run("config endpoint replaces the env endpoint", func(t *testing.T) {
		env := otelEnvForBuildkitd(getenv(map[string]string{
			"OTEL_EXPORTER_OTLP_ENDPOINT":        "https://old.example",
			"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": "https://old.example/v1/traces",
			"OTEL_EXPORTER_OTLP_HEADERS":         "X-Tenant=a",
		}), TracesExport{Endpoint: "https://config.example"})
		require.Contains(t, env, "OTEL_EXPORTER_OTLP_ENDPOINT=https://config.example")
		require.Contains(t, env, "OTEL_EXPORTER_OTLP_HEADERS=X-Tenant=a")
		require.NotContains(t, env, "OTEL_EXPORTER_OTLP_ENDPOINT=https://old.example")
		require.NotContains(t, env, "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=https://old.example/v1/traces",
			"buildkitd would prefer a stale signal-specific endpoint over the configured one")
	})

	// Relayed, buildkitd must carry no credential of its own and must not be
	// able to bypass the relay through a signal-specific endpoint. The
	// metrics-side settings are none of the relay's business and survive.
	t.Run("relayed export strips credentials and pins the relay", func(t *testing.T) {
		env := otelEnvForBuildkitd(getenv(map[string]string{
			"OTEL_EXPORTER_OTLP_ENDPOINT":         "https://tempo.example",
			"OTEL_EXPORTER_OTLP_HEADERS":          "Authorization=Basic abc",
			"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT":  "https://tempo.example/v1/traces",
			"OTEL_EXPORTER_OTLP_TRACES_HEADERS":   "Authorization=Basic def",
			"OTEL_EXPORTER_OTLP_PROTOCOL":         "grpc",
			"OTEL_EXPORTER_OTLP_METRICS_HEADERS":  "X-Metrics=1",
			"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT": "http://127.0.0.1:8429/opentelemetry/v1/metrics",
		}), TracesExport{Endpoint: "http://127.0.0.1:14318", Relayed: true})
		require.ElementsMatch(t, []string{
			"OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:14318",
			"OTEL_EXPORTER_OTLP_TRACES_PROTOCOL=http/protobuf",
			"OTEL_EXPORTER_OTLP_PROTOCOL=grpc",
			"OTEL_EXPORTER_OTLP_METRICS_HEADERS=X-Metrics=1",
			"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=http://127.0.0.1:8429/opentelemetry/v1/metrics",
		}, env)
	})

	t.Run("disabled export forwards no trace destination or credential", func(t *testing.T) {
		env := otelEnvForBuildkitd(getenv(map[string]string{
			"OTEL_EXPORTER_OTLP_ENDPOINT": "https://tempo.example",
			"OTEL_EXPORTER_OTLP_HEADERS":  "Authorization=Basic abc",
			"OTEL_SERVICE_NAME":           "miren",
		}), TracesExport{Disabled: true})
		require.Equal(t, []string{"OTEL_SERVICE_NAME=miren"}, env)
	})
}
