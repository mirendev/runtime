---
title: Elixir on Miren
description: Deploy Elixir and Phoenix apps on Miren with a Dockerfile.miren that builds a Mix release.
keywords: [elixir, phoenix, mix release, otp, ecto, secret_key_base, dockerfile, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# Elixir on Miren

Elixir isn't auto-detected, so you deploy it with a `Dockerfile.miren` that builds a
Mix release. This guide uses Phoenix as the example — the same pattern works for any
Elixir release. The Dockerfile and steps below were validated end-to-end on a live
Miren cluster with a fresh `mix phx.new` app, Postgres addon, and migrations.

:::tip[Let your agent do this]
Ask your AI coding agent to "set up this Phoenix app on Miren" after installing the
[Miren agent skills](/agent-skills). It can generate the release
(`mix phx.gen.release`), drop in the `Dockerfile.miren`, wire up the database addon and
secrets, and deploy — using this page as its reference.
:::

## Do you need a Dockerfile?

Yes. Miren doesn't auto-detect the BEAM yet, so add a `Dockerfile.miren` to your
project root. Miren builds from it instead of guessing the stack — see
[Using Dockerfile.miren](/guides#using-dockerfilemiren).

:::tip[Want native support?]
Miren auto-detects and builds common stacks (Python, Node, Bun, Go, Ruby, Rust)
without a Dockerfile. This language isn't one of them yet — if you'd like first-class
support, [request it](https://linear.miren.garden/suggest).
:::

## Generate a release

If you're on Phoenix and haven't set up a release yet:

```bash
mix phx.gen.release
```

This generates `rel/overlays/bin/server` and `rel/overlays/bin/migrate`, which get
copied into the release root during `mix release`.

:::info[Version requirements]
Phoenix 1.8+ requires Elixir 1.15+ (1.18 recommended) and works well on Erlang/OTP 27.
Match the Elixir image's OTP suffix to your Erlang version.
:::

## The Dockerfile

Create `Dockerfile.miren` in your project root. Replace `my_app` with your OTP app name
(the `:app` in `mix.exs`):

```dockerfile
ARG ELIXIR_VERSION=1.18.4
ARG OTP_VERSION=27.3.4
ARG DEBIAN_CODENAME=bookworm

ARG BUILDER_IMAGE="hexpm/elixir:${ELIXIR_VERSION}-erlang-${OTP_VERSION}-debian-${DEBIAN_CODENAME}-20250929-slim"
ARG RUNNER_IMAGE="debian:${DEBIAN_CODENAME}-slim"

# ----- Build stage -----
FROM ${BUILDER_IMAGE} AS builder

RUN apt-get update -y && apt-get install -y build-essential git \
    && apt-get clean && rm -f /var/lib/apt/lists/*_*

WORKDIR /app
RUN mix local.hex --force && mix local.rebar --force

ENV MIX_ENV="prod"

COPY mix.exs mix.lock ./
RUN mix deps.get --only $MIX_ENV
RUN mkdir config
COPY config/config.exs config/${MIX_ENV}.exs config/
RUN mix deps.compile

COPY priv priv
COPY lib lib
COPY assets assets
COPY rel rel

RUN mix compile
RUN mix assets.deploy

COPY config/runtime.exs config/
RUN mix release

# ----- Runtime stage -----
FROM ${RUNNER_IMAGE}

RUN apt-get update -y && \
    apt-get install -y libstdc++6 openssl libncurses5 locales ca-certificates \
    && apt-get clean && rm -f /var/lib/apt/lists/*_*

RUN sed -i '/en_US.UTF-8/s/^# //g' /etc/locale.gen && locale-gen
ENV LANG=en_US.UTF-8
ENV LANGUAGE=en_US:en
ENV LC_ALL=en_US.UTF-8

WORKDIR /app
RUN chown nobody /app
ENV MIX_ENV="prod"

COPY --from=builder --chown=nobody:root /app/_build/${MIX_ENV}/rel/my_app ./

USER nobody

ENV PHX_SERVER=true
EXPOSE 4000

CMD ["/app/bin/server"]
```

:::warning[Two easy-to-miss build steps]
`mix compile` must run **before** `mix assets.deploy` — Phoenix 1.8's colocated
LiveView JavaScript is generated during compile and esbuild needs it. And you must
`COPY rel rel` so the `bin/server` and `bin/migrate` overlay scripts exist in the
release. Skipping either produces a build that fails at runtime.
:::

Pick a `hexpm/elixir` tag that actually exists — not every date is published. Browse
[hub.docker.com/r/hexpm/elixir/tags](https://hub.docker.com/r/hexpm/elixir/tags).

### .dockerignore

Keep build artifacts out of the image context:

```text
.git
_build
deps
*.ez
erl_crash.dump
priv/static/assets
priv/static/cache_manifest.json
node_modules
.elixir_ls
.env
mise.toml
```

## Set up the app

Even with a `Dockerfile.miren`, Miren needs at least one **service** defined — it
doesn't use the image's `CMD` as the start command. Add a `Procfile` next to your
`Dockerfile.miren` that starts the release (replace `my_app` with your OTP app name):

```procfile
web: /app/bin/my_app start
```

Then create `.miren/app.toml` naming your app and declaring the database addon
(covered in the next section):

```toml
name = "my_app"

[addons.miren-postgresql]
variant = "small"
```

:::note[Deploying without a service fails]
If no service is defined, the build succeeds but the deploy stops with
`no services defined: please define at least one service in a Procfile or
.miren/app.toml`. A `[services.web]` block with the same `command` in `app.toml` works
too.
:::

Phoenix's generated `config/runtime.exs` already reads `PORT` (defaulting to 4000) and
binds the endpoint to `0.0.0.0`, so it works with Miren's injected `PORT` without changes.

Before you deploy, wire up the database and secrets the release needs at boot — see the
next section — then deploy from your project root:

<CliCommand context="client">
```miren
miren deploy
```
</CliCommand>

## Environment variables

A production Phoenix app needs a database and a few secrets, and they must exist
**before the app boots** — `config/runtime.exs` raises on a missing `DATABASE_URL` or
`SECRET_KEY_BASE`. Set them before your first `miren deploy`.

:::warning[Set secrets before the app serves traffic]
A web app autoscales to zero, so a deploy can report success without ever starting an
instance — the missing-secret error only surfaces when the first request tries to boot
one, and the instance crashes. Configure the addon and secrets first.
:::

### Database via an addon

The simplest way to get `DATABASE_URL` is a managed Postgres [addon](/addons) — Miren
provisions it and injects the connection string (plus `PG*` variables) as environment
variables automatically. Declare it in `.miren/app.toml` (as shown in
[Set up the app](#set-up-the-app)):

```toml
[addons.miren-postgresql]
variant = "small"
```

### Secrets and settings

Set the remaining variables with `miren env set` — `-s` masks secrets in output and logs:

<CliCommand context="client">
```miren
miren env set -s SECRET_KEY_BASE
miren env set -e PHX_HOST=my_app.example.com
```
</CliCommand>

Generate `SECRET_KEY_BASE` with `mix phx.gen.secret` (or `openssl rand -base64 64`) and
paste it at the masked prompt. You can also set these at deploy time with
`miren deploy -s SECRET_KEY_BASE=... -e PHX_HOST=...`.

| Variable | Required | Notes |
|----------|----------|-------|
| `DATABASE_URL` | Yes | Injected by the `miren-postgresql` addon; or set manually (`ecto://USER:PASS@HOST/DATABASE`) |
| `SECRET_KEY_BASE` | Yes | Generate with `mix phx.gen.secret` |
| `PHX_HOST` | Yes | Public hostname used for URL generation |
| `PHX_SERVER` | Set in Dockerfile | Enables the HTTP server in the release |
| `PORT` | No | Injected by Miren; `runtime.exs` defaults to 4000 |
| `POOL_SIZE` | No | DB pool size, defaults to 10 |
| `DNS_CLUSTER_QUERY` | No | Enables Erlang clustering via DNS discovery |

See [App Configuration — Environment Variables](/app-configuration#environment-variables).

## Migrations

The release includes a `migrate` script at `/app/bin/migrate` (from
`mix phx.gen.release`). Run it against your deployed app to apply migrations:

<CliCommand context="client">
```miren
miren app run -a my_app -- /app/bin/migrate
```
</CliCommand>

## Clustering

A plain Phoenix app with controller routes is stateless — run multiple replicas behind
Miren's load balancer with no extra setup. Erlang clustering (for cross-node LiveView
or channels) is off unless you set `DNS_CLUSTER_QUERY` to a headless DNS name that
resolves all instance IPs, which activates the scaffolded `DNSCluster`.

## Agent quick reference

- **Detection:** none — requires `Dockerfile.miren` (multi-stage Mix release)
- **Release:** `mix phx.gen.release`; `mix compile` **before** `mix assets.deploy`; `COPY rel rel`
- **Runtime image:** `debian-slim` is fine — `mix release` bundles ERTS, so the runner needs no Erlang install
- **Runtime env:** `PHX_SERVER=true` in the Dockerfile; endpoint binds `0.0.0.0:$PORT` via `runtime.exs`
- **Service is required:** define a `Procfile` (`web: /app/bin/<app> start`) or `[services.web]` — the image `CMD` is not used
- **Secrets first:** set `SECRET_KEY_BASE` + `PHX_HOST` before the app serves traffic, or the instance crashloops
- **Database:** `[addons.miren-postgresql]` injects `DATABASE_URL` (and `PG*`) automatically
- **Migrations:** `miren app run -a <app> -- /app/bin/migrate`
- **Image tags:** pick an existing `hexpm/elixir` tag; match the OTP suffix to your Erlang version

## Next steps

- [Using Dockerfile.miren](/guides#using-dockerfilemiren) — how custom builds work
- [Addons](/addons) — managed Postgres and other backing services
- [App Configuration](/app-configuration) — customize `.miren/app.toml`
- [Deployment](/deployment) — how deploys build and activate
