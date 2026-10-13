---
title: "miren cloud invite url"
sidebar_label: "cloud invite url"
description: "Retrieve a copyable invite URL (admin only)"
---

# miren cloud invite url

Retrieve a copyable invite URL (admin only)

## Usage

```bash
miren cloud invite url [flags]
```

## Flags

- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--invite` — Invite link ID
- `--json` — Shorthand for --format json
- `--organization` — Organization XID (from cloud org list)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren cloud invite`](./cloud-invite.md)
