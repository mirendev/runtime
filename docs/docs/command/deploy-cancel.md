---
title: "miren deploy cancel"
sidebar_label: "deploy cancel"
description: "Deprecated: use 'miren deployment cancel'"
---

# miren deploy cancel

Deprecated: use 'miren deployment cancel'

## Usage

```bash
miren deploy cancel [deployment] [flags]
```

## Arguments

- `deployment` — ID of the deployment to cancel

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--deployment, -d` — Deprecated: pass the deployment ID as an argument

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Cancel the current deployment:**

```bash
miren deployment cancel dep_abc123
```

**Cancel a specific deployment:**

```bash
miren deployment cancel dep_abc123
```

## See also

- [`miren deploy`](./deploy.md)
