---
title: "miren cloud invite revoke"
sidebar_label: "cloud invite revoke"
description: "Revoke a shared organization invite link (admin only)"
---

# miren cloud invite revoke

Revoke a shared organization invite link (admin only)

## Usage

```bash
miren cloud invite revoke [flags]
```

## Flags

- `--force, -f` — Skip confirmation of this destructive operation
- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--invite` — Invite link ID
- `--json` — Shorthand for --format json
- `--organization, -o` — Organization name or org- ID (defaults to your only organization)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren cloud invite`](./cloud-invite.md)
