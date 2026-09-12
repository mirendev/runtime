---
title: "miren debug disk delete"
sidebar_label: "debug disk delete"
description: "Delete a disk entity"
---

# miren debug disk delete

Delete a disk entity

:::warning
This is a dangerous command. Only disks without bound leases should be deleted. This marks the disk for deletion. The disk controller will clean up the underlying storage. Ensure no apps are using the disk before deletion.
:::

## Usage

```bash
miren debug disk delete [flags]
```

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--id, -i` — Disk ID to delete

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren debug disk`](./debug-disk.md)
