---
title: "miren logs run"
sidebar_label: "logs run"
description: "View logs for a task run"
---

# miren logs run

View logs for a task run

## Usage

```bash
miren logs run <run> [flags]
```

## Arguments

- `run` — Run ID

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

- `--app, -a` — Application name
- `--dir, -d` — Directory to run from (default: `.`)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**View a run's output:**

```bash
miren logs run run/myapp-migrate-4kq2np
```

**Follow a running task:**

```bash
miren logs run run/myapp-reindex-8xh1dc -f
```

## See also

- [`miren logs`](/command/logs)
