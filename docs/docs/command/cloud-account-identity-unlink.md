---
title: "miren cloud account identity unlink"
sidebar_label: "cloud account identity unlink"
description: "Unlink a sign-in identity (cannot remove the last one)"
---

# miren cloud account identity unlink

Unlink a sign-in identity (cannot remove the last one)

## Usage

```bash
miren cloud account identity unlink [flags]
```

## Flags

- `--force, -f` — Skip confirmation of this destructive operation
- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--provider` — Sign-in provider (choices: `github`, `google`)
- `--provider-id` — Provider's account ID from identity list (not a Cloud user XID)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren cloud account identity`](./cloud-account-identity.md)
