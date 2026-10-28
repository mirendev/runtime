---
title: "miren account key revoke"
sidebar_label: "account key revoke"
description: "Revoke a Cloud authentication key without removing local credentials"
---

# miren account key revoke

Revoke a Cloud authentication key without removing local credentials

## Usage

```bash
miren account key revoke [key] [flags]
```

## Arguments

- `key` — Cloud authentication key XID from key list

## Flags

- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--yes, -y` — Confirm this destructive operation without prompting

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren account key`](./account-key.md)
