---
title: "miren cloud whoami"
sidebar_label: "cloud whoami"
description: "Display information about the current authenticated user"
---

# miren cloud whoami

Display information about the current authenticated user

## Usage

```bash
miren cloud whoami [flags]
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

**Show current user:**

```bash
miren cloud whoami
```

**JSON output:**

```bash
miren cloud whoami --json
```

## See also

- [`miren cloud`](./cloud.md)
