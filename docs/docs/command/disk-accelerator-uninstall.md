---
title: "miren disk accelerator uninstall"
sidebar_label: "disk accelerator uninstall"
description: "Unload and remove the lbd kernel module"
---

# miren disk accelerator uninstall

Unload and remove the lbd kernel module

## Usage

```bash
miren disk accelerator uninstall [flags]
```

## Flags

- `--data-path` — Path to miren data (default: `/var/lib/miren`)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Go back to loop devices:**

```bash
sudo miren disk accelerator uninstall
```

## See also

- [`miren disk accelerator`](/command/disk-accelerator)
