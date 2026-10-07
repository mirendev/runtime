---
title: "miren deploy target set-default"
sidebar_label: "deploy target set-default"
description: "Set the default deployment target"
---

# miren deploy target set-default

Set the default deployment target

## Usage

```bash
miren deploy target set-default <name> [flags]
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

**Make production the default:**

```bash
miren deploy target set-default prod
```

## See also

- [`miren deploy target`](./deploy-target.md)
