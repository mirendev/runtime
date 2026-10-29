---
title: "miren cloud cluster cloud-init"
sidebar_label: "cloud cluster cloud-init"
description: "Generate cloud-init user-data to install and enroll a new cluster (admin only)"
---

# miren cloud cluster cloud-init

Generate cloud-init user-data to install and enroll a new cluster (admin only)

## Usage

```bash
miren cloud cluster cloud-init [flags]
```

## Flags

- `--default-tags` — Default cluster tags as a JSON object
- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--organization, -o` — Organization name or org- ID (defaults to your only organization)
- `--ttl-hours` — Token lifetime in hours (Cloud default 24, maximum 168) (default: `0`)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren cloud cluster`](./cloud-cluster.md)
