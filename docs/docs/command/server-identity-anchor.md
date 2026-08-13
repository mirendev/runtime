---
title: "miren server identity-anchor"
sidebar_label: "server identity-anchor"
description: "Move where this cluster's workload identity is anchored"
---

# miren server identity-anchor

Move where this cluster's workload identity is anchored

## Usage

```bash
miren server identity-anchor <anchor> [flags]
```

## Arguments

- `anchor` — Where to anchor workload identity: cluster or cloud

## Flags

- `--data-path, -d` — Server data path (default: `/var/lib/miren`)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Let miren.cloud serve discovery for this cluster:**

```bash
miren server identity-anchor cloud
```

**Serve discovery from the cluster itself:**

```bash
miren server identity-anchor cluster
```

## See also

- [`miren server`](/command/server)
