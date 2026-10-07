---
title: "miren deploy target list"
sidebar_label: "deploy target list"
description: "List deployment targets"
---

# miren deploy target list

List deployment targets

## Usage

```bash
miren deploy target list [flags]
```

## Flags

- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json

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

## See also

- [`miren deploy target`](./deploy-target.md)
