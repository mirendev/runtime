---
title: "miren whoami"
sidebar_label: "whoami"
description: "Alias for 'miren cloud whoami'"
---

# miren whoami

Alias for 'miren cloud whoami'

## Usage

```bash
miren whoami [flags]
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
