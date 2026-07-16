---
title: "miren addon create"
sidebar_label: "addon create"
description: "Attach an addon to an application"
---

# miren addon create

Attach an addon to an application

Attaching an addon provisions the backing resource and injects its connection details as environment variables into your app. Once provisioning completes, Miren creates a new app version with those variables and rolls it out automatically — you do not need to run `miren deploy` or `miren app restart`. The rollout is deferred until the addon finishes provisioning, so it may not be immediate.

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

- `--app, -a` — Application name
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
