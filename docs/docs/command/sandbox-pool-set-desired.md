---
title: "miren sandbox-pool set-desired"
sidebar_label: "sandbox-pool set-desired"
description: "Deprecated: use 'miren debug sandbox-pool set-desired'"
---

# miren sandbox-pool set-desired

Deprecated: use 'miren debug sandbox-pool set-desired'

## Usage

```bash
miren sandbox-pool set-desired <poolid> <desired> [flags]
```

## Arguments

- `poolid` — Pool ID (e.g., pool-CUSkT8J58BmgkDeGyPP2e or pool/pool-CUSkT8J58BmgkDeGyPP2e)
- `desired` — Desired instance count (absolute number, +N to increase, or -N to decrease)

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--raw-id` — Use the provided ID as-is without adding the pool/ prefix

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Scale a pool to 3 instances:**

```bash
miren debug sandbox-pool set-desired web 3
```

## See also

- [`miren sandbox-pool`](./sandbox-pool.md)
