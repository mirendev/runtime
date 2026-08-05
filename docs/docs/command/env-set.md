---
title: "miren env set"
sidebar_label: "env set"
description: "Set environment variables for an application"
---

# miren env set

Set environment variables for an application

Setting an environment variable creates a new app version and rolls it out automatically — you do not need to run `miren deploy` or `miren app restart` afterward. The new version reuses your existing container image (no rebuild); Miren boots new sandboxes with the updated environment and drains the old ones. The command waits for the new version to become healthy before returning.

Use `-e` for plain values and `-s` for sensitive values (masked in output and logs). Note that `-s` affects display only — the value itself is stored in the clear. For a credential that should be encrypted at rest, store it with `miren secret set` instead. Pass `--service` to scope the change to a single service instead of all services.

:::note[No restart needed]
Environment variable changes take effect on their own. Running `miren app restart` afterward only triggers a redundant second rollout.
:::

## Usage

```bash
miren env set [flags]
```

## Flags

- `--env, -e` — Set environment variables (use KEY to prompt, KEY=VALUE to set directly, KEY=@file to read from file)
- `--sensitive, -s` — Set sensitive environment variables (use KEY to prompt with masking, KEY=VALUE to set directly, KEY=@file to read from file)
- `--service, -S` — Set env var for specific service only (if not specified, sets for all services)

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

**Set an environment variable:**

```bash
miren env set -e DATABASE_URL=postgres://localhost/mydb
```

**Set a sensitive variable (prompted with masking):**

```bash
miren env set -s SECRET_KEY
```

**Set a variable from a file:**

```bash
miren env set -e CONFIG=@config.json
```

**Set a variable for a specific service:**

```bash
miren env set -e WORKERS=4 --service worker
```

## See also

- [`miren env`](/command/env)
