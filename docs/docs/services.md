---
title: Services
description: Run multiple processes in a single app — web servers, workers, databases — each with independent scaling.
keywords: [services, processes, worker, web, background jobs, multi-process]
---

import CliCommand from '@site/src/components/CliCommand';

# Services

A Miren app can run multiple **services**—separate processes that work together as part of the same application. Each service can have its own command, image, environment variables, and scaling configuration.

## Minimum working example

Define one service per process in `.miren/app.toml`:

```toml title=".miren/app.toml"
name = "myapp"

[services.web]
command = "npm start"

[services.worker]
command = "npm run worker"
```

Deploy, and both processes run from your app version's primary image. `web` receives HTTP traffic, `worker` runs alongside it, and each scales independently.

## What is a Service?

A service is a named process within your app. Common patterns include:

- **web**: Your main HTTP server (receives external traffic)
- **worker**: Background job processor
- **postgres**: A database running alongside your app

Services share the same deployment lifecycle—when you deploy your app, all services are updated together. But each service scales independently and can run different code.

## Defining Services

Services can be defined in two ways:

1. **Procfile** — Simple format for defining commands per service
2. **`.miren/app.toml`** — Full configuration with scaling, env vars, and images

### Service Detection

Miren detects services in this order:

1. **`.miren/app.toml`** — Services defined in the `[services.*]` sections
2. **`Procfile`** — Services inferred from Procfile entries
3. **Detected start command** — For an auto-detected language stack (Python, Node, Bun, Go, Ruby, Rust), Miren synthesizes a `web` service from the start command it detects for your framework

If none of these provide a service definition, Miren usually synthesizes a `web` service
for a runnable container image. That service uses the image's `ENTRYPOINT` and `CMD`
unchanged. Set `web = false` to suppress it. An app with tasks but no services must set
`web` explicitly, so Miren does not guess whether it needs a long-running process.

:::note[Custom images keep their startup defaults]
An image built from `Dockerfile.miren` (or a `[build] dockerfile`) can run without an
explicit service. Unless `web = false`, Miren creates a `web` service and uses the image's
`ENTRYPOINT` and `CMD`. Add `[services.web]` when you need to customize how it runs. An app
with tasks but no services must set `web` to `true` or `false` explicitly.
:::

### Using a Procfile

If your app has a `Procfile`, Miren automatically infers services from it:

```text
web: npm start
worker: npm run worker
```

Each line defines a service: the name before the colon, and the command after. This is compatible with Heroku's Procfile format.

For more control (scaling, environment variables, different images), use `.miren/app.toml`:

```toml
[services.web]
command = "npm start"

[services.worker]
command = "npm run worker"

[services.worker.concurrency]
mode = "fixed"
num_instances = 2
```

### Same Image, Different Commands

The most common pattern is running multiple processes from the same codebase. Define a command for each service:

```toml
name = "myapp"

[services.web]
command = "npm start"

[services.worker]
command = "npm run worker"
```

Both services use your app version's primary image. The `web` service runs your HTTP server, while `worker` runs a background processor.

#### Example: Rails with Sidekiq

```toml
name = "railsapp"

[services.web]
command = "bundle exec puma -C config/puma.rb"

[services.worker]
command = "bundle exec sidekiq"

[services.worker.concurrency]
mode = "fixed"
num_instances = 2
```

#### Example: Python with Celery

```toml
name = "djangoapp"

[services.web]
command = "gunicorn myapp.wsgi:application --bind 0.0.0.0:8000"
port = 8000

[services.worker]
command = "celery -A myapp worker --loglevel=info"

[services.beat]
command = "celery -A myapp beat --loglevel=info"
```

### Existing and Different Images

If your web app already exists as a container image, you can deploy it without source detection or a Dockerfile:

```toml
name = "myapp"

[services.web]
image = "ghcr.io/example/myapp:latest"
```

With no other service fields, Miren keeps the image's working directory, `ENTRYPOINT`,
and `CMD`. A single exposed TCP port becomes the web port automatically. Set `port`
when the image exposes no ports or several, and use `args` or `command` only when its
startup defaults need to change.

:::note[Image selection]
Without a Dockerfile, `services.web.image` selects the app's primary image and Miren launches it directly. This explicit image wins over automatic source detection. A `[build].dockerfile` setting or a discovered `Dockerfile.miren` still wins over both.

An image on another service does not override a buildable source stack. This keeps database and cache sidecars from changing how the web app is built. Only when source detection finds no buildable stack can a lone service image become the fallback; with several service images, set `services.web.image` to choose the primary one.
:::

:::tip[Use addons for databases]
If you just need a PostgreSQL database, consider using an [addon](/addons) instead of running it as a service. Addons are fully managed — Miren provisions the database, injects credentials, and handles cleanup. Use a service when you need full control over the database configuration.
:::

For services that need entirely different software—like a database—specify an `image`:

```toml
name = "myapp"

[services.web]
command = "npm start"

[services.postgres]
image = "postgres:16"

[[services.postgres.env]]
key = "PGDATA"
value = "/miren/data/local/pgdata"

[services.postgres.concurrency]
mode = "fixed"
num_instances = 1
```

When you specify an `image` on a sidecar service, Miren pulls that container image instead of using your app version's primary image. This lets you run standard database images alongside your application code.

#### Example: Full Stack with PostgreSQL and Redis

```toml
name = "fullstack"

# Your application code
[services.web]
command = "node server.js"

[services.worker]
command = "node worker.js"

[services.worker.concurrency]
mode = "fixed"
num_instances = 2

# PostgreSQL database
[services.postgres]
image = "postgres:16"

[[services.postgres.env]]
key = "POSTGRES_PASSWORD"
value = "secret"

[[services.postgres.env]]
key = "PGDATA"
value = "/miren/data/local/pgdata"

[services.postgres.concurrency]
mode = "fixed"
num_instances = 1

# Redis cache (data stored in memory, no persistence needed for caching)
[services.redis]
image = "redis:7-alpine"

[services.redis.concurrency]
mode = "fixed"
num_instances = 1
```

## Service Configuration Reference

Each service can configure:

| Option | Description | Default |
|--------|-------------|---------|
| `command` | Shell command that replaces the image startup command (`/bin/sh -c`) | (none) |
| `args` | Exec-form arguments that replace image `CMD` while preserving `ENTRYPOINT`; mutually exclusive with `command` | (none) |
| `image` | Container image to use | App version's primary image |
| `port` | Port the service listens on (single-port shorthand) | For `web` inheriting the primary image: its single exposed TCP port, otherwise 3000 |
| `ports` | Port configuration array (multi-port, see [Traffic Routing](/traffic-routing)) | (none) |
| `port_timeout` | Time to wait for the service to bind its port at startup (e.g. `"60s"`, `"2m"`) | `15s` |
| `env` | Service-specific environment variables | (none) |
| `concurrency` | Scaling configuration | See [Scaling](/scaling) |
| `concurrency.shutdown_timeout` | Time to wait for graceful shutdown during redeploy | `10s` |
| `disks` | Persistent disk attachments (experimental, see [Disks](/disks)) | (none) |

With neither `command` nor `args`, the image's `ENTRYPOINT` and `CMD` run
unchanged. `args` preserves each array element exactly, without shell expansion.

### Environment Variables

Services inherit global environment variables from your app, and can add their own:

```toml
name = "myapp"

# Global env vars - available to all services
[[env]]
key = "LOG_LEVEL"
value = "info"

# Service-specific env vars
[services.web]
command = "npm start"

[[services.web.env]]
key = "NODE_ENV"
value = "production"

[services.worker]
command = "npm run worker"

[[services.worker.env]]
key = "WORKER_CONCURRENCY"
value = "5"
```

## Service Communication

Services within the same app can communicate using internal DNS. Each service is discoverable at `<service>.app.miren`:

```toml
name = "myapp"

[[env]]
key = "DATABASE_URL"
value = "postgres://user:pass@postgres.app.miren:5432/mydb"

[[env]]
key = "REDIS_URL"
value = "redis://redis.app.miren:6379"

[services.web]
command = "npm start"

[services.postgres]
image = "postgres:16"

[[services.postgres.env]]
key = "POSTGRES_PASSWORD"
value = "pass"

[[services.postgres.env]]
key = "PGDATA"
value = "/miren/data/local/pgdata"

[services.postgres.concurrency]
mode = "fixed"
num_instances = 1

[services.redis]
image = "redis:7-alpine"

[services.redis.concurrency]
mode = "fixed"
num_instances = 1
```

Connect to other services using their DNS name and standard port—`postgres.app.miren:5432` for PostgreSQL, `redis.app.miren:6379` for Redis. The container images listen on their standard ports by default; Miren doesn't manage these ports.

## Traffic Routing

The `web` service is the default target for external HTTP traffic through Miren's HTTP ingress. Create a route to make your app reachable:

<CliCommand context="client">
```miren
miren route add myapp.example.com --app myapp
```
</CliCommand>

When `web` does not inherit a single exposed TCP port from the primary image, it
defaults to port 3000. Override it if your app listens elsewhere:

```toml
[services.web]
command = "gunicorn app:app --bind 0.0.0.0:8000"
port = 8000
```

For non-HTTP services (TCP/UDP), you can expose ports directly using the `ports` array and `node_port`. See [Traffic Routing](/traffic-routing) for the full picture — HTTP ingress, L4 routing, multi-port services, and the `PORT` environment variable.

## Service Scaling

Each service scales independently. By default:

- **`web` service**: Autoscales based on traffic (scale-to-zero enabled)
- **All other services**: Fixed at 1 instance

Configure scaling per-service:

```toml
[services.web.concurrency]
mode = "auto"
requests_per_instance = 20
scale_down_delay = "10m"

[services.worker.concurrency]
mode = "fixed"
num_instances = 3
```

For detailed scaling configuration, see [Application Scaling](/scaling).

## Persistent Storage

For stateful services like databases, use [Local Storage](/disks#local-storage)—persistent storage automatically available at `/miren/data/local`. Configure your database to store data there:

```toml
[services.postgres]
image = "postgres:16"

[[services.postgres.env]]
key = "PGDATA"
value = "/miren/data/local/pgdata"

[services.postgres.concurrency]
mode = "fixed"
num_instances = 1
```

The `PGDATA` environment variable tells PostgreSQL where to store its data.

:::warning[PostgreSQL data directory]
Using a subdirectory (`pgdata`) under `/miren/data/local` is required because PostgreSQL expects to own its data directory.
:::

For cloud-synced storage that travels with your app, see [Miren Disks](/disks#miren-disks) (experimental).

## Sandbox Pools

When you deploy an app, Miren creates a **sandbox pool** for each service. The pool manages the desired number of instances (sandboxes) for that service.

The hierarchy is:
- **App** → has one active deployment (version)
- **Sandbox Pool** → one per service, manages instance count
- **Sandbox** → individual running container

### Inspecting What's Running

Use these commands to drill down from apps to running instances:

<CliCommand context="client">
```miren
# List all apps and their current versions
miren app list
```
</CliCommand>

```text
NAME          VERSION                              DEPLOYED  COMMIT
demo          demo-vCVkjR6u7744AsMebwMjGU          1d ago    5f4dd55
conference    conference-vCVkjJSe4fydvxEHfhsKfA    1d ago    5f4dd55
```

<CliCommand context="client">
```miren
# List sandbox pools (one per service per version)
miren sandbox-pool list
```
</CliCommand>

```text
ID                          VERSION                              SERVICE  DESIRED  CURRENT  READY
pool-CVkjTGJhRddyZDVq9CmnN  demo-vCVkjR6u7744AsMebwMjGU          web      1        1        1
pool-CVkjMv2R2VwcLdHJUoGKD  conference-vCVkjJSe4fydvxEHfhsKfA    web      3        3        3
pool-CVmuoeQCzjoNN9hGsu14c  conference-vCVkjJSe4fydvxEHfhsKfA    worker   2        2        2
```

<CliCommand context="client">
```miren
# List individual sandboxes (instances)
miren sandbox list
```
</CliCommand>

```text
ID                                SERVICE  POOL                        ADDRESS        STATUS
demo-web-CVok1wptmHEsJ6DmTRy7g    web      pool-CVkjTGJhRddyZDVq9CmnN  10.8.32.9/24   running
conference-web-CVnbNhSjUbGEAC5L   web      pool-CVkjMv2R2VwcLdHJUoGKD  10.8.32.12/24  running
conference-web-CVnbNhVDNcqapDcX   web      pool-CVkjMv2R2VwcLdHJUoGKD  10.8.32.19/24  running
```

<CliCommand context="client">
```miren
# View logs for a specific sandbox
miren logs -s demo-web-CVok1wptmHEsJ6DmTRy7g
```
</CliCommand>

## Complete Examples

### Node.js API with Worker

```toml
name = "api"

[[env]]
key = "DATABASE_URL"
value = "postgres://user:pass@postgres.app.miren:5432/api"

[services.web]
command = "node dist/server.js"

[services.web.concurrency]
mode = "auto"
requests_per_instance = 50

[services.worker]
command = "node dist/worker.js"

[services.worker.concurrency]
mode = "fixed"
num_instances = 2

[services.postgres]
image = "postgres:16"

[[services.postgres.env]]
key = "POSTGRES_PASSWORD"
value = "pass"

[[services.postgres.env]]
key = "PGDATA"
value = "/miren/data/local/pgdata"

[services.postgres.concurrency]
mode = "fixed"
num_instances = 1
```

### Go Service with PostgreSQL

```toml
name = "goapp"

[[env]]
key = "DATABASE_URL"
value = "postgres://goapp:changeme@postgres.app.miren:5432/goapp"

[services.web]
command = "./server"

[services.web.concurrency]
mode = "auto"
requests_per_instance = 100
scale_down_delay = "5m"

[services.postgres]
image = "postgres:16-alpine"

[[services.postgres.env]]
key = "POSTGRES_USER"
value = "goapp"

[[services.postgres.env]]
key = "POSTGRES_PASSWORD"
value = "changeme"

[[services.postgres.env]]
key = "POSTGRES_DB"
value = "goapp"

[[services.postgres.env]]
key = "PGDATA"
value = "/miren/data/local/pgdata"

[services.postgres.concurrency]
mode = "fixed"
num_instances = 1
```

### Python App with Redis Queue

```toml
name = "taskqueue"

[[env]]
key = "REDIS_URL"
value = "redis://redis.app.miren:6379"

[services.web]
command = "gunicorn app:app --bind 0.0.0.0:8000"
port = 8000

[services.web.concurrency]
mode = "auto"
requests_per_instance = 20

[services.worker]
command = "rq worker --url redis://redis.app.miren:6379"

[services.worker.concurrency]
mode = "fixed"
num_instances = 3

[services.redis]
image = "redis:7-alpine"

[services.redis.concurrency]
mode = "fixed"
num_instances = 1
```

## Next Steps

- [App Configuration](/app-configuration) — Overview of the configuration model
- [app.toml Reference](/app-toml) — Complete field reference for `.miren/app.toml`
- [Traffic Routing](/traffic-routing) — HTTP ingress, TCP/UDP routing, multi-port services
- [Persistent Storage](/disks) — Local storage and disk options for databases
- [Application Scaling](/scaling) — Configure how each service scales
- [Getting Started](/getting-started) — Deploy your first app
