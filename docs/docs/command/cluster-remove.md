---
title: "miren cluster remove"
sidebar_label: "cluster remove"
description: "Remove a cluster from the configuration"
---

# miren cluster remove

Remove a cluster from the configuration

## Usage

```bash
miren cluster remove <cluster> [flags]
```

## Arguments

- `cluster` — Name of the cluster to remove

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Remove a cluster:**

```bash
miren cluster remove my-cluster
```

## See also

- [`miren cluster`](./cluster.md)
