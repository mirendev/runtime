---
title: "miren org add-member"
sidebar_label: "org add-member"
description: "Add an existing Cloud user to an organization (admin only)"
---

# miren org add-member

Add an existing Cloud user to an organization (admin only)

## Usage

```bash
miren org add-member [user] [flags]
```

## Arguments

- `user` — Existing user XID from user search (admin only)

## Flags

- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--organization, -o` — Organization name or org- ID (defaults to your only organization)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren org`](./org.md)
