---
title: "miren debug disk backup"
sidebar_label: "debug disk backup"
description: "Back up a disk by reading its image directly (break-glass)"
---

# miren debug disk backup

Back up a disk by reading its image directly (break-glass)

## Usage

```bash
miren debug disk backup [flags]
```

## Flags

- `--cloud` — Also upload the snapshot to miren.cloud as a restore point
- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--data-path` — Path to miren data directory (default: `/var/lib/miren`)
- `--name, -n` — Disk name to backup
- `--output, -o` — Output snapshot path (default: DISK-YYYYMMDD-HHMMSS.miren.zst)
- `--pin` — Name the uploaded restore point, pinning it against cleanup

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren debug disk`](/command/debug-disk)
