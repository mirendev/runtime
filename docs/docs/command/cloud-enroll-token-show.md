---
title: "miren cloud enroll-token show"
sidebar_label: "cloud enroll-token show"
description: "Show enrollment token state and bootstrap progress"
---

# miren cloud enroll-token show

Show enrollment token state and bootstrap progress

## Usage

```bash
miren cloud enroll-token show [flags]
```

## Flags

- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--organization, -o` — Organization name or org- ID (defaults to your only organization)
- `--token` — Enrollment token ID (not its secret)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren cloud enroll-token`](./cloud-enroll-token.md)
