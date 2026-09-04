---
title: "miren debug disk list-deleted"
sidebar_label: "debug disk list-deleted"
description: "Read the soft-delete holding area directly (break-glass)"
---

# miren debug disk list-deleted

Read the soft-delete holding area directly (break-glass)

## Usage

```bash
miren debug disk list-deleted [flags]
```

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--data-path` — Path to miren data directory (default: `/var/lib/miren`)
- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren debug disk`](/command/debug-disk)
