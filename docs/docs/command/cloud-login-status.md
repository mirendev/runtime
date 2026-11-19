---
title: "miren cloud login status"
sidebar_label: "cloud login status"
description: "Show whether each cloud identity is signed in (exits 1 when none is, 2 when none could be checked)"
---

# miren cloud login status

Show whether each cloud identity is signed in (exits 1 when none is, 2 when none could be checked)

## Usage

```bash
miren cloud login status [flags]
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

- [`miren cloud login`](./cloud-login.md)
