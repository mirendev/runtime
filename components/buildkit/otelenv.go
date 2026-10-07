package buildkit

import "slices"

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
//
// traces overlays the server's resolved traces destination on the env it
// inherited; see TracesExport.
func otelEnvForBuildkitd(getenv func(string) string, traces TracesExport) []string {
	getenv = traces.overlay(getenv)
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

// TracesExport says where buildkitd sends its spans, as the server resolved it.
// The zero value forwards the server's OTel env untouched.
type TracesExport struct {
	// Endpoint replaces OTEL_EXPORTER_OTLP_ENDPOINT, for a destination that
	// came from server config rather than the env.
	Endpoint string

	// Relayed means Endpoint is the server's loopback relay, which holds the
	// credentials. buildkitd lives far longer than a workload identity token
	// and has no way to refresh one, so it gets no credentials at all and
	// speaks the only protocol the relay does.
	Relayed bool

	// Disabled keeps buildkitd from exporting traces. It is what traces get
	// when they need a token and the relay isn't there to supply one: better
	// no build spans than a static credential the operator asked to retire.
	Disabled bool
}

var traceKeys = []string{
	"OTEL_EXPORTER_OTLP_ENDPOINT",
	"OTEL_EXPORTER_OTLP_HEADERS",
	"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
	"OTEL_EXPORTER_OTLP_TRACES_HEADERS",
	"OTEL_EXPORTER_OTLP_TRACES_PROTOCOL",
}

func (t TracesExport) overlay(getenv func(string) string) func(string) string {
	var set map[string]string
	switch {
	case t.Disabled:
		set = map[string]string{}
	case t.Relayed:
		set = map[string]string{
			"OTEL_EXPORTER_OTLP_ENDPOINT": t.Endpoint,
			// The signal-specific protocol beats a generic one the operator
			// may have set for metrics.
			"OTEL_EXPORTER_OTLP_TRACES_PROTOCOL": "http/protobuf",
		}
	case t.Endpoint != "":
		return func(k string) string {
			switch k {
			case "OTEL_EXPORTER_OTLP_ENDPOINT":
				return t.Endpoint
			case "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT":
				// buildkitd lets the signal-specific endpoint win over the
				// generic one, so a stale one in the env would keep build
				// spans going to the old collector. The server's own exporter
				// is pointed at the configured endpoint explicitly, and
				// buildkitd has to agree with it.
				return ""
			}
			return getenv(k)
		}
	default:
		return getenv
	}
	return func(k string) string {
		if slices.Contains(traceKeys, k) {
			return set[k]
		}
		return getenv(k)
	}
}
