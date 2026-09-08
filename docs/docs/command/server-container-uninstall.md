---
title: "miren server container uninstall"
sidebar_label: "server container uninstall"
description: "Uninstall miren server container"
---

# miren server container uninstall

Uninstall miren server container

## Usage

```bash
miren server container uninstall [flags]
```

## Flags

- `--force, -f` — Force removal even if container is running
- `--name, -n` — Container name (default: `miren`)
- `--remove-volume` — Remove the data volume
- `--runtime` — Container runtime to use: docker or podman (auto-detected by default, preferring docker)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Uninstall the container:**

```bash
miren server container uninstall
```

**Uninstall and remove all data:**

```bash
miren server container uninstall --remove-volume
```

## See also

- [`miren server container`](./server-container.md)
