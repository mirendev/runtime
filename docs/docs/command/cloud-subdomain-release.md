---
title: "miren cloud subdomain release"
sidebar_label: "cloud subdomain release"
description: "Release an organization's subdomain claim (admin only)"
---

# miren cloud subdomain release

Release an organization's subdomain claim (admin only)

## Usage

```bash
miren cloud subdomain release [flags]
```

## Flags

- `--force, -f` — Skip confirmation of this destructive operation
- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--organization` — Organization XID
- `--subdomain` — Claimed subdomain ID

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren cloud subdomain`](./cloud-subdomain.md)
