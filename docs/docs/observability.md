---
title: Observability
description: Managed application metrics and OpenTelemetry distributed tracing in Miren.
keywords: [observability, metrics, prometheus, remote write, tracing, opentelemetry, spans]
---

# Observability

Miren can scrape a Prometheus-compatible metrics endpoint from every running
replica of an application service. It also instruments the request lifecycle
with [OpenTelemetry](https://opentelemetry.io/) distributed tracing.

## Managed application metrics

First, configure a remote-write destination for your cluster in
[`server.toml`](/server-config#managed-app-metrics). An application then opts a
service in through `.miren/app.toml`:

```toml
[services.web.metrics]
enabled = true
path = "/metrics"
port = 3000
interval = "30s"
```

Miren scrapes each running sandbox over its private overlay address and adds
`miren_app`, `miren_app_version`, `miren_service`, `miren_sandbox`,
`miren_runner`, and `miren_cluster` labels. Runtime labels take precedence over
labels emitted by the application, so a workload cannot impersonate another
replica or cluster. Registered clusters use their Miren Cloud cluster ID for
`miren_cluster`; other clusters use the configured cluster name.

The metrics path is private by default. Public requests to that exact path get
a 404 even when the app has a public route. Set `public = true` only when you
intend the app's normal public ingress and authentication policy to apply.

Miren sends these samples directly to the configured destination with a
short-lived workload identity token. It does not retain a second copy in the
cluster's embedded VictoriaMetrics instance.

The coordinator owns both scraping and remote write, so failures appear in the
managed scraper's system logs:

```bash
miren logs system vmagent --last 15m
```

This includes endpoint errors, invalid or oversized responses, authentication
failures, and delivery retries. `miren logs app` includes only output written by
the application itself.

## Distributed tracing

### Minimum working example

Miren's side needs no setup — every request is already traced, and the `traceparent` header is forwarded to your app. To join your app's spans to those traces, point an OpenTelemetry SDK at your backend in `.miren/app.toml`:

```toml
[[env]]
key = "OTEL_EXPORTER_OTLP_ENDPOINT"
value = "https://your-otel-collector:4318"

[[env]]
key = "OTEL_SERVICE_NAME"
value = "my-app"
```

The SDK picks up `traceparent` from incoming requests, so your app's spans land in the same trace as Miren's routing and cold-start spans.

### What Miren Traces

Every HTTP request that arrives at Miren generates a trace with spans covering:

- **httpingress** — The full request lifecycle: routing, lease acquisition, and proxying to your app
- **httpingress.lease** — Sandbox lease management, including whether a cached lease was used or a cold start was required
- **RPC calls** — Internal service-to-service communication within Miren
- **containerd gRPC** — Container operations like image pulls, container creation, and task management

The most useful spans for app developers are `httpingress` (overall request latency) and `httpingress.lease` (cold start visibility). The RPC and containerd spans are primarily useful for operators debugging Miren itself.

### Trace Context Propagation

Miren participates in [W3C Trace Context](https://www.w3.org/TR/trace-context/) propagation in both directions:

**Inbound:** If your request includes a `traceparent` header, Miren continues that trace rather than starting a new one. This means requests from an instrumented frontend or upstream service produce a single connected trace that includes Miren's processing.

**Outbound:** When Miren forwards a request to your app, it injects a `traceparent` header. Your app can pick this up to create child spans that appear in the same trace as the Miren infrastructure spans.

### Connecting Your App's Traces

Miren's tracing and your app's tracing are configured independently — Miren handles its own trace export, and your app handles its own. The `traceparent` header is what connects them: when both sides send traces to the same backend, they show up as one unified trace because they share the same trace ID.

To participate, add an OpenTelemetry SDK to your app and point it at your OTLP-compatible backend. The SDK will automatically read the `traceparent` header from incoming requests and create child spans.

Set these environment variables on your app in `.miren/app.toml`:

```toml
[[env]]
key = "OTEL_EXPORTER_OTLP_ENDPOINT"
value = "https://your-otel-collector:4318"

[[env]]
key = "OTEL_EXPORTER_OTLP_HEADERS"
value = "Authorization=Bearer your-api-key"

[[env]]
key = "OTEL_SERVICE_NAME"
value = "my-app"
```

This works with any OTel-compatible backend: Grafana Tempo, Honeycomb, Datadog, Jaeger, and others. You can use the same backend as your Miren cluster or a different one — as long as traces with the same trace ID end up in the same place, they'll be correlated.

### Python Example

Using the OpenTelemetry auto-instrumentation for Flask:

```bash
pip install opentelemetry-distro opentelemetry-exporter-otlp
opentelemetry-bootstrap -a install
```

```toml
[[env]]
key = "OTEL_EXPORTER_OTLP_ENDPOINT"
value = "https://your-otel-collector:4318"

[[env]]
key = "OTEL_SERVICE_NAME"
value = "my-flask-app"
```

```text
# Procfile
web: opentelemetry-instrument flask run --host 0.0.0.0 --port 3000
```

The `opentelemetry-instrument` wrapper automatically reads the `traceparent` header from incoming requests and creates spans for your Flask routes.

### Node.js Example

Using the OpenTelemetry auto-instrumentation for Node.js:

```bash
npm install @opentelemetry/auto-instrumentations-node
```

```toml
[[env]]
key = "OTEL_EXPORTER_OTLP_ENDPOINT"
value = "https://your-otel-collector:4318"

[[env]]
key = "OTEL_SERVICE_NAME"
value = "my-node-app"

[[env]]
key = "NODE_OPTIONS"
value = "--require @opentelemetry/auto-instrumentations-node/register"
```

The `--require` flag loads the auto-instrumentation before your app starts, automatically instrumenting HTTP, Express, and other common libraries.

### What a Trace Looks Like

A typical request trace shows the full path through Miren:

```text
httpingress                          [350ms]
├─ httpingress.lease                 [200ms]  (cold start)
│  ├─ rpc.call.AcquireLease         [195ms]
│  │  ├─ containerd...Images/Pull   [150ms]
│  │  └─ containerd...Tasks/Create  [40ms]
├─ [proxy to app]                   [150ms]
│  └─ my-app: GET /api/users        [145ms]  (your app's span)
```

On a warm request where a sandbox is already running:

```text
httpingress                          [15ms]
├─ httpingress.lease                 [0.1ms]  (cached lease)
├─ [proxy to app]                   [14ms]
│  └─ my-app: GET /api/users        [12ms]
```

## Next Steps

- [Logs](/logs) — View and filter application, build, and system logs
- [Services](/services) — Configure your app's services
- [Application Scaling](/scaling) — Understand cold starts and autoscaling
