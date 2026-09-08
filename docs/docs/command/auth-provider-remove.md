---
title: "miren auth provider remove"
sidebar_label: "auth provider remove"
description: "Remove an identity provider"
---

# miren auth provider remove

Remove an identity provider

## Usage

```bash
miren auth provider remove <name> [flags]
```

## Arguments

- `name` — Name of the identity provider to remove

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--force` — Remove the provider even if it is attached to routes

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren auth provider`](./auth-provider.md)
