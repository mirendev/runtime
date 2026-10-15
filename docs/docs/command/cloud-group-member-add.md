---
title: "miren cloud group member add"
sidebar_label: "cloud group member add"
description: "Add a user to an access group (admin only)"
---

# miren cloud group member add

Add a user to an access group (admin only)

## Usage

```bash
miren cloud group member add [flags]
```

## Flags

- `--format` — Output format (text, json) (default: `text`)
- `--group` — Cloud group XID
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--user` — Cloud user XID (must already belong to the organization)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren cloud group member`](./cloud-group-member.md)
