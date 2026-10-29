---
title: "miren cloud app history"
sidebar_label: "cloud app history"
description: "List mirrored app deployment history (latest 50)"
---

# miren cloud app history

List mirrored app deployment history (latest 50)

## Usage

```bash
miren cloud app history [flags]
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
