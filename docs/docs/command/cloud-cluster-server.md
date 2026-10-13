---
title: "miren cloud cluster server"
sidebar_label: "cloud cluster server"
description: "Show server lifecycle status and supported actions"
---

# miren cloud cluster server

Show server lifecycle status and supported actions

## Usage

```bash
miren cloud cluster server [flags]
```

## Flags

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

- [`miren cloud cluster`](./cloud-cluster.md)
