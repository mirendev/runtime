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
http_request_timeout = 60

[ingress]
mode = "tls-autoprovision"

[tls]
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
## `[ingress]` — Ingress Settings {#ingress}

Selects the deployment shape for Miren's HTTP/HTTPS ingress. The mode determines where Miren listens and whether it terminates TLS. See [TLS](/tls) for cert sourcing under each mode.

| Field | Type | Default | Description | Env Var | CLI Flag |
|-------|------|---------|-------------|---------|----------|
| `mode` | string | `tls-autoprovision` | Ingress mode: `tls-autoprovision`, `behind-proxy-http`, or `behind-proxy-https` | `MIREN_INGRESS_MODE` | `--ingress-mode` |
| `address` | string | — | Optional bind override (full `host:port`). Replaces the mode's default bind entirely. Ignored under `tls-autoprovision`. | `MIREN_INGRESS_ADDRESS` | `--ingress-address` |

### Modes

| Mode | Default bind | TLS terminated | Cert source |
|------|--------------|----------------|-------------|
| `tls-autoprovision` (default) | `0.0.0.0:443` plus `:80` for redirect / HTTP-01 ACME | yes | `[tls]` (ACME or self-signed) |
| `behind-proxy-http` | `127.0.0.1:80` | no | n/a |
| `behind-proxy-https` | `127.0.0.1:443` | yes | `[tls]` (self-signed or DNS-01 ACME) |

The `behind-proxy-*` modes default to localhost to keep accidental misconfigurations from quietly exposing an internal endpoint to the network. Set `ingress.address = "0.0.0.0:80"` (or similar) explicitly when the proxy is on a different host.

:::info[Unix socket addresses]
`unix:/path` is reserved for a future release and rejected today with a clear error.
:::

## `[tls]` — TLS Settings {#tls}

Settings under `[tls]` cover two kinds of certs. `acme_email`, `acme_dns_provider`, and `self_signed` configure the ingress cert and only apply when Miren terminates TLS (`tls-autoprovision` or `behind-proxy-https`); they're rejected at startup under `behind-proxy-http`. `additional_names` and `additional_ips` are different: they extend the SANs on the API server and etcd certs, which exist regardless of ingress mode, so they're valid under any mode. See [TLS](/tls) for setup guides.

`additional_ips` does more than its name suggests. Alongside adding SANs, every address listed there is passed straight through to the addresses the server advertises to Miren Cloud, skipping the filtering that discovered addresses go through. That makes it the way to pin an address discovery gets wrong — a host behind a static NAT, or one where you want a specific interface used. See [Running Miren on a Tailnet](/tailscale) for a worked example, and run `miren debug advertise` on the host to see what discovery decided and why.

| Field | Type | Default | Description | Env Var | CLI Flag |
|-------|------|---------|-------------|---------|----------|
| `additional_names` | string[] | `[]` | Extra DNS names for the server certificate | `MIREN_TLS_ADDITIONAL_NAMES` | `--dns-names` |
| `additional_ips` | string[] | `[]` | Extra IPs for the server certificate, and forced into the advertised address list | `MIREN_TLS_ADDITIONAL_IPS` | `--ips` |
| `acme_dns_provider` | string | — | DNS provider for ACME DNS-01 challenges (e.g. `cloudflare`, `route53`). Required under `behind-proxy-https` if not using `self_signed`. | `MIREN_TLS_ACME_DNS_PROVIDER` | `--acme-dns-provider` |
| `acme_email` | string | — | Email for ACME account registration | `MIREN_TLS_ACME_EMAIL` | `--acme-email` |
| `self_signed` | bool | `false` | Use self-signed certificates (development only, or behind a TLS-terminating proxy that doesn't verify) | `MIREN_TLS_SELF_SIGNED` | `--self-signed-tls` |

## `[etcd]` — Etcd Settings {#etcd}

Miren uses etcd as its entity store. In standalone mode, an embedded etcd server starts automatically.

| Field | Type | Default | Description | Env Var | CLI Flag |
|-------|------|---------|-------------|---------|----------|
| `endpoints` | string[] | `[]` | Etcd endpoints (auto-configured when embedded) | `MIREN_ETCD_ENDPOINTS` | `--etcd`, `-e` |
| `prefix` | string | `/miren` | Key prefix in etcd | `MIREN_ETCD_PREFIX` | `--etcd-prefix`, `-p` |
| `start_embedded` | bool | `true`\* | Start embedded etcd server | `MIREN_ETCD_START_EMBEDDED` | `--start-etcd` |
| `client_port` | int | `12379` | Embedded etcd client port | `MIREN_ETCD_CLIENT_PORT` | `--etcd-client-port` |
| `peer_port` | int | `12380` | Embedded etcd peer port | `MIREN_ETCD_PEER_PORT` | `--etcd-peer-port` |
| `http_client_port` | int | `12381` | Embedded etcd HTTP client port (bound to loopback) | `MIREN_ETCD_HTTP_CLIENT_PORT` | `--etcd-http-client-port` |

\* Defaults to `true` in standalone mode only.

:::info[Embedded etcd is loopback-bound unless mTLS is on]
`client_port` binds to all interfaces only when etcd mTLS is configured, which
today means when distributed runners are enabled. Otherwise it binds
`127.0.0.1`, as do `peer_port` and `http_client_port` in either case. etcd's
JSON gateway is disabled outright; its health and metrics endpoints are
unaffected.
:::

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

## `[app_version]` — Version Retention {#app-version}

Every deploy creates a new version of an app, and Miren keeps a bounded history of them rather than retaining every version forever. Pruning old versions frees the disk space their container images take up and keeps the server's per-app state from growing with every deploy.

A version is retained if it is among the most recent `retention_count` **or** newer than `retention_period` — whichever rule keeps it. The two settings are a floor, not a budget: raising either one keeps more versions. The currently active version is always retained regardless of these limits, and ephemeral (preview) versions are managed separately by their own TTL.

On a frequently-deployed cluster this window can pin more image data than the disk can hold, so Miren tightens retention automatically under pressure: once storage reaches 80%, a sweep drops the `retention_period` floor and keeps the active version plus `retention_count` (plus anything still in use). That makes `retention_count` a hard floor, always honored, while `retention_period` is best-effort and yields when the disk is tight. See [Managing Disk Space](/managing-disk-space) for how the reclaim works end to end and what to tune.

| Field | Type | Default | Description | Env Var | CLI Flag |
|-------|------|---------|-------------|---------|----------|
| `retention_count` | int | `10` | Most-recent versions to keep per app, regardless of age | `MIREN_APP_VERSION_RETENTION_COUNT` | `--app-version-retention-count` |
| `retention_period` | string | `30d` | Keep versions newer than this, regardless of count (e.g. `30d`, `2w`) | `MIREN_APP_VERSION_RETENTION_PERIOD` | `--app-version-retention-period` |

## `[saga]` — Saga Execution Retention {#saga}

Miren records a saga execution for multi-step operations like creating a sandbox or running a build, so that a server crash mid-operation can resume or roll back cleanly instead of leaving things half-done. Each record holds the outputs of every step it ran.

Once an execution finishes, that record is only useful for looking back at what happened, so Miren deletes it after `retention_period`. Successes and failures are treated the same way. Executions still in progress are never deleted regardless of age, including ones stuck retrying a rollback, since those are exactly what the server needs to recover.

:::warning[Indefinite retention grows without bound]
Setting `retention_period` to `0` keeps finished executions forever. That's useful while investigating an incident, but a cluster where one app repeatedly fails to start can write thousands of executions a day, so it's worth putting back afterward.
:::

| Field | Type | Default | Description | Env Var | CLI Flag |
|-------|------|---------|-------------|---------|----------|
| `retention_period` | string | `7d` | Delete finished saga executions older than this (e.g. `7d`, `24h`). `0` keeps them indefinitely | `MIREN_SAGA_RETENTION_PERIOD` | `--saga-retention-period` |
