---
title: "miren org member-search"
sidebar_label: "org member-search"
description: "Search existing organization members for group membership"
---

# miren org member-search

Search existing organization members for group membership

## Usage

```bash
miren org member-search [query] [flags]
```

## Arguments

- `query` — User name or email to search for

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
