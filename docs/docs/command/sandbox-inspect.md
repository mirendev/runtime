---
title: "miren sandbox inspect"
sidebar_label: "sandbox inspect"
description: "Show one sandbox's resource usage and failure history"
---

# miren sandbox inspect

Show one sandbox's resource usage and failure history

## Usage

```bash
miren sandbox inspect <sandbox> [flags]
```

## Arguments

- `sandbox` — ID or short ID of the sandbox to inspect

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json
- `--series` — Include CPU and memory history
- `--since` — Measure over this window, e.g. 30s, 5m, 1h (default 1m)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Inspect a sandbox:**

```bash
miren sandbox inspect sb_abc123
```

**Inspect over the last hour:**

```bash
miren sandbox inspect sb_abc123 --since 1h
```

**Include CPU and memory history:**

```bash
miren sandbox inspect sb_abc123 --series
```

## See also

- [`miren sandbox`](/command/sandbox)
