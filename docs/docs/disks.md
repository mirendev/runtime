---
title: Persistent Storage
description: Configure persistent storage for your app using local storage or Miren Disks managed volumes.
keywords: [disks, storage, persistent, volumes, local storage, backup]
---

import CliCommand from '@site/src/components/CliCommand';

# Persistent Storage

Miren provides three options for persistent storage: **Local Storage** (simple, node-local), **SQLite Databases** (node-local, continuously backed up), and **Miren Disks** (managed persistent volumes). All three are configured as disks in your `app.toml`. A SQLite database is usually easier to ask for as an [addon](/addons#sqlite-is-different); this page covers declaring one directly, which is what you want for finer control.

Your data lives on the server where your app runs. Local storage and Miren Disks are backed up on request; a SQLite disk is backed up continuously as you write to it. See each section below.

## Minimum working example

Attach a persistent directory to a service in `.miren/app.toml`:

```toml
[services.web]
command = "node server.js"

[[services.web.disks]]
name = "data"
provider = "local"
mount_path = "/data"
```

Anything the service writes under `/data` survives restarts and redeploys. For a managed [Miren Disk](#miren-disks) with exclusive leasing and its own size, drop `provider = "local"` and set `size_gb` instead — and give the service fixed concurrency (`mode = "fixed"`, `num_instances = 1`), which miren disks require.

## Local Storage

Local storage gives your app a persistent directory on the server's filesystem. Data survives container restarts and redeployments.

### Configuration

Add a disk with `provider = "local"` to your service in `.miren/app.toml`:

```toml
[services.web]
command = "node server.js"

[[services.web.disks]]
name = "data"
provider = "local"
mount_path = "/miren/data/local"
```

You can mount to any path — for example, directly to a database's data directory:

```toml
[services.db]
image = "postgres:16"

[[services.db.disks]]
name = "pgdata"
provider = "local"
mount_path = "/var/lib/postgresql/data"
```

### How It Works

- **Persistent**: Data survives container restarts and redeployments
- **Shared**: All containers within your app share the same storage
- **Host-local**: Data lives on the server's filesystem
- **Node-pinned**: Apps with local storage are scheduled to the coordinator node

:::warning[Local disks share one per-app store]
Local storage is keyed per app, not per disk. Every `provider = "local"` disk your app declares — across all its services, regardless of the disk's `name` or `mount_path` — maps to the same directory on the host. Two local disks mounted at `/cache` and `/data` are two windows onto the same files: a write to one shows up in the other.

This is handy for sharing node-local state between an app's services, but if you want isolated areas, use subdirectories under a single local disk (for example `/data/cache` and `/data/uploads`) instead of declaring multiple local disks.
:::

### When to Use Local Storage

- File uploads and user content
- Application cache
- Session storage
- Any data that needs to persist across restarts

For a SQLite database specifically, prefer [SQLite Databases](#sqlite-databases): you get the same node-local directory plus continuous backup and automatic restore.

### Limitations

- **Host-local**: Data is tied to the server. If you move your app to a different server, you'll need to migrate the data manually.
- **No managed backups**: Back up your data by copying the host directory, or use your own backup tooling.
- **Shared access**: All containers in your app can read/write simultaneously—your application needs to handle concurrent access (SQLite handles this well when configured with `PRAGMA journal_mode=WAL`).
- **Node affinity**: Apps with any disk (local or miren) are pinned to the coordinator and won't be scheduled to [distributed runners](/distributed-runners).

### Migrating from Automatic Local Storage

Previously, Miren automatically mounted `/miren/data/local` for every app. This is now opt-in via the disk config above.

If any of your environment variables reference `/miren/data/local`, Miren will automatically add the local storage volume for you — so most apps will keep working without changes. You'll see a log message when this happens, and we recommend adding the explicit disk config when convenient.

---

## SQLite Databases

A `provider = "sqlite"` disk gives your app a directory containing a SQLite
database that Miren keeps backed up for you. It is the same kind of node-local
directory as local storage, with two things added: the database is created in
WAL mode before your app starts, and committed transactions are replicated to
the coordinator continuously in the background.

:::tip[Most apps should use the addon instead]
[`[addons.miren-sqlite]`](/addons#sqlite-is-different) sets all of this up for
you and injects `DATABASE_URL`, and is the shorter path to the same database.
Declare the disk directly when you need what the addon cannot express: more
than one database in an app, attaching a database to specific services, or a
particular filename.
:::

:::warning[Experimental]
The SQLite disk provider is new and its behavior may change. Keep your own
backups of anything you cannot afford to lose.
:::

### Configuration

```toml
[services.api]
command = "./server"

[services.api.concurrency]
mode = "fixed"
num_instances = 1

[[services.api.disks]]
name = "state"
provider = "sqlite"
mount_path = "/data"
```

Your app opens the database at `/data/data.db`. It already exists and is
already in WAL mode, so there is no setup step — connect and start querying.

### How It Works

- **Created for you**: Miren creates the database in WAL mode before your
  container starts, so your app never has to handle a missing file or set
  `journal_mode` itself.
- **Backed up continuously**: A replication process on the server watches the
  database and ships committed transactions to the coordinator. There is no
  schedule to configure and no backup job to run. Replication is asynchronous,
  so a server that dies abruptly can lose the last moment of writes — a commit
  is durable on that server immediately, but takes a beat to reach the backup.
- **Restored automatically**: When a disk is attached on a server that has no
  copy of the database, Miren restores it from the coordinator first. A brand
  new disk with no backup simply starts empty.
- **Survives redeploys**: Replication stops cleanly when a container shuts
  down, flushing outstanding transactions, and resumes when the replacement
  starts.

Backups live under `/var/lib/miren/sqlite-backups` on the coordinator, one
directory per database.

### Naming a Database

A sqlite disk's `id` decides *which* database it attaches to. It defaults to
`"default"`, so the example above needs no `id` at all.

IDs are scoped to your app. Two services of the same app that name the same id
share one database; the same id in a different app is a different database.

```toml
# Both services attach the same database; only one of them should write.
[[services.api.disks]]
name = "state"
provider = "sqlite"
mount_path = "/data"
id = "orders"

[[services.worker.disks]]
name = "state"
provider = "sqlite"
mount_path = "/data"
id = "orders"
```

Use `db_file` to change the filename within the directory (default
`data.db`). It is a filename, not a path — the directory is created for you,
but subdirectories inside it are not.

:::warning[One writer at a time]
SQLite allows a single writer, so services with a sqlite disk must use
`mode = "fixed"` and `num_instances = 1`. Two instances writing one database
would corrupt it, so Miren rejects the config rather than letting that happen.

If two services share an id as above, make sure only one of them writes.
:::

### Limitations

- **Node-local**: The database lives on the server your app runs on. Apps with
  any disk are pinned to the coordinator and are not scheduled to
  [distributed runners](/distributed-runners).
- **Single writer**: See the warning above.
- **No read-only mounts**: `read_only = true` is rejected. SQLite needs to
  write its write-ahead log even to read.
- **Not a formatted volume**: A sqlite disk is a directory, so `size_gb`,
  `filesystem` and `lease_timeout` do not apply and are rejected. It grows with
  the server's free space rather than a fixed allocation.

---

## Miren Disks

:::note[Backups]
Miren Disks live on your server. Back up important data with `miren disk backup` and restore it with `miren disk restore`. Cloud backup is on the [roadmap](#roadmap-cloud-backup--sync).
:::

Miren Disks provide managed persistent storage for your applications. Disks are provisioned with a specific size and filesystem, support exclusive leasing for data consistency, and persist across app restarts and redeployments.

### Why Use Disks?

- **Managed lifecycle**: Miren handles disk creation, formatting, and attachment automatically
- **Configurable size and filesystem**: Specify exactly what you need
- **Thin provisioning**: Storage is allocated as needed, not all at once
- **Persist across redeployments**: Disks survive app deletion — reattach by name

### How Disks Work

When you configure a disk for your application:

1. **Miren creates the disk** with the size and filesystem you specify
2. **Your app instance acquires a lease** on the disk (exclusive access)
3. **The disk is mounted** at the path you specified in your container

When your app stops or restarts:
- The lease is released
- Data remains on the disk
- Your next instance can acquire the lease and continue where it left off

### How Much Storage Does Miren Provide?

During the Developer Preview, we're providing unmetered storage. The intention is to implement a free tier
and usage-based pricing on the storage. We'll be sure to communicate often and clearly how we intend
to proceed.

The feature is designed to keep our costs low, and our intention is to pass that low cost on to our users.

### Configuring Disks

Add a disk to your application by including a `disks` section in your service configuration in `.miren/app.toml`. Because a miren disk is leased exclusively to one instance, the service must use fixed concurrency with `num_instances = 1`:

```toml
[services.web]
image = "myapp:latest"

[services.web.concurrency]
mode = "fixed"
num_instances = 1

[[services.web.disks]]
name = "my-app-data"
mount_path = "/data"
size_gb = 10
filesystem = "ext4"
```

#### Configuration Options

| Option | Required | Description |
|--------|----------|-------------|
| `name` | Yes | Unique name for the disk (alphanumeric, hyphens allowed) |
| `mount_path` | Yes | Where to mount the disk in your container |
| `size_gb` | Yes* | Size in gigabytes (required for auto-creation) |
| `filesystem` | No | Filesystem type: `ext4` (default), `xfs`, or `btrfs` |
| `read_only` | No | Mount as read-only (default: false) |
| `owner` | No | Ownership for a writable disk (default: the container's run user) |

*`size_gb` is required when the disk doesn't already exist. If the disk exists, this field is ignored.

:::tip[Disks are writable by the run user by default]
A writable disk is owned by the user your container runs as, so an image that
runs as a non-root user can write to its disk without a `chown` shim in the
entrypoint. Ownership is set on the top-level directory when it doesn't already
match, so there's no slow recursive walk on every boot.

Two cases keep their existing ownership untouched: a container that runs as root
(the disk already comes up root-owned, so there's nothing to fix) and a
`read_only` mount (its filesystem can't be written to anyway).

Set `owner` to override: `"keep"` leaves the raw mount ownership untouched, and
a numeric `"uid"` or `"uid:gid"` pins a specific owner.
:::

:::warning[Changing ownership on a large existing disk is a one-time pass]
Ownership is only rewritten when the disk's top-level directory doesn't already
match, and then the whole disk is walked to update it. That pass runs once and
scales with the number of files on the disk, so the first boot after a disk
starts being owned by a different user (for example, an existing disk first
mounted by a non-root image) can take a while to become ready. Later boots skip
the walk. Set `owner = "keep"` to opt out entirely.
:::

### Example: PostgreSQL with Persistent Storage

```toml
[services.db]
image = "postgres:16"

[services.db.concurrency]
mode = "fixed"
num_instances = 1

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

### Example: File Upload Storage

```toml
[services.web]
image = "myapp:latest"

[services.web.concurrency]
mode = "fixed"
num_instances = 1

[[services.web.disks]]
name = "myapp-uploads"
mount_path = "/app/uploads"
size_gb = 50
```

### Disk Lifecycle

#### Creation

Disks are automatically created when your app first deploys with a volume configuration that includes `size_gb`. The disk is provisioned with the specified size and filesystem.

#### Reuse

If you deploy an app with a `name` that matches an existing disk, Miren will attach that disk instead of creating a new one. This allows you to:
- Share data between app versions
- Preserve data across complete redeployments
- Reference disks created by other apps

#### Deletion

:::warning[Disks survive app deletion]
Disks are **not** automatically deleted when you delete an app. This is intentional - your data is precious.
:::

To delete a disk:

<CliCommand context="client">
```miren
miren debug disk delete -i <disk-id>
```
</CliCommand>

### Inspecting Disks

List all disks:

<CliCommand context="client">
```miren
miren debug disk list
```
</CliCommand>

Check a specific disk's status:

<CliCommand context="client">
```miren
miren debug disk status -i <disk-id>
```
</CliCommand>

View active disk leases:

<CliCommand context="client">
```miren
miren debug disk lease-list
```
</CliCommand>

See [CLI Reference - Disk Commands](/command/debug-disk) for complete command documentation.

### Important Considerations

#### One Instance per Disk

Disks use exclusive leasing - only one app instance can mount a disk at a time. This ensures data consistency but means:

- Multiple replicas of your app cannot share the same disk
- If you need shared storage, use separate disks per instance or external storage

#### Disk Sizing

- Disks use thin provisioning, so storage is only allocated as needed
- Choose a size that accommodates growth

#### Filesystem Choice

- **ext4**: Best general-purpose choice, widely compatible
- **xfs**: Better for large files and high-throughput workloads

:::note[mkfs tools required]
Your server must have the mkfs tools to format the disk types.
:::

### Roadmap: Cloud Backup & Sync

We're building toward cloud-connected storage for Miren Disks. Here's what's planned:

- **Remote backup & restore** (next up): Trigger backups of your disks to Miren Cloud and restore them on any cluster. This extends the existing local backup/restore functionality to work remotely.
- **Automatic cloud sync**: Background replication of disk data to Miren Cloud, enabling seamless portability across clusters.

We'll update this page and the [changelog](https://miren.md/changelog) as these capabilities land.

### Next Steps

- [app.toml Reference — Disks](/app-toml#disks) — Complete field reference for disk configuration (including `id`, `db_file` and `lease_timeout`)
- [Services](/services) — Define services that use persistent storage
- [Getting Started](/getting-started) — Deploy your first app
- [CLI Reference - Disk Commands](/command/debug-disk) — Complete disk CLI reference
- [Miren Cloud](/miren-cloud/overview) — Set up cloud features
