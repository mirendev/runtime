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

## Scripting and CI

When stdout is not a terminal (a CI job, a pipe, a file), deploy prints plain text with no cursor-control escape codes, condenses the build to one summary line, and always ends with an explicit verdict and the full version ID on its own line:

```
✓ Deploy successful
Version: myapp-vCVkjR6u7744AsMebwMjGU
```

Use `--format json` to get the result as a single JSON document on stdout (`status`, `app_version`, `deploy_id`, `urls`); progress text moves to stderr, no prompts are shown, and the document is still written when the deploy fails, with `status` set to `failed` and an `error` field. Add `--quiet` to drop upload and build progress and keep only the phase summaries and the result.

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
- `--explain-format` — Explain format (default: `auto`) (choices: `auto`, `plain`, `tty`, `rawjson`, `quiet`)
- `--force, -f` — Skip confirmation prompt
- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json
- `--quiet, -q` — Suppress upload and build progress; print only phase summaries and the result
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

**Deploy from a script or CI:**

```bash
Progress goes to stderr; stdout carries one JSON document
with the status, version, and URLs:

miren deploy --format json | jq -r .app_version
```

## Subcommands

- [`miren deploy cancel`](./deploy-cancel.md) — Cancel an in-progress deployment
