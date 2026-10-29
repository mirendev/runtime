---
title: "miren cloud logout"
sidebar_label: "cloud logout"
description: "Remove local authentication credentials"
---

# miren cloud logout

Remove local authentication credentials

## Usage

```bash
miren cloud logout [flags]
```

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--identity, -i` — Name of the identity to remove

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Logout:**

```bash
miren cloud logout
```

## See also

- [`miren cloud`](./cloud.md)
