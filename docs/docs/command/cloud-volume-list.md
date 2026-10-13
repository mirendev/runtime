---
title: "miren cloud volume list"
sidebar_label: "cloud volume list"
description: "List cloud volumes and active leases"
---

# miren cloud volume list

List cloud volumes and active leases

## Usage

```bash
miren cloud volume list [flags]
```

## Flags

- `--cursor` — Cursor from the previous response
- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--limit` — Page size (Cloud default 50, range 1-100) (default: `0`)
- `--organization` — Organization XID (from cloud org list)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren cloud volume`](./cloud-volume.md)
