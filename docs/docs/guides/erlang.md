---
title: Erlang on Miren
description: Deploy Erlang apps on Miren with a Dockerfile.miren using Cowboy and a rebar3 release.
keywords: [erlang, cowboy, rebar3, otp, beam, release, dockerfile, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# Erlang on Miren

Erlang isn't auto-detected, so you deploy it with a `Dockerfile.miren` that builds a
rebar3 release and runs it on the BEAM. This guide uses [Cowboy](https://github.com/ninenines/cowboy)
as the HTTP server. (For Elixir and Gleam, which also run on the BEAM, see their own
guides: [Elixir](/guides/elixir), [Gleam](/guides/gleam).)

:::tip[Let your agent do this]
Ask your AI coding agent to "set up this Erlang app on Miren" after installing the
[Miren agent skills](/agent-skills). It adds the `Dockerfile.miren`, binds Cowboy to
`0.0.0.0:$PORT`, and deploys — using this page as its reference.
:::

## Do you need a Dockerfile?

Yes. Miren doesn't auto-detect Erlang, so add a `Dockerfile.miren` to your project root.
Miren builds from it instead of guessing the stack — see
[Using Dockerfile.miren](/guides#using-dockerfilemiren).

:::tip[Want native support?]
Miren auto-detects and builds common stacks (Python, Node, Bun, Go, Ruby, Rust)
without a Dockerfile. This language isn't one of them yet — if you'd like first-class
support, [request it](https://linear.miren.garden/suggest).
:::

## Read the injected port

Miren injects `PORT` and routes traffic to it. In the application's `start/2`, read
`PORT` and start Cowboy's listener on it (Cowboy binds all interfaces by default):

```erlang
-module(erlang_bench_app).
-behaviour(application).
-export([start/2, stop/1]).

start(_Type, _Args) ->
    Dispatch = cowboy_router:compile([
        {'_', [{"/", erlang_bench_handler, []}]}
    ]),
    Port = list_to_integer(os:getenv("PORT", "8080")),
    {ok, _} = cowboy:start_clear(http_listener,
        [{port, Port}],
        #{env => #{dispatch => Dispatch}}),
    erlang_bench_sup:start_link().

stop(_State) -> ok.
```

A minimal request handler:

```erlang
-module(erlang_bench_handler).
-export([init/2]).

init(Req0, State) ->
    Req = cowboy_req:reply(200,
        #{<<"content-type">> => <<"text/plain">>},
        <<"Hello from Erlang on Miren!\n">>,
        Req0),
    {ok, Req, State}.
```

Declare Cowboy in `rebar.config` and the release:

```erlang
{deps, [{cowboy, "2.12.0"}]}.
{relx, [{release, {erlang_bench, "0.1.0"}, [erlang_bench, cowboy]},
        {dev_mode, false},
        {include_erts, true}]}.
```

`include_erts, true` bundles the runtime, so the release is self-contained.

## The Dockerfile

Create `Dockerfile.miren` in your project root. The build produces a release; the
runtime image just needs a couple of shared libraries the bundled ERTS links against:

```dockerfile
# ----- Build stage -----
FROM erlang:27 AS builder
WORKDIR /app
COPY . .
RUN rebar3 release

# ----- Runtime stage -----
FROM debian:12-slim
RUN apt-get update -y && apt-get install -y libncurses6 libssl3 && rm -rf /var/lib/apt/lists/*
COPY --from=builder /app/_build/default/rel/erlang_bench /app
EXPOSE 8080
CMD ["/app/bin/erlang_bench", "foreground"]
```

### .dockerignore

```text
.git
_build
```

## Set up the app

Even with a `Dockerfile.miren`, Miren needs at least one **service** defined — it
doesn't use the image's `CMD` as the start command. Run the release in the foreground:

```procfile
web: /app/bin/erlang_bench foreground
```

Then create `.miren/app.toml` naming your app and deploy from your project root:

```toml
name = "erlang-bench"
```

<CliCommand context="client">
```miren
miren deploy
```
</CliCommand>

:::note[Deploying without a service fails]
If no service is defined, the build succeeds but the deploy stops with
`no services defined: please define at least one service in a Procfile or
.miren/app.toml`.
:::

## Environment variables

Set variables with `miren env set` — `-e` for plain values, `-s` for secrets (masked in
output and logs). Read them with `os:getenv("KEY")`:

<CliCommand context="client">
```miren
miren env set -e LOG_LEVEL=info
miren env set -s DATABASE_URL
```
</CliCommand>

You can also declare variables in `.miren/app.toml`:

```toml
[[env]]
key = "DATABASE_URL"
value = ""
required = true
sensitive = true
```

See [App Configuration — Environment Variables](/app-configuration#environment-variables).

## Agent quick reference

- **Detection:** none — requires `Dockerfile.miren` (rebar3 release)
- **Build:** `rebar3 release` on `erlang:27`; `include_erts, true` makes it self-contained
- **Runtime libs:** `libncurses6 libssl3` on `debian-slim`
- **Service is required:** `Procfile` `web: /app/bin/<release> foreground` — the image `CMD` is not used
- **Port:** `os:getenv("PORT", "8080")`; `cowboy:start_clear(_, [{port, Port}], _)`
- **Env vars:** `miren env set -e/-s`; read with `os:getenv`

## Next steps

- [Elixir on Miren](/guides/elixir) and [Gleam on Miren](/guides/gleam) — other BEAM guides
- [Using Dockerfile.miren](/guides#using-dockerfilemiren) — how custom builds work
- [App Configuration](/app-configuration) — customize `.miren/app.toml`
- [Deployment](/deployment) — how deploys build and activate
