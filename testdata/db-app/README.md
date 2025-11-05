# db-app

Test application for demonstrating persistent service pools with PostgreSQL.

## Overview

This app demonstrates:
- Custom service images (postgres:15)
- Fixed-mode concurrency (persistent singleton service)
- Environment variable propagation from app.toml
- Manual service wiring workflow (since cross-container networking isn't first-class yet)

## Setup

### 1. Deploy the app

```bash
cd testdata/db-app
miren deploy --app db-app
```

This will create a persistent PostgreSQL service running in fixed mode.

### 2. Find the PostgreSQL service IP

```bash
miren sandbox list
```

Look for the sandbox running the `postgres` service and note its IP address (e.g., `10.8.29.5`).

### 3. Configure the database connection

Set the `DATABASE_URL` environment variable with the discovered IP:

```bash
miren env set --app db-app --env DATABASE_URL="postgres://postgres@<POSTGRES_IP>:5432/postgres?sslmode=disable"
```

Replace `<POSTGRES_IP>` with the actual IP from step 2.

Alternatively, you can edit `.miren/app.toml` directly:

```toml
[[env]]
key = "DATABASE_URL"
value = "postgres://postgres@10.8.29.5:5432/postgres?sslmode=disable"
```

Then redeploy:

```bash
miren deploy --app db-app
```

## Testing Pool Persistence

After initial deployment, the PostgreSQL sandbox pool remains persistent:

```bash
# Check the pool
miren sandbox-pool list

# Deploy a new version (the pool is reused!)
miren deploy --app db-app

# Pool should still show the same ID
miren sandbox-pool list
```

The persistent pool means the PostgreSQL service stays running across deployments, avoiding cold starts.

## Notes

- The `POSTGRES_HOST_AUTH_METHOD=trust` environment variable is required for the postgres container to work without password authentication
- Currently, cross-container service discovery/DNS isn't first-class, so you need to manually look up and configure service IPs
- The PostgreSQL service runs in fixed mode with `num_instances = 1`, ensuring a single persistent instance
