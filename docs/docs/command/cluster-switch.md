---
title: "miren cluster switch"
sidebar_label: "cluster switch"
description: "Switch to a different cluster"
---

# miren cluster switch

Switch to a different cluster

## Usage

```bash
miren cluster switch <cluster> [flags]
```

## Arguments

- `cluster` — Name of the cluster to switch to

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Switch to a cluster:**

```bash
miren cluster switch production
```

## See also

- [`miren cluster`](./cluster.md)
