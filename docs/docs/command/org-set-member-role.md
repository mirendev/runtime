---
title: "miren org set-member-role"
sidebar_label: "org set-member-role"
description: "Set an organization member's role (admin only)"
---

# miren org set-member-role

Set an organization member's role (admin only)

## Usage

```bash
miren org set-member-role [user] [flags]
```

## Arguments

- `user` — Cloud user XID

## Flags

- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--organization, -o` — Organization name or org- ID (defaults to your only organization)
- `--role` — Organization role: admin or member (choices: `admin`, `member`)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren org`](./org.md)
