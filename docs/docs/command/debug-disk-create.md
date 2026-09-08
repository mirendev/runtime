---
title: "miren debug disk create"
sidebar_label: "debug disk create"
description: "Create a disk entity for testing"
---

# miren debug disk create

Create a disk entity for testing

Disks are normally created automatically when referenced from an app.toml. This command exists to test manual disk creation only.

## Usage

```bash
miren debug disk create [flags]
```

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--created-by, -c` — Creator ID for the disk
- `--filesystem, -f` — Filesystem type (ext4, xfs, btrfs) (default: `ext4`)
- `--name, -n` — Name for the disk
- `--remote-only, -r` — Store disk only in remote storage (no local replica)
- `--size, -s` — Size of disk in GB (default: `10`)
- `--volume-id, -V` — Attach to existing volume instead of creating new one

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren debug disk`](./debug-disk.md)
