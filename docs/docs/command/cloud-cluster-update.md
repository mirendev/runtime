---
title: "miren cloud cluster update"
sidebar_label: "cloud cluster update"
description: "Update cloud cluster metadata (admin only)"
---

# miren cloud cluster update

Update cloud cluster metadata (admin only)

## Usage

```bash
miren cloud cluster update [flags]
```

## Flags

- `--anywhere-mode` — Miren Anywhere mode (choices: `default`, `never`, `auto`, `always`)
- `--clear-description` — Remove the cluster description
- `--cluster` — Cloud cluster XID (not a local config name)
- `--description` — New description as plain text; omitted preserves it
- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--name` — New cluster name
- `--organization, -o` — Organization name or org- ID (defaults to your only organization)
- `--tags` — Replacement tags as a JSON object

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren cloud cluster`](./cloud-cluster.md)
