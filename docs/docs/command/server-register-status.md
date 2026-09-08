---
title: "miren server register status"
sidebar_label: "server register status"
description: "Show cluster registration status"
---

# miren server register status

Show cluster registration status

## Usage

```bash
miren server register status [flags]
```

## Flags

- `--dir, -d` — Registration directory (default: `/var/lib/miren/server`)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Check registration status:**

```bash
miren server register status
```

## See also

- [`miren server register`](./server-register.md)
