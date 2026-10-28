---
title: "miren org cluster-list"
sidebar_label: "org cluster-list"
description: "List clusters and mirrored status in an organization"
---

# miren org cluster-list

List clusters and mirrored status in an organization

## Usage

```bash
miren org cluster-list [flags]
```

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
