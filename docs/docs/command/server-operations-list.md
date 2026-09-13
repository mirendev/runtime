---
title: "miren server operations list"
sidebar_label: "server operations list"
description: "List recorded restart and upgrade operations"
---

# miren server operations list

List recorded restart and upgrade operations

## Usage

```bash
miren server operations list [flags]
```

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--dir` — Read operation records from this directory instead of asking the server (default: `/var/lib/miren/server/lifecycle`)
- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**List operations:**

```bash
miren server operations list
```

## See also

- [`miren server operations`](./server-operations.md)
