---
title: "miren logs system"
sidebar_label: "logs system"
description: "View system logs"
---

# miren logs system

View system logs

## Usage

```bash
miren logs system <component> [flags]
```

## Arguments

- `component` — System component to filter by (e.g., 'etcd', 'scheduler')

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--follow, -f` — Follow log output (live tail)
- `--format` — Output format (text, json) (default: `text`)
- `--grep, -g` — Filter logs (e.g., 'error', '"exact phrase"', 'error -debug', '/regex/')
- `--json` — Shorthand for --format json
- `--last, -l` — Show logs from the last duration
- `--since` — Show logs since a time (RFC3339, '2006-01-02 15:04', or a duration like '2h' ago)
- `--until` — Show logs until a time (RFC3339, '2006-01-02 15:04', or a duration like '30m' ago); not valid with --follow

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**View all system logs:**

```bash
miren logs system
```

**View logs for a specific component:**

```bash
miren logs system etcd
```

**Follow system logs:**

```bash
miren logs system -f
```

## See also

- [`miren logs`](./logs.md)
