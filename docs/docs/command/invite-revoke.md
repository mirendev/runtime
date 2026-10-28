---
title: "miren invite revoke"
sidebar_label: "invite revoke"
description: "Revoke a shared organization invite link (admin only)"
---

# miren invite revoke

Revoke a shared organization invite link (admin only)

## Usage

```bash
miren invite revoke [invite] [flags]
```

## Arguments

- `invite` — Invite link ID

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

- [`miren invite`](./invite.md)
