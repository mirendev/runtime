---
title: "miren group delete"
sidebar_label: "group delete"
description: "Delete a non-default access group (admin only)"
---

# miren group delete

Delete a non-default access group (admin only)

## Usage

```bash
miren group delete [group] [flags]
```

## Arguments

- `group` — Access group XID

## Flags

- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--yes, -y` — Confirm this destructive operation without prompting

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren group`](./group.md)
