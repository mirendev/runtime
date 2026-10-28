---
title: "miren org remove-member"
sidebar_label: "org remove-member"
description: "Remove an organization member (admin only)"
---

# miren org remove-member

Remove an organization member (admin only)

## Usage

```bash
miren org remove-member [user] [flags]
```

## Arguments

- `user` — Cloud user XID

## Flags

- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--organization, -o` — Organization name or org- ID (defaults to your only organization)
- `--yes, -y` — Confirm this destructive operation without prompting

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren org`](./org.md)
