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

- [`miren app attach`](/command/app-attach) — Attach to a running task
- [`miren app delete`](/command/app-delete) — Delete an application and all its resources
- [`miren app history`](/command/app-history) — Show deployment history for an application
- [`miren app list`](/command/app-list) — List all applications
- [`miren app restart`](/command/app-restart) — Restart an application
- [`miren app run`](/command/app-run) — Open interactive shell in a new sandbox
- [`miren app set-workload-role`](/command/app-set-workload-role) — Set the API role for an app's sandbox identity tokens
- [`miren app runs`](/command/app-runs) — List recent task runs
- [`miren app status`](/command/app-status) — Show current status of an application
- [`miren app versions`](/command/app-versions) — List app versions with status
