---
title: "miren cloud subdomain assign"
sidebar_label: "cloud subdomain assign"
description: "Assign a subdomain to a cluster (admin only)"
---

# miren cloud subdomain assign

Assign a subdomain to a cluster (admin only)

## Usage

```bash
miren cloud subdomain assign [flags]
```

## Flags

- `--cluster` — Cloud cluster XID to serve this subdomain
- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--organization` — Organization XID (from cloud org list)
- `--subdomain` — Claimed subdomain ID

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren cloud subdomain`](./cloud-subdomain.md)
