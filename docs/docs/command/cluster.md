---
title: "miren cluster"
sidebar_label: "cluster"
description: "Manage configured clusters"
---

# miren cluster

Manage configured clusters

## Usage

```bash
miren cluster [flags]
```

## Subcommands

- [`miren cluster add`](./cluster-add.md) — Add a new cluster configuration
- [`miren cluster available`](./cluster-available.md) — List the clusters Miren Cloud has for your account
- [`miren cluster current`](./cluster-current.md) — Show the globally selected cluster
- [`miren cluster export-address`](./cluster-export-address.md) — Export cluster address with TLS fingerprint for MIREN_CLUSTER
- [`miren cluster list`](./cluster-list.md) — List all configured clusters
- [`miren cluster remove`](./cluster-remove.md) — Remove a cluster from the configuration
- [`miren cluster restart`](./cluster-restart.md) — Restart the active cluster's server and wait for it to report ready
- [`miren cluster switch`](./cluster-switch.md) — Switch to a different cluster
- [`miren cluster upgrade`](./cluster-upgrade.md) — A remote cluster: upgrade its server and wait for it to report ready
