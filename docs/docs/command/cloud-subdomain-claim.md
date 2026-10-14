---
title: "miren cloud subdomain claim"
sidebar_label: "cloud subdomain claim"
description: "Claim a subdomain for an organization (admin only)"
---

# miren cloud subdomain claim

Claim a subdomain for an organization (admin only)

## Usage

```bash
miren cloud subdomain claim [flags]
```

## Flags

- `--base-domain` — Base domain (defaults to Cloud's first enabled domain)
- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--name` — Subdomain name to claim
- `--organization, -o` — Organization name or org- ID (defaults to your only organization)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren cloud subdomain`](./cloud-subdomain.md)
