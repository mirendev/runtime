---
title: Rust on Miren
description: Deploy Rust apps on Miren — automatic Cargo builds to a single binary, no Dockerfile required.
keywords: [rust, cargo, axum, actix, binary, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# Rust on Miren

Miren auto-detects Rust apps from `Cargo.toml`, builds them with Cargo, and ships a
single release binary at `/bin/app` on a minimal runtime image — no Dockerfile required.

:::tip[Let your agent do this]
Ask your AI coding agent to "set up this Rust app on Miren" after installing the
[Miren agent skills](/agent-skills). It confirms your binary, checks the server binds
`0.0.0.0:$PORT`, wires up environment variables, and deploys — using this page as its
reference.
:::

## Does this source build need a Dockerfile?

No. Miren detects Rust from `Cargo.toml` and runs `cargo build --release` for you. The
default is the **latest Rust 1.x**. Provide a `Dockerfile.miren` only for custom build
steps — see [Using Dockerfile.miren](/guides#using-dockerfilemiren).

## Set up the app

From your project root:

<CliCommand context="client">
```miren
miren init
miren deploy
```
</CliCommand>

Preview what Miren detects — binary name, version, entrypoint — without building:

<CliCommand context="client">
```miren
miren deploy --analyze
```
</CliCommand>

### Build process

Miren builds on the official Rust base image, runs `cargo build --release`, and copies
the resulting binary to `/bin/app`.

### Binary name

Miren reads `Cargo.toml` to find the binary, then copies it to `/bin/app`:

1. Uses the `[[bin]]` name if specified.
2. Otherwise uses the package name from `[package]`.

### Start command

Your server must bind to `0.0.0.0` on `$PORT` — Miren injects `PORT` and routes traffic
to it. Read it with `std::env::var("PORT")`. Here's a minimal [axum](https://docs.rs/axum)
server:

```rust
use axum::{routing::get, Router};
use std::env;
use tokio::net::TcpListener;

#[tokio::main]
async fn main() {
    let app = Router::new().route("/", get(|| async { "Hello from Rust on Miren!\n" }));
    let port = env::var("PORT").unwrap_or_else(|_| "8080".to_string());
    let listener = TcpListener::bind(format!("0.0.0.0:{port}")).await.unwrap();
    axum::serve(listener, app).await.unwrap();
}
```

The compiled binary lands at `/bin/app`, which is the default start command. Use a
`Procfile` only to pass flags or define extra processes:

```procfile
# Default — run the compiled binary
web: /bin/app

# Background worker
worker: /bin/app --mode worker
```

### Example Cargo.toml

```toml
[package]
name = "myapp"
version = "0.1.0"
edition = "2021"

[dependencies]
axum = "0.7"
tokio = { version = "1", features = ["full"] }
```

## Environment variables

Set variables with `miren env set` — `-e` for plain values, `-s` for secrets (masked
in output and logs):

<CliCommand context="client">
```miren
miren env set -e RUST_LOG=info
miren env set -s DATABASE_URL
miren env set -s API_TOKEN
```
</CliCommand>

`miren env set -s API_TOKEN` (no value) prompts with masked input. You can also declare
variables in `.miren/app.toml`:

```toml
[[env]]
key = "DATABASE_URL"
value = ""
required = true
sensitive = true
description = "Postgres connection string"
```

See [App Configuration — Environment Variables](/app-configuration#environment-variables).

## Agent quick reference

- **Detection:** `Cargo.toml` in the project
- **Default version:** latest Rust 1.x (override via `[build] version` in `.miren/app.toml`)
- **Build:** `cargo build --release`; binary copied to `/bin/app`
- **Binary name:** `[[bin]]` name if set, else the `[package]` name
- **Start command:** `web: /bin/app` (default); bind `0.0.0.0:$PORT` via `std::env::var("PORT")`
- **Env vars:** `miren env set -e KEY=VALUE`, `-s` for secrets, or `[[env]]` in `app.toml`
- **Dockerfile:** not needed; add `Dockerfile.miren` only for custom builds

## Next steps

- [App Configuration](/app-configuration) — customize `.miren/app.toml`
- [Services](/services) — web + workers
- [Deployment](/deployment) — how deploys build and activate
