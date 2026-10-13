---
title: "miren cloud group delete"
sidebar_label: "cloud group delete"
description: "Delete a non-default access group (admin only)"
---

# miren cloud group delete

Delete a non-default access group (admin only)

## Usage

```bash
miren cloud group delete [flags]
```

## Flags

- `--force, -f` — Skip confirmation of this destructive operation
- `--format` — Output format (text, json) (default: `text`)
- `--group` — Access group XID
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren cloud group`](./cloud-group.md)
