---
title: "miren route unprotect"
sidebar_label: "route unprotect"
description: "Remove identity-provider protection from an HTTP route"
---

# miren route unprotect

Remove identity-provider protection from an HTTP route

## Usage

```bash
miren route unprotect <host> [flags]
```

## Arguments

- `host` — Hostname for the route (e.g., example.com); omit and pass --default for the default route

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--default` — Remove protection from the default route (instead of a hostname)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Remove protection from a route:**

```bash
miren route unprotect example.com
```

## See also

- [`miren route`](./route.md)
