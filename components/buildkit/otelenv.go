package buildkit

// otelEnvForBuildkitd builds the OTEL_* environment handed to buildkitd so the
// daemon can export its internal spans to the same collector the runtime uses.
//
// The runtime's own exporters (pkg/rpc/otel.go, servers/telemetry) only speak
// OTLP over HTTP/protobuf and ignore the OTEL_EXPORTER_OTLP_PROTOCOL family
// entirely. buildkitd, on the other hand, honours those vars and defaults to
// gRPC when none is set. Forwarding the endpoint alone therefore leaves the two
// halves of one process disagreeing about protocol, and against an HTTP-only
// collector every build's spans die with a misleading "404 Unimplemented". We
// pin the generic protocol var to what the runtime actually uses so they agree
// by construction; an operator who sets it themselves still wins.
//
// buildkitd also pushes metrics on a periodic reader whenever an endpoint is
// set. In the buildkit we ship those are only otelgrpc's server-side RPC
// histograms, which the spans already cover with more context, and a
// traces-only collector 404s them once a minute forever. We turn that exporter
// off unless the operator points metrics somewhere explicitly. If buildkit's
// metrics ever earn a home, buildkitd already serves a Prometheus /metrics on
// --debugaddr, which fits the vmagent scrape pipeline better than OTLP push.
func otelEnvForBuildkitd(getenv func(string) string) []string {
	var env []string
	for _, key := range []string{
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_HEADERS",
		"OTEL_EXPORTER_OTLP_PROTOCOL",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
		"OTEL_EXPORTER_OTLP_TRACES_HEADERS",
		"OTEL_EXPORTER_OTLP_TRACES_PROTOCOL",
		"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
		"OTEL_EXPORTER_OTLP_METRICS_HEADERS",
		"OTEL_EXPORTER_OTLP_METRICS_PROTOCOL",
		"OTEL_METRICS_EXPORTER",
		"OTEL_SERVICE_NAME",
	} {
		if v := getenv(key); v != "" {
			env = append(env, key+"="+v)
		}
	}

	// Any endpoint, per-signal or generic, switches on the matching buildkitd
	// exporter, as does OTEL_METRICS_EXPORTER=otlp on its own (it then targets
	// the SDK's localhost default), so any of them needs the protocol pinned.
	exporting := getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" ||
		getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") != "" ||
		getenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT") != "" ||
		getenv("OTEL_METRICS_EXPORTER") == "otlp"
	if !exporting {
		return env
	}

	if getenv("OTEL_EXPORTER_OTLP_PROTOCOL") == "" {
		env = append(env, "OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf")
	}
	// buildkitd consults OTEL_METRICS_EXPORTER before it looks at any endpoint
	// and "none" is a registered exporter there, so this wins over the generic
	// endpoint we still forward (buildkit v0.19.0, util/tracing/detect). Checked
	// against the image we ship: with this env a full build posts only to
	// /v1/traces and the once-a-minute metrics push never happens.
	if getenv("OTEL_METRICS_EXPORTER") == "" && getenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT") == "" {
		env = append(env, "OTEL_METRICS_EXPORTER=none")
	}
	return env
}
