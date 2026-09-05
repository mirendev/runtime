---
title: Haskell on Miren
description: Deploy Haskell apps on Miren with a Dockerfile.miren that builds a binary with Cabal.
keywords: [haskell, scotty, servant, cabal, stack, ghc, dockerfile, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# Haskell on Miren

Haskell isn't auto-detected, so you deploy it with a `Dockerfile.miren` that compiles a
binary with Cabal and runs it on a minimal image. This guide uses the
[Scotty](https://hackage.haskell.org/package/scotty) web framework as the example.

:::tip[Let your agent do this]
Ask your AI coding agent to "set up this Haskell app on Miren" after installing the
[Miren agent skills](/agent-skills). It adds the `Dockerfile.miren`, confirms the server
binds `0.0.0.0:$PORT`, wires up environment variables, and deploys — using this page as
its reference.
:::

## Does this source build need a Dockerfile?

Yes. Miren doesn't auto-detect Haskell, so add a `Dockerfile.miren` to your project root.
Miren builds from it instead of guessing the stack — see
[Using Dockerfile.miren](/guides#using-dockerfilemiren).

:::tip[Want native support?]
Miren auto-detects and builds common stacks (Python, Node, Bun, Go, Ruby, Rust)
without a Dockerfile. This language isn't one of them yet — if you'd like first-class
support, [request it](https://linear.miren.garden/suggest).
:::

## Bind to the injected port

Miren injects `PORT` and routes traffic to it. Scotty (via Warp) binds to all interfaces
already, so you just read `PORT`:

```haskell
{-# LANGUAGE OverloadedStrings #-}
module Main where

import Web.Scotty
import System.Environment (lookupEnv)

main :: IO ()
main = do
  portStr <- lookupEnv "PORT"
  let port = maybe 8080 read portStr
  scotty port $
    get "/" $ text "Hello from Haskell on Miren!\n"
```

## The Dockerfile

Create `Dockerfile.miren` in your project root. The build uses the official `haskell`
image (GHC + Cabal), then copies the binary onto a slim runtime:

```dockerfile
# ----- Build stage -----
FROM haskell:9.6.6 AS builder

WORKDIR /app
COPY . .
RUN cabal update
RUN cabal build
RUN cp "$(cabal list-bin haskell-bench)" /app/app.bin

# ----- Runtime stage -----
FROM debian:12-slim

RUN apt-get update -y \
    && apt-get install -y libgmp10 zlib1g ca-certificates \
    && apt-get clean && rm -rf /var/lib/apt/lists/*

COPY --from=builder /app/app.bin /usr/local/bin/app

EXPOSE 8080
CMD ["app"]
```

`cabal list-bin <exe>` prints the built binary's path, which the build copies to a
stable location. Replace `haskell-bench` with your executable name from the `.cabal`
file. A minimal `.cabal`:

```cabal
cabal-version:      2.4
name:               haskell-bench
version:            0.1.0

executable haskell-bench
    main-is:          Main.hs
    hs-source-dirs:   app
    build-depends:    base, scotty, text
    default-language: Haskell2010
    ghc-options:      -threaded -rtsopts "-with-rtsopts=-N"
```

:::warning[Warp needs the threaded runtime]
`-threaded` in `ghc-options` is required. Scotty runs on Warp, which uses GHC's event
manager — without the threaded runtime the app builds and starts but crashes on the
first request with `getSystemTimerManager: the TimerManager requires linking against
the threaded runtime`. `-with-rtsopts=-N` lets it use all available cores.
:::

:::info[Haskell builds are slow]
GHC compiles Scotty and its dependencies from source, so the first build takes several
minutes. Miren caches image layers, so rebuilds that don't change dependencies are much
faster.
:::

### .dockerignore

```text
.git
dist-newstyle
```

## Deploy

The Dockerfile's `CMD` starts the app. Miren uses it as the web service's startup default,
so you don't need a `Procfile` or service command.

Create `.miren/app.toml` naming your app and deploy from your project root:

```toml
name = "haskell-bench"
```

<CliCommand context="client">
```miren
miren deploy
```
</CliCommand>

## Environment variables

Set variables with `miren env set` — `-e` for plain values, `-s` for secrets (masked in
output and logs). Read them with `System.Environment.lookupEnv`:

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
- **Build:** `cabal build` on the `haskell` image; copy `$(cabal list-bin <exe>)` to a slim image
- **Threaded runtime:** add `ghc-options: -threaded` — Warp/Scotty crashes on first request without it
- **Runtime libs:** `libgmp10 zlib1g` on `debian-slim`
- **Startup:** inherited from the Dockerfile `CMD`; no `Procfile` or service command needed
- **Port:** read `lookupEnv "PORT"`; Scotty/Warp binds all interfaces
- **Env vars:** `miren env set -e/-s`; read with `lookupEnv`

## Next steps

- [Using Dockerfile.miren](/guides#using-dockerfilemiren) — how custom builds work
- [App Configuration](/app-configuration) — customize `.miren/app.toml`
- [Deployment](/deployment) — how deploys build and activate
