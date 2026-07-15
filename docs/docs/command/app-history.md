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
- `--hide-failed` — Hide failed deployments
- `--json` — Shorthand for --format json
- `--limit, -n` — Maximum number of deployments to show (default: `10`)
- `--status, -s` — Filter by status (active, failed, rolled_back)

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

**Show deployment history:**

```bash
miren app history
```

**Show detailed history with git info:**

```bash
miren app history --detailed
```

**Show only active deployments, limited to 5:**

```bash
miren app history --status active --limit 5
```

## See also

- [`miren app`](/command/app)
