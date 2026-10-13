---
title: "miren cloud org member add"
sidebar_label: "cloud org member add"
description: "Add an existing Cloud user to an organization (admin only)"
---

# miren cloud org member add

Add an existing Cloud user to an organization (admin only)

## Usage

```bash
miren cloud org member add [flags]
```

## Flags

- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--organization` — Organization XID (from cloud org list)
- `--user` — Existing user XID from cloud user search (admin only)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren cloud org member`](./cloud-org-member.md)
