---
title: "miren cluster add"
sidebar_label: "cluster add"
description: "Add a new cluster configuration"
---

# miren cluster add

Add a new cluster configuration

## Usage

```bash
miren cluster add [flags]
```

## Flags

- `--address, -a` — Address/hostname of the cluster (optional - will use from selected cluster)
- `--cluster, -c` — Name of the cluster to create (optional - will list available)
- `--force, -f` — Overwrite existing cluster configuration
- `--identity, -i` — Name of the identity to use (optional - will use the only one if single)
- `--via-cloud` — Reach the cluster through Miren Cloud instead of dialing it, for a cluster this machine has no route to

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Add a cluster interactively:**

```bash
miren cluster add
```

**Add a cluster with a specific address:**

```bash
miren cluster add --cluster my-cluster --address 10.0.0.1:8443
```

## See also

- [`miren cluster`](/command/cluster)
