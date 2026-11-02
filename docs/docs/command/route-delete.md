---
title: "miren route delete"
sidebar_label: "route delete"
description: "Remove an HTTP route"
---

# miren route delete

Remove an HTTP route

## Usage

```bash
miren route delete [host] [flags]
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
