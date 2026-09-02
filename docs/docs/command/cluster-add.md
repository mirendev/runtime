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
- `--as` — Local name to store the cluster under, when it should differ from its name in Miren Cloud
- `--cluster, -c` — Name of the cluster to add, looked up in Miren Cloud unless --address is given (optional - will list available)
- `--force, -f` — Overwrite existing cluster configuration
- `--identity, -i` — Name of the identity to use (optional - will use the only one if single)
- `--organization` — Organization the named cluster belongs to, for when the same name exists in more than one
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

**Add a cluster by name, without the picker:**

```bash
miren cluster add --cluster my-cluster
```

**Add a cluster by name under a different local name:**

```bash
miren cluster add --cluster my-cluster --as staging
```

**Add a cluster with a specific address:**

```bash
miren cluster add --cluster my-cluster --address 10.0.0.1:8443
```

## See also

- [`miren cluster`](/command/cluster)
