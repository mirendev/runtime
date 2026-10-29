---
title: "miren invite url"
sidebar_label: "invite url"
description: "Retrieve a copyable invite URL (admin only)"
---

# miren invite url

Retrieve a copyable invite URL (admin only)

## Usage

```bash
miren invite url [invite] [flags]
```

## Arguments

- `invite` — Invite link ID

## Flags

- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--organization, -o` — Organization name or org- ID (defaults to your only organization)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren invite`](./invite.md)
