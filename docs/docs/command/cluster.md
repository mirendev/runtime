---
title: "miren cluster"
sidebar_label: "cluster"
description: "List configured clusters"
---

# miren cluster

List configured clusters

## Usage

```bash
miren cluster [flags]
```

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**List clusters:**

```bash
miren cluster
```

## Subcommands

- [`miren cluster add`](/command/cluster-add) — Add a new cluster configuration
- [`miren cluster available`](/command/cluster-available) — List the clusters Miren Cloud has for your account
- [`miren cluster current`](/command/cluster-current) — Show the pinned cluster for this app
- [`miren cluster export-address`](/command/cluster-export-address) — Export cluster address with TLS fingerprint for MIREN_CLUSTER
- [`miren cluster list`](/command/cluster-list) — List all configured clusters
- [`miren cluster remove`](/command/cluster-remove) — Remove a cluster from the configuration
- [`miren cluster switch`](/command/cluster-switch) — Switch to a different cluster
