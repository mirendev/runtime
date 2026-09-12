---
title: "miren debug entity patch"
sidebar_label: "debug entity patch"
description: "Patch an existing entity"
---

# miren debug entity patch

Patch an existing entity

## Usage

```bash
miren debug entity patch [flags]
```

## Flags

- `--address, -a` — Address to listen on (default: `localhost:8443`)
- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--dry-run, -d` — Dry run, do not actually patch the entity
- `--id, -i` — ID of the entity (required)
- `--path, -p` — Path to the entity file with updates
- `--revision, -r` — Expected revision for optimistic concurrency (default: `0`)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren debug entity`](./debug-entity.md)
