---
title: "miren deploy target remove"
sidebar_label: "deploy target remove"
description: "Deprecated: use 'miren target remove'"
---

# miren deploy target remove

Deprecated: use 'miren target remove'

## Usage

```bash
miren deploy target remove <name> [flags]
```

## Arguments

- `name` — Deployment target name

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

## Examples

**Remove a target:**

```bash
miren target remove staging
```

## See also

- [`miren deploy target`](./deploy-target.md)
