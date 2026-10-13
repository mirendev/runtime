---
title: "miren cloud group update"
sidebar_label: "cloud group update"
description: "Update an access group, preserving omitted fields (admin only)"
---

# miren cloud group update

Update an access group, preserving omitted fields (admin only)

## Usage

```bash
miren cloud group update [flags]
```

## Flags

- `--clear-description` — Remove the group description
- `--description` — New description; omitted preserves it
- `--format` — Output format (text, json) (default: `text`)
- `--group` — Cloud group XID
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--name` — New group name; omitted preserves it

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren cloud group`](./cloud-group.md)
