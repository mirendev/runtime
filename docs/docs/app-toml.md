---
title: app.toml Reference
sidebar_label: app.toml
description: Complete reference for .miren/app.toml — services, scaling, disks, environment variables, and build configuration.
keywords: [app.toml, configuration, reference, services, scaling, environment variables, build]
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
```

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| `key` | string | Variable name. **Required.** | — |
| `value` | string | Variable value | `""` |
| `required` | bool | Fail deploy if value is empty | `false` |
| `sensitive` | bool | Mask value in CLI output and logs | `false` |
| `description` | string | Human-readable explanation of this variable | — |

:::note[Validation]
Every env entry must have a non-empty `key`. If `required` is `true` and `value` is empty at deploy time, the deploy fails.
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

Attaches persistent storage to a service. See [Persistent Storage](/disks) for details on local storage vs. Miren Disks.

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
```

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| `name` | string | Unique disk name. **Required.** | — |
| `provider` | string | `"miren"` for cloud-synced disks, `"local"` for node-local storage | `"miren"` |
| `mount_path` | string | Mount point inside the container. **Required.** | — |
| `size_gb` | int | Disk size in gigabytes (required for new miren disks, ignored for local) | — |
| `filesystem` | string | `"ext4"`, `"xfs"`, or `"btrfs"` (miren disks only) | `"ext4"` |
| `read_only` | bool | Mount as read-only | `false` |
| `lease_timeout` | duration | How long to wait when acquiring the exclusive disk lease (miren disks only) | — |
| `owner` | string | Ownership for a writable miren disk: empty makes it writable by the run user, `"keep"` opts out, `"uid"`/`"uid:gid"` pins an owner | — |

:::note[Validation]
- `name` and `mount_path` are required.
- `provider` must be `"miren"` (default) or `"local"`.
- For miren disks: `filesystem` must be `ext4`, `xfs`, or `btrfs`; `size_gb` must be non-negative; services **must** use `mode = "fixed"` and `num_instances = 1`.
- `lease_timeout` must be a valid Go duration (e.g. `"30s"`, `"2m"`).
:::

By default a writable miren disk is chowned to the user your container runs as,
so a non-root image can write to it without a `chown` shim. Read-only mounts and
containers that run as root are left untouched. See
[Disks](/disks#configuring-disks) for the ownership rules and the one-time
migration pass on large existing disks.

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
