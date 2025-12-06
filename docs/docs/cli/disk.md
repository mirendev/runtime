---
sidebar_position: 4
---

# Disk Commands

Commands for managing Miren disks. These commands are under the `debug` namespace as they're primarily used for troubleshooting and advanced operations.

## miren debug disk list

List all disk entities.

### Usage

```bash
miren debug disk list
```

### Example Output

```
Disk entities:

ID: disk/abc123
  Name: myapp-data
  Size: 10 GB
  Filesystem: ext4
  Status: provisioned
  Remote Only: false
  LSVD Volume ID: lsvd-vol-xyz789

ID: disk/def456
  Name: myapp-uploads
  Size: 50 GB
  Filesystem: ext4
  Status: attached
  Remote Only: false
  Created By: app/myapp
  LSVD Volume ID: lsvd-vol-uvw123
```

## miren debug disk status

Show detailed status of a specific disk.

### Usage

```bash
miren debug disk status -i <disk-id>
```

### Flags

- `-i, --id` - Disk ID to check (required)

### Examples

```bash
# Check disk status by full ID
miren debug disk status -i disk/abc123

# Short ID also works
miren debug disk status -i abc123
```

## miren debug disk create

Create a new disk entity manually.

**NOTE:** Disks are normally created automatically when referenced from a app.toml.
This option exists to test manual disk creation only.

### Usage

```bash
miren debug disk create -n <name> [flags]
```

### Flags

- `-n, --name` - Name for the disk (required)
- `-s, --size` - Size of disk in GB (default: 10)
- `-f, --filesystem` - Filesystem type: ext4, xfs, or btrfs (default: ext4)
- `-c, --created-by` - Creator ID for the disk
- `-r, --remote-only` - Store disk only in remote storage (no local replica)
- `-v, --volume-id` - Attach to existing LSVD volume instead of creating new one

### Examples

```bash
# Create a basic 10GB disk
miren debug disk create -n my-data

# Create a 50GB disk with XFS
miren debug disk create -n large-storage -s 50 -f xfs

# Create a remote-only disk (data only in cloud)
miren debug disk create -n cloud-only-data -s 20 --remote-only
```

## miren debug disk delete

Delete a disk entity.

**NOTE:** This is a dangerous command. Only disks without bound leases should be deleted.

### Usage

```bash
miren debug disk delete -i <disk-id>
```

### Flags

- `-i, --id` - Disk ID to delete (required)

### Examples

```bash
# Delete a disk
miren debug disk delete -i disk/abc123
```

**Warning**: This marks the disk for deletion. The disk controller will clean up the underlying storage. Ensure no apps are using the disk before deletion.

## miren debug disk lease-list

List all disk lease entities.

### Usage

```bash
miren debug disk lease-list [flags]
```

### Flags

- `-d, --disk` - Filter by disk ID
- `-s, --sandbox` - Filter by sandbox ID
- `--status` - Filter by status (pending, bound, released, failed)

### Examples

```bash
# List all leases
miren debug disk lease-list

# List leases for a specific disk
miren debug disk lease-list -d disk/abc123

# List only bound leases
miren debug disk lease-list --status bound
```

## miren debug disk lease-status

Show detailed status of a disk lease.

### Usage

```bash
miren debug disk lease-status -i <lease-id>
```

### Flags

- `-i, --id` - Lease ID to check (required)

### Examples

```bash
miren debug disk lease-status -i disk-lease/xyz789
```

## miren debug disk lease-release

Release a disk lease (mark it for cleanup).

### Usage

```bash
miren debug disk lease-release -i <lease-id>
```

### Flags

- `-i, --id` - Lease ID to release (required)

### Examples

```bash
miren debug disk lease-release -i disk-lease/xyz789
```

## miren debug disk lease-delete

Delete a disk lease entity.

### Usage

```bash
miren debug disk lease-delete -i <lease-id> [--force]
```

### Flags

- `-i, --id` - Lease ID to delete (required)
- `--force` - Force delete even if lease is bound

### Examples

```bash
# Delete a released lease
miren debug disk lease-delete -i disk-lease/xyz789

# Force delete a bound lease (use with caution)
miren debug disk lease-delete -i disk-lease/xyz789 --force
```

## miren debug disk mounts

List all currently mounted Miren disks by reading /proc/mounts.

### Usage

```bash
miren debug disk mounts
```

### Example Output

```
/dev/nbd0 on /var/lib/miren/disks/lsvd-vol-xyz789 type ext4
/dev/nbd1 on /var/lib/miren/disks/lsvd-vol-uvw123 type ext4
```

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

### Disk stuck in "provisioning"

Check server logs for storage backend errors:
```bash
miren debug disk status -i <disk-id>
```

### Lease stuck in "pending"

The disk may not be provisioned yet, or another lease may have the disk:
```bash
miren debug disk lease-list -d <disk-id>
```

### App won't start due to disk timeout

Increase the `lease_timeout` in your app configuration, or check if another app has an active lease on the disk.

## Next Steps

- [Disks Overview](/disks) - Learn about disk concepts and configuration
- [CLI Reference](/cli-reference) - See all available commands
