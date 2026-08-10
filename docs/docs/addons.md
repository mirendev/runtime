---
title: Addons
description: Managed services like databases that Miren provisions and operates for your app, with automatic credential injection.
keywords: [addons, managed services, database, postgres, credentials, environment variables]
---

import CliCommand from '@site/src/components/CliCommand';

# Addons

Addons are managed services that Miren provisions and operates for your app. Instead of running your own database as a service, you configure an addon, and Miren handles the infrastructure — creating the server, injecting connection credentials, and cleaning up when you're done.

## Minimum working example

Declare an addon in `.miren/app.toml` and it's provisioned on the next deploy:

```toml
name = "myapp"

[services.web]
command = "npm start"

[addons.miren-postgresql]
variant = "small"
```

Miren creates the PostgreSQL server, injects `DATABASE_URL` (and the other `PG*` variables) into every service, and holds your app's start until the database is ready.

## Addons vs. Services

| | Addons | Services |
|---|--------|----------|
| **Setup** | One line in app.toml | Configure image, env, disks, scaling |
| **Management** | Miren provisions and manages | You manage the process |
| **Credentials** | Automatically injected as env vars | You configure manually |
| **Best for** | Production databases, managed infrastructure | Custom software, full control |

If you just need a PostgreSQL database for your app, use an addon. If you need custom PostgreSQL extensions or full control over the configuration, run it as a [service](/services).

## Available Addons

| Addon | Description | Supported Versions | Default |
|-------|-------------|--------------------|---------|
| `miren-postgresql` | Managed PostgreSQL database | 14, 15, 16, 17, 18 | 18 |
| `miren-mysql` | Managed MySQL database | 8, 9 | 9 |
| `miren-valkey` | Managed Valkey key-value store (Redis-compatible) | 7, 8, 9 | 9 |
| `miren-rabbitmq` | Managed RabbitMQ message broker | 3, 4 | 4 |
| `miren-memcache` | Managed Memcached in-memory cache | 1.4, 1.5, 1.6 | 1.6 |

List available addons on your cluster:

<CliCommand context="client">
```miren
miren addon list-available
```
</CliCommand>

## Installing an Addon

### Via app.toml (recommended)

Declare addons in your `.miren/app.toml`. They're provisioned automatically on deploy:

```toml
name = "myapp"

[services.web]
command = "npm start"

[addons.miren-postgresql]
variant = "small"
```

### Via CLI

Attach an addon to an existing app:

<CliCommand context="client">
```miren
miren addon create miren-postgresql:small -a myapp
```
</CliCommand>

## Version Selection

Each addon uses a default software version (e.g., PostgreSQL 17). You can choose a different version when installing an addon.

### Via app.toml

```toml
[addons.miren-postgresql]
variant = "small"
version = "16"
```

### Via CLI

<CliCommand context="client">
```miren
miren addon create miren-postgresql:small -a myapp --version 16
```
</CliCommand>

The `version` value is typically a tag from the [supported versions](#available-addons) listed above (e.g., `16` or `17`).

If no version is specified, the addon's default version is used.

Miren validates that the image is accessible in its registry before provisioning begins. If the image cannot be found, the `addon create` or deploy will fail immediately with a clear error.

### Custom images

If you need a custom build of the addon software (e.g., with additional extensions or patches), you can set `version` to a full OCI image reference:

```toml
[addons.miren-postgresql]
variant = "small"
version = "registry.example.com/custom-postgres:16-custom"
```

When the version value contains `:`, Miren uses it as the complete image reference instead of appending it as a tag to the addon's default base image. The image must be accessible from the cluster at provisioning time.

## Environment Variables

When an addon is provisioned, Miren injects connection credentials as environment variables into your app. These are available to all services in the app. Variables marked sensitive are redacted in `miren env list` output.

### PostgreSQL

| Variable | Description | Example |
|----------|-------------|---------|
| `DATABASE_URL` | Full connection URL (sensitive) | `postgres://user:pass@host:5432/dbname` |
| `PGHOST` | Database hostname | `10.10.0.196` |
| `PGPORT` | Database port | `5432` |
| `PGUSER` | Database username | `myapp` |
| `PGPASSWORD` | Database password (sensitive) | — |
| `PGDATABASE` | Database name | `myapp` |

Most frameworks and ORMs connect automatically using `DATABASE_URL`.

### MySQL

| Variable | Description | Example |
|----------|-------------|---------|
| `DATABASE_URL` | Full connection URL (sensitive) | `mysql://user:pass@host:3306/dbname` |
| `MYSQL_HOST` | Database hostname | `10.10.0.196` |
| `MYSQL_PORT` | Database port | `3306` |
| `MYSQL_USER` | Database username | `myapp` |
| `MYSQL_PASSWORD` | Database password (sensitive) | — |
| `MYSQL_DATABASE` | Database name | `myapp` |

### Valkey

Valkey is wire-compatible with Redis, so Miren injects both `VALKEY_*` and `REDIS_*` variables pointing at the same server. Use whichever your client library expects.

| Variable | Description | Example |
|----------|-------------|---------|
| `VALKEY_URL` / `REDIS_URL` | Full connection URL (sensitive) | `redis://:pass@host:6379` |
| `VALKEY_HOST` / `REDIS_HOST` | Server hostname | `10.10.0.196` |
| `VALKEY_PORT` / `REDIS_PORT` | Server port | `6379` |
| `VALKEY_PASSWORD` / `REDIS_PASSWORD` | Password (sensitive) | — |

### RabbitMQ

| Variable | Description | Example |
|----------|-------------|---------|
| `RABBITMQ_URL` | Full AMQP connection URL (sensitive) | `amqp://user:pass@host:5672/%2F` |
| `RABBITMQ_HOST` | Broker hostname | `10.10.0.196` |
| `RABBITMQ_PORT` | Broker port | `5672` |
| `RABBITMQ_USER` | Broker username | `myapp` |
| `RABBITMQ_PASSWORD` | Broker password (sensitive) | — |
| `RABBITMQ_VHOST` | Virtual host | `/` |

### Memcached

| Variable | Description | Example |
|----------|-------------|---------|
| `MEMCACHE_URL` | Connection URL | `memcache://host:11211` |
| `MEMCACHE_HOST` | Server hostname | `10.10.0.196` |
| `MEMCACHE_PORT` | Server port | `11211` |

### Inspecting injected variables

You can verify the variables are set on your app:

<CliCommand context="client">
```miren
miren env list -a myapp
```
</CliCommand>

## Variants

Each addon offers variants that control the resource allocation and architecture:

<CliCommand context="client">
```miren
miren addon variants miren-postgresql
```
</CliCommand>

### PostgreSQL & MySQL Variants

PostgreSQL and MySQL each offer two variants:

| Variant | Description | Use case |
|---------|-------------|----------|
| `small` | Dedicated server (1 GB storage) | Production apps needing isolation |
| `shared` | Multi-app shared server | Development, staging, or small apps |

**Dedicated** (`small`): Each app gets its own database instance with dedicated storage. Best for production workloads where you need isolation and predictable performance. Start here if your app might grow.

**Shared** (`shared`): Multiple apps share a single database server, each with their own logical database and credentials. The shared server does not isolate workloads — a heavy query in one app can affect others on the same server. This variant is designed for small internal tools, staging environments, and apps you know will stay lightweight.

### Valkey, RabbitMQ, and Memcached Variants

These addons currently offer only the dedicated `small` variant — each app gets its own server instance.

If no variant is specified, the default (`small`) is used.

:::note[Changing variants]
There is currently no way to migrate between variants (e.g. upgrading from `shared` to `small`). If you need to switch, you would need to back up your data, destroy the addon, recreate it with the new variant, and restore. We plan to add in-place variant upgrades in a future release.
:::

## Addon Lifecycle

### Provisioning

When you deploy an app with addons or run `addon create`, Miren:

1. Creates the addon association (status: **pending**)
2. Provisions the backing infrastructure (status: **provisioning**)
3. Injects environment variables into your app configuration
4. Marks the addon as ready (status: **active**)
5. Starts your app with the injected credentials

Provisioning typically takes 1–2 minutes for a new dedicated server (longer if the PostgreSQL image needs to be pulled for the first time).

:::note[Provisioning blocks startup]
Your app won't start until addon provisioning completes — Miren holds off launching your app's processes until all addons reach active status.
:::

### Checking Status

List addons attached to your app:

<CliCommand context="client">
```miren
miren addon list -a myapp
```
</CliCommand>

### Removing an Addon

Remove an addon and delete its data:

<CliCommand context="client">
```miren
miren addon destroy miren-postgresql -a myapp
```
</CliCommand>

:::danger[Destroying an addon is permanent]
Destroying an addon permanently deletes the database and all its data. This cannot be undone.
:::

To remove an addon via app.toml, delete the `[addons.miren-postgresql]` section and redeploy. Miren detects the removal and deprovisions the addon.

## Example: Bun + PostgreSQL

A simple web server that tracks page visits using PostgreSQL:

**`.miren/app.toml`**:
```toml
name = "my-bun-app"

[services.web]
command = "bun run index.ts"

[addons.miren-postgresql]
variant = "shared"
```

**`index.ts`**:
```typescript
import { SQL } from "bun";

const sql = new SQL({ url: process.env.DATABASE_URL });

await sql`
  CREATE TABLE IF NOT EXISTS visits (
    id SERIAL PRIMARY KEY,
    visited_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
  )
`;

const server = Bun.serve({
  port: process.env.PORT || 3000,
  async fetch(req) {
    await sql`INSERT INTO visits DEFAULT VALUES`;
    const [{ count }] = await sql`SELECT COUNT(*) as count FROM visits`;
    return new Response(`Visits: ${count}\n`);
  },
});
```

Deploy:
<CliCommand context="client">
```miren
miren deploy
```
</CliCommand>

Miren provisions PostgreSQL, injects `DATABASE_URL`, and starts your app once the database is ready.

## Backing Up and Restoring

:::info[Early version]
Addon backup and restore uses the general-purpose disk backup system. We plan to add addon-aware backup commands in a future release that will simplify this workflow. For now, the steps below work reliably for PostgreSQL addon data.

The `disk backup` and `disk restore` commands must be run directly on the server (via SSH or `miren ssh`), not from your local machine. Remote backup support is planned.
:::

Each PostgreSQL addon stores its data on a Miren disk. You can back up and restore this disk using the `miren disk backup` and `miren disk restore` commands.

### Finding the Disk Name

List disks to find the one belonging to your addon:

<CliCommand context="client">
```miren
miren debug disk list
```
</CliCommand>

Addon disks are named with a `pg-` prefix. For dedicated (`small`) addons, the name includes your app name (e.g. `pg-pg-myapp-s...-data`). For shared addons, it starts with `pg-shared-data-`.

### Creating a Backup

Back up the disk to a compressed snapshot file. This must be run on the server:

<CliCommand context="server">
```miren
miren disk backup -n <disk-name>
```
</CliCommand>

This creates a timestamped `.miren.zst` file in the current directory. If the disk is currently in use, the backup will be crash-consistent (safe for PostgreSQL, which uses write-ahead logging).

Example:

<CliCommand context="server">
```miren
miren disk backup -n pg-pg-myapp-sCZDabc123-data
# Output: pg-pg-myapp-sCZDabc123-data-20260324-120000.miren.zst
```
</CliCommand>

You can specify a custom output path with `-o`:

<CliCommand context="server">
```miren
miren disk backup -n pg-pg-myapp-sCZDabc123-data -o /backups/myapp-db.miren.zst
```
</CliCommand>

### Restoring from a Backup

To restore from a backup, provide the snapshot file. This must also be run on the server:

<CliCommand context="server">
```miren
miren disk restore -s <snapshot-file>
```
</CliCommand>

The restore procedure recreates the disk with the original name. If the disk already exists, use `--force` to overwrite:

<CliCommand context="server">
```miren
miren disk restore -s myapp-db.miren.zst --force
```
</CliCommand>

To restore to a different disk name:

<CliCommand context="server">
```miren
miren disk restore -s myapp-db.miren.zst -n new-disk-name
```
</CliCommand>

After restoring, restart your app to pick up the restored data:

<CliCommand context="client">
```miren
miren app restart myapp
```
</CliCommand>

### Backup Recommendations

- **Schedule regular backups** for production databases, especially before destructive operations like `addon destroy`
- **Store backups off-server** — copy snapshot files to external storage
- **Test restores periodically** to verify your backups are valid
- Backups are compressed with zstd and include checksum verification
