---
title: "miren deploy target"
sidebar_label: "deploy target"
description: "Deprecated: use 'miren target list'"
---

# miren deploy target

Deprecated: use 'miren target list'

## Usage

```bash
miren deploy target [flags]
```

## Flags

- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json

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

## Subcommands

- [`miren deploy target add`](./deploy-target-add.md) — Deprecated: use 'miren target add'
- [`miren deploy target list`](./deploy-target-list.md) — Deprecated: use 'miren target list'
- [`miren deploy target remove`](./deploy-target-remove.md) — Deprecated: use 'miren target remove'
- [`miren deploy target set-default`](./deploy-target-set-default.md) — Set the default deployment target

## See also

- [`miren deploy`](./deploy.md)
