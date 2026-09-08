---
title: "miren addon list-available"
sidebar_label: "addon list-available"
description: "List available addons"
---

# miren addon list-available

List available addons

## Usage

```bash
miren addon list-available [flags]
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

**List available addons:**

```bash
miren addon list-available
```

## See also

- [`miren addon`](./addon.md)
