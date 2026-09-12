---
title: "miren route set-default"
sidebar_label: "route set-default"
description: "Set an app as the default route"
---

# miren route set-default

Set an app as the default route

## Usage

```bash
miren route set-default <appname> [flags]
```

## Arguments

- `appname` — Application name to set as default route

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Set the default route:**

```bash
miren route set-default myapp
```

## See also

- [`miren route`](./route.md)
