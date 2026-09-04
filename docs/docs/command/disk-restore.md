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
- `--force, -f` — Overwrite an existing disk image
- `--from-cloud` — Restore from a miren.cloud restore point
- `--name, -n` — Disk name to restore to (default: the name recorded in the snapshot)
- `--restore-point` — Restore point to use (implies --from-cloud; default: the newest)
- `--snapshot, -s` — Path to a snapshot file to restore from

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren disk`](/command/disk)
