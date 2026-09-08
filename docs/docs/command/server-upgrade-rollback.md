---
title: "miren server upgrade rollback"
sidebar_label: "server upgrade rollback"
description: "Rollback server to previous version"
---

# miren server upgrade rollback

Rollback server to previous version

## Usage

```bash
miren server upgrade rollback [flags]
```

## Flags

- `--skip-health` — Skip health check after rollback

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Rollback to the previous version:**

```bash
miren server upgrade rollback
```

## See also

- [`miren server upgrade`](./server-upgrade.md)
