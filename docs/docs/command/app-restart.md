---
title: "miren app restart"
sidebar_label: "app restart"
description: "Restart an application"
---

# miren app restart

Restart an application

Restart stops your app's running sandboxes and lets the pool manager re-create them from the *current* active version. It does not create a new version, change any configuration, or rebuild your image — the app comes back on exactly the spec it was already running.

Use restart to:
- Clear stuck or wedged process state
- Reset the crash-loop cooldown so a crashing app is retried immediately
- Pick up data restored out-of-band (for example, after `miren disk restore`)

:::note[Config and env changes restart on their own]
You do not need to restart after `miren env set`, `miren env delete`, `miren addon create`, or `miren addon destroy`. Each of those already creates a new version and rolls out new sandboxes automatically. A manual restart on top only adds a redundant rollout.
:::

## Usage

```bash
miren app restart [flags]
```

## Flags

- `--service, -s` — Restart only a specific service

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

**Restart the current app:**

```bash
miren app restart
```

**Restart a specific service:**

```bash
miren app restart -s web
```

## See also

- [`miren app`](/command/app)
