---
title: "miren debug disk lease-list"
sidebar_label: "debug disk lease-list"
description: "List all disk lease entities"
---

# miren debug disk lease-list

List all disk lease entities

## Usage

```bash
miren debug disk lease-list [flags]
```

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--disk, -d` — Filter by disk ID
- `--sandbox, -s` — Filter by sandbox ID
- `--status` — Filter by status (pending, bound, released, failed)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren debug disk`](./debug-disk.md)
