---
title: "miren enroll-token show"
sidebar_label: "enroll-token show"
description: "Show enrollment token state and bootstrap progress"
---

# miren enroll-token show

Show enrollment token state and bootstrap progress

## Usage

```bash
miren enroll-token show [token] [flags]
```

## Arguments

- `token` — Enrollment token ID (not its secret)

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

- [`miren enroll-token`](./enroll-token.md)
