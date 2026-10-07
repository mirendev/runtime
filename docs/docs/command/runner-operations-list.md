---
title: "miren runner operations list"
sidebar_label: "runner operations list"
description: "List recorded restart and upgrade operations"
---

# miren runner operations list

List recorded restart and upgrade operations

## Usage

```bash
miren runner operations list [flags]
```

## Flags

- `--dir` — Operation directory (default: `/var/lib/miren/runner/lifecycle`)
- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**List operations:**

```bash
miren runner operations list
```

## See also

- [`miren runner operations`](./runner-operations.md)
