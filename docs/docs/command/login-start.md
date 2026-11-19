---
title: "miren login start"
sidebar_label: "login start"
description: "Alias for 'miren cloud login start'"
---

# miren login start

Alias for 'miren cloud login start'

## Usage

```bash
miren login start [flags]
```

## Flags

- `--force, -f` — Start a new login even if the identity is still signed in or a login is already pending
- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Name for this identity in config (default: `cloud`)
- `--json` — Shorthand for --format json
- `--key-name, -k` — Name for the authentication key (default: `miren-cli`)
- `--persistent-key` — Register a persistent key instead of the default renewable token login
- `--url, -u` — Cloud URL (default: `https://miren.cloud`)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Start a login and print the URL as JSON:**

```bash
miren login start --json
```

## See also

- [`miren login`](./login.md)
