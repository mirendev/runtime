---
title: "miren app history"
sidebar_label: "app history"
description: "Show deployment history for an application"
---

# miren app history

Show deployment history for an application

## Usage

```bash
miren app history [flags]
```

## Flags

- `--all` — Show all deployments (ignore limit)
- `--detailed` — Show all columns including git information
- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json
- `--limit, -n` — Maximum number of deployments to show (default: `10`)

## Config Options

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file

## App Options

- `--app, -a` — Application name
- `--dir, -d` — Directory to run from (default: `.`)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Show deployment history:**

```bash
miren app history
```

**Show detailed history with git info:**

```bash
miren app history --detailed
```

## See also

- [`miren app`](/command/app)
