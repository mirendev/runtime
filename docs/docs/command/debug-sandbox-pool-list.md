---
title: "miren debug sandbox-pool list"
sidebar_label: "debug sandbox-pool list"
description: "List all sandbox pools"
---

# miren debug sandbox-pool list

List all sandbox pools

## Usage

```bash
miren debug sandbox-pool list [flags]
```

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**List all pools:**

```bash
miren debug sandbox-pool list
```

## See also

- [`miren debug sandbox-pool`](./debug-sandbox-pool.md)
