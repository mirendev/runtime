---
title: Nim on Miren
description: Deploy Nim apps on Miren with a Dockerfile.miren that compiles a native binary.
keywords: [nim, jester, httpbeast, nimble, dockerfile, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# Nim on Miren

Nim isn't auto-detected, so you deploy it with a `Dockerfile.miren` that compiles a
native binary and runs it on a minimal image. This guide uses the
[Jester](https://github.com/dom96/jester) web framework.

:::tip[Let your agent do this]
Ask your AI coding agent to "set up this Nim app on Miren" after installing the
[Miren agent skills](/agent-skills). It adds the `Dockerfile.miren`, confirms the server
binds `0.0.0.0:$PORT`, and deploys — using this page as its reference.
:::

## Does this source build need a Dockerfile?

Yes. Miren doesn't auto-detect Nim, so add a `Dockerfile.miren` to your project root.
Miren builds from it instead of guessing the stack — see
[Using Dockerfile.miren](/guides#using-dockerfilemiren).

:::tip[Want native support?]
Miren auto-detects and builds common stacks (Python, Node, Bun, Go, Ruby, Rust)
without a Dockerfile. This language isn't one of them yet — if you'd like first-class
support, [request it](https://linear.miren.garden/suggest).
:::

## Bind to the injected port

Miren injects `PORT` and routes traffic to it, so read `PORT` and bind `0.0.0.0`. With
Jester:

```nim
import jester
import std/[os, strutils]

let appPort = parseInt(getEnv("PORT", "8080"))

settings:
  port = Port(appPort)
  bindAddr = "0.0.0.0"

routes:
  get "/":
    resp "Hello from Nim on Miren!\n"
```

A minimal `.nimble` file declares the dependency and binary:

```nim
version       = "0.1.0"
author        = "you"
description   = "nim on miren"
license       = "MIT"
srcDir        = "."
bin           = @["app"]

requires "nim >= 2.0.0"
requires "jester >= 0.6.0"
```

## The Dockerfile

Create `Dockerfile.miren` in your project root:

```dockerfile
# ----- Build stage -----
FROM nimlang/nim:2.0.8 AS builder
WORKDIR /app
COPY . .
RUN nimble install -dy && nim c -d:release --opt:speed --mm:refc -o:app app.nim

# ----- Runtime stage -----
FROM debian:12-slim
RUN apt-get update -y && apt-get install -y libpcre3 openssl ca-certificates \
    && apt-get clean && rm -rf /var/lib/apt/lists/*
COPY --from=builder /app/app /usr/local/bin/app
EXPOSE 8080
CMD ["app"]
```

:::warning[Compile Jester with `--mm:refc`]
Jester runs on httpbeast, which spawns worker threads. Built with Nim 2.0's default ORC
memory manager, the app **segfaults on startup** (`SIGSEGV: Illegal storage access`,
right after "Starting N threads"). Compiling with `--mm:refc` (the older reference-counting
GC) fixes it. This guide also uses a glibc base (`debian`, via the non-Alpine `nimlang/nim`
image) and installs `libpcre3` for Jester's routing.
:::

### .dockerignore

```text
.git
app
```

## Deploy

The Dockerfile's `CMD` starts the app. Miren uses it as the web service's startup default,
so you don't need a `Procfile` or service command.

Create `.miren/app.toml` naming your app and deploy from your project root:

```toml
name = "nim-bench"
```

<CliCommand context="client">
```miren
miren deploy
```
</CliCommand>

## Environment variables

Set variables with `miren env set` — `-e` for plain values, `-s` for secrets (masked in
output and logs). Read them with `getEnv("KEY")`:

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

- **Detection:** none — requires `Dockerfile.miren` (native binary)
- **Build:** `nimble install -dy && nim c -d:release --mm:refc -o:app app.nim`
- **`--mm:refc` required:** Jester/httpbeast segfaults on startup with Nim 2.0's default ORC GC
- **Runtime libs:** `libpcre3 openssl` on `debian-slim` (glibc base)
- **Startup:** inherited from the Dockerfile `CMD`; no `Procfile` or service command needed
- **Port:** `getEnv("PORT", "8080")`; Jester `settings: bindAddr = "0.0.0.0"`
- **Env vars:** `miren env set -e/-s`; read with `getEnv`

## Next steps

- [Using Dockerfile.miren](/guides#using-dockerfilemiren) — how custom builds work
- [App Configuration](/app-configuration) — customize `.miren/app.toml`
- [Deployment](/deployment) — how deploys build and activate
