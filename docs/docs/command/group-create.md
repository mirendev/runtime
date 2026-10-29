---
title: "miren group create"
sidebar_label: "group create"
description: "Create an access group (admin only)"
---

# miren group create

Create an access group (admin only)

## Usage

```bash
miren group create [name] [flags]
```

## Arguments

- `name` — Access group name

## Flags

- `--description` — Access group description
- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--organization, -o` — Organization name or org- ID (defaults to your only organization)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren group`](./group.md)
