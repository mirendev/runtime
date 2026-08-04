---
title: "miren runner upgrade rollback"
sidebar_label: "runner upgrade rollback"
description: "Rollback runner to previous version"
---

# miren runner upgrade rollback

Rollback runner to previous version

## Usage

```bash
miren runner upgrade rollback [flags]
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
miren runner upgrade rollback
```

## See also

- [`miren runner upgrade`](/command/runner-upgrade)
