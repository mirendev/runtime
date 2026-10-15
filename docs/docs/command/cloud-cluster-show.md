---
title: "miren cloud cluster show"
sidebar_label: "cloud cluster show"
description: "Show cloud cluster metadata and mirrored status"
---

# miren cloud cluster show

Show cloud cluster metadata and mirrored status

## Usage

```bash
miren cloud cluster show [flags]
```

## Flags

- `--cluster` — Cloud cluster XID (not a local config name)
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
