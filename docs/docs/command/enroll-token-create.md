---
title: "miren enroll-token create"
sidebar_label: "enroll-token create"
description: "Mint a cluster enrollment credential and bootstrap config (admin only)"
---

# miren enroll-token create

Mint a cluster enrollment credential and bootstrap config (admin only)

## Usage

```bash
miren enroll-token create [flags]
```

## Flags

- `--default-tags` — Default cluster tags as a JSON object
- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--organization, -o` — Organization name or org- ID (defaults to your only organization)
- `--ttl-hours` — Token lifetime in hours (Cloud default 24, maximum 168) (default: `0`)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren enroll-token`](./enroll-token.md)
