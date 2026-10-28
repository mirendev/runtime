---
title: "miren subdomain delete"
sidebar_label: "subdomain delete"
description: "Release an organization's subdomain claim (admin only)"
---

# miren subdomain delete

Release an organization's subdomain claim (admin only)

## Usage

```bash
miren subdomain delete [subdomain] [flags]
```

## Arguments

- `subdomain` — Claimed subdomain ID

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

- [`miren subdomain`](./subdomain.md)
