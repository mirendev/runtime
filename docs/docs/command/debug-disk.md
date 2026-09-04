---
title: "miren debug disk"
sidebar_label: "debug disk"
description: "Disk entity debug commands"
---

# miren debug disk

Disk entity debug commands

Commands for managing Miren disks. These commands are primarily used for troubleshooting and advanced operations.

## Disk Status Values

| Status | Description |
|--------|-------------|
| `provisioning` | Disk is being created and storage is being allocated |
| `provisioned` | Disk is ready and available for lease |
| `attached` | Disk has an active lease and is mounted |
| `detached` | Disk was previously attached but lease was released |
| `deleting` | Disk is marked for deletion |
| `error` | Disk encountered an error during provisioning |

## Lease Status Values

| Status | Description |
|--------|-------------|
| `pending` | Lease is waiting to acquire the disk |
| `bound` | Lease is active and disk is mounted |
| `released` | Lease has been released, cleanup pending |
| `failed` | Lease failed to acquire or mount the disk |

## Troubleshooting

**Disk stuck in "provisioning":**
Check server logs for storage backend errors:
```bash
miren debug disk status -i <disk-id>
```

**Lease stuck in "pending":**
The disk may not be provisioned yet, or another lease may have the disk:
```bash
miren debug disk lease-list -d <disk-id>
```

**App won't start due to disk timeout:**
Increase the `lease_timeout` in your app configuration, or check if another app has an active lease on the disk.

## Usage

```bash
miren debug disk [flags]
```

## Subcommands

- [`miren debug disk backup`](/command/debug-disk-backup) — Back up a disk by reading its image directly (break-glass)
- [`miren debug disk create`](/command/debug-disk-create) — Create a disk entity for testing
- [`miren debug disk delete`](/command/debug-disk-delete) — Delete a disk entity
- [`miren debug disk lease`](/command/debug-disk-lease) — Create a disk lease for testing
- [`miren debug disk lease-delete`](/command/debug-disk-lease-delete) — Delete a disk lease entity
- [`miren debug disk lease-list`](/command/debug-disk-lease-list) — List all disk lease entities
- [`miren debug disk lease-release`](/command/debug-disk-lease-release) — Release a disk lease
- [`miren debug disk lease-status`](/command/debug-disk-lease-status) — Show detailed status of a disk lease
- [`miren debug disk list`](/command/debug-disk-list) — List all disk entities
- [`miren debug disk mounts`](/command/debug-disk-mounts) — List all mounted disks from /proc/mounts
- [`miren debug disk restore`](/command/debug-disk-restore) — Restore a disk by writing its image directly (break-glass)
- [`miren debug disk status`](/command/debug-disk-status) — Show status of a disk entity

## See also

- [`miren debug`](/command/debug)
