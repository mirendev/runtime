---
title: "miren app status"
sidebar_label: "app status"
description: "Show current status of an application"
---

# miren app status

Show current status of an application

## Usage

```bash
miren app status [flags]
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

**Show status for the current app:**

```bash
miren app status
```

**Show status for a specific app:**

```bash
miren app status -a myapp
```

## See also

- [`miren app`](/command/app)
