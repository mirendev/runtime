---
title: "miren target add"
sidebar_label: "target add"
description: "Add a deployment target"
---

# miren target add

Add a deployment target

## Usage

```bash
miren target add [name] [clustername] [flags]
```

## Arguments

- `name` — Name for the deployment target (prompts when omitted)
- `clustername` — Configured cluster name (prompts when omitted)

## Flags

- `--default` — Make this the default deployment target

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

**Select a configured cluster interactively:**

```bash
miren target add staging
```

**Add a target non-interactively:**

```bash
miren target add prod my-prod-cluster
```

**Add the default target:**

```bash
miren target add staging my-staging-cluster --default
```

## See also

- [`miren target`](./target.md)
