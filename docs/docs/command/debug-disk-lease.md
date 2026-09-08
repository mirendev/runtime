---
title: "miren debug disk lease"
sidebar_label: "debug disk lease"
description: "Create a disk lease for testing"
---

# miren debug disk lease

Create a disk lease for testing

## Usage

```bash
miren debug disk lease [flags]
```

## Flags

- `--app, -a` — App ID for the lease
- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--disk, -d` — Disk ID to lease
- `--hours, -H` — Lease duration in hours (default: `2`)
- `--path, -p` — Mount path in sandbox (default: `/data`)
- `--readonly, -r` — Mount as read-only
- `--sandbox, -s` — Sandbox ID for the lease

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren debug disk`](./debug-disk.md)
