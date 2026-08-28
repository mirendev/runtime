---
title: "miren route list"
sidebar_label: "route list"
description: "List all HTTP routes"
---

# miren route list

List all HTTP routes

## Usage

```bash
miren route list [flags]
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

**List all routes:**

```bash
miren route list
```

**List as JSON:**

```bash
miren route list --format json
```

## See also

- [`miren route`](/command/route)
