---
title: "miren runner operations abandon"
sidebar_label: "runner operations abandon"
description: "Give up on an unfinished operation"
---

# miren runner operations abandon

Give up on an unfinished operation

## Usage

```bash
miren runner operations abandon [id] [flags]
```

## Arguments

- `id` — Operation id

## Flags

- `--dir` — Operation directory (default: `/var/lib/miren/runner/lifecycle`)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Abandon a stuck operation:**

```bash
sudo miren runner operations abandon 01J8X2M0QK4V6Z9W1N3RB5T7YC
```

## See also

- [`miren runner operations`](./runner-operations.md)
