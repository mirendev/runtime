---
title: "miren env delete"
sidebar_label: "env delete"
description: "Delete environment variables"
---

# miren env delete

Delete environment variables

Deleting an environment variable creates a new app version and rolls it out automatically — you do not need to run `miren deploy` or `miren app restart` afterward. The new version reuses your existing container image (no rebuild), and the command waits for it to become healthy before returning.

:::warning[Deploy app.toml changes first]
`miren env delete` builds the new version by copying the *current server-side* spec, not your local `app.toml`. If you have pending `app.toml` changes, deploy them first, then delete the stale variable — otherwise the delete rolls out the server-side spec and your local edits won't be included. Variables declared in `app.toml` drop automatically when you remove them from the file and redeploy.
:::

## Usage

```bash
miren env delete [args...] [flags]
```

## Flags

- `--force, -f` — Skip confirmation prompt
- `--service, -S` — Delete env var from specific service only (if not specified, deletes global env var)

## Config Options

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file

## App Options

- `--app, -a` — Application name
- `--dir, -d` — Directory to run from (default: `.`)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Delete a variable:**

```bash
miren env delete DATABASE_URL
```

**Delete without confirmation:**

```bash
miren env delete DATABASE_URL --force
```

**Delete a service-specific variable:**

```bash
miren env delete WORKERS --service worker
```

## See also

- [`miren env`](/command/env)
