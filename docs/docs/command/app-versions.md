---
title: "miren app versions"
sidebar_label: "app versions"
description: "List app versions with status"
---

# miren app versions

List app versions with status

## Usage

```bash
miren app versions [flags]
```

## Flags

- `--ephemeral` — Show only ephemeral versions
- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json
- `--limit, -n` — Max versions to show (default: `20`)

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

**List all versions:**

```bash
miren app versions
```

**List only ephemeral versions:**

```bash
miren app versions --ephemeral
```

## See also

- [`miren app`](/command/app)
