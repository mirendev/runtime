---
title: "miren app restart"
sidebar_label: "app restart"
description: "Restart an application"
---

# miren app restart

Restart an application

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

- `--app, -a` — Application name (defaults to .miren/app.toml, then $MIREN_APP)
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
