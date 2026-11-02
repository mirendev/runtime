---
title: "miren app"
sidebar_label: "app"
description: "Get information about an application"
---

# miren app

Get information about an application

## Usage

```bash
miren app [flags]
```

## Flags

- `--format` — Output format (text, json) (default: `text`)
- `--graph, -g` — Graph the app stats
- `--json` — Shorthand for --format json
- `--watch, -w` — Watch the app stats

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

**Show app info for the current directory:**

```bash
miren app
```

**Show info for a specific app:**

```bash
miren app -a myapp
```

**Watch app stats in real time:**

```bash
miren app --watch
```

## Subcommands

- [`miren app admin`](./app-admin.md) — Application-defined admin methods
- [`miren app attach`](./app-attach.md) — Deprecated: use 'miren run attach'
- [`miren app delete`](./app-delete.md) — Delete an application and all its resources
- [`miren app history`](./app-history.md) — Show deployment history for an application
- [`miren app init`](./app-init.md) — Initialize a new application
- [`miren app list`](./app-list.md) — List all applications
- [`miren app pool-size`](./app-pool-size.md) — Show or set a service's active sandbox pool size
- [`miren app restart`](./app-restart.md) — Restart an application
- [`miren app rollback`](./app-rollback.md) — Roll back to a previous version
- [`miren app run`](./app-run.md) — Open interactive shell in a new sandbox
- [`miren app runs`](./app-runs.md) — Deprecated: use 'miren run list'
- [`miren app set-workload-role`](./app-set-workload-role.md) — Set the API role for an app's sandbox identity tokens
- [`miren app status`](./app-status.md) — Show current status of an application
- [`miren app top`](./app-top.md) — Show resource usage for an application
- [`miren app versions`](./app-versions.md) — Deprecated: use 'miren app history --versions'
- [`miren app watch`](./app-watch.md) — Watch application health and resource usage
