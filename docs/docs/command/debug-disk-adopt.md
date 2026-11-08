---
title: "miren debug disk adopt"
sidebar_label: "debug disk adopt"
description: "Create a disk bound to an existing cloud volume"
---

# miren debug disk adopt

Create a disk bound to an existing cloud volume

## Usage

```bash
miren debug disk adopt [flags]
```

## Flags

- `--cloud-volume` — miren.cloud volume ID to adopt (vol-...)
- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--filesystem, -f` — Filesystem type the volume holds (default: `ext4`)
- `--name, -n` — Name for the new disk
- `--node` — Node the volume lives on (default: `node/miren`)
- `--size, -s` — Size of the cloud volume in GB (default: `0`)
- `--takeover` — The volume's previous writer is lost: break any cloud lease granted before now, fencing it

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren debug disk`](./debug-disk.md)
