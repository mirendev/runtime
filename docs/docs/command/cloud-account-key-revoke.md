---
title: "miren cloud account key revoke"
sidebar_label: "cloud account key revoke"
description: "Revoke a Cloud authentication key without removing local credentials"
---

# miren cloud account key revoke

Revoke a Cloud authentication key without removing local credentials

## Usage

```bash
miren cloud account key revoke [flags]
```

## Flags

- `--force, -f` — Skip confirmation of this destructive operation
- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--key` — Cloud authentication key XID from key list

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren cloud account key`](./cloud-account-key.md)
