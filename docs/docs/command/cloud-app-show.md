---
title: "miren cloud app show"
sidebar_label: "cloud app show"
description: "Show mirrored app details, deployments and health"
---

# miren cloud app show

Show mirrored app details, deployments and health

## Usage

```bash
miren cloud app show [flags]
```

## Flags

- `--app, -a` — Application name
- `--cluster, -C` — Cloud cluster XID (not a local config name)
- `--config` — Path to the config file
- `--dir, -d` — Directory to find app configuration in (default: `.`)
- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--organization, -o` — Organization name or org- ID (defaults to your only organization)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren cloud app`](./cloud-app.md)
