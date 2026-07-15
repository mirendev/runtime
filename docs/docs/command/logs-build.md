---
title: "miren logs build"
sidebar_label: "logs build"
description: "View build logs"
---

# miren logs build

View build logs

## Usage

```bash
miren logs build <version> [flags]
```

## Arguments

- `version` — Build version (e.g., v3)

## Flags

- `--follow, -f` — Follow log output (live tail)
- `--format` — Output format (text, json) (default: `text`)
- `--grep, -g` — Filter logs (e.g., 'error', '"exact phrase"', 'error -debug', '/regex/')
- `--json` — Shorthand for --format json
- `--last, -l` — Show logs from the last duration
- `--since` — Show logs since a time (RFC3339, '2006-01-02 15:04', or a duration like '2h' ago)
- `--until` — Show logs until a time (RFC3339, '2006-01-02 15:04', or a duration like '30m' ago); not valid with --follow

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

**View build logs for a version:**

```bash
miren logs build v3
```

**View build logs for a specific app:**

```bash
miren logs build v3 -a myapp
```

## See also

- [`miren logs`](/command/logs)
