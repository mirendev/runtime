---
title: App Configuration
description: How Miren detects and configures your app automatically, and when to customize with app.toml.
keywords: [configuration, app.toml, convention over configuration, environment variables, miren init]
---

import CliCommand from '@site/src/components/CliCommand';

# App Configuration

Miren uses a **convention over configuration** approach. Most apps deploy with zero configuration—Miren detects your language, builds your image, and runs it with sensible defaults. When you need to customize, you add a `.miren/app.toml` file.

## When You Don't Need app.toml

If your app is a single web service with a standard language stack, Miren handles everything:

- **Language and build**: Detected from your project files (`package.json`, `go.mod`, `Gemfile`, etc.) — see [Language Guides](/guides)
- **Start command**: Detected from your framework or `Procfile`
- **Scaling**: Web services autoscale based on traffic by default

You can deploy with just:

<CliCommand context="client">

```miren
miren init
miren deploy
```

</CliCommand>

## What `miren init` Does for You

`miren init` does more than scaffold a config file. It scans your project for the environment variables your app actually needs to boot, splits them into things it can handle for you and things it can't, and stages whatever it can find.

### Detection

For each supported stack (Python, Node.js, Bun, Go, Ruby, Rust), `miren init`:

- Reads your manifest (`Gemfile`, `package.json`, `pyproject.toml`, `Cargo.toml`, `go.mod`) to map known libraries to the env vars they typically expect — `pg` → `DATABASE_URL`, `@sentry/node` → `SENTRY_DSN`, and so on.
- Greps your source code for direct env reads (`ENV['X']`, `process.env.X`, `os.Getenv("X")`, `std::env::var("X")`, `Bun.env.X`) and notes whether each one has a fallback.
- Parses any `.env.sample` / `.env.example` files in the repo as a declaration of what's expected.
- Recognizes framework-specific names like `RAILS_ENV`, `NODE_ENV`, `RUST_LOG`, and `RAILS_MASTER_KEY`.

Each detected variable gets a confidence: **required**, **recommended**, or **optional**. A direct source reference without a fallback is required. Library-based guesses are recommended unless the source confirms them, in which case they're elevated. Variables with a default-valued fallback in code (`process.env.X ?? "..."`, `cmp.Or(os.Getenv("X"), "...")`) are optional.

### Staging

For required variables, `miren init` tries to handle them automatically:

- **Has a sensible default** (e.g. `RAILS_ENV=production`) → written to `app.toml` so it's visible.
- **Can be generated** (e.g. Rails `SECRET_KEY_BASE`) → a cryptographically random value is generated and pre-set on the app, the same as if you'd run `miren config set` yourself.
- **Can be read from a local file** (e.g. `RAILS_MASTER_KEY` from `config/master.key` or `config/credentials/production.key`) → read from disk and pre-set on the app, again the same as `miren config set`.
- **Anything else** → listed as "must be configured manually" with `miren config set`.

Pre-set values are picked up by your first `miren deploy` automatically, so generated secrets and read-in keys are present from the very first build without an extra step.

Sensitive variables marked as such (whether by detection or because the key looks like a secret) are masked in CLI output and never written to `app.toml` in plaintext.

## When You Need app.toml

Create `.miren/app.toml` when you need to:

- **Run multiple services** — web server plus workers, databases, or caches
- **Set environment variables** — configuration your app reads at runtime
- **Tune scaling** — adjust concurrency thresholds or use fixed instance counts
- **Attach persistent disks** — for databases or file storage
- **Customize builds** — specify a Dockerfile, language version, or extra build steps
- **Configure addons** — managed databases and other backing services (see [Addons](/addons))

## Configuration Sections

Here's how the sections of `app.toml` map to your application's lifecycle:

### Build

The `[build]` section controls how Miren builds your container image. Override the detected language version, point to a custom Dockerfile, or add post-build steps.

```toml
[build]
version = "3.12"
onbuild = ["npm run build"]
```

See [Language Guides](/guides) for build details per language.

### Services

The `[services.<name>]` sections define the processes your app runs. Each service gets its own command, scaling configuration, and optionally its own container image.

```toml
[services.web]
command = "node server.js"

[services.worker]
command = "node worker.js"
```

See [Services](/services) for patterns like running databases alongside your app.

### Scaling

Each service has a `[services.<name>.concurrency]` section that controls how it scales. Web services default to autoscaling; everything else defaults to a single fixed instance.

```toml
[services.web.concurrency]
mode = "auto"
requests_per_instance = 20

[services.worker.concurrency]
mode = "fixed"
num_instances = 3
```

See [Application Scaling](/scaling) for tuning guidance.

### Persistent Storage

Services can attach disks for data that needs to survive restarts. Disks use exclusive leasing and require fixed concurrency with a single instance.

```toml
[services.db.concurrency]
mode = "fixed"
num_instances = 1

[[services.db.disks]]
name = "postgres-data"
mount_path = "/var/lib/postgresql/data"
size_gb = 20
```

See [Persistent Storage](/disks) for local shared storage and Miren Disks.

### Environment Variables

Environment variables are declared with `[[env]]` at the top level (available to all services) or `[[services.<name>.env]]` for a specific service. Service-level env vars are merged with global ones.

```toml
# Available to all services
[[env]]
key = "DATABASE_URL"
value = "postgres://db.app.miren:5432/myapp"

# Only for the worker service
[[services.worker.env]]
key = "WORKER_CONCURRENCY"
value = "5"
```

#### Env Var Metadata

Each env var supports optional metadata fields for documentation and validation:

```toml
[[env]]
key = "API_KEY"
value = ""
required = true
sensitive = true
description = "Third-party API key for payment processing"
```

| Field | Type | Description |
|-------|------|-------------|
| `key` | string | Variable name (required) |
| `value` | string | Variable value |
| `required` | bool | If `true`, deploy will fail when this variable has no value |
| `sensitive` | bool | If `true`, the value is masked in CLI output and logs |
| `description` | string | Human-readable explanation of what this variable is for |

The `required` flag is useful for variables whose values differ per environment—declare them in `app.toml` with an empty value and `required = true`, then set the actual value with `miren env set` before deploying. The `sensitive` flag ensures secrets aren't accidentally exposed in terminal output.

#### Variables Miren Injects

Miren injects these automatically. You don't declare them, and your app can read them to find out what it is:

| Variable | Value | Injected |
|----------|-------|----------|
| `MIREN_RUNTIME_APP` | The app name | Every sandbox |
| `MIREN_RUNTIME_VERSION` | The deployed version, e.g. `v1` | Every sandbox |
| `MIREN_RUNTIME_INSTANCE_NUM` | This instance's number, starting at `0`. See [Scaling](/scaling). | Instance-backed sandboxes only |

Sandboxes also get `PORT` (the port your web service should listen on) and the [workload identity](/workload-identity) variables.

:::info[Injected vs. CLI variables]
`MIREN_RUNTIME_*` is the namespace Miren injects **into** your app; the `MIREN_IDENTITY_*` [workload identity](/workload-identity) variables are injected too. The other `MIREN_*` variables — such as `MIREN_APP`, `MIREN_CLUSTER`, and `MIREN_CONFIG` — are input **to** the `miren` CLI, used to pick what a command acts on (see [CI/CD Deployment](/ci-deploy)). Keeping the injected and input variables separate means running `miren` inside a sandbox still resolves the app from your `.miren/app.toml`.
:::

:::warning[The MIREN_ prefix is reserved]
You can't set your own variables under `MIREN_` — `miren env set MIREN_FOO=bar` fails with `cannot set MIREN_ environment variables`. Miren owns the whole namespace so it can add injected variables without colliding with yours. Name your own `APP_*` or anything else.
:::

:::info[Deprecated aliases: MIREN_APP, MIREN_VERSION, MIREN_INSTANCE_NUM]
These vars were previously injected under the un-prefixed names `MIREN_APP`, `MIREN_VERSION`, and `MIREN_INSTANCE_NUM`. Those names are still injected as deprecated aliases alongside the `MIREN_RUNTIME_*` names above, so apps reading the old names keep working. They will be removed in a future release — read the `MIREN_RUNTIME_*` names instead.
:::

### Traffic Routing

For HTTP services, Miren handles routing automatically. For non-HTTP services (TCP/UDP), you can expose ports directly using the `ports` array:

```toml
[services.irc]
command = "./ircd"

[[services.irc.ports]]
port = 6667
name = "irc"
type = "tcp"
node_port = 6667
```

See [Traffic Routing](/traffic-routing) for the full picture — HTTP ingress, TCP/UDP routing, multi-port services, and the `PORT` environment variable.

## Complete Example

```toml
name = "myapp"

[[env]]
key = "DATABASE_URL"
value = "postgres://user:pass@postgres.app.miren:5432/myapp"

[[env]]
key = "SECRET_KEY"
required = true
sensitive = true
description = "Application secret for session signing"

[build]
version = "3.12"

[services.web]
command = "gunicorn app:app --bind 0.0.0.0:8000"
port = 8000

[services.web.concurrency]
mode = "auto"
requests_per_instance = 20

[services.worker]
command = "celery -A app worker"

[services.worker.concurrency]
mode = "fixed"
num_instances = 2

[services.postgres]
image = "postgres:16"

[[services.postgres.env]]
key = "PGDATA"
value = "/var/lib/postgresql/data/pgdata"

[services.postgres.concurrency]
mode = "fixed"
num_instances = 1

[[services.postgres.disks]]
name = "myapp-pgdata"
mount_path = "/var/lib/postgresql/data"
size_gb = 20
```

## Reference

For a complete field-by-field listing of every `app.toml` option, see the [app.toml Reference](/app-toml).
