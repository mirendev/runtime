---
title: "miren cloud app logs"
sidebar_label: "cloud app logs"
description: "Fetch bounded live app or build logs through Miren Cloud"
---

# miren cloud app logs

Fetch bounded live app or build logs through Miren Cloud

## Usage

```bash
miren cloud app logs [flags]
```

## Flags

- `--app` — Application name
- `--cluster` — Cloud cluster XID (not a local config name)
- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--kind` — Log kind: app or build (default: `app`)
- `--limit` — Maximum entries (cloud default 500, maximum 2000) (default: `0`)
- `--organization, -o` — Organization name or org- ID (defaults to your only organization)
- `--service` — Service to filter
- `--since` — Lookback duration (e.g. 30m, 2h)
- `--version` — Build version name (e.g. v3)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren cloud app`](./cloud-app.md)
