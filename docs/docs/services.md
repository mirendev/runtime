---
sidebar_position: 6
---

# Services

A Miren app can run multiple **services**—separate processes that work together as part of the same application. Each service can have its own command, image, environment variables, and scaling configuration.

## What is a Service?

A service is a named process within your app. Common patterns include:

- **web**: Your main HTTP server (receives external traffic)
- **worker**: Background job processor
- **postgres**: A database running alongside your app

Services share the same deployment lifecycle—when you deploy your app, all services are updated together. But each service scales independently and can run different code.

## Defining Services

Services are configured in your `.miren/app.toml` file. There are two main approaches:

1. **Same image, different commands** — Run different entrypoints from your built container
2. **Different images** — Pull separate container images for each service

### Same Image, Different Commands

The most common pattern is running multiple processes from the same codebase. Define a command for each service:

```toml
name = "myapp"

[services.web]
command = "npm start"

[services.worker]
command = "npm run worker"
```

Both services use your app's built image. The `web` service runs your HTTP server, while `worker` runs a background processor.

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

### Different Images

For services that need entirely different software—like a database—specify an `image`:

```toml
name = "myapp"

[services.web]
command = "npm start"

[services.postgres]
image = "postgres:16"

[services.postgres.concurrency]
mode = "fixed"
num_instances = 1

[[services.postgres.disks]]
name = "postgres-data"
mount_path = "/var/lib/postgresql/data"
size_gb = 20
```

When you specify an `image`, Miren pulls that container image instead of using your app's built image. This lets you run standard database images alongside your application code.

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

[services.postgres.concurrency]
mode = "fixed"
num_instances = 1

[[services.postgres.disks]]
name = "pg-data"
mount_path = "/var/lib/postgresql/data"
size_gb = 50

# Redis cache
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
| `command` | Command to run | Image's default entrypoint |
| `image` | Container image to use | App's built image |
| `port` | Port the web service listens on | 3000 (web only) |
| `env` | Service-specific environment variables | (none) |
| `concurrency` | Scaling configuration | See [Scaling](/scaling) |
| `disks` | Persistent disk attachments | (none) |

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

[services.postgres.concurrency]
mode = "fixed"
num_instances = 1

[services.redis]
image = "redis:7-alpine"

[services.redis.concurrency]
mode = "fixed"
num_instances = 1
```

Your web service connects to postgres at `postgres.app.miren:5432` and redis at `redis.app.miren:6379`. Database images listen on their standard ports automatically—you don't need to configure ports for them in Miren.

## HTTP Routing

Only the `web` service receives external HTTP traffic. When you create a route to your app, requests go to the web service:

```bash
# Creates route to the web service
miren route add myapp.example.com --app myapp
```

Other services (workers, databases) are internal—they can't be reached from outside your app.

The web service defaults to port 3000. Override it if your app listens elsewhere:

```toml
[services.web]
command = "gunicorn app:app --bind 0.0.0.0:8000"
port = 8000
```

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

Services can attach persistent disks. This is required for databases and other stateful workloads:

```toml
[services.postgres]
image = "postgres:16"

[services.postgres.concurrency]
mode = "fixed"
num_instances = 1

[[services.postgres.disks]]
name = "postgres-data"
mount_path = "/var/lib/postgresql/data"
size_gb = 20
```

Disks require fixed mode with exactly 1 instance because only one process can mount a disk at a time.

## Sandbox Pools

When you deploy an app, Miren creates a **sandbox pool** for each service. The pool manages the desired number of instances (sandboxes) for that service.

The hierarchy is:
- **App** → has one active deployment (version)
- **Sandbox Pool** → one per service, manages instance count
- **Sandbox** → individual running container

### Inspecting What's Running

Use these commands to drill down from apps to running instances:

```bash
# List all apps and their current versions
miren app list
```

```
NAME          VERSION                              DEPLOYED  COMMIT
demo          demo-vCVkjR6u7744AsMebwMjGU          1d ago    5f4dd55
conference    conference-vCVkjJSe4fydvxEHfhsKfA    1d ago    5f4dd55
```

```bash
# List sandbox pools (one per service per version)
miren sandbox-pool list
```

```
ID                          VERSION                              SERVICE  DESIRED  CURRENT  READY
pool-CVkjTGJhRddyZDVq9CmnN  demo-vCVkjR6u7744AsMebwMjGU          web      1        1        1
pool-CVkjMv2R2VwcLdHJUoGKD  conference-vCVkjJSe4fydvxEHfhsKfA    web      3        3        3
pool-CVmuoeQCzjoNN9hGsu14c  conference-vCVkjJSe4fydvxEHfhsKfA    worker   2        2        2
```

```bash
# List individual sandboxes (instances)
miren sandbox list
```

```
ID                                SERVICE  POOL                        ADDRESS        STATUS
demo-web-CVok1wptmHEsJ6DmTRy7g    web      pool-CVkjTGJhRddyZDVq9CmnN  10.8.32.9/24   running
conference-web-CVnbNhSjUbGEAC5L   web      pool-CVkjMv2R2VwcLdHJUoGKD  10.8.32.12/24  running
conference-web-CVnbNhVDNcqapDcX   web      pool-CVkjMv2R2VwcLdHJUoGKD  10.8.32.19/24  running
```

```bash
# View logs for a specific sandbox
miren logs -s demo-web-CVok1wptmHEsJ6DmTRy7g
```

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

[services.postgres.concurrency]
mode = "fixed"
num_instances = 1

[[services.postgres.disks]]
name = "pgdata"
mount_path = "/var/lib/postgresql/data"
size_gb = 10
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

[services.postgres.concurrency]
mode = "fixed"
num_instances = 1

[[services.postgres.disks]]
name = "pgdata"
mount_path = "/var/lib/postgresql/data"
size_gb = 10
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

- [Application Scaling](/scaling) — Configure how each service scales
- [Getting Started](/getting-started) — Deploy your first app
- [CLI Reference](/cli-reference) — All available commands
