---
title: "miren cloud login"
sidebar_label: "cloud login"
description: "Authenticate with miren.cloud"
---

# miren cloud login

Authenticate with miren.cloud

## Usage

```bash
miren cloud login [flags]
```

## Flags

- `--force, -f` — Overwrite existing identity without prompting
- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Name for this identity in config (default: `cloud`)
- `--json` — Shorthand for --format json
- `--key-name, -k` — Name for the authentication key (default: `miren-cli`)
- `--no-save` — Don't save credentials to config file
- `--persistent-key` — Register a persistent key instead of the default renewable token login
- `--url, -u` — Cloud URL (default: `https://miren.cloud`)
- `--wait` — How long to wait for the login to be approved (default 10m at a terminal; without one, check once and exit 75 while it is pending)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Login:**

```bash
miren cloud login
```

**Login to a specific cloud instance:**

```bash
miren cloud login --url https://cloud.example.com
```

**From a script or agent: print the URL, then finish once approved:**

```bash
miren login --json
miren login --wait 50s --json
```

## See also

- [`miren cloud`](./cloud.md)
