---
title: "miren cloud org user-search"
sidebar_label: "cloud org user-search"
description: "Search existing organization members for group membership"
---

# miren cloud org user-search

Search existing organization members for group membership

## Usage

```bash
miren cloud org user-search [flags]
```

## Flags

- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--organization, -o` — Organization name or org- ID (defaults to your only organization)
- `--query` — User name or email to search for

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren cloud org`](./cloud-org.md)
