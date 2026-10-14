---
title: "miren cloud enroll-token list"
sidebar_label: "cloud enroll-token list"
description: "List enrollment token status without secrets"
---

# miren cloud enroll-token list

List enrollment token status without secrets

## Usage

```bash
miren cloud enroll-token list [flags]
```

## Flags

- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--limit` — Maximum tokens (Cloud default 50, cap 200) (default: `0`)
- `--offset` — Number of tokens to skip (default: `0`)
- `--organization, -o` — Organization name or org- ID (defaults to your only organization)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren cloud enroll-token`](./cloud-enroll-token.md)
