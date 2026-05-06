---
title: Server Configuration Reference
sidebar_label: server.toml
description: Complete reference for Miren server configuration — config file, environment variables, and CLI flags.
keywords: [server.toml, server configuration, environment variables, flags, settings]
---

import CliCommand from '@site/src/components/CliCommand';

# Server Configuration Reference

Complete reference for Miren server configuration. Settings can be specified via config file, environment variables, or CLI flags.

## Configuration Precedence

Settings are resolved in this order (highest priority first):

1. **CLI flags** — e.g. `--address :9443`
2. **Environment variables** — e.g. `MIREN_SERVER_ADDRESS=:9443`
3. **Config file** — `server.toml`
4. **Defaults**

## Config File

The server reads its config from the first file found:

1. Path specified via `--config`
2. `/etc/miren/server.toml`
3. `{data_path}/config/server.toml` (default: `/var/lib/miren/config/server.toml`)

### Example

```toml
mode = "standalone"

[server]
address = ":8443"
data_path = "/var/lib/miren"
network_backend = "vxlan"
http_request_timeout = 60

[tls]
standard_tls = true
acme_email = "admin@example.com"

[etcd]
start_embedded = true

[buildkit]
gc_keep_storage = "20GB"
gc_keep_duration = "14d"
```

## Server Modes

Miren has two operating modes:

| Mode | Description |
|------|-------------|
| `standalone` | All components (etcd, containerd, buildkit, logs, metrics) run embedded within a single process. **This is the default.** |
| `distributed` | Components run as separate services. Experimental. |

In standalone mode, embedded services start automatically unless explicitly disabled.

## Top-Level Fields

| Field | Type | Default | Env Var | CLI Flag |
|-------|------|---------|---------|----------|
| `mode` | string | `standalone` | `MIREN_MODE` | `--mode`, `-m` |
| `labs` | string[] | `[]` | `MIREN_LABS` | `--labs` |

## `[server]` — Core Settings {#server}

| Field | Type | Default | Description | Env Var | CLI Flag |
|-------|------|---------|-------------|---------|----------|
| `address` | string | `:8443` | Address to listen on (`host:port`) | `MIREN_SERVER_ADDRESS` | `--address`, `-a` |
| `runner_address` | string | `localhost:8444` | Runner address (`host:port`) | `MIREN_SERVER_RUNNER_ADDRESS` | `--runner-address` |
| `data_path` | string | `/var/lib/miren` | Root data directory | `MIREN_SERVER_DATA_PATH` | `--data-path`, `-d` |
| `runner_id` | string | `miren` | Runner identifier | `MIREN_SERVER_RUNNER_ID` | `--runner-id`, `-r` |
| `release_path` | string | — | Path to release directory containing binaries | `MIREN_SERVER_RELEASE_PATH` | `--release-path` |
| `config_cluster_name` | string | `local` | Cluster name in client config | `MIREN_SERVER_CONFIG_CLUSTER_NAME` | `--config-cluster-name`, `-C` |
| `skip_client_config` | bool | `false` | Skip writing client config to `clientconfig.d` | `MIREN_SERVER_SKIP_CLIENT_CONFIG` | `--skip-client-config` |
| `http_request_timeout` | int | `60` | HTTP request timeout in seconds (minimum: 1) | `MIREN_SERVER_HTTP_REQUEST_TIMEOUT` | `--http-request-timeout` |
| `stop_sandboxes_on_shutdown` | bool | `false` | Stop all sandboxes when server shuts down (useful in development) | `MIREN_SERVER_STOP_SANDBOXES_ON_SHUTDOWN` | `--stop-sandboxes-on-shutdown` |
| `network_backend` | string | `vxlan` | Network backend: `vxlan` or `wireguard` | `MIREN_SERVER_NETWORK_BACKEND` | `--network-backend` |

## `[tls]` — TLS Settings {#tls}

Controls TLS certificates for the server and HTTP ingress. See [TLS](/tls) for setup guides.

| Field | Type | Default | Description | Env Var | CLI Flag |
|-------|------|---------|-------------|---------|----------|
| `additional_names` | string[] | `[]` | Extra DNS names for the server certificate | `MIREN_TLS_ADDITIONAL_NAMES` | `--dns-names` |
| `additional_ips` | string[] | `[]` | Extra IPs for the server certificate | `MIREN_TLS_ADDITIONAL_IPS` | `--ips` |
| `standard_tls` | bool | `true` | Expose HTTP ingress on standard TLS ports (443) | `MIREN_TLS_STANDARD_TLS` | `--serve-tls` |
| `acme_dns_provider` | string | — | DNS provider for ACME DNS-01 challenges (e.g. `cloudflare`, `route53`) | `MIREN_TLS_ACME_DNS_PROVIDER` | `--acme-dns-provider` |
| `acme_email` | string | — | Email for ACME account registration | `MIREN_TLS_ACME_EMAIL` | `--acme-email` |
| `self_signed` | bool | `false` | Use self-signed certificates (development only) | `MIREN_TLS_SELF_SIGNED` | `--self-signed-tls` |

## `[ingress]` — HTTP Ingress Settings {#ingress}

Controls where the HTTP ingress listens. Leave empty for the default behavior driven by `[tls]`. Set `https_address` to bind the HTTPS ingress to a non-default `host:port` — useful when another service on the host already owns port 443.

| Field | Type | Default | Description | Env Var | CLI Flag |
|-------|------|---------|-------------|---------|----------|
| `https_address` | string | — | `host:port` to bind the HTTPS ingress to instead of `0.0.0.0:443`. When set, port 80 is not bound and `tls.standard_tls` has no effect; cert issuance must use DNS-01 (`tls.acme_dns_provider`) or `tls.self_signed`, since HTTP-01 cannot complete without port 80. | `MIREN_INGRESS_HTTPS_ADDRESS` | `--ingress-https-address` |

```toml
[ingress]
https_address = "127.0.0.1:8444"

[tls]
acme_email = "you@example.com"
acme_dns_provider = "cloudflare"
```

When `ingress.https_address` is set:

- Miren binds **only** to that address. Ports 80 and 443 are not bound.
- `tls.standard_tls` has no effect.
- ACME HTTP-01 challenges are not possible (they require port 80). Use DNS-01 (`tls.acme_dns_provider`) or `tls.self_signed` as the cert source. Miren refuses to start if neither is configured.
- Self-signed mode is appropriate for development and for deployments behind a TLS-terminating proxy that uses `proxy_ssl_verify off` (or equivalent), where the upstream certificate identity does not need to be trusted by clients.
- `tls.additional_ips` and `tls.additional_names` continue to add SAN entries to the issued certificate; they do not bind additional listeners and are unaffected.

## `[etcd]` — Etcd Settings {#etcd}

Miren uses etcd as its entity store. In standalone mode, an embedded etcd server starts automatically.

| Field | Type | Default | Description | Env Var | CLI Flag |
|-------|------|---------|-------------|---------|----------|
| `endpoints` | string[] | `[]` | Etcd endpoints (auto-configured when embedded) | `MIREN_ETCD_ENDPOINTS` | `--etcd`, `-e` |
| `prefix` | string | `/miren` | Key prefix in etcd | `MIREN_ETCD_PREFIX` | `--etcd-prefix`, `-p` |
| `start_embedded` | bool | `true`\* | Start embedded etcd server | `MIREN_ETCD_START_EMBEDDED` | `--start-etcd` |
| `client_port` | int | `12379` | Embedded etcd client port | `MIREN_ETCD_CLIENT_PORT` | `--etcd-client-port` |
| `peer_port` | int | `12380` | Embedded etcd peer port | `MIREN_ETCD_PEER_PORT` | `--etcd-peer-port` |
| `http_client_port` | int | `12381` | Embedded etcd HTTP client port | `MIREN_ETCD_HTTP_CLIENT_PORT` | `--etcd-http-client-port` |

\* Defaults to `true` in standalone mode only.

## `[containerd]` — Containerd Settings {#containerd}

| Field | Type | Default | Description | Env Var | CLI Flag |
|-------|------|---------|-------------|---------|----------|
| `start_embedded` | bool | `true`\* | Start embedded containerd daemon | `MIREN_CONTAINERD_START_EMBEDDED` | `--start-containerd` |
| `binary_path` | string | `containerd` | Path to containerd binary | `MIREN_CONTAINERD_BINARY_PATH` | `--containerd-binary` |
| `socket_path` | string | — | Path to containerd socket | `MIREN_CONTAINERD_SOCKET_PATH` | `--containerd-socket` |

\* Defaults to `true` in standalone mode only.

## `[buildkit]` — BuildKit Settings {#buildkit}

Controls the BuildKit daemon used for building container images.

| Field | Type | Default | Description | Env Var | CLI Flag |
|-------|------|---------|-------------|---------|----------|
| `start_embedded` | bool | `true`\* | Start embedded BuildKit daemon | `MIREN_BUILDKIT_START_EMBEDDED` | `--start-buildkit` |
| `socket_path` | string | — | Path to external BuildKit socket (distributed mode) | `MIREN_BUILDKIT_SOCKET_PATH` | `--buildkit-socket` |
| `socket_dir` | string | — | Directory for embedded BuildKit socket | `MIREN_BUILDKIT_SOCKET_DIR` | `--buildkit-socket-dir` |
| `gc_keep_storage` | string | `10GB` | Maximum BuildKit layer cache size | `MIREN_BUILDKIT_GC_KEEP_STORAGE` | `--buildkit-gc-storage` |
| `gc_keep_duration` | string | `7d` | How long to keep cache entries | `MIREN_BUILDKIT_GC_KEEP_DURATION` | `--buildkit-gc-duration` |

\* Defaults to `true` in standalone mode only.

## `[victorialogs]` — Log Storage Settings {#victorialogs}

Controls the embedded VictoriaLogs instance used for application log storage.

| Field | Type | Default | Description | Env Var | CLI Flag |
|-------|------|---------|-------------|---------|----------|
| `start_embedded` | bool | `true`\* | Start embedded VictoriaLogs server | `MIREN_VICTORIALOGS_START_EMBEDDED` | `--start-victorialogs` |
| `http_port` | int | `9428` | HTTP port in embedded mode | `MIREN_VICTORIALOGS_HTTP_PORT` | `--victorialogs-http-port` |
| `retention_period` | string | `30d` | Retention period (e.g. `30d`, `2w`, `1y`) | `MIREN_VICTORIALOGS_RETENTION_PERIOD` | `--victorialogs-retention` |
| `address` | string | `victorialogs:9428` | Address when not using embedded | `MIREN_VICTORIALOGS_ADDRESS` | `--victorialogs-addr` |

\* Defaults to `true` in standalone mode only.

## `[victoriametrics]` — Metrics Storage Settings {#victoriametrics}

Controls the embedded VictoriaMetrics instance used for application metrics.

| Field | Type | Default | Description | Env Var | CLI Flag |
|-------|------|---------|-------------|---------|----------|
| `start_embedded` | bool | `true`\* | Start embedded VictoriaMetrics server | `MIREN_VICTORIAMETRICS_START_EMBEDDED` | `--start-victoriametrics` |
| `http_port` | int | `8428` | HTTP port in embedded mode | `MIREN_VICTORIAMETRICS_HTTP_PORT` | `--victoriametrics-http-port` |
| `retention_period` | string | `1` | Retention period in months | `MIREN_VICTORIAMETRICS_RETENTION_PERIOD` | `--victoriametrics-retention` |
| `address` | string | `victoriametrics:8428` | Address when not using embedded | `MIREN_VICTORIAMETRICS_ADDRESS` | `--victoriametrics-addr` |

\* Defaults to `true` in standalone mode only.
