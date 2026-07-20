---
title: Deploy the Hermes agent
description: A worked example — run Nous Research's Hermes AI agent on Miren by wrapping its Docker image, with a persistent disk and an auth-protected dashboard.
keywords: [hermes, nous research, ai agent, docker image, disk, dashboard, example, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# Deploy the Hermes agent

This worked example deploys Nous Research's
[Hermes agent](https://hermes-agent.nousresearch.com/docs/user-guide/docker) (Docker
image `nousresearch/hermes-agent:latest`) onto a Miren cluster. Hermes ships only as a
Docker image, so you wrap it in a thin `Dockerfile` and let Miren build and run it.

The end state: the Hermes **dashboard** served over HTTPS behind basic auth, the
OpenAI-compatible **API server intentionally disabled**, and all agent state persisted
on a Miren disk at `/opt/data`.

Along the way it exercises a lot of real Miren behavior — custom Dockerfile builds,
the `web` service convention, `0.0.0.0` binding, `port_timeout`, and network disks — so
it doubles as a tour of what matters when you bring your own image.

:::info[This is an application recipe, not a language guide]
For getting your own source code onto Miren, start with [Deployment](/deployment) and the
[Language Guides](/guides). This page is about wrapping a prebuilt third-party image.
:::

## Prerequisites

- `miren` CLI installed and authenticated (`miren whoami`).
- Access to the target cluster and its org.
- An Anthropic API key for the agent's LLM calls. (This example uses Anthropic; the
  `OPENAI_API_KEY` in `app.toml` is optional — supply it too if your agent uses OpenAI.)

## Select the target cluster

If the cluster isn't configured locally yet, list what your identity can see and add it
(this pins the TLS fingerprint):

<CliCommand context="client">
```bash
# List clusters available to your cloud identity
miren cluster add -i cloud

# Add it by name + public address (from the list above)
miren cluster add -c hermes -a <your-cluster-ip>:8443 -i cloud

miren whoami -C hermes
```
</CliCommand>

The commands below target it explicitly with `-C hermes`. Omit `-C` to use your default
cluster.

## The Dockerfile

Wrap the upstream image and clear its entrypoint:

```dockerfile
FROM nousresearch/hermes-agent:latest
ENTRYPOINT []
```

:::warning[Miren drops the image's ENTRYPOINT]
The upstream image's real entrypoint is s6-overlay:
`["/init", "/opt/hermes/docker/main-wrapper.sh"]`. `/init` seeds `/opt/data` on first
boot and drops privileges; `main-wrapper.sh` maps `gateway run` → `hermes gateway run`.
But Miren's Dockerfile stack does **not** carry an inherited ENTRYPOINT — it runs the
service command via `/bin/sh -c "<cmd>"`. So `CMD ["gateway","run"]` gives
`/bin/sh: gateway: not found`, because `gateway` is only resolved by the wrapper, not a
PATH binary. Clear the entrypoint here and reconstruct the real invocation in the service
command below, using `exec` so `/init` becomes PID 1.
:::

## The app.toml

```toml
name = "hermes"

[build]
dockerfile = "Dockerfile"

[services.web]
command = "exec /init /opt/hermes/docker/main-wrapper.sh gateway run"
port = 9119
port_type = "http"
port_timeout = "180s"

[services.web.concurrency]
mode = "fixed"
num_instances = 1

[[services.web.disks]]
name = "data"
provider = "miren"
mount_path = "/opt/data"
size_gb = 10
filesystem = "ext4"

# Dashboard (the single routed, health-checked port), basic auth.
[[env]]
key = "HERMES_DASHBOARD"
value = "1"
[[env]]
key = "HERMES_DASHBOARD_HOST"
value = "0.0.0.0"
[[env]]
key = "HERMES_DASHBOARD_PORT"
value = "9119"
[[env]]
key = "HERMES_DASHBOARD_BASIC_AUTH_USERNAME"
value = "admin"  # change to any username you prefer
[[env]]
key = "HERMES_DASHBOARD_BASIC_AUTH_PASSWORD"
required = true
sensitive = true

[[env]]
key = "ANTHROPIC_API_KEY"
required = true
sensitive = true
[[env]]
key = "OPENAI_API_KEY"
sensitive = true
```

:::warning[The HTTP service must be named `web`]
Miren's HTTP ingress routes an app's hostname to the service named `web`. Name it anything
else and every request returns `error acquiring lease: app/hermes` (HTTP 500) even though
the container is healthy. If you want a **portless background worker** instead (reachable
only via a messaging platform or `miren sandbox exec`), name the service something else
and omit `port`/`port_type` — then Miren does no HTTP ingress and no port health check.
:::

Two details in that config bite if you skip them. Miren health-checks and routes to the
port from *outside* the container, but Hermes components default to `127.0.0.1` — set
`HERMES_DASHBOARD_HOST=0.0.0.0` (and `API_SERVER_HOST=0.0.0.0` if you ever enable the API
server) or the health check reports `nothing is listening on :<port>`. And first boot is
slow: s6 init, first-boot seeding, and a config-schema migration all run before the port
binds, so `port_timeout = "180s"` keeps the check from giving up at the 15-second default.

:::warning[A Miren disk forces single-instance, churny rollouts]
A `provider = "miren"` (network) disk holds one exclusive lease, so it requires
`concurrency mode = "fixed"` with `num_instances = 1`. On redeploy the new instance can't
mount `/opt/data` until the old one releases the lease, so `miren deploy` frequently prints
`did not become healthy` while the instance actually comes up a few seconds later —
re-check `miren app status` / `miren sandbox list` before assuming failure. If you'd rather
have snappy rollouts and don't need node-independent storage, use `provider = "local"` (no
lease, no fixed-instance requirement; data is node-local).
:::

:::danger[Keep the OpenAI API server disabled unless sandboxed]
Hermes warns that a network-accessible OpenAI-compatible API server with the default
`local` terminal backend runs agent work **as the host user with full file and terminal
access**. This config leaves it off. Enable it only deliberately and sandboxed
(`terminal.backend: docker`) with the port firewalled.
:::

Add a `.dockerignore` to keep your local `.env` and `.miren` directory out of the build
context (secrets are passed with `miren env set -s` / `-s` deploy flags, never baked into
the image). Extend it with any other local secret or config paths your project has:

```text
.env
.miren
```

## Deploy

Non-secret config lives in `app.toml`. Pass secrets with `-s` (masked in output, stored
server-side); never bake them into the image:

<CliCommand context="client">
```bash
# Generate the dashboard password once and save it somewhere safe:
DASH_PW=$(openssl rand -base64 18 | tr -d '/+=' | cut -c1-20)

miren deploy -a hermes -C hermes -f \
  -s "ANTHROPIC_API_KEY=sk-ant-..." \
  -s "HERMES_DASHBOARD_BASIC_AUTH_PASSWORD=$DASH_PW"
```
</CliCommand>

Validate the config without building at any time:

<CliCommand context="client">
```miren
miren deploy --analyze -a hermes -C hermes
```
</CliCommand>

:::warning[`miren env delete` redeploys the current server spec]
To remove a previously-set secret (`manual`) env var, use `miren env delete` — but it
builds the new version by copying the *current server-side* spec, **not** your local
`app.toml`. Order matters: deploy your new `app.toml` first, then delete the stale env
var. Config-source vars (declared in `app.toml`) drop automatically when you remove them
and redeploy.
:::

## Add a hostname route

<CliCommand context="client">
```miren
miren route set gw.hermes.clusters.miren.run hermes -C hermes
miren route list -C hermes
```
</CliCommand>

`route set <host> <app>` has no port selector — it routes the host to the app's `web`
service. `*.clusters.miren.run` DNS and TLS are managed by the cluster; give it a few
seconds to provision.

## Verify

<CliCommand context="client">
```bash
miren app status -a hermes -C hermes        # Current Version + active
miren sandbox list -C hermes                # one running sandbox, service "web"
miren logs -a hermes -C hermes --since 5m   # look for HERMES_DASHBOARD_READY port=9119

# Route should 302 -> /login (200), served by uvicorn:
curl -sL -o /dev/null -w "%{http_code} %{url_effective}\n" https://gw.hermes.clusters.miren.run/
```
</CliCommand>

Healthy signs in the logs: `s6-rc: service main-hermes successfully started`,
`Fixing ownership of /opt/data`, `config schema 0 -> NN`, and
`HERMES_DASHBOARD_READY port=9119`.

## Connecting to the agent

- **Dashboard:** `https://gw.hermes.clusters.miren.run` (the basic-auth username from
  `app.toml` — `admin` unless you changed it — plus the password from the deploy step).
- **CLI (no port needed):** `miren sandbox exec <sandbox-id> -C hermes`, then run the
  `hermes` CLI inside the container.
- **Messaging platform (no inbound port):** set `TELEGRAM_BOT_TOKEN`; the agent dials out
  and you DM the bot.

## Roadblock checklist

1. Clear `ENTRYPOINT []` and run `exec /init /opt/hermes/docker/main-wrapper.sh gateway run` — don't rely on the image entrypoint/CMD.
2. Name the HTTP service `web`, or ingress returns `error acquiring lease`.
3. Bind services to `0.0.0.0`, not `127.0.0.1`.
4. `port_timeout = "180s"` for the slow s6 first boot.
5. A `miren` disk means `fixed` / `num_instances = 1`; expect `did not become healthy` noise on redeploy (verify state before retrying). Use `local` for snappy rollouts.
6. Deploy the new `app.toml` *before* `miren env delete` (it copies the live spec).
7. Keep the OpenAI API server off unless sandboxed — it runs agent work as the host user.

## Next steps

- [App Configuration](/app-configuration) — the full `app.toml` reference in context
- [Persistent Storage](/disks) — Miren disks vs. local disks
- [Traffic Routing](/traffic-routing) — how the `web` service and routes fit together
- [Using Dockerfile.miren](/guides#using-dockerfilemiren) — building from your own Dockerfile
