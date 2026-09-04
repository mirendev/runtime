---
title: "miren disk accelerator install"
sidebar_label: "disk accelerator install"
description: "Build and load the lbd kernel module for this kernel"
---

# miren disk accelerator install

Build and load the lbd kernel module for this kernel

## Usage

```bash
miren disk accelerator install [flags]
```

## Flags

- `--data-path` — Path to miren data (default: `/var/lib/miren`)
- `--force, -f` — Rebuild even when the module is already current
- `--image` — Override the builder image
- `--socket` — Path to the containerd socket

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Enable accelerator mode:**

```bash
sudo miren disk accelerator install
```

**Rebuild after a kernel upgrade:**

```bash
sudo miren disk accelerator install --force
```

## See also

- [`miren disk accelerator`](/command/disk-accelerator)
