---
title: "miren cloud cluster online-batch"
sidebar_label: "cloud cluster online-batch"
description: "Check connectivity for multiple accessible clusters"
---

# miren cloud cluster online-batch

Check connectivity for multiple accessible clusters

## Usage

```bash
miren cloud cluster online-batch [flags]
```

## Flags

- `--clusters` — Required JSON array of Cloud cluster XIDs; inaccessible clusters are omitted
- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren cloud cluster`](./cloud-cluster.md)
