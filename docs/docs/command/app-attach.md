---
title: "miren app attach"
sidebar_label: "app attach"
description: "Attach to a running task"
---

# miren app attach

Attach to a running task

Joins your terminal to a task run that is already going.

Attaching and detaching are invisible to the run itself: it is started by the platform and outlives any client. So you can attach to a run started with `--detach`, reattach after a dropped connection, or never attach at all.

Several clients can attach to one run at once; they share the same terminal.

:::note
Only runs whose command is still executing can be attached. For one that has finished, read its output back with `miren logs run`.
:::

## Usage

```bash
miren app attach <run> [flags]
```

## Arguments

- `run` — Run to attach to

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

**Rejoin a run's terminal:**

```bash
miren app attach run/myapp-session-4kq2np
```

## See also

- [`miren app`](/command/app)
