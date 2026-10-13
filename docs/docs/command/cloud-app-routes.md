---
title: "miren cloud app routes"
sidebar_label: "cloud app routes"
description: "Show mirrored app routes and sync state"
---

# miren cloud app routes

Show mirrored app routes and sync state

## Usage

```bash
miren cloud app routes [flags]
```

## Flags

- `--app` — Application name
- `--cluster` — Cloud cluster XID (not a local config name)
- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--organization` — Organization XID (from cloud org list)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren cloud app`](./cloud-app.md)
