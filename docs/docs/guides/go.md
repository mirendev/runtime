---
title: Go on Miren
description: Deploy Go apps on Miren — automatic module builds to a single binary, no Dockerfile required.
keywords: [go, golang, go.mod, binary, cmd, go:embed, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# Go on Miren

Miren auto-detects Go apps from `go.mod`, builds them to a single binary at
`/bin/app`, and ships it on a minimal runtime image — no Dockerfile required.

:::tip[Let your agent do this]
Ask your AI coding agent to "set up this Go app on Miren" after installing the
[Miren agent skills](/agent-skills). It finds your main package, proposes a start
command, wires up environment variables, and deploys — using this page as its reference.
:::

## Do you need a Dockerfile?

No. Miren detects Go from `go.mod` and compiles the binary for you. The Go version
comes from the `go` directive in your `go.mod` (falling back to 1.23). Provide a
`Dockerfile.miren` only for custom build steps — see
[Using Dockerfile.miren](/guides#using-dockerfilemiren).

## Set up the app

From your project root:

<CliCommand context="client">
```miren
miren init
miren deploy
```
</CliCommand>

Preview what Miren detects — main package, version, entrypoint — without building:

<CliCommand context="client">
```miren
miren deploy --analyze
```
</CliCommand>

### Build process

Miren builds on the official Go image, runs `go mod download` (skipped when a `vendor/`
directory is present), and compiles the binary to `/bin/app`. The builder already ships
`git` and `ca-certificates`, so public modules resolve with no extra setup.

:::warning[Private modules need vendoring or a Dockerfile]
The automatic Go build can't authenticate to a private module host. It doesn't read
`GOPRIVATE`, a `.netrc`, or git credentials, and variables you set with `miren env set`
are not injected into the module download step. If your project depends on private
modules, either commit a `vendor/` directory — Miren then builds with `-mod=vendor` and
skips the download entirely — or use a [`Dockerfile.miren`](/guides#using-dockerfilemiren)
where you control how modules are fetched.
:::

### Which package gets built

Miren looks for your main package in `cmd/`:

1. If `cmd/` has a single subdirectory, that one is built.
2. If `cmd/` has a subdirectory matching the app name, that one is built.
3. Otherwise, Miren builds from the project root.

If your project has a `vendor/` directory, Miren builds with `-mod=vendor` for faster,
network-free builds.

### Start command

The compiled binary is at `/bin/app`. Your server must bind to `0.0.0.0` on `$PORT` —
Miren injects `PORT` and routes traffic to it. Read it with `os.Getenv("PORT")`:

```go
port := os.Getenv("PORT")
if port == "" {
    port = "8080"
}
log.Fatal(http.ListenAndServe("0.0.0.0:"+port, nil))
```

Use a `Procfile` to set flags or define additional processes:

```procfile
# Run the compiled binary
web: /bin/app

# With flags
web: /bin/app -addr=0.0.0.0:$PORT

# Background worker
worker: /bin/app -mode=worker

# Scheduler
scheduler: /bin/app -mode=scheduler
```

See [Services](/services) for running multiple processes.

### Runtime files

The runtime image is minimal but carries your non-Go files (READMEs, templates,
migrations, static assets, data directories, and other assets) alongside the binary so
the app can read them at runtime relative to `/app`. Go source (`*.go`) along with
`go.mod`, `go.sum`, and `vendor/` are excluded — the compiled binary needs none of them.

:::tip[Prefer go:embed for required assets]
If your app depends on files it must have at runtime, embed them with
[`go:embed`](https://pkg.go.dev/embed) rather than relying on them being copied. Embedded
files are compiled into the binary and always present.
:::

## Environment variables

Set variables with `miren env set` — `-e` for plain values, `-s` for secrets (masked
in output and logs):

<CliCommand context="client">
```miren
miren env set -e LOG_LEVEL=info
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

- **Detection:** `go.mod` in the project
- **Version:** parsed from the `go` directive in `go.mod` (fallback 1.23)
- **Binary:** built to `/bin/app`; main package resolved from `cmd/` (see rules above)
- **Vendored deps:** `vendor/` present → build uses `-mod=vendor`
- **Start command:** `web: /bin/app`; bind `0.0.0.0:$PORT` via `os.Getenv("PORT")`
- **Runtime files:** non-Go files carried to `/app`; prefer `go:embed` for required assets
- **Env vars:** `miren env set -e KEY=VALUE`, `-s` for secrets, or `[[env]]` in `app.toml`
- **Dockerfile:** not needed; add `Dockerfile.miren` only for custom builds

## Next steps

- [App Configuration](/app-configuration) — customize `.miren/app.toml`
- [Services](/services) — web + workers
- [Deployment](/deployment) — how deploys build and activate
