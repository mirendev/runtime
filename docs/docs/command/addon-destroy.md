---
title: "miren addon destroy"
sidebar_label: "addon destroy"
description: "Deprecated: use 'miren addon delete'"
---

# miren addon destroy

Deprecated: use 'miren addon delete'

Removing an addon deprovisions the backing resource and strips its injected environment variables from your app. Miren creates a new app version without those variables and rolls it out automatically — you do not need to run `miren deploy` or `miren app restart`.

## Usage

```bash
miren addon destroy <name> [flags]
```

## Arguments

- `name` — Addon name (e.g., miren-postgresql)

## Flags

- `--force, -f` — Deprecated: use --yes to skip confirmation
- `--yes, -y` — Skip confirmation prompt

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

**Remove an addon:**

```bash
miren addon delete miren-postgresql
```

**Remove without confirmation:**

```bash
miren addon delete miren-postgresql --yes
```

## See also

- [`miren addon`](./addon.md)
