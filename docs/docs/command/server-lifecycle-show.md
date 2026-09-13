---
title: "miren server lifecycle show"
sidebar_label: "server lifecycle show"
description: "Show one restart or upgrade operation"
---

# miren server lifecycle show

Show one restart or upgrade operation

## Usage

```bash
miren server lifecycle show <id> [flags]
```

## Arguments

- `id` — Operation id

## Flags

- `--dir` — Operation directory (default: `/var/lib/miren/server/lifecycle`)
- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Show an operation:**

```bash
miren server lifecycle show 01J8X2M0QK4V6Z9W1N3RB5T7YC
```

## See also

- [`miren server lifecycle`](./server-lifecycle.md)
