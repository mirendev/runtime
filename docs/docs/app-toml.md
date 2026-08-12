---
title: app.toml Reference
sidebar_label: app.toml
description: Complete reference for .miren/app.toml — services, tasks, scaling, disks, environment variables, and build configuration.
keywords: [app.toml, configuration, reference, services, tasks, scheduled jobs, scaling, environment variables, build]
---

# app.toml Reference

Complete reference for `.miren/app.toml` — the configuration file for Miren applications.

For a guide-style introduction, see [App Configuration](/app-configuration).

## File Structure

```toml
name = "myapp"
include = ["configs/"]

# Global environment variables
[[env]]
key = "DATABASE_URL"
value = "postgres://db.app.miren:5432/myapp"

# Build configuration
[build]
version = "3.12"
dockerfile = "Dockerfile.miren"
onbuild = ["npm run build"]

# Service definitions
[services.web]
command = "node server.js"
port = 3000

[services.web.concurrency]
mode = "auto"
requests_per_instance = 10
scale_down_delay = "15m"
shutdown_timeout = "10s"

[services.worker]
command = "node worker.js"

[services.worker.concurrency]
mode = "fixed"
num_instances = 2
shutdown_timeout = "10s"

[services.db]
image = "postgres:16"

[[services.db.disks]]
name = "pgdata"
mount_path = "/var/lib/postgresql/data"
size_gb = 20

# Task definitions
[tasks.migrate]
command = "bin/rails db:migrate"
trigger = "deploy"

[tasks.cleanup]
command = "bin/cleanup-sessions"
trigger = "schedule"
every = "6h"

# Addons
[addons.storage]
variant = "minio"

# CLI Aliases
[aliases]
console = "app run bin/rails console"
tail = "logs app -f"
```

## Top-Level Fields

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| `name` | string | Application name | Inferred from directory name |
| `include` | string[] | Extra files or directories to include in the build context | — |
| `concurrency` | int | **Legacy.** Global concurrency target. Use `[services.<name>.concurrency]` instead. | — |
| `workload_role` | string | Role for this app's sandbox [in-cluster API access](/in-cluster-api). Only app-scoped roles may be set here; cluster-scoped roles require an operator. | `app-readonly` |
| `web` | bool | Whether the app has a long-running web process. Set `web = false` for an app made entirely of [tasks](#tasks). | Unset — a web service is synthesized if nothing declares one, except for a task-only app with no services, where leaving it unset is an error |

### `web` and the synthesized web service {#web}

If neither `app.toml` nor your `Procfile` declares a `web` service, Miren
synthesizes one from the image's entrypoint. That is the historical default and
it stays the default: leaving `web` unset never changes what your app does.

There is one exception, and it's the case where guessing would be expensive: an
app that declares [tasks](#tasks) and no services has to say which it meant. See
the validation note below.

`web = false` opts out. It's how an app that only declares tasks says it has no
long-running process at all — no web service, no route, and nothing running (or
billed for compute) between invocations.

It opts out of the *synthesized* service, not of a web service you asked for. A
`web` declared in `app.toml` or named by a `web:` line in your `Procfile` is an
explicit request and still runs, `web = false` or not — so an app being migrated
keeps whatever its `Procfile` says until that line is removed. Delete the
declaration to be rid of the service.

:::note[Validation]
If an app declares tasks, declares no services, and doesn't set `web`, the build
fails rather than guessing. Without that, a task-only app would quietly acquire
a web service built from its image entrypoint and keep it running forever.
:::

## `[[env]]` — Environment Variables {#env}

Declares environment variables available to all services. Service-level `[[services.<name>.env]]` entries are merged with these.

```toml
[[env]]
key = "DATABASE_URL"
value = "postgres://db.app.miren:5432/myapp"

[[env]]
key = "SECRET_KEY"
required = true
sensitive = true
description = "Used for session signing"

[[env]]
key = "STRIPE_API_KEY"
backend = "cluster"
ref = "payments/stripe-key"
```

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| `key` | string | Variable name. **Required.** | — |
| `value` | string | Variable value | `""` |
| `backend` | string | [Secret](/secrets) backend to source the value from | `""` |
| `ref` | string | Reference to the secret within that backend | `""` |
| `required` | bool | Fail deploy if value is empty | `false` |
| `sensitive` | bool | Mask value in CLI output and logs | `false` |
| `description` | string | Human-readable explanation of this variable | — |

:::note[Validation]
Every env entry must have a non-empty `key`. If `required` is `true` and `value` is empty at deploy time, the deploy fails. `ref` requires `backend`, and cannot be combined with `value`.
:::

## `[build]` — Build Configuration {#build}

Controls how Miren builds your container image.

```toml
[build]
version = "3.12"
dockerfile = "Dockerfile.custom"
onbuild = ["npm run build", "npm prune --production"]
alpine_image = "alpine:3.19"
```

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| `version` | string | Language/runtime version (e.g. `"20"` for Node, `"3.12"` for Python) | Detected from project files |
| `dockerfile` | string | Path to a custom Dockerfile | Auto-detected (`Dockerfile.miren` or built-in) |
| `onbuild` | string[] | Commands to run in `/app` after the main build steps | — |
| `alpine_image` | string | Custom Alpine base image for the runtime stage | Built-in default |

## `[services.<name>]` — Service Configuration {#services}

Each named section under `services` defines a process in your app. See [Services](/services) for usage patterns.

```toml
[services.web]
command = "node server.js"
port = 3000
port_name = "http"
port_type = "http"

[services.postgres]
image = "postgres:16"
```

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| `command` | string | Command to run | Image's default entrypoint |
| `port` | int | Port the service listens on (single-port shorthand) | `3000` (web only) |
| `port_name` | string | Named port identifier (single-port shorthand) | Service name |
| `port_type` | string | `"http"` or `"tcp"` (single-port shorthand) | `"http"` |
| `ports` | [[port]](#ports) | Multi-port configuration array | — |
| `port_timeout` | duration | Time to wait for the service to bind its port at startup (e.g. `"60s"`, `"2m"`) | `"15s"` |
| `image` | string | Container image to use instead of the app's built image | App's built image |
| `env` | [[env]](#env) | Service-specific environment variables (same schema as global `[[env]]`) | — |
| `concurrency` | [concurrency](#concurrency) | Scaling configuration | See defaults below |
| `disks` | [[disk]](#disks) | Persistent disk attachments | — |

:::note[Validation]
You cannot mix the single-port fields (`port`, `port_name`, `port_type`) with the `ports` array on the same service.
:::

### `[services.<name>.concurrency]` — Scaling {#concurrency}

Controls how many instances of a service run. See [Application Scaling](/scaling) for tuning guidance.

**Default for `web`:** auto mode, 10 requests per instance, 15m scale-down delay, 10s shutdown timeout.

**Default for all other services:** fixed mode, 1 instance, 10s shutdown timeout.

```toml
# Autoscaling
[services.web.concurrency]
mode = "auto"
requests_per_instance = 10
scale_down_delay = "15m"
shutdown_timeout = "10s"

# Fixed instances
[services.worker.concurrency]
mode = "fixed"
num_instances = 2
shutdown_timeout = "10s"
```

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| `mode` | string | `"auto"` or `"fixed"` | `"auto"` for web, `"fixed"` for others |
| `requests_per_instance` | int | Target concurrent requests per instance (auto mode only) | `10` |
| `scale_down_delay` | duration | Time to wait before removing idle instances (auto mode only) | `"15m"` |
| `num_instances` | int | Exact number of instances to run (fixed mode only) | `1` |
| `shutdown_timeout` | duration | Time to wait for graceful shutdown during redeploy | `"10s"` |

:::note[Validation]
- `mode` must be `"auto"` or `"fixed"`.
- In **auto** mode: `requests_per_instance` must be non-negative, `scale_down_delay` must be a valid Go duration, and `num_instances` must not be set.
- In **fixed** mode: `num_instances` must be at least 1, and `requests_per_instance` / `scale_down_delay` must not be set.
- `shutdown_timeout` must be a valid Go duration (e.g. `"10s"`, `"30s"`).
:::

### `[[services.<name>.ports]]` — Ports {#ports}

Configures network ports for a service. Use this when a service needs multiple ports or non-HTTP protocols. See [Traffic Routing](/traffic-routing) for usage patterns and examples.

```toml
[[services.app.ports]]
port = 3000
name = "http"
type = "http"

[[services.app.ports]]
port = 7000
name = "data"
type = "tcp"
node_port = 7000
```

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| `port` | int | Port your process listens on (1–65535). **Required.** | — |
| `name` | string | Unique name for this port. **Required.** | — |
| `type` | string | `"http"` for web traffic, `"tcp"` for raw TCP, `"udp"` for UDP | `"http"` |
| `node_port` | int | Port to expose on the host machine (1–65535) | (none) |

:::note[Validation]
- `port` must be between 1 and 65535.
- `name` is required and must be unique within the service.
- `type` must be `"http"`, `"tcp"`, or `"udp"`.
- Each `(port, type)` pair must be unique within the service (`"tcp"` and `"http"` share the TCP transport, so port 8080 with type `"http"` and port 8080 with type `"tcp"` conflict, but port 53 with `"tcp"` and port 53 with `"udp"` are allowed).
- `node_port` must be between 1 and 65535 and unique across the cluster.
:::

### `[[services.<name>.disks]]` — Persistent Disks {#disks}

Attaches persistent storage to a service. See [Persistent Storage](/disks) for details on local storage, SQLite databases, and Miren Disks. For a SQLite database specifically, the [`miren-sqlite` addon](/addons#sqlite-is-different) is usually the shorter path; declare the disk directly when you need more than one database or want to target specific services.

```toml
# Local storage (simple, node-local)
[[services.web.disks]]
name = "data"
provider = "local"
mount_path = "/miren/data/local"

# Miren Disk (cloud-synced, experimental)
[[services.db.disks]]
name = "pgdata"
mount_path = "/var/lib/postgresql/data"
size_gb = 20
filesystem = "ext4"

# SQLite database, backed up continuously (experimental)
[[services.api.disks]]
name = "state"
provider = "sqlite"
mount_path = "/data"
```

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| `name` | string | Unique disk name. **Required.** | — |
| `provider` | string | `"miren"` for cloud-synced disks, `"local"` for node-local storage, `"sqlite"` for a continuously backed-up SQLite database | `"miren"` |
| `mount_path` | string | Mount point inside the container. **Required.** | — |
| `size_gb` | int | Disk size in gigabytes (required for new miren disks, ignored for local and sqlite) | — |
| `filesystem` | string | `"ext4"`, `"xfs"`, or `"btrfs"` (miren disks only) | `"ext4"` |
| `read_only` | bool | Mount as read-only (not supported for sqlite disks) | `false` |
| `id` | string | Which database a sqlite disk attaches to, scoped to the app (sqlite disks only) | `"default"` |
| `db_file` | string | Database filename inside the mounted directory — a filename, not a path (sqlite disks only) | `"data.db"` |
| `lease_timeout` | duration | How long to wait when acquiring the exclusive disk lease (miren disks only) | — |
| `owner` | string | Ownership for a writable miren disk: empty makes it writable by the run user, `"keep"` opts out, `"uid"`/`"uid:gid"` pins an owner | — |

:::note[Validation]
- `name` and `mount_path` are required.
- `provider` must be `"miren"` (default), `"local"`, or `"sqlite"`.
- For miren disks: `filesystem` must be `ext4`, `xfs`, or `btrfs`; `size_gb` must be non-negative.
- For sqlite disks: `size_gb`, `filesystem`, `lease_timeout` and `read_only` are not supported; `id` must be alphanumeric (`.`, `_` and `-` allowed after the first character); `db_file` must be a filename with no path separators.
- Miren and sqlite disks require `mode = "fixed"` and `num_instances = 1`.
- `lease_timeout` must be a valid Go duration (e.g. `"30s"`, `"2m"`).
:::

By default a writable miren disk is chowned to the user your container runs as,
so a non-root image can write to it without a `chown` shim. Read-only mounts and
containers that run as root are left untouched. See
[Disks](/disks#configuring-disks) for the ownership rules and the one-time
migration pass on large existing disks.

## `[tasks.<name>]` — Task Configuration {#tasks}

A **task** is a command the platform knows how to run. A **service** is a
process the platform keeps up. Migrations, backfills, cleanup jobs, and one-off
batch work are tasks.

Each run of a task gets a fresh sandbox built from the app's deployed image and
config, runs one command, records an exit code, and is torn down.

```toml
# Runs once per deploy, before the new version takes traffic
[tasks.migrate]
command = "bin/rails db:migrate"
trigger = "deploy"
timeout = "10m"

# Runs on a schedule
[tasks.cleanup]
command = "bin/cleanup-sessions"
trigger = "schedule"
every = "6h"
retries = 2

# Runs only when someone asks
[tasks.reindex]
command = "bin/reindex"
timeout = "4h"
max_concurrent = 4

[[tasks.reindex.env]]
key = "BATCH_SIZE"
value = "500"
```

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| `command` | string | The command to run. An invoke can override it. | Required |
| `trigger` | string | What starts the task: `deploy`, `schedule`, or `manual` | `manual` |
| `every` | duration | Run this often. Sugar over `schedule` — see below. | — |
| `schedule` | string | Calendar expression to fire on, in systemd `OnCalendar` syntax | — |
| `timeout` | duration | Kill the run after this long and mark it timed out. `0` means unbounded. | Platform default |
| `retries` | int | Retry budget for `deploy` and `schedule` triggers | `0` |
| `max_concurrent` | int | Cap on simultaneous runs of this task | `1` |
| `[[tasks.<name>.env]]` | table[] | Environment variables for this task only. Same fields as [`[[env]]`](#env). | — |

A task always runs in the app's image and takes no `image` or `disks` of its
own. It reaches everything the app's addons expose, since those arrive as
environment variables.

### Triggers {#triggers}

| `trigger` | Starts when |
|-----------|-------------|
| `deploy` | A new version is deployed, before it takes traffic. The deploy fails if the task does. |
| `schedule` | A calendar expression comes due |
| `manual` | Only when someone asks |

Any task can be invoked by hand whatever its trigger — that's how you re-run a
migration without redeploying.

### Scheduling {#scheduling}

Two ways to say when, and they're mutually exclusive. One is required when
`trigger = "schedule"`.

**`every`** takes a duration:

```toml
every = "6h"     # 00:00, 06:00, 12:00, 18:00 UTC
every = "30m"    # on the hour and the half hour
every = "24h"    # midnight UTC
```

Intervals are anchored to midnight UTC, not to your deploy. A daily job fires
daily no matter how often you ship.

That anchoring is why `every` only accepts durations that divide a day evenly:
values under an hour must divide 60 minutes, and values of an hour or more must
be a whole number of hours dividing 24. `every = "7h"` is rejected at build time
because there's no honest day-aligned reading of it — use `schedule` for
anything that doesn't tile.

**`schedule`** takes a systemd `OnCalendar` expression, which reads left to
right:

```toml
schedule = "Mon *-*-* 09:00:00"   # Mondays at 9am
schedule = "*-*-01 00:00:00"      # the first of every month
schedule = "Mon..Fri 18:00"       # weekdays at 6pm
schedule = "daily"                # also: hourly, weekly, monthly
```

Every numeric field accepts `*`, a value, a comma list (`0,15,30,45`), a range
(`09..17`), and a repetition (`00/6`). Everything is UTC. Cron syntax is not
accepted; `man systemd.time` documents the full grammar.

:::note[Validation]
- `command` is required.
- `trigger` must be `deploy`, `schedule`, or `manual`.
- `every` and `schedule` are mutually exclusive, and exactly one is required when `trigger = "schedule"`. Setting either without that trigger is an error rather than a silent no-op.
- `every` must divide a day evenly.
- `timeout` must be a valid Go duration and must not be negative.
- `retries` must be non-negative, and is rejected on a `manual` task — the caller decides whether to retry.
- `max_concurrent` must be at least 1.
:::

## `[addons.<name>]` — Addons {#addons}

Configures managed backing services. The `<name>` is the addon identifier (e.g. `miren-postgresql`). See [Addons](/addons) for a full guide.

When you deploy, Miren provisions declared addons and injects connection credentials as environment variables before starting your app.

```toml
[addons.miren-postgresql]
variant = "small"
version = "16"
```

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| `variant` | string | Addon variant (e.g. `small`, `shared`) | Addon's default variant |
| `version` | string | Software version tag, or a full image reference if it contains `:` | Addon's default version |

Run `miren addon variants <addon-name>` to see available variants and `miren addon list-available` to see default versions.

Addons removed from app.toml are automatically deprovisioned on the next deploy.

## `[aliases]` — CLI Aliases {#aliases}

Defines custom shortcuts for frequently-used CLI commands. When you run `miren <alias>`, it expands to the full command before execution.

```toml
[aliases]
console = "app run bin/rails console"
tail = "logs app -f"
```

With the above configuration:

- `miren console` expands to `miren app run bin/rails console`
- `miren tail` expands to `miren logs app -f`

Alias names can contain multiple words, which lets you create command namespaces:

```toml
[aliases]
"x tail" = "logs app -f"
"x console" = "app run bin/rails console"
```

Then `miren x tail` and `miren x console` work as shortcuts.

Any extra arguments you pass after the alias name are appended to the expanded command.

:::note[Validation]
- Each word in the alias name must start with a lowercase letter and contain only lowercase letters, numbers, dashes, and underscores.
- The command string must not be empty.
- Alias names must not shadow built-in commands (e.g. you cannot define an alias named `version` or `app list`).
- Aliases are expanded only once — an alias cannot reference another alias.
:::

## Duration Format

Fields marked as `duration` accept Go duration strings: a sequence of decimal numbers with unit suffixes. Valid units are `s` (seconds), `m` (minutes), `h` (hours).

Examples: `"10s"`, `"2m"`, `"1h30m"`, `"15m"`.
