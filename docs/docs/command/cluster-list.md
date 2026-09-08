---
title: "miren cluster list"
sidebar_label: "cluster list"
description: "List all configured clusters"
---

# miren cluster list

List all configured clusters

## Usage

```bash
miren cluster list [flags]
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

**List all clusters:**

```bash
miren cluster list
```

**List as JSON:**

```bash
miren cluster list --format json
```

## See also

- [`miren cluster`](./cluster.md)
