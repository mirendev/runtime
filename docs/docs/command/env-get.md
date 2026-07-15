---
title: "miren env get"
sidebar_label: "env get"
description: "Get an environment variable value"
---

# miren env get

Get an environment variable value

## Usage

```bash
miren env get <key> [flags]
```

## Arguments

- `key` — Environment variable key to get

## Flags

- `--service, -S` — Get env var for specific service (if not specified, gets global env var)
- `--unmask, -u` — Show actual value of sensitive variables instead of masking them

## Config Options

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file

## App Options

- `--app, -a` — Application name (defaults to .miren/app.toml, then $MIREN_APP)
- `--dir, -d` — Directory to run from (default: `.`)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Get a variable value:**

```bash
miren env get DATABASE_URL
```

**Reveal a sensitive variable:**

```bash
miren env get SECRET_KEY --unmask
```

## See also

- [`miren env`](/command/env)
