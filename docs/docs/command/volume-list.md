---
title: "miren volume list"
sidebar_label: "volume list"
description: "List cloud volumes and active leases"
---

# miren volume list

List cloud volumes and active leases

## Usage

```bash
miren volume list [flags]
```

## Flags

- `--cursor` — Cursor from the previous response
- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--limit` — Page size (Cloud default 50, range 1-100) (default: `0`)
- `--organization, -o` — Organization name or org- ID (defaults to your only organization)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren volume`](./volume.md)
