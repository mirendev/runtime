---
title: Raku on Miren
description: Deploy Raku apps on Miren with a Dockerfile.miren using the Cro framework.
keywords: [raku, perl6, cro, rakudo, zef, dockerfile, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# Raku on Miren

Raku isn't auto-detected, so you deploy it with a `Dockerfile.miren`. This guide uses
[Cro](https://cro.raku.org), the standard Raku framework for building HTTP services.

:::tip[Let your agent do this]
Ask your AI coding agent to "set up this Raku app on Miren" after installing the
[Miren agent skills](/agent-skills). It adds the `Dockerfile.miren`, binds the server to
`0.0.0.0:$PORT`, and deploys — using this page as its reference.
:::

## Do you need a Dockerfile?

Yes. Miren doesn't auto-detect Raku, so add a `Dockerfile.miren` to your project root.
Miren builds from it instead of guessing the stack — see
[Using Dockerfile.miren](/guides#using-dockerfilemiren).

:::tip[Want native support?]
Miren auto-detects and builds common stacks (Python, Node, Bun, Go, Ruby, Rust)
without a Dockerfile. This language isn't one of them yet — if you'd like first-class
support, [request it](https://linear.miren.garden/suggest).
:::

## Bind to the injected port

Miren injects `PORT` and routes traffic to it, so read `PORT` and bind `0.0.0.0`. The
final `sleep;` keeps the process alive after the server starts in the background:

```raku
use Cro::HTTP::Server;
use Cro::HTTP::Router;

my $application = route {
    get -> {
        content 'text/plain', "Hello from Raku on Miren!\n";
    }
}

my $port = %*ENV<PORT> // 8080;
my Cro::Service $service = Cro::HTTP::Server.new(
    :host('0.0.0.0'), :port(+$port), :$application,
);
$service.start;
say "listening on 0.0.0.0:$port";
sleep;
```

## The Dockerfile

Create `Dockerfile.miren` in your project root. The `rakudo-star` image bundles Rakudo
and the `zef` package manager; Cro's TLS support links against OpenSSL, so install
`libssl-dev` before `zef install`:

```dockerfile
FROM rakudo-star:latest
RUN apt-get update -y && apt-get install -y libssl-dev && rm -rf /var/lib/apt/lists/*
WORKDIR /app
RUN zef install --/test Cro::HTTP
COPY . /app
EXPOSE 8080
```

:::warning[Cro needs the OpenSSL headers]
Without `libssl-dev`, installing `Cro::HTTP` fails while compiling `Cro::TLS` with
`Cannot locate native library 'libssl.so'`. The dev package provides the `libssl.so`
symlink Cro's native bindings look for.
:::

### .dockerignore

```text
.git
```

## Set up the app

Add a `Procfile`:

```procfile
web: raku /app/app.raku
```

Then create `.miren/app.toml` naming your app and deploy from your project root:

```toml
name = "raku-bench"
```

<CliCommand context="client">
```miren
miren deploy
```
</CliCommand>

## Environment variables

Set variables with `miren env set` — `-e` for plain values, `-s` for secrets (masked in
output and logs). Read them with `%*ENV<KEY>`:

<CliCommand context="client">
```miren
miren env set -e LOG_LEVEL=info
miren env set -s DATABASE_URL
```
</CliCommand>

See [App Configuration — Environment Variables](/app-configuration#environment-variables).

## Agent quick reference

- **Detection:** none — requires `Dockerfile.miren`
- **Base image:** `rakudo-star:latest` (Rakudo + zef); `zef install --/test Cro::HTTP`
- **OpenSSL:** install `libssl-dev` before `zef install` or Cro::TLS fails to build
- **Service is required:** define a `Procfile` (`web: raku /app/app.raku`) — the image `CMD` is not used
- **Keep-alive:** end with `sleep;` so the process stays up after `$service.start`
- **Port:** `%*ENV<PORT>`; `Cro::HTTP::Server.new(:host('0.0.0.0'), :port(...))`
- **Env vars:** `miren env set -e/-s`; read with `%*ENV<KEY>`

## Next steps

- [Perl on Miren](/guides/perl) — the Perl guide
- [Using Dockerfile.miren](/guides#using-dockerfilemiren) — how custom builds work
- [Deployment](/deployment) — how deploys build and activate
