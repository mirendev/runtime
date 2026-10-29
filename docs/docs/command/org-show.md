---
title: "miren org show"
sidebar_label: "org show"
description: "Show an organization and its members"
---

# miren org show

Show an organization and its members

## Usage

```bash
miren org show [organization] [flags]
```

## Arguments

- `organization` — Organization name or org- ID (defaults to your only organization)

## Flags

- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren org`](./org.md)
