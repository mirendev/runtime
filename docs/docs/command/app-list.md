---
title: "miren app list"
sidebar_label: "app list"
description: "List all applications"
---

# miren app list

List all applications

## Usage

```bash
miren app list [flags]
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

**List all apps:**

```bash
miren app list
```

**List apps as JSON:**

```bash
miren app list --format json
```

## See also

- [`miren app`](./app.md)
