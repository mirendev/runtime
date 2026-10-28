---
title: "miren org create"
sidebar_label: "org create"
description: "Create an organization and become its admin"
---

# miren org create

Create an organization and become its admin

## Usage

```bash
miren org create [name] [flags]
```

## Arguments

- `name` — New organization name

## Flags

- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren org`](./org.md)
