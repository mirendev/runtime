---
title: "miren apps"
sidebar_label: "apps"
description: "Deprecated: use 'miren app list'"
---

# miren apps

Deprecated: use 'miren app list'

## Usage

```bash
miren apps [flags]
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

**List all apps:**

```bash
miren app list
```

**List apps as JSON:**

```bash
miren app list --format json
```
