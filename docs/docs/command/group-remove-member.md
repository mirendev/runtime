---
title: "miren group remove-member"
sidebar_label: "group remove-member"
description: "Remove a user from an access group (admin only)"
---

# miren group remove-member

Remove a user from an access group (admin only)

## Usage

```bash
miren group remove-member [user] [flags]
```

## Arguments

- `user` — Cloud user XID

## Flags

- `--format` — Output format (text, json) (default: `text`)
- `--group` — Access group XID
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--yes, -y` — Confirm this destructive operation without prompting

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren group`](./group.md)
