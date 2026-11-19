---
title: "miren cloud login wait"
sidebar_label: "cloud login wait"
description: "Finish a login begun with 'login start' (exits 75 while still awaiting approval)"
---

# miren cloud login wait

Finish a login begun with 'login start' (exits 75 while still awaiting approval)

## Usage

```bash
miren cloud login wait [flags]
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

- [`miren cloud login`](./cloud-login.md)
