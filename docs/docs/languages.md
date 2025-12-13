---
sidebar_position: 3
---

# Supported Languages

Miren automatically detects your application's language and configures the build environment. When you run `miren deploy`, your project files are analyzed to determine the appropriate build stack.

## Ruby

**Detection:** Presence of `Gemfile`

**Default Version:** 3.2

Miren detects Ruby applications by looking for a `Gemfile`. Dependencies are installed using Bundler with production settings.

### Build Process

1. Installs system dependencies (build-essential, libpq-dev, nodejs, libyaml-dev, postgresql-client)
2. Runs `bundle install` with `BUNDLE_WITHOUT=development`
3. If Bootsnap is detected, precompiles the cache
4. If a Rakefile exists, runs `rake assets:precompile` (if available)

### Entrypoint Detection

Miren automatically detects and configures the appropriate web server:

| Framework | Entrypoint |
|-----------|------------|
| Rails | `bundle exec rails server -b 0.0.0.0 -p $PORT` |
| Puma (with config) | `bundle exec puma -C config/puma.rb` |
| Puma (without config) | `bundle exec puma -b tcp://0.0.0.0 -p $PORT` |
| Rack | `bundle exec rackup -p $PORT` |

### Environment Variables

The following environment variables are set automatically:

- `BUNDLE_PATH=/usr/local/bundle`
- `BUNDLE_WITHOUT=development`
- `RACK_ENV=production`
- `RAILS_ENV=production` (for Rails apps)

### Example Procfile

```
# Rails application
web: bundle exec rails server -b 0.0.0.0 -p $PORT

# Puma with config file
web: bundle exec puma -C config/puma.rb

# Sidekiq background worker
worker: bundle exec sidekiq
```

---

## Python

**Detection:** Presence of `requirements.txt`, `Pipfile`, `pyproject.toml`, or `uv.lock`

**Default Version:** 3.11

Miren supports four Python dependency management systems, detected in priority order.

### Dependency Management

| File | Package Manager | Install Command |
|------|-----------------|-----------------|
| `Pipfile` | pipenv | `pipenv install --deploy` |
| `uv.lock` | uv | `uv sync --frozen` |
| `pyproject.toml` | poetry | `poetry install --no-root` |
| `requirements.txt` | pip | `pip install -r requirements.txt` |

### Framework Detection

Miren automatically detects popular Python web frameworks and configures the start command:

| Framework | Detection | Start Command |
|-----------|-----------|---------------|
| FastAPI | `fastapi` in dependencies | `fastapi run` |
| Django | `django` in dependencies | `gunicorn` or `uvicorn` |
| Flask | `flask` in dependencies | `gunicorn` |
| Gunicorn | `gunicorn` in dependencies | `gunicorn` |
| Uvicorn | `uvicorn` in dependencies | `uvicorn` |

### Example Procfile

```
# pip with gunicorn
web: gunicorn app:app --bind 0.0.0.0:$PORT

# pip with uvicorn (FastAPI/Starlette)
web: uvicorn main:app --host 0.0.0.0 --port $PORT

# FastAPI (auto-detected)
web: fastapi run

# uv
web: uv run gunicorn app:app --bind 0.0.0.0:$PORT

# Pipenv
web: pipenv run gunicorn app:app --bind 0.0.0.0:$PORT

# Poetry
web: poetry run gunicorn app:app --bind 0.0.0.0:$PORT

# Celery worker
worker: celery -A tasks worker --loglevel=info
```

---

## Node.js

**Detection:** `package.json` AND (`package-lock.json` OR `yarn.lock` OR Procfile with `web: node|npm|yarn`)

**Default Version:** 20

Miren detects Node.js applications and automatically uses the appropriate package manager.

### Package Manager Detection

| Lock File | Package Manager | Install Command |
|-----------|-----------------|-----------------|
| `yarn.lock` | yarn | `yarn install` |
| `package-lock.json` | npm | `npm install` |

### Example Procfile

```
# Direct node execution
web: node server.js

# Using npm scripts
web: npm start

# Using yarn
web: yarn start

# Express.js
web: node dist/index.js

# Next.js
web: npm run start

# Background worker
worker: node worker.js
```

---

## Bun

**Detection:** `package.json` AND (`bun.lock` OR Procfile with `web: bun`)

**Default Version:** 1

Miren detects Bun applications by the presence of `bun.lock` or a Bun command in the Procfile.

### Example Procfile

```
# Run TypeScript directly
web: bun run src/index.ts

# Run JavaScript
web: bun run src/index.js

# Using bun scripts from package.json
web: bun run start

# Elysia framework
web: bun run src/server.ts

# Background worker
worker: bun run worker.ts
```

---

## Go

**Detection:** Presence of `go.mod`

**Default Version:** Parsed from `go.mod`, or 1.23

Miren builds Go applications to a single binary at `/bin/app`.

### Build Process

1. Installs git and ca-certificates (for private dependencies)
2. Downloads modules (or uses vendor directory if present)
3. Builds the binary to `/bin/app`

### Command Directory Detection

Miren looks for your main package in the `cmd/` directory:

1. If `cmd/` contains a single subdirectory, that's used
2. If `cmd/` contains a subdirectory matching the app name, that's used
3. Otherwise, builds from the project root

### Vendored Dependencies

If your project has a `vendor/` directory, Miren uses `-mod=vendor` for faster builds.

### Example Procfile

```
# Run the compiled binary
web: /bin/app

# With flags
web: /bin/app -addr=0.0.0.0:$PORT

# Background worker
worker: /bin/app -mode=worker

# Scheduler
scheduler: /bin/app -mode=scheduler
```

---

## Rust

**Detection:** Presence of `Cargo.toml`

**Default Version:** 1.83

Miren builds Rust applications using Cargo and produces a single binary at `/bin/app`.

### Build Process

1. Uses the official Rust base image
2. Runs `cargo build --release`
3. Copies the binary to `/bin/app`

### Binary Name Detection

Miren reads your `Cargo.toml` to determine the binary name:

1. Uses the `[[bin]]` name if specified
2. Falls back to the package name from `[package]`

### Example Procfile

```
# Run the compiled binary
web: /bin/app

# With environment-based port
web: /bin/app

# Background worker
worker: /bin/app --mode worker
```

### Example Cargo.toml

```toml
[package]
name = "myapp"
version = "0.1.0"
edition = "2021"

[dependencies]
axum = "0.7"
tokio = { version = "1", features = ["full"] }
```

---

## Using Dockerfile.miren

For applications that require custom build steps or don't fit the automatic detection, you can provide a `Dockerfile.miren` in your project root.

### When to Use Dockerfile.miren

- Your application requires custom system dependencies
- You need a multi-stage build
- You're using a language not listed above
- You need specific build-time configurations

### Example

```dockerfile
FROM node:20-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM node:20-alpine
WORKDIR /app
COPY --from=builder /app/dist ./dist
COPY --from=builder /app/node_modules ./node_modules
EXPOSE 3000
CMD ["node", "dist/index.js"]
```

### Build Priority

1. `build.dockerfile` setting in `app.toml` (if specified)
2. `Dockerfile.miren` in project root
3. Automatic language detection

### Build Arguments

The following build arguments are available in your Dockerfile.miren:

- `MIREN_VERSION` - The version identifier for this build

---

## Specifying Language Version

Override the default language version in `.miren/app.toml`:

```toml
[build]
version = "3.12"  # e.g., Python 3.12
```

For Go, the version is automatically parsed from your `go.mod` file.

---

## Build Customization

The `[build]` section in `.miren/app.toml` supports additional options:

```toml
[build]
version = "3.2"           # Language/runtime version
dockerfile = "Dockerfile" # Use a specific Dockerfile
onbuild = [              # Commands to run after dependencies install
  "npm run build",
  "npm prune --production"
]
```

The `onbuild` commands run in the `/app` directory after the main build steps complete.

---

## Next Steps

- [Services](/services) - Configure multiple processes
- [Scaling](/scaling) - Set up autoscaling
- [Getting Started](/getting-started) - Deploy your first app
