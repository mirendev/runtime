---
title: "miren disk accelerator status"
sidebar_label: "disk accelerator status"
description: "Show whether accelerator mode can run on this host"
---

# miren disk accelerator status

Show whether accelerator mode can run on this host

## Usage

```bash
miren disk accelerator status [flags]
```

## Flags

- `--data-path` — Path to miren data (default: `/var/lib/miren`)
- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Check accelerator mode:**

```bash
miren disk accelerator status
```

## See also

- [`miren disk accelerator`](/command/disk-accelerator)
