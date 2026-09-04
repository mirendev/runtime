---
title: "miren debug disk restore"
sidebar_label: "debug disk restore"
description: "Restore a disk by writing its image directly (break-glass)"
---

# miren debug disk restore

Restore a disk by writing its image directly (break-glass)

## Usage

```bash
miren debug disk restore [flags]
```

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--data-path` — Path to miren data directory (default: `/var/lib/miren`)
- `--force, -f` — Overwrite existing disk image without confirmation
- `--name, -n` — Disk name to restore to (default: original name from snapshot)
- `--snapshot, -s` — Path to snapshot file

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren debug disk`](/command/debug-disk)
