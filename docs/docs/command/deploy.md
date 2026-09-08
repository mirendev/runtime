---
title: "miren deploy"
sidebar_label: "deploy"
description: "Deploy an application"
---

# miren deploy

Deploy an application

Deploy uploads your project files and configuration, selects the app's primary image, and activates the resulting version. A Dockerfile selected by `[build].dockerfile` or discovered as `Dockerfile.miren` is built first. Without one, a configured web image is resolved directly; otherwise Miren builds an image from automatically detected source. When source needs rebuilding, this is the command that does it.

To activate an existing version without selecting or building another image, pass `--version`:
```bash
miren deploy --version myapp-vCVkjR6u7744AsMebwMjGU
```
This reuses the existing image and rolls it out immediately. It is useful for rolling forward to a known-good version without waiting for an image to resolve or build. Find version IDs with `miren app history`.

:::note[Config changes deploy on their own]
Changing environment variables (`miren env set` / `miren env delete`) or addons (`miren addon create` / `miren addon destroy`) already creates and rolls out a new version. You only need `miren deploy` when your code or `app.toml` has changed.
:::

## Usage

```bash
miren deploy [flags]
```

## Flags

- `--analyze` — Analyze the app without building (show detected stack, services, etc.)
- `--env, -e` — Set environment variable (KEY=VALUE, KEY=@file, or KEY to prompt)
- `--ephemeral` — Deploy as ephemeral preview with this label (e.g. feat-login)
- `--explain, -x` — Explain the build process
- `--explain-format` — Explain format (default: `auto`) (choices: `auto`, `plain`, `tty`, `rawjson`)
- `--force, -f` — Skip confirmation prompt
- `--sensitive, -s` — Set sensitive environment variable (masked in output)
- `--summary-json` — Write a JSON summary of the deploy result (deploy id, version, and route URLs) to this path
- `--ttl` — TTL for ephemeral version (e.g. 48h) (default: `24h`)
- `--version, -V` — Deploy an existing version (reuse its resolved image; skip image selection and build)

## Config Options

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file

## App Options

- `--app, -a` — Application name
- `--dir, -d` — Directory to run from (default: `.`)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Basic:**

```bash
miren deploy
```

**Analyze:**

```bash
Before deploying, the system can tell you how it's going
to treat your application by running:

miren deploy --analyze
```

**Set environment variables during deploy:**

```bash
miren deploy -e DATABASE_URL=postgres://localhost/mydb
```

**Deploy an existing version:**

```bash
miren deploy --version v3
```

## Subcommands

- [`miren deploy cancel`](/command/deploy-cancel) — Cancel an in-progress deployment
