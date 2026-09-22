---
title: "miren server operations abandon"
sidebar_label: "server operations abandon"
description: "Give up on an unfinished operation, including a data restore that keeps the server from starting"
---

# miren server operations abandon

Give up on an unfinished operation, including a data restore that keeps the server from starting

## Usage

```bash
miren server operations abandon [id] [flags]
```

## Arguments

- `id` — Operation id

## Flags

- `--dir` — Operation directory (default: `/var/lib/miren/server/lifecycle`)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Abandon an operation whose executor died:**

```bash
sudo miren server operations abandon 01J8X2M0QK4V6Z9W1N3RB5T7YC
```

## See also

- [`miren server operations`](./server-operations.md)
