---
title: "miren addon variants"
sidebar_label: "addon variants"
description: "Show variants for an addon"
---

# miren addon variants

Show variants for an addon

## Usage

```bash
miren addon variants <addon> [flags]
```

## Arguments

- `addon` — Addon name (e.g., miren-postgresql)

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

**Show variants for PostgreSQL:**

```bash
miren addon variants miren-postgresql
```

## See also

- [`miren addon`](./addon.md)
