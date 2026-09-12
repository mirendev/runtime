---
title: "miren disk accelerator install"
sidebar_label: "disk accelerator install"
description: "Build and load the lbd kernel module for this kernel"
---

# miren disk accelerator install

Build and load the lbd kernel module for this kernel

## Usage

```bash
miren disk accelerator install <node> [flags]
```

## Arguments

- `node` — Runner to install on (name, ID, or short ID)

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--force, -f` — Rebuild even when the module is already current

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Enable accelerator mode on a runner:**

```bash
miren disk accelerator install runner1
```

**Rebuild after a kernel upgrade:**

```bash
miren disk accelerator install runner1 --force
```

## See also

- [`miren disk accelerator`](./disk-accelerator.md)
