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
- `--identity, -i` — Name for this identity in config (default: `cloud`)
- `--key-name, -k` — Name for the authentication key (default: `miren-cli`)
- `--no-save` — Don't save credentials to config file
- `--persistent-key` — Register a persistent key instead of the default renewable token login
- `--url, -u` — Cloud URL (default: `https://miren.cloud`)

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

## Subcommands

- [`miren cloud login start`](./cloud-login-start.md) — Begin a login without waiting for it; finish with 'login wait'
- [`miren cloud login status`](./cloud-login-status.md) — Show whether each cloud identity is signed in (exits 1 when none is, 2 when none could be checked)
- [`miren cloud login wait`](./cloud-login-wait.md) — Finish a login begun with 'login start' (exits 75 while still awaiting approval)

## See also

- [`miren cloud`](./cloud.md)
