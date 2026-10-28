---
title: "miren group add-member"
sidebar_label: "group add-member"
description: "Add a user to an access group (admin only)"
---

# miren group add-member

Add a user to an access group (admin only)

## Usage

```bash
miren group add-member [user] [flags]
```

## Arguments

- `user` — Cloud user XID (must already belong to the organization)

## Flags

- `--format` — Output format (text, json) (default: `text`)
- `--group` — Access group XID
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren group`](./group.md)
