---
title: "miren server lifecycle list"
sidebar_label: "server lifecycle list"
description: "List recorded restart and upgrade operations"
---

# miren server lifecycle list

List recorded restart and upgrade operations

## Usage

```bash
miren server lifecycle list [flags]
```

## Flags

- `--dir` — Operation directory (default: `/var/lib/miren/server/lifecycle`)
- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**List operations:**

```bash
miren server lifecycle list
```

## See also

- [`miren server lifecycle`](./server-lifecycle.md)
