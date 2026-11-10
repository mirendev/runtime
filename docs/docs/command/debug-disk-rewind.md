---
title: "miren debug disk rewind"
sidebar_label: "debug disk rewind"
description: "Create a disk from a cloud volume as of a point in time"
---

# miren debug disk rewind

Create a disk from a cloud volume as of a point in time

## Usage

```bash
miren debug disk rewind [flags]
```

## Flags

- `--at` — Point in time to restore to (RFC 3339); omit for the latest mirrored state
- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--filesystem, -f` — Filesystem type the volume holds (default: `ext4`)
- `--from` — miren.cloud accelerator volume ID to restore from (vol-...)
- `--name, -n` — Name for the new disk
- `--node` — Node to create the disk on (default: `node/miren`)
- `--size, -s` — Size of the source volume in GB (default: `0`)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren debug disk`](./debug-disk.md)
