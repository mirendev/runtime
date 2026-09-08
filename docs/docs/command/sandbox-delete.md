---
title: "miren sandbox delete"
sidebar_label: "sandbox delete"
description: "Delete a dead sandbox"
---

# miren sandbox delete

Delete a dead sandbox

## Usage

```bash
miren sandbox delete <sandboxid> [flags]
```

## Arguments

- `sandboxid` — ID of the sandbox to delete

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--force, -f` — Force delete without confirmation

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Delete a sandbox:**

```bash
miren sandbox delete sb_abc123
```

**Force delete without confirmation:**

```bash
miren sandbox delete sb_abc123 --force
```

## See also

- [`miren sandbox`](./sandbox.md)
