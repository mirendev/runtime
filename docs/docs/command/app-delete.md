---
title: "miren app delete"
sidebar_label: "app delete"
description: "Delete an application and all its resources"
---

# miren app delete

Delete an application and all its resources

## Usage

```bash
miren app delete [appname] [flags]
```

## Arguments

- `appname` — Deprecated: use --app instead

## Flags

- `--force, -f` — Deprecated: use --yes to skip confirmation
- `--yes, -y` — Skip confirmation prompt

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

**Delete an app (with confirmation prompt):**

```bash
miren app delete -a myapp
```

**Delete without confirmation:**

```bash
miren app delete -a myapp --yes
```

## See also

- [`miren app`](./app.md)
