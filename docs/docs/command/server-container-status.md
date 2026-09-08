---
title: "miren server container status"
sidebar_label: "server container status"
description: "Show status of miren server container"
---

# miren server container status

Show status of miren server container

## Usage

```bash
miren server container status [flags]
```

## Flags

- `--follow, -f` — Follow logs in real-time
- `--name, -n` — Container name (default: `miren`)
- `--runtime` — Container runtime to use: docker or podman (auto-detected by default, preferring docker)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Show status:**

```bash
miren server container status
```

**Follow logs:**

```bash
miren server container status --follow
```

## See also

- [`miren server container`](./server-container.md)
