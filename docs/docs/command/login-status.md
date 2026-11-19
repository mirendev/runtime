---
title: "miren login status"
sidebar_label: "login status"
description: "Alias for 'miren cloud login status'"
---

# miren login status

Alias for 'miren cloud login status'

## Usage

```bash
miren login status [flags]
```

## Flags

- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Check login state as JSON:**

```bash
miren login status --json
```

## See also

- [`miren login`](./login.md)
