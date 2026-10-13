---
title: "miren cloud cluster apps"
sidebar_label: "cloud cluster apps"
description: "List mirrored apps in a cloud cluster"
---

# miren cloud cluster apps

List mirrored apps in a cloud cluster

## Usage

```bash
miren cloud cluster apps [flags]
```

## Flags

- `--cluster` — Cloud cluster XID
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
