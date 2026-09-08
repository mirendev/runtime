---
title: "miren disk restore"
sidebar_label: "disk restore"
description: "Restore a disk from a snapshot file"
---

# miren disk restore

Restore a disk from a snapshot file

## Usage

```bash
miren disk restore [flags]
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

- [`miren disk`](./disk.md)
