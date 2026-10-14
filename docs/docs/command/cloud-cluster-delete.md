---
title: "miren cloud cluster delete"
sidebar_label: "cloud cluster delete"
description: "Delete a Cloud cluster record (admin only); leaves server registration files unchanged"
---

# miren cloud cluster delete

Delete a Cloud cluster record (admin only); leaves server registration files unchanged

## Usage

```bash
miren cloud cluster delete [flags]
```

## Flags

- `--cluster` — Cloud cluster XID
- `--force, -f` — Skip confirmation of this destructive operation
- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--organization, -o` — Organization name or org- ID (defaults to your only organization)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren cloud cluster`](./cloud-cluster.md)
