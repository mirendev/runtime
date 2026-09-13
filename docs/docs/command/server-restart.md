---
title: "miren server restart"
sidebar_label: "server restart"
description: "Restart the systemd-managed miren server and wait for it to report ready"
---

# miren server restart

Restart the systemd-managed miren server and wait for it to report ready

## Usage

```bash
miren server restart [flags]
```

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Restart the server:**

```bash
sudo miren server restart
```

## See also

- [`miren server`](./server.md)
