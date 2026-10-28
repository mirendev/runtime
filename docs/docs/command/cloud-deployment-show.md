---
title: "miren cloud deployment show"
sidebar_label: "cloud deployment show"
description: "Show a mirrored app deployment"
---

# miren cloud deployment show

Show a mirrored app deployment

## Usage

```bash
miren cloud deployment show [deployment] [flags]
```

## Arguments

- `deployment` — Deployment ID

## Flags

- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--organization, -o` — Organization name or org- ID (defaults to your only organization)

## Config Options

- `--cluster, -C` — Cloud cluster XID (not a local config name)
- `--config` — Path to the config file

## App Options

- `--app, -a` — Application name
- `--dir, -d` — Directory to run from (default: `.`)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren cloud deployment`](./cloud-deployment.md)
