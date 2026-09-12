---
title: "miren debug entity put"
sidebar_label: "debug entity put"
description: "Put an entity"
---

# miren debug entity put

Put an entity

:::warning
This is an advanced command. Use the higher-level commands like `miren deploy` instead when possible.
:::

## Usage

```bash
miren debug entity put [flags]
```

## Flags

- `--address, -a` — Address to listen on (default: `localhost:8443`)
- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--dry-run, -d` — Dry run, do not actually put the entity
- `--id, -i` — ID of the entity
- `--path, -p` — Path to the entity
- `--update, -u` — Update the entity if it exists

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren debug entity`](./debug-entity.md)
