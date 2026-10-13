---
title: "miren cloud group create"
sidebar_label: "cloud group create"
description: "Create an access group (admin only)"
---

# miren cloud group create

Create an access group (admin only)

## Usage

```bash
miren cloud group create [flags]
```

## Flags

- `--description` — Access group description
- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--name` — Access group name
- `--organization` — Organization XID (from cloud org list)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren cloud group`](./cloud-group.md)
