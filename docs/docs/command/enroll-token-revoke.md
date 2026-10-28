---
title: "miren enroll-token revoke"
sidebar_label: "enroll-token revoke"
description: "Revoke an unused cluster enrollment token (admin only)"
---

# miren enroll-token revoke

Revoke an unused cluster enrollment token (admin only)

## Usage

```bash
miren enroll-token revoke [token] [flags]
```

## Arguments

- `token` — Enrollment token ID (not its secret)

## Flags

- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--organization, -o` — Organization name or org- ID (defaults to your only organization)
- `--yes, -y` — Confirm this destructive operation without prompting

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren enroll-token`](./enroll-token.md)
