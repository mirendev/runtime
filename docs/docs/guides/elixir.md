---
title: Elixir on Miren
description: Deploy Elixir and Phoenix apps on Miren. Miren detects mix.exs, builds a Mix release, and runs it on a slim image, no Dockerfile required.
keywords: [elixir, phoenix, mix release, otp, ecto, liveview, secret_key_base, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# Elixir on Miren

Miren auto-detects Elixir apps from `mix.exs`, builds a [Mix
release](https://hexdocs.pm/mix/Mix.Tasks.Release.html), and ships just the release on a
slim Debian image. Phoenix apps get their assets built and their endpoint started
automatically. No Dockerfile required.

:::tip[Let your agent do this]
Ask your AI coding agent to "set up this Phoenix app on Miren" after installing the
[Miren agent skills](../agent-skills.md). It wires up the database addon and secrets and
deploys, using this page as its reference.
:::

## Does this source build need a Dockerfile?

No. Miren detects Elixir from `mix.exs` and builds the release for you. Provide a
`Dockerfile.miren` only for custom build steps. See
[Using Dockerfile.miren](./index.md#using-dockerfilemiren).

## Set up the app

From your project root:

<CliCommand context="client">
```miren
miren init
miren deploy
```
</CliCommand>

For a Phoenix app, `miren init` generates and stores `SECRET_KEY_BASE` for you. If
the app uses DNSCluster, it also stores a shared `RELEASE_COOKIE` and configures
`DNS_CLUSTER_QUERY=web.app.miren`. Set `PHX_HOST` to your public route hostname
before deploying: Phoenix uses it for LiveView websocket origin checks. If the
app requires `DATABASE_URL`, configure a database addon or supply the URL before
deploying (see [Environment variables](#environment-variables)).

### Build process

Miren builds on the [hexpm/elixir](https://hub.docker.com/r/hexpm/elixir) image with
`MIX_ENV=prod`:

1. `mix deps.get --only prod` and `mix deps.compile`, from `mix.exs`, `mix.lock` and
   `config/` alone, so dependency builds are cached across code changes.
2. `mix compile`.
3. `mix assets.deploy`, if your `mix.exs` defines that alias (Phoenix apps do).
4. `mix release`.

The release bundles the Erlang runtime, so the final image is `debian:bookworm-slim`
plus the release at `/app`. The Elixir toolchain and your source stay behind.

### Versions

Miren picks the Elixir and Erlang/OTP versions in this order:

1. `[build] version` in `.miren/app.toml`
2. The `elixir` (and `erlang`) entries in `.tool-versions` or `mise.toml`
3. The default, **Elixir 1.19 on Erlang/OTP 28**

Versions take the forms version managers use: `1.18`, `1.18.4`, or `1.18.4-otp-27`.
An `erlang` entry picks the OTP when the Elixir version doesn't. Miren builds
Elixir 1.16 through 1.20, each on its most recent patch release, so `1.18.4` builds on
1.18.5. The build output says exactly which versions it used.

```toml
[build]
version = "1.18-otp-26"
```

For an exact pairing outside that set, set `version` to a full
[hexpm/elixir tag](https://hub.docker.com/r/hexpm/elixir/tags) on Debian bookworm,
such as `1.18.4-erlang-27.3.4.18-debian-bookworm-20260918-slim`.

### Release name

Miren builds the release named in a `releases:` block in `mix.exs`, or, without one,
the default release named after your OTP app (the `app:` in `project/0`). Umbrella
projects need a `releases:` block.

In an umbrella Phoenix app, Miren doesn't build the web app's assets yet, since they
live under `apps/`. Build them with an `onbuild` command, which runs before the
release is assembled:

```toml
[build]
onbuild = ["cd apps/my_app_web && mix assets.deploy"]
```

### Start command

The web service runs `/app/bin/<release> start`. For DNSCluster apps, Miren
uses the node name `<release>@<instance IP>` by default; explicit
`RELEASE_NODE` and `RELEASE_DISTRIBUTION` values take precedence. For Phoenix, Miren sets
`PHX_SERVER=true` so the endpoint starts, and the `runtime.exs` from `mix phx.new`
already binds `$PORT` on all interfaces. Other apps should read `PORT` from the
environment and bind `0.0.0.0`.

## Environment variables

A production Phoenix app needs a database and a few secrets, and they must exist
**before the app boots**: `config/runtime.exs` raises on a missing `DATABASE_URL` or
`SECRET_KEY_BASE`.

:::warning[Set secrets before the app serves traffic]
A web app autoscales to zero, so a deploy can report success without ever starting an
instance. The missing-secret error only surfaces when the first request tries to boot
one, and the instance crashes. Configure the addon and secrets first.
:::

### Database via an addon

The simplest way to get `DATABASE_URL` is a managed Postgres [addon](../addons.md).
Miren provisions it and injects the connection string (plus `PG*` variables)
automatically. Declare it in `.miren/app.toml`:

```toml
[addons.miren-postgresql]
variant = "small"
```

### Secrets and settings

`miren init` stores a generated `SECRET_KEY_BASE` and, for DNSCluster apps, a
shared `RELEASE_COOKIE`. Set your public hostname with `miren env set`,
where `-s` masks secrets in output and logs:

<CliCommand context="client">
```miren
miren env set -e PHX_HOST=my_app.example.com
```
</CliCommand>

| Variable | Required | Notes |
|----------|----------|-------|
| `DATABASE_URL` | Yes | Injected by the `miren-postgresql` addon |
| `SECRET_KEY_BASE` | Yes | Generated by `miren init`, or `mix phx.gen.secret` |
| `PHX_HOST` | For LiveView | Set to your public route hostname before deploy. Phoenix uses it for URLs and the websocket origin check |
| `PHX_SERVER` | Set by Miren | Starts the endpoint in the release |
| `PORT` | No | Injected by Miren |
| `POOL_SIZE` | No | DB pool size, defaults to 10 |
| `DNS_CLUSTER_QUERY` | For DNSCluster apps | Defaults to `web.app.miren` on `miren init`; the app must also share a `RELEASE_COOKIE` |

Miren also scans your code and config for `System.fetch_env!/1` and
`System.get_env/1` and reports what it finds. A `fetch_env!` or a `get_env(...) ||
raise` counts as required.

See [App Configuration: Environment Variables](../app-configuration.md#environment-variables).

## Migrations

Run migrations with a one-off command against the release. If you generated release
helpers with `mix phx.gen.release`, use its `migrate` script:

<CliCommand context="client">
```miren
miren app run -a my_app -- /app/bin/migrate
```
</CliCommand>

Otherwise, call your release module directly, for example
`/app/bin/my_app eval "MyApp.Release.migrate()"`. Running migrations at boot from your
application's supervision tree (with `Ecto.Migrator`) also works well for small apps.

## Clustering

A Phoenix app is stateless by default, so you can run multiple replicas behind Miren's
load balancer. When the app depends on DNSCluster and reads `DNS_CLUSTER_QUERY`,
`miren init` configures discovery at `web.app.miren` and stores a shared, sensitive
`RELEASE_COOKIE` on the app. Miren names nodes `<release>@<instance IP>` so
DNSCluster can reach them. For apps initialized before this support, set both
`DNS_CLUSTER_QUERY=web.app.miren` and the same `RELEASE_COOKIE` on all replicas.
Custom node naming or discovery must agree on the node name and cookie.

## Agent quick reference

- **Detection:** `mix.exs` in the project
- **Version:** `[build] version`, else `.tool-versions` / `mise.toml`, else Elixir 1.19 / OTP 28; forms like `1.18`, `1.18.4-otp-27`, or a full bookworm hexpm tag
- **Build:** `deps.get`, `deps.compile`, `compile`, `assets.deploy` (if aliased), `release`, all with `MIX_ENV=prod`
- **Release:** the `releases:` entry in `mix.exs`, else the OTP app name; umbrellas need `releases:`
- **Umbrella Phoenix:** web app assets aren't built automatically; add `cd apps/<app>_web && mix assets.deploy` to `[build] onbuild`
- **Start command:** `/app/bin/<release> start` (IP-named for DNSCluster apps); `PHX_SERVER=true` is set for Phoenix
- **Secrets:** `SECRET_KEY_BASE` and (for DNSCluster apps) `RELEASE_COOKIE` generated by `miren init`; set `PHX_HOST` to the route host for LiveView
- **Database:** `[addons.miren-postgresql]` injects `DATABASE_URL` (and `PG*`)
- **Migrations:** `miren app run -a <app> -- /app/bin/migrate` (from `mix phx.gen.release`)
- **Dockerfile:** not needed; add `Dockerfile.miren` only for custom builds

## Next steps

- [Addons](../addons.md): managed Postgres and other backing services
- [App Configuration](../app-configuration.md): customize `.miren/app.toml`
- [Deployment](../deployment.md): how deploys build and activate
