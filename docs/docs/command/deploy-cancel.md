---
title: "miren deploy cancel"
sidebar_label: "deploy cancel"
description: "Cancel an in-progress deployment"
---

# miren deploy cancel

Cancel an in-progress deployment

## Usage

```bash
miren deploy cancel [flags]
```

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--deployment, -d` — ID of the deployment to cancel

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Cancel the current deployment:**

```bash
miren deploy cancel
```

**Cancel a specific deployment:**

```bash
miren deploy cancel -d dep_abc123
```

## See also

- [`miren deploy`](./deploy.md)
