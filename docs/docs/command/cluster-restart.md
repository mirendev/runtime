---
title: "miren cluster restart"
sidebar_label: "cluster restart"
description: "Restart the active cluster's server and wait for it to report ready"
---

# miren cluster restart

Restart the active cluster's server and wait for it to report ready

## Usage

```bash
miren cluster restart [flags]
```

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--yes, -y` — Skip the confirmation prompt

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Restart a named cluster:**

```bash
miren cluster restart -C production
```

## See also

- [`miren cluster`](./cluster.md)
