---
title: "miren subdomain unassign"
sidebar_label: "subdomain unassign"
description: "Remove a subdomain's cluster assignment (admin only)"
---

# miren subdomain unassign

Remove a subdomain's cluster assignment (admin only)

## Usage

```bash
miren subdomain unassign [subdomain] [flags]
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
