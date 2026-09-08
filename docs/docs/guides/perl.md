---
title: Perl on Miren
description: Deploy Perl apps on Miren with a Dockerfile.miren using Mojolicious.
keywords: [perl, mojolicious, dancer, plack, psgi, cpanm, dockerfile, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# Perl on Miren

Perl isn't auto-detected, so you deploy it with a `Dockerfile.miren`. This guide uses
[Mojolicious](https://mojolicious.org), which ships its own production web server — no
separate PSGI wiring needed. The same pattern works for Dancer2 or any PSGI app run
under Plack.

:::tip[Let your agent do this]
Ask your AI coding agent to "set up this Perl app on Miren" after installing the
[Miren agent skills](/agent-skills). It adds the `Dockerfile.miren`, points the server
at `0.0.0.0:$PORT`, and deploys — using this page as its reference.
:::

## Does this source build need a Dockerfile?

Yes. Miren doesn't auto-detect Perl, so add a `Dockerfile.miren` to your project root.
Miren builds from it instead of guessing the stack — see
[Using Dockerfile.miren](/guides#using-dockerfilemiren).

:::tip[Want native support?]
Miren auto-detects and builds common stacks (Python, Node, Bun, Go, Ruby, Rust)
without a Dockerfile. This language isn't one of them yet — if you'd like first-class
support, [request it](https://linear.miren.garden/suggest).
:::

## Bind to the injected port

Miren injects `PORT` and routes traffic to it. Mojolicious's `daemon` command takes a
listen URL — use `http://*:$PORT` to bind all interfaces:

```perl
use Mojolicious::Lite -signatures;

get '/' => sub ($c) {
  $c->render(text => "Hello from Perl on Miren!\n");
};

app->start;
```

## The Dockerfile

Create `Dockerfile.miren` in your project root. Install dependencies with `cpanm`:

```dockerfile
FROM perl:5.40

WORKDIR /app
RUN cpanm --notest Mojolicious
COPY . /app

EXPOSE 8080
CMD ["sh", "-c", "exec perl /app/app.pl daemon -l \"http://*:$PORT\""]
```

For an app with a `cpanfile`, install from it instead: `RUN cpanm --notest --installdeps .`

### .dockerignore

```text
.git
```

## Deploy

The Dockerfile's `CMD` starts the app. Miren uses it as the web service's startup default,
so you don't need a `Procfile` or service command.

Create `.miren/app.toml` naming your app and deploy from your project root:

```toml
name = "perl-bench"
```

<CliCommand context="client">
```miren
miren deploy
```
</CliCommand>

For a PSGI app (Dancer2, Catalyst), install Plack and Starman in the Dockerfile:

```dockerfile
RUN cpanm --notest Plack Starman
```

Then replace the Dockerfile's `CMD` with one that starts the PSGI app:

```dockerfile
CMD ["sh", "-c", "exec plackup -s Starman --host 0.0.0.0 --port \"$PORT\" /app/app.psgi"]
```

## Environment variables

Set variables with `miren env set` — `-e` for plain values, `-s` for secrets (masked in
output and logs). Read them with `$ENV{KEY}`:

<CliCommand context="client">
```miren
miren env set -e MOJO_MODE=production
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

- **Detection:** none — requires `Dockerfile.miren`
- **Base image:** `perl:5.40`; `cpanm --notest Mojolicious` (or `--installdeps .` with a `cpanfile`)
- **Startup:** inherited from the Dockerfile `CMD`; no `Procfile` or service command needed
- **Port:** Mojolicious `daemon -l http://*:$PORT`; PSGI apps use `plackup --host 0.0.0.0 --port $PORT`
- **Env vars:** `miren env set -e/-s`; read with `$ENV{KEY}`

## Next steps

- [Using Dockerfile.miren](/guides#using-dockerfilemiren) — how custom builds work
- [App Configuration](/app-configuration) — customize `.miren/app.toml`
- [Deployment](/deployment) — how deploys build and activate
