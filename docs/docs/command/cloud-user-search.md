---
title: "miren cloud user search"
sidebar_label: "cloud user search"
description: "Find existing Cloud users to add to an organization"
---

# miren cloud user search

Find existing Cloud users to add to an organization

## Usage

```bash
miren cloud user search [flags]
```

## Flags

- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--query` — Name, email or user XID to search across Cloud

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren cloud user`](./cloud-user.md)
