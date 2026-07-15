---
title: "miren cluster current"
sidebar_label: "cluster current"
description: "Show the pinned cluster for this app"
---

# miren cluster current

Show the pinned cluster for this app

## Usage

```bash
miren cluster current [flags]
```

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

**Show current cluster:**

```bash
miren cluster current
```

## See also

- [`miren cluster`](/command/cluster)
