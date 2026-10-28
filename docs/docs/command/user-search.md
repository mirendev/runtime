---
title: "miren user search"
sidebar_label: "user search"
description: "Find existing Cloud users to add to an organization"
---

# miren user search

Find existing Cloud users to add to an organization

## Usage

```bash
miren user search [query] [flags]
```

## Arguments

- `query` — Name, email or user XID to search across Cloud

## Flags

- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren user`](./user.md)
