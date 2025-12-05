---
sidebar_position: 4
---

# Disks

Miren Disks provide persistent storage for your applications. Unlike ephemeral container storage that disappears when your app restarts, disks preserve your data across deployments, restarts, and even cluster migrations.

## Why Use Disks?

- **Persistent data**: Store databases, uploads, cache files, or any data that needs to survive restarts
- **Portable across clusters**: Your disk data is automatically synced to Miren Cloud and can be restored on any cluster
- **Automatic backups**: Data is replicated to Miren Cloud, giving you peace of mind
- **Simple configuration**: Just specify a disk name and mount path in your app config

## How Disks Work

When you configure a disk for your application:

1. **Miren creates the disk** with the size and filesystem you specify
2. **Your app instance acquires a lease** on the disk (exclusive access)
3. **The disk is mounted** at the path you specified in your container
4. **Data is replicated** to Miren Cloud in the background

When your app stops or restarts:
- The lease is released
- Data remains on the disk
- Your next instance can acquire the lease and continue where it left off

## Configuring Disks

Add a disk to your application by including a `disks` section in your service configuration in `.miren/app.toml`:

```toml
[services.web]
image = "myapp:latest"

[[services.web.disks]]
name = "my-app-data"
mount_path = "/data"
size_gb = 10
filesystem = "ext4"
```

### Configuration Options

| Option | Required | Description |
|--------|----------|-------------|
| `name` | Yes | Unique name for the disk (alphanumeric, hyphens allowed) |
| `mount_path` | Yes | Where to mount the disk in your container |
| `size_gb` | Yes* | Size in gigabytes (required for auto-creation) |
| `filesystem` | No | Filesystem type: `ext4` (default), `xfs`, or `btrfs` |
| `read_only` | No | Mount as read-only (default: false) |

*`size_gb` is required when the disk doesn't already exist. If the disk exists, this field is ignored.

## Example: PostgreSQL with Persistent Storage

```toml
[services.db]
image = "postgres:16"

[[services.db.env]]
key = "POSTGRES_PASSWORD"
value = "secret"

[[services.db.env]]
key = "PGDATA"
value = "/var/lib/postgresql/data/pgdata"

[[services.db.disks]]
name = "myapp-postgres"
mount_path = "/var/lib/postgresql/data"
size_gb = 20
filesystem = "ext4"
```

## Example: File Upload Storage

```toml
[services.web]
image = "myapp:latest"

[[services.web.disks]]
name = "myapp-uploads"
mount_path = "/app/uploads"
size_gb = 50
```

## Disk Lifecycle

### Creation

Disks are automatically created when your app first deploys with a volume configuration that includes `size_gb`. The disk is provisioned with the specified size and filesystem.

### Reuse

If you deploy an app with a `disk_name` that already exists, Miren will attach the existing disk instead of creating a new one. This allows you to:
- Share data between app versions
- Preserve data across complete redeployments
- Reference disks created by other apps

### Deletion

Disks are **not** automatically deleted when you delete an app. This is intentional - your data is precious. To delete a disk:

```bash
miren debug disk delete -i <disk-id>
```

## Viewing Disks in Miren Cloud

When connected to Miren Cloud, you can view and monitor your disks:

1. **Dashboard**: See all disks across your clusters with their status and usage
2. **Data sync status**: Monitor replication progress to the cloud
3. **Disk history**: View when disks were created, attached, and modified

Visit [miren.cloud](https://miren.cloud) and navigate to your cluster to view disk details.

## Inspecting Disks via CLI

List all disks:

```bash
miren debug disk list
```

Check a specific disk's status:

```bash
miren debug disk status -i <disk-id>
```

View active disk leases:

```bash
miren debug disk lease-list
```

See [CLI Reference - Disk Commands](/cli/disk) for complete command documentation.

## Important Considerations

### One Instance per Disk

Disks use exclusive leasing - only one app instance can mount a disk at a time. This ensures data consistency but means:

- Multiple replicas of your app cannot share the same disk
- If you need shared storage, use separate disks per instance or external storage

### Disk Sizing

- Disks use a "thin provisioning" technology, enabling it to only allocated storage when needed
- Choose a size that accommodates growth

### Filesystem Choice

- **ext4**: Best general-purpose choice, widely compatible
- **xfs**: Better for large files and high-throughput workloads

**NOTE:** Your server must have the mkfs tools to format the disk types.

## Next Steps

- [Getting Started](/getting-started) - Deploy your first app
- [CLI Reference - Disk Commands](/cli/disk) - Complete disk CLI reference
- [Working with Miren Cloud](/working-with-miren-cloud) - Set up cloud features
