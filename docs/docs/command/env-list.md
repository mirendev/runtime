---
title: "miren env list"
sidebar_label: "env list"
description: "List all environment variables"
---

# miren env list

List all environment variables

## Usage

```bash
miren env list [flags]
```

## Flags

- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json

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

**List all variables:**

```bash
miren env list
```

**List as JSON:**

```bash
miren env list --format json
```

## See also

- [`miren env`](/command/env)
