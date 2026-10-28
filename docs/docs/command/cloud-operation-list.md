---
title: "miren cloud operation list"
sidebar_label: "cloud operation list"
description: "List server restart and upgrade operation history"
---

# miren cloud operation list

List server restart and upgrade operation history

## Usage

```bash
miren cloud operation list [flags]
```

## Flags

- `--cluster, -C` — Cloud cluster XID (not a local config name)
- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--limit` — Maximum operations (Cloud default 50, cap 200) (default: `0`)
- `--organization, -o` — Organization name or org- ID (defaults to your only organization)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren cloud operation`](./cloud-operation.md)
