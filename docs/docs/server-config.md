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
| `config_cluster_name` | string | `local` | Name for this cluster in client config and as the telemetry label fallback when it is not registered with Miren Cloud | `MIREN_SERVER_CONFIG_CLUSTER_NAME` | `--config-cluster-name`, `-C` |
| `skip_client_config` | bool | `false` | Skip writing client config to `clientconfig.d` | `MIREN_SERVER_SKIP_CLIENT_CONFIG` | `--skip-client-config` |
| `http_request_timeout` | int | `60` | HTTP request timeout in seconds (minimum: 1). Cluster-wide default; override per route with `miren route timeout` — see [Request Timeouts](./traffic-routing.md#request-timeouts) | `MIREN_SERVER_HTTP_REQUEST_TIMEOUT` | `--http-request-timeout` |
| `stop_sandboxes_on_shutdown` | bool | `false` | Stop all sandboxes when server shuts down (useful in development) | `MIREN_SERVER_STOP_SANDBOXES_ON_SHUTDOWN` | `--stop-sandboxes-on-shutdown` |
## `[ingress]` — Ingress Settings {#ingress}

Selects the deployment shape for Miren's HTTP/HTTPS ingress. The mode determines where Miren listens and whether it terminates TLS. See [TLS](./tls.md) for cert sourcing under each mode.

| Field | Type | Default | Description | Env Var | CLI Flag |
|-------|------|---------|-------------|---------|----------|
| `mode` | string | `tls-autoprovision` | Ingress mode: `tls-autoprovision`, `behind-proxy-http`, or `behind-proxy-https` | `MIREN_INGRESS_MODE` | `--ingress-mode` |
| `address` | string | — | Optional bind override (full `host:port`). Replaces the mode's default bind entirely. Ignored under `tls-autoprovision`. | `MIREN_INGRESS_ADDRESS` | `--ingress-address` |
| `trusted_proxy_hops` | int | `1` | Number of trusted proxies immediately in front of Miren. Used to select the visitor address from `X-Forwarded-For` under `behind-proxy-http`. | `MIREN_INGRESS_TRUSTED_PROXY_HOPS` | `--ingress-trusted-proxy-hops` |
| `error_page` | string | — | Absolute path to a cluster-wide HTML error template. Loaded at server startup. | `MIREN_INGRESS_ERROR_PAGE` | — |

### Custom error pages

Set `error_page = "/etc/miren/error.html"` under `[ingress]` to replace the
built-in HTML page for the cluster. The file must exist and be a valid Go
`html/template` (up to 128 KiB), or ingress will fail to start. Restart the
server after changing it. For an app-specific override, see
[app.toml static error pages](./app-toml.md#static-error-pages).

Templates receive `.Status` (HTTP status number), `.Title`, `.Description`,
`.Site` (visitor hostname on maintenance pages), `.Reason`, `.BackAt`, and
`.Maintenance` (boolean). `{{brandLogo}}` renders the built-in Miren logo.
Use self-contained markup and inline CSS if the page must work when the app
is unavailable. Only HTML responses use these templates: `Accept` negotiation
still selects JSON or plain text for API clients. If the app's template is
missing or cannot render, ingress falls back to the cluster template, then
to the built-in page. Errors without a resolved app use the cluster template.

For ordinary errors, `.Status`, `.Title`, and `.Description` are set; `.Site`,
`.Reason`, and `.BackAt` are empty. During maintenance, `.Status` is 503,
`.Maintenance` is true, `.Site` is the visitor's hostname, `.Reason` is the
operator's message, and `.BackAt` is a formatted UTC time when provided.
`.Title` and `.Description` are empty on maintenance pages. These are the only
template data fields; app IDs, raw failure messages, and request details are
not exposed. HTML escaping is automatic. Templates must be at most 128 KiB;
rendered output is capped at 256 KiB and falls back if it exceeds that limit.
App templates are also checked at deployment: they may use `if` and `with`,
but not `define`, `block`, `template`, or `range` actions. Only `brandLogo`,
`eq`, `ne`, `lt`, `le`, `gt`, `ge`, `and`, `or`, `not`, and `len` are available
as functions. This prevents expressions from growing without writing output
in the shared ingress process. Cluster templates may use the full Go template
syntax.

Copy this into `/etc/miren/error.html` for a cluster template, or into the
app's `static.dir` output for an app template. Replace “Example” with your
brand. It requires no external assets, so it still works during an outage:

```html
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{if .Maintenance}}Maintenance{{else}}{{.Status}} · {{.Title}}{{end}} — Example</title>
  <style>
    * { box-sizing: border-box; }
    body { margin: 0; min-height: 100vh; background: #fdfaf2; color: #1b1f27;
      font-family: system-ui, sans-serif; }
    .page { min-height: 100vh; display: flex; flex-direction: column;
      padding: 0 clamp(24px, 7vw, 112px); }
    header, footer { padding: 28px 0; border-bottom: 1px solid #eadfd6; }
    header { color: #0059ff; font-size: 24px; font-weight: 700; }
    footer { border-top: 1px solid #eadfd6; border-bottom: 0; color: #656b76; }
    main { flex: 1; display: flex; align-items: center; padding: 64px 0; }
    .content { max-width: 760px; }
    .label { color: #545868; font-size: 13px; font-weight: 700;
      letter-spacing: .14em; text-transform: uppercase; }
    h1 { font-size: clamp(40px, 6vw, 72px); line-height: 1.08;
      letter-spacing: -.04em; overflow-wrap: anywhere; }
    .detail { color: #545868; font-size: 20px; line-height: 1.6; }
    @media (prefers-color-scheme: dark) {
      body { background: #151a23; color: #f4f5f5; }
      header, footer { border-color: #393e48; }
      .label, .detail, footer { color: #b6bac1; }
    }
  </style>
</head>
<body>
  <div class="page">
    <header>Example</header>
    <main><div class="content">
      <div class="label">{{if .Maintenance}}Maintenance{{else}}Error {{.Status}}{{end}}</div>
      {{if .Maintenance}}
        <h1>{{if .Site}}{{.Site}} is down for maintenance{{else}}Down for maintenance{{end}}</h1>
        {{if .Reason}}<p class="detail">{{.Reason}}</p>{{else}}<p class="detail">Please check back soon.</p>{{end}}
        {{if .BackAt}}<p class="detail">Expected back at {{.BackAt}}.</p>{{end}}
      {{else}}
        <h1>{{.Title}}</h1>
        <p class="detail">{{.Description}}</p>
      {{end}}
    </div></main>
    <footer>Example</footer>
  </div>
</body>
</html>
```

### Modes

| Mode | Default bind | TLS terminated | Cert source |
|------|--------------|----------------|-------------|
| `tls-autoprovision` (default) | `0.0.0.0:443` plus `:80` for redirect / HTTP-01 ACME | yes | `[tls]` (ACME or self-signed) |
| `behind-proxy-http` | `127.0.0.1:80` | no | n/a |
| `behind-proxy-https` | `127.0.0.1:443` | yes | `[tls]` (self-signed or DNS-01 ACME) |

Only `behind-proxy-http` trusts `X-Forwarded-Proto` / `Forwarded` from the peer; the proxy must set it. It also uses `X-Forwarded-For` for access logs, selecting the address immediately before the configured number of trusted proxy hops from the right. The other modes derive the scheme and visitor address from the connection itself.

The `behind-proxy-*` modes default to localhost to keep accidental misconfigurations from quietly exposing an internal endpoint to the network. Set `ingress.address = "0.0.0.0:80"` (or similar) explicitly when the proxy is on a different host.

:::warning[Widening `behind-proxy-http` off loopback]
Under `behind-proxy-http`, Miren trusts `X-Forwarded-Proto` from *every* connection to the listener; it does not check the peer address. If you bind to `0.0.0.0`, `[::]`, or any non-loopback address, a firewall or security group must restrict that port to the proxy's addresses. Any other client that can reach it directly can send `X-Forwarded-Proto: http` and be issued auth cookies without the `Secure` flag.
:::

:::info[Unix socket addresses]
`unix:/path` is reserved for a future release and rejected today with a clear error.
:::

## `[tls]` — TLS Settings {#tls}

Settings under `[tls]` cover two kinds of certs. `acme_email`, `acme_dns_provider`, and `self_signed` configure the ingress cert and only apply when Miren terminates TLS (`tls-autoprovision` or `behind-proxy-https`); they're rejected at startup under `behind-proxy-http`. `additional_names` and `additional_ips` are different: they extend the SANs on the API server and etcd certs, which exist regardless of ingress mode, so they're valid under any mode. See [TLS](./tls.md) for setup guides.

`additional_ips` does more than its name suggests. Alongside adding SANs, every address listed there is passed straight through to the addresses the server advertises to Miren Cloud, skipping the filtering that discovered addresses go through. That makes it the way to pin an address discovery gets wrong — a host behind a static NAT, or one where you want a specific interface used. See [Running Miren on a Tailnet](./tailscale.md) for a worked example, and run `miren debug advertise` on the host to see what discovery decided and why.

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

:::info[Embedded etcd always comes up with mTLS]
`client_port` binds to all interfaces since embedded etcd always starts with
mTLS. `peer_port` and `http_client_port` bind to `127.0.0.1`. etcd's JSON
gateway is disabled outright; its health and metrics endpoints are
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

Controls the embedded VictoriaMetrics instance used for Miren's own runtime
metrics. Managed application metrics are sent directly to their remote-write
destination and are not copied into this instance.

| Field | Type | Default | Description | Env Var | CLI Flag |
|-------|------|---------|-------------|---------|----------|
| `start_embedded` | bool | `true`\* | Start embedded VictoriaMetrics server | `MIREN_VICTORIAMETRICS_START_EMBEDDED` | `--start-victoriametrics` |
| `http_port` | int | `8428` | HTTP port in embedded mode | `MIREN_VICTORIAMETRICS_HTTP_PORT` | `--victoriametrics-http-port` |
| `retention_period` | string | `1` | Retention period in months | `MIREN_VICTORIAMETRICS_RETENTION_PERIOD` | `--victoriametrics-retention` |
| `address` | string | `victoriametrics:8428` | Address when not using embedded | `MIREN_VICTORIAMETRICS_ADDRESS` | `--victoriametrics-addr` |

\* Defaults to `true` in standalone mode only.

## `[metrics.remote_write]` — Managed Application Metrics {#managed-app-metrics}

Configures the Prometheus Remote Write destination for services that enable
managed metrics in `app.toml`. When this section is absent, Miren does not start
vmagent and does not scrape application endpoints.

```toml
[metrics.remote_write]
url = "https://metrics.example.com/api/v1/write"
workload_identity_audience = "metrics.example.com"
```

| Field | Type | Default | Description | Env Var | CLI Flag |
|-------|------|---------|-------------|---------|----------|
| `url` | string | — | Absolute HTTP or HTTPS remote-write endpoint | `MIREN_METRICS_REMOTE_WRITE_URL` | `--metrics-remote-write-url` |
| `workload_identity_audience` | string | — | Audience for the short-lived `system:telemetrywriter` bearer token | `MIREN_METRICS_REMOTE_WRITE_AUDIENCE` | `--metrics-remote-write-audience` |

:::warning[Remote-write requirements]
Both fields must be set together. URLs containing credentials are rejected;
authentication always uses the cluster's workload identity.
:::

The coordinator runs one supervised vmagent and keeps its on-disk retry queue
under `<data_path>/app-metrics`.

:::info[Cluster label selection]
Registered clusters use their Miren Cloud cluster ID for the `miren_cluster`
label. Other clusters use [`server.config_cluster_name`](#server), so set it to
a stable name when several clusters write to the same destination.
:::

## `[app_version]` — Version Retention {#app-version}

Every deploy creates a new version of an app, and Miren keeps a bounded history of them rather than retaining every version forever. Pruning old versions frees the disk space their container images take up and keeps the server's per-app state from growing with every deploy.

A version is retained if it is among the most recent `retention_count` **or** newer than `retention_period` — whichever rule keeps it. The two settings are a floor, not a budget: raising either one keeps more versions. The currently active version is always retained regardless of these limits, and ephemeral (preview) versions are managed separately by their own TTL.

On a frequently-deployed cluster this window can pin more image data than the disk can hold, so Miren tightens retention automatically under pressure: once storage reaches 80%, a sweep drops the `retention_period` floor and keeps the active version plus `retention_count` (plus anything still in use). That makes `retention_count` a hard floor, always honored, while `retention_period` is best-effort and yields when the disk is tight. See [Managing Disk Space](./managing-disk-space.md) for how the reclaim works end to end and what to tune.

| Field | Type | Default | Description | Env Var | CLI Flag |
|-------|------|---------|-------------|---------|----------|
| `retention_count` | int | `10` | Most-recent versions to keep per app, regardless of age | `MIREN_APP_VERSION_RETENTION_COUNT` | `--app-version-retention-count` |
| `retention_period` | string | `30d` | Keep versions newer than this, regardless of count (e.g. `30d`, `2w`) | `MIREN_APP_VERSION_RETENTION_PERIOD` | `--app-version-retention-period` |

## `[deployment]` — Deployment History Retention {#deployment}

Every deploy, rollback, and config change writes a deployment record, and `miren app history` reads them back. Miren keeps a bounded window of these per app rather than every record forever. The records are small, but a cluster that runs for years accumulates thousands, and every history lookup and reconciliation pass pays to scan them.

A record is retained if it is among the most recent `retention_count` for its app **or** newer than `retention_period` — whichever rule keeps it. A few records are always kept regardless of these limits: the deployment that made the app's current version active, any deployment still in progress or holding the app's deploy lock, and any older record whose status still reads `active`. Pruning a record does not touch the app version it produced; versions have their own [retention](#app-version).

Clusters registered with Miren Cloud keep their full history there. Cloud stores deployments as an archive, so a record pruned here stays visible in cloud, and the runtime only prunes a record once cloud has confirmed it holds it. If the cluster cannot reach cloud, eligible records simply wait; nothing is lost while the link is down. On a cluster that runs without cloud, this window *is* the history, so size it to how far back you want `miren app history` to reach.

| Field | Type | Default | Description | Env Var | CLI Flag |
|-------|------|---------|-------------|---------|----------|
| `retention_count` | int | `25` | Most-recent deployment records to keep per app, regardless of age | `MIREN_DEPLOYMENT_RETENTION_COUNT` | `--deployment-retention-count` |
| `retention_period` | string | `30d` | Keep records newer than this, regardless of count (e.g. `30d`, `2w`). `0` keeps every record indefinitely | `MIREN_DEPLOYMENT_RETENTION_PERIOD` | `--deployment-retention-period` |

## `[saga]` — Saga Execution Retention {#saga}

Miren records a saga execution for multi-step operations like creating a sandbox or running a build, so that a server crash mid-operation can resume or roll back cleanly instead of leaving things half-done. Each record holds the outputs of every step it ran.

Once an execution finishes, that record is only useful for looking back at what happened, so Miren deletes it after `retention_period`. Successes and failures are treated the same way. An execution that is still in progress is never deleted at any age, including one stuck retrying a rollback, since those are exactly what the server needs to recover.

That leaves one gap, which Miren closes on its own. An execution can end up with nothing driving it and no way for anything to find it again, and because it never reaches a finished state, retention never considers it either. It would sit in the store forever, and every recovery pass would pay to read it. So an execution that has gone a week without changing state is marked failed, which is both the honest description of it and what lets the ordinary rules take over: whoever owns the operation can retry it, and retention collects the record a `retention_period` later. There's no separate setting for this — a week is far longer than the gap between two steps of a saga that's actually running, and anything the server is working on, or retrying on a loop, stays well clear of it.

:::warning[Indefinite retention grows without bound]
Setting `retention_period` to `0` freezes saga records: nothing is deleted, and nothing in progress is marked failed either. That's useful while investigating an incident, but a cluster where one app repeatedly fails to start can write thousands of executions a day, so it's worth putting back afterward.
:::

| Field | Type | Default | Description | Env Var | CLI Flag |
|-------|------|---------|-------------|---------|----------|
| `retention_period` | string | `7d` | Delete finished saga executions older than this (e.g. `7d`, `24h`). `0` freezes saga records entirely | `MIREN_SAGA_RETENTION_PERIOD` | `--saga-retention-period` |

## Workload Identity Anchor {#workload-identity}

The anchor is the `iss` claim in the tokens this cluster mints for its apps, and the address an outside verifier fetches its public keys from. The signing key is generated on the cluster and never leaves it either way — the anchor decides who *serves* the keys, not who holds them.

There is no configuration field for it, because it is a property of the registration rather than of the server. A cluster registered with Miren Cloud is anchored there, and Miren Cloud serves its discovery; a cluster installed with `--without-cloud` anchors at its own hostname and serves its own.

To change it on a registered cluster, use [`miren server identity-anchor`](./command/server-identity-anchor.md), which handles the restart and the verification overlap that keeps in-flight tokens working. See [Moving the anchor](./workload-identity.md#moving-the-anchor).
