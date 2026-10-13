---
title: "miren cloud app deployment"
sidebar_label: "cloud app deployment"
description: "Show a mirrored app deployment"
---

# miren cloud app deployment

Show a mirrored app deployment

## Usage

```bash
miren cloud app deployment [flags]
```

## Flags

- `--app` — Application name
- `--cluster` — Cloud cluster XID (not a local config name)
- `--deployment` — Deployment ID
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
