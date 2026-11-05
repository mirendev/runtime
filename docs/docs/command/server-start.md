---
title: "miren server start"
sidebar_label: "server start"
description: "Start the miren server"
---

# miren server start

Start the miren server

## Usage

```bash
miren server start [flags]
```

## Flags

- `--acme-dns-provider` — DNS provider for ACME DNS-01 challenges (e.g., cloudflare, route53, exec). When set, uses DNS challenge instead of HTTP challenge. See https://go-acme.github.io/lego/dns/ for available providers.
- `--acme-email` — Email address for ACME account registration (recommended for account recovery and notifications)
- `--address, -a` — Address to listen on (host:port). For IPv6 use brackets, e.g. "[::1]:8443".
- `--app-version-retention-count` — Number of most-recent versions to retain per app regardless of age
- `--app-version-retention-period` — Retain versions newer than this duration regardless of count (e.g. 30d, 2w)
- `--buildkit-gc-duration` — How long to keep BuildKit cache entries (e.g., 7d, 24h)
- `--buildkit-gc-storage` — Maximum BuildKit layer cache size (e.g., 10GB, 50GB)
- `--buildkit-socket` — Retired and ignored: miren always runs its own BuildKit
- `--buildkit-socket-dir` — Directory for embedded BuildKit Unix socket (defaults to data_path/buildkit/socket)
- `--config` — Path to configuration file
- `--config-cluster-name, -C` — Name for this cluster in client config and as the telemetry label fallback when it is not registered with Miren Cloud
- `--containerd-binary` — Path to containerd binary
- `--containerd-socket` — Path to containerd socket
- `--data-path, -d` — Data path
- `--deployment-retention-count` — Number of most-recent deployment records to retain per app regardless of age
- `--deployment-retention-period` — Retain deployment records newer than this duration regardless of count (e.g. 30d, 2w). Set to 0 to keep them indefinitely.
- `--disk-mode` — Disk I/O mode: auto (default, detect from hardware), universal (loop devices), or accelerator (lbd devices)
- `--dns-names` — Additional DNS names assigned to the server cert
- `--etcd, -e` — Retired and ignored: miren always runs its own etcd
- `--etcd-client-port` — Etcd client port
- `--etcd-http-client-port` — Etcd HTTP client port
- `--etcd-peer-port` — Etcd peer port
- `--etcd-prefix, -p` — Etcd prefix
- `--etcd-quota-backend-bytes` — Etcd backend quota in bytes (0 = auto-size from system RAM)
- `--http-request-timeout` — HTTP request timeout in seconds
- `--ingress-address` — Optional bind override. Replaces the mode's default bind entirely (interface and port). Rejected by validation in tls-autoprovision (where :443 + :80 is structural). Reserved unix:/path prefix is not yet supported.
- `--ingress-mode` — Ingress mode: tls-autoprovision (default, :443 + :80 with ACME or self-signed), behind-proxy-http (plain HTTP for use behind a TLS-terminating proxy), behind-proxy-https (TLS terminated by Miren; certs come from self-signed or DNS-01 ACME, since :80 isn't bound for HTTP-01)
- `--ingress-trusted-proxy-hops` — Number of trusted proxies immediately in front of Miren when ingress.mode is behind-proxy-http. Used to select the visitor address from X-Forwarded-For.
- `--ips` — Additional IPs assigned to the server cert
- `--labs` — Comma-separated list of Miren Labs features to enable/disable. Prefix with - to disable.
- `--metrics-remote-write-audience` — Deprecated: use --telemetry-metrics-audience
- `--metrics-remote-write-url` — Deprecated: use --telemetry-metrics-remote-write-url
- `--mode, -m` — Deprecated: standalone is the only server mode
- `--release-path` — Path to release directory containing binaries
- `--runner-address` — Runner address (host:port). For IPv6 use brackets, e.g. "[::1]:8444".
- `--runner-id, -r` — Runner ID
- `--saga-retention-period` — Delete finished saga executions older than this duration (e.g. 7d, 24h). Set to 0 to keep them indefinitely.
- `--secret-key-rotation-period` — Rotate the cluster key that encrypts stored secrets once it reaches this age (e.g. 90d, 1y). Set to 0 to rotate only on request.
- `--self-signed-tls` — Use self-signed certificates for TLS (for development/testing only)
- `--serve-tls` — Deprecated and ignored. Retained as a no-op so existing systemd unit files, env vars, and config files from pre-RFD-84 installs still parse. Use ingress.mode to pick the deployment shape.
- `--skip-client-config` — Skip writing client config file to clientconfig.d
- `--start-buildkit` — Retired: BuildKit always runs embedded
- `--start-containerd` — Retired: containerd always runs embedded
- `--start-etcd` — Retired: etcd always runs embedded
- `--start-victorialogs` — Retired: VictoriaLogs always runs embedded
- `--start-victoriametrics` — Retired: VictoriaMetrics always runs embedded
- `--stop-sandboxes-on-shutdown` — Stop all sandboxes when server shuts down (useful in development)
- `--telemetry-metrics-audience` — Workload identity audience for the metrics destination
- `--telemetry-metrics-remote-write-url` — Prometheus Remote Write destination for managed application metrics
- `--telemetry-traces-audience` — Authenticate trace export with a workload identity token for this audience
- `--telemetry-traces-endpoint` — OTLP/HTTP base URL for traces (defaults to OTEL_EXPORTER_OTLP_ENDPOINT)
- `--victorialogs-addr` — Retired and ignored: miren always runs its own VictoriaLogs
- `--victorialogs-http-port` — VictoriaLogs HTTP port in embedded mode
- `--victorialogs-retention` — VictoriaLogs retention period (e.g. 30d, 2w, 1y)
- `--victoriametrics-addr` — Retired and ignored: miren always runs its own VictoriaMetrics
- `--victoriametrics-http-port` — VictoriaMetrics HTTP port in embedded mode
- `--victoriametrics-retention` — VictoriaMetrics retention period in months

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Start the server:**

```bash
sudo miren server start
```

## See also

- [`miren server`](./server.md)
