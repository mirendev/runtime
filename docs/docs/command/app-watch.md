---
title: "miren app watch"
sidebar_label: "app watch"
description: "Watch application health and resource usage"
---

# miren app watch

Watch application health and resource usage

## Usage

```bash
miren app watch [flags]
```

## Flags

- `--format` — Output format (text, json) (default: `text`)
- `--graph, -g` — Graph the app stats
- `--json` — Shorthand for --format json
- `--watch, -w` — Watch the app stats

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

- [`miren app`](./app.md)
