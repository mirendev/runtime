---
title: "miren login wait"
sidebar_label: "login wait"
description: "Alias for 'miren cloud login wait'"
---

# miren login wait

Alias for 'miren cloud login wait'

## Usage

```bash
miren login wait [flags]
```

## Flags

- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Identity whose pending login to finish (default: `cloud`)
- `--json` — Shorthand for --format json
- `--timeout` — How long to wait for approval before exiting with status 75 (0 waits until the login expires) (default: `50s`)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Wait up to 50 seconds for approval:**

```bash
miren login wait
```

**Wait until the login expires:**

```bash
miren login wait --timeout 0
```

## See also

- [`miren login`](./login.md)
