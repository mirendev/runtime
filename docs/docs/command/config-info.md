---
title: "miren config info"
sidebar_label: "config info"
description: "Show configuration file locations and format"
---

# miren config info

Show configuration file locations and format

## Usage

```bash
miren config info [flags]
```

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Show config info:**

```bash
miren config info
```

## See also

- [`miren config`](./config.md)
