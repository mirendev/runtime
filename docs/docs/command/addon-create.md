---
title: "miren addon create"
sidebar_label: "addon create"
description: "Attach an addon to an application"
---

# miren addon create

Attach an addon to an application

## Usage

```bash
miren addon create <spec> [flags]
```

## Arguments

- `spec` — Addon spec (e.g., miren-postgresql:small)

## Flags

- `--version, -V` — Software version or full image reference (e.g., 16, or registry.example.com/postgres:16-custom)

## Config Options

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file

## App Options

- `--app, -a` — Application name (defaults to .miren/app.toml, then $MIREN_APP)
- `--dir, -d` — Directory to run from (default: `.`)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Attach a PostgreSQL addon:**

```bash
miren addon create miren-postgresql:small
```

**Attach a PostgreSQL addon with a specific version:**

```bash
miren addon create miren-postgresql:small --version 16
```

## See also

- [`miren addon`](/command/addon)
