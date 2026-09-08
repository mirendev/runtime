---
title: "miren server status"
sidebar_label: "server status"
description: "Show miren service status"
---

# miren server status

Show miren service status

## Usage

```bash
miren server status [flags]
```

## Flags

- `--follow, -f` — Follow logs in real-time

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Show server status:**

```bash
miren server status
```

**Follow server logs:**

```bash
miren server status --follow
```

## See also

- [`miren server`](./server.md)
