---
title: OCaml on Miren
description: Deploy OCaml apps on Miren with a Dockerfile.miren that builds a native binary with opam and dune.
keywords: [ocaml, dream, opam, dune, dockerfile, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# OCaml on Miren

OCaml isn't auto-detected, so you deploy it with a `Dockerfile.miren` that builds a
native binary with opam and dune and runs it on a minimal image. This guide uses the
[Dream](https://aantron.github.io/dream/) web framework as the example.

:::tip[Let your agent do this]
Ask your AI coding agent to "set up this OCaml app on Miren" after installing the
[Miren agent skills](/agent-skills). It adds the `Dockerfile.miren`, confirms the server
binds `0.0.0.0:$PORT`, wires up environment variables, and deploys — using this page as
its reference.
:::

## Do you need a Dockerfile?

Yes. Miren doesn't auto-detect OCaml, so add a `Dockerfile.miren` to your project root.
Miren builds from it instead of guessing the stack — see
[Using Dockerfile.miren](/guides#using-dockerfilemiren).

:::tip[Want native support?]
Miren auto-detects and builds common stacks (Python, Node, Bun, Go, Ruby, Rust)
without a Dockerfile. This language isn't one of them yet — if you'd like first-class
support, [request it](https://linear.miren.garden/suggest).
:::

## Bind to the injected port

Miren injects `PORT` and routes traffic to it, so your server must read `PORT` and
listen on `0.0.0.0`. With Dream:

```ocaml
let () =
  let port =
    match Sys.getenv_opt "PORT" with
    | Some p -> int_of_string p
    | None -> 8080
  in
  Dream.run ~interface:"0.0.0.0" ~port
  @@ Dream.logger
  @@ Dream.router [
       Dream.get "/" (fun _ -> Dream.respond "Hello from OCaml on Miren!\n");
     ]
```

`~interface:"0.0.0.0"` is required — Dream binds to `localhost` by default, which Miren
can't route to.

## The Dockerfile

Create `Dockerfile.miren` in your project root:

```dockerfile
# ----- Build stage -----
FROM ocaml/opam:debian-12-ocaml-5.2 AS builder

# Install system libraries Dream needs. Miren's build sandbox runs without
# new privileges, so `sudo` fails here — switch to root instead.
USER root
RUN apt-get update -y \
    && apt-get install -y libev-dev libgmp-dev libssl-dev pkg-config m4
USER opam

WORKDIR /app
COPY --chown=opam:opam . .
# System deps are already installed above, so skip opam's sudo-based depext step.
RUN opam install -y --no-depexts dream dune
RUN opam exec -- dune build --profile release ./main.exe

# ----- Runtime stage -----
FROM debian:12-slim

RUN apt-get update -y \
    && apt-get install -y libev4 libgmp10 libssl3 ca-certificates \
    && apt-get clean && rm -rf /var/lib/apt/lists/*

COPY --from=builder /app/_build/default/main.exe /usr/local/bin/app

EXPOSE 8080
CMD ["app"]
```

:::warning[Use `USER root`, not `sudo`, for system packages]
The `ocaml/opam` image runs as the `opam` user and expects `sudo` for `apt-get`. Miren's
build sandbox runs with no new privileges, so `sudo` fails with an exit code. Switch to
`USER root` to install packages, switch back to `USER opam`, and pass `--no-depexts` to
`opam install` so it doesn't try to `sudo apt-get` the same libraries again.
:::

A minimal `dune-project` and `dune`:

```lisp
; dune-project
(lang dune 3.0)
```

```lisp
; dune
(executable
 (name main)
 (libraries dream))
```

### .dockerignore

```text
.git
_build
```

## Set up the app

Add a `Procfile`:

```procfile
web: /usr/local/bin/app
```

Then create `.miren/app.toml` naming your app and deploy from your project root:

```toml
name = "ocaml-bench"
```

<CliCommand context="client">
```miren
miren deploy
```
</CliCommand>

## Environment variables

Set variables with `miren env set` — `-e` for plain values, `-s` for secrets (masked in
output and logs). Read them with `Sys.getenv_opt "KEY"`:

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
- **Build:** `opam install --no-depexts dream dune`, `dune build --profile release ./main.exe`
- **System deps:** use `USER root` for `apt-get` (`sudo` fails in the build sandbox); runtime needs `libev4 libgmp10 libssl3`
- **Service is required:** define a `Procfile` (`web: /usr/local/bin/app`) — the image `CMD` is not used
- **Port:** read `Sys.getenv_opt "PORT"`; Dream needs `~interface:"0.0.0.0"`
- **Env vars:** `miren env set -e/-s`; read with `Sys.getenv_opt`

## Next steps

- [Using Dockerfile.miren](/guides#using-dockerfilemiren) — how custom builds work
- [App Configuration](/app-configuration) — customize `.miren/app.toml`
- [Deployment](/deployment) — how deploys build and activate
