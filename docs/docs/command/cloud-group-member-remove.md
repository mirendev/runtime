---
title: "miren cloud group member remove"
sidebar_label: "cloud group member remove"
description: "Remove a user from an access group (admin only)"
---

# miren cloud group member remove

Remove a user from an access group (admin only)

## Usage

```bash
miren cloud group member remove [flags]
```

## Flags

- `--force, -f` — Skip confirmation of this destructive operation
- `--format` — Output format (text, json) (default: `text`)
- `--group` — Access group XID
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--user` — Cloud user XID

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren cloud group member`](./cloud-group-member.md)
