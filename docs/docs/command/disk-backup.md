---
title: "miren disk backup"
sidebar_label: "disk backup"
description: "Backup a disk to a snapshot file"
---

# miren disk backup

Backup a disk to a snapshot file

## Usage

```bash
miren disk backup [flags]
```

## Flags

- `--cloud` — Upload the snapshot to miren.cloud as a restore point instead of writing a local file
- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--name, -n` — Disk name to backup
- `--output, -o` — Output snapshot path (default: DISK-YYYYMMDD-HHMMSS.miren.zst)
- `--pin` — Name the uploaded restore point, pinning it against cleanup

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren disk`](./disk.md)
