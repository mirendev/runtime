---
title: "miren debug disk undelete"
sidebar_label: "debug disk undelete"
description: "Recover a deleted disk by moving its data directly (break-glass)"
---

# miren debug disk undelete

Recover a deleted disk by moving its data directly (break-glass)

## Usage

```bash
miren debug disk undelete [flags]
```

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--data-path` — Path to miren data directory (default: `/var/lib/miren`)
- `--name, -n` — Disk name to undelete
- `--volume-id, -V` — Volume ID to restore (when multiple deleted disks share a name)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren debug disk`](/command/debug-disk)
