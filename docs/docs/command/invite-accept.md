---
title: "miren invite accept"
sidebar_label: "invite accept"
description: "Join an organization using an invitation"
---

# miren invite accept

Join an organization using an invitation

## Usage

```bash
miren invite accept [token] [flags]
```

## Arguments

- `token` — Credential token from the invitation URL (not an invite link ID)

## Flags

- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren invite`](./invite.md)
