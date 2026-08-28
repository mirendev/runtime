---
title: "miren route show"
sidebar_label: "route show"
description: "Show details of an HTTP route"
---

# miren route show

Show details of an HTTP route

## Usage

```bash
miren route show <host> [flags]
```

## Arguments

- `host` — Hostname of the route to show; omit and pass --default for the default route

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--default` — Show the default route (instead of a hostname)
- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Show route details:**

```bash
miren route show example.com
```

## See also

- [`miren route`](/command/route)
