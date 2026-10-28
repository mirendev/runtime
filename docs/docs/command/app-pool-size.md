---
title: "miren app pool-size"
sidebar_label: "app pool-size"
description: "Show or set a service's active sandbox pool size"
---

# miren app pool-size

Show or set a service's active sandbox pool size

Changes the active pool's desired size without rebuilding or deploying. For autoscaled services this is temporary: the autoscaler may change it again. Set fixed concurrency in app.toml and deploy for a persistent size.

## Usage

```bash
miren app pool-size [size] [flags]
```

## Arguments

- `size` — Desired instance count (omit to show current size)

## Flags

- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json
- `--service, -s` — Service to scale (default: `web`)

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

**Keep three web instances ready:**

```bash
miren app pool-size 3
```

## See also

- [`miren app`](./app.md)
