---
title: "miren cloud enroll-token revoke"
sidebar_label: "cloud enroll-token revoke"
description: "Revoke an unused cluster enrollment token (admin only)"
---

# miren cloud enroll-token revoke

Revoke an unused cluster enrollment token (admin only)

## Usage

```bash
miren cloud enroll-token revoke [flags]
```

## Flags

- `--force, -f` — Skip confirmation of this destructive operation
- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--organization` — Organization XID
- `--token` — Enrollment token ID (not its secret)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren cloud enroll-token`](./cloud-enroll-token.md)
