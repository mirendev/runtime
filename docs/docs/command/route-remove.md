---
title: "miren route remove"
sidebar_label: "route remove"
description: "Deprecated: use 'miren route delete'"
---

# miren route remove

Deprecated: use 'miren route delete'

## Usage

```bash
miren route remove [host] [flags]
```

## Arguments

- `host` — Hostname of the route to remove

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--yes, -y` — Skip confirmation prompt

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Remove a route:**

```bash
miren route delete example.com
```

## See also

- [`miren route`](./route.md)
