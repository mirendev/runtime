---
title: "miren cloud app deployments"
sidebar_label: "cloud app deployments"
description: "List mirrored app deployment history (latest 50)"
---

# miren cloud app deployments

List mirrored app deployment history (latest 50)

## Usage

```bash
miren cloud app deployments [flags]
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
