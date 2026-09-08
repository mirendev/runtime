---
title: "miren cluster export-address"
sidebar_label: "cluster export-address"
description: "Export cluster address with TLS fingerprint for MIREN_CLUSTER"
---

# miren cluster export-address

Export cluster address with TLS fingerprint for MIREN_CLUSTER

## Usage

```bash
miren cluster export-address [flags]
```

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Export active cluster:**

```bash
miren cluster export-address
```

**Export specific cluster:**

```bash
miren cluster export-address -C my-cluster
```

## See also

- [`miren cluster`](./cluster.md)
