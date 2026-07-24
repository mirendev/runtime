---
title: "miren login"
sidebar_label: "login"
description: "Authenticate with miren.cloud"
---

# miren login

Authenticate with miren.cloud

## Usage

```bash
miren login [flags]
```

## Flags

- `--force, -f` — Overwrite existing identity without prompting
- `--identity, -i` — Name for this identity in config (default: `cloud`)
- `--key-name, -k` — Name for the authentication key (default: `miren-cli`)
- `--no-save` — Don't save credentials to config file
- `--persistent-key` — Register a persistent key instead of the default ephemeral token login
- `--url, -u` — Cloud URL (default: `https://miren.cloud`)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Login:**

```bash
miren login
```

**Login to a specific cloud instance:**

```bash
miren login --url https://cloud.example.com
```
