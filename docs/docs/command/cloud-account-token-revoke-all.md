---
title: "miren cloud account token revoke-all"
sidebar_label: "cloud account token revoke-all"
description: "Revoke all previously issued tokens for your account"
---

# miren cloud account token revoke-all

Revoke all previously issued tokens for your account

## Usage

```bash
miren cloud account token revoke-all [flags]
```

## Flags

- `--force, -f` — Skip confirmation of this destructive operation
- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--reason` — Optional revocation reason

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren cloud account token`](./cloud-account-token.md)
