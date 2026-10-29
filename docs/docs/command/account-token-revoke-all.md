---
title: "miren account token revoke-all"
sidebar_label: "account token revoke-all"
description: "Revoke all previously issued tokens for your account"
---

# miren account token revoke-all

Revoke all previously issued tokens for your account

## Usage

```bash
miren account token revoke-all [flags]
```

## Flags

- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--reason` — Optional revocation reason
- `--yes, -y` — Confirm this destructive operation without prompting

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren account token`](./account-token.md)
