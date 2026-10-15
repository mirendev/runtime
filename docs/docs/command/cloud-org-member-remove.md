---
title: "miren cloud org member remove"
sidebar_label: "cloud org member remove"
description: "Remove an organization member (admin only)"
---

# miren cloud org member remove

Remove an organization member (admin only)

## Usage

```bash
miren cloud org member remove [flags]
```

## Flags

- `--force, -f` — Skip confirmation of this destructive operation
- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--organization, -o` — Organization name or org- ID (defaults to your only organization)
- `--user` — Cloud user XID

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren cloud org member`](./cloud-org-member.md)
