---
title: "miren subdomain assign"
sidebar_label: "subdomain assign"
description: "Assign a subdomain to a cluster (admin only)"
---

# miren subdomain assign

Assign a subdomain to a cluster (admin only)

## Usage

```bash
miren subdomain assign [subdomain] [flags]
```

## Arguments

- `subdomain` — Claimed subdomain ID

## Flags

- `--cluster, -C` — Cloud cluster XID to serve this subdomain
- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--organization, -o` — Organization name or org- ID (defaults to your only organization)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren subdomain`](./subdomain.md)
