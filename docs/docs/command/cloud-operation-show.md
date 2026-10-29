---
title: "miren cloud operation show"
sidebar_label: "cloud operation show"
description: "Show a server operation's progress and outcome"
---

# miren cloud operation show

Show a server operation's progress and outcome

## Usage

```bash
miren cloud operation show [operation] [flags]
```

## Arguments

- `operation` — Server operation ID

## Flags

- `--cluster, -C` — Cloud cluster XID
- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--organization, -o` — Organization name or org- ID (defaults to your only organization)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren cloud operation`](./cloud-operation.md)
