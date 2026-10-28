---
title: "miren target remove"
sidebar_label: "target remove"
description: "Remove a deployment target"
---

# miren target remove

Remove a deployment target

## Usage

```bash
miren target remove <name> [flags]
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

- [`miren target`](./target.md)
