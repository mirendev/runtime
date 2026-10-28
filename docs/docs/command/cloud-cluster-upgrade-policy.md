---
title: "miren cloud cluster upgrade-policy"
sidebar_label: "cloud cluster upgrade-policy"
description: "Show the cluster's automatic-upgrade policy"
---

# miren cloud cluster upgrade-policy

Show the cluster's automatic-upgrade policy

## Usage

```bash
miren cloud cluster upgrade-policy [cluster] [flags]
```

## Arguments

- `cluster` — Cloud cluster XID (not a local config name)

## Flags

- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--organization, -o` — Organization name or org- ID (defaults to your only organization)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren cloud cluster`](./cloud-cluster.md)
