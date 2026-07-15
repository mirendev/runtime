---
title: "miren env delete"
sidebar_label: "env delete"
description: "Delete environment variables"
---

# miren env delete

Delete environment variables

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

- `--app, -a` — Application name (defaults to .miren/app.toml, then $MIREN_APP)
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
