---
title: "miren cloud login start"
sidebar_label: "cloud login start"
description: "Begin a login without waiting for it; finish with 'login wait'"
---

# miren cloud login start

Begin a login without waiting for it; finish with 'login wait'

## Usage

```bash
miren cloud login start [flags]
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

- [`miren cloud login`](./cloud-login.md)
