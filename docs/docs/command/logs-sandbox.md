---
title: "miren logs sandbox"
sidebar_label: "logs sandbox"
description: "View sandbox logs"
---

# miren logs sandbox

View sandbox logs

## Usage

```bash
miren logs sandbox <sandboxid> [flags]
```

## Arguments

- `sandboxid` — Sandbox ID

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

**View logs for a sandbox:**

```bash
miren logs sandbox sb_abc123
```

**Follow sandbox logs:**

```bash
miren logs sandbox sb_abc123 -f
```

## See also

- [`miren logs`](./logs.md)
