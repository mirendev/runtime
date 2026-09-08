---
title: "miren debug rbac"
sidebar_label: "debug rbac"
description: "Fetch and display RBAC rules from miren.cloud"
---

# miren debug rbac

Fetch and display RBAC rules from miren.cloud

## Usage

```bash
miren debug rbac [flags]
```

## Flags

- `--dir, -d` — Registration directory (default: `/var/lib/miren/server`)
- `--raw, -r` — Show raw JSON response

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Subcommands

- [`miren debug rbac test`](./debug-rbac-test.md) — Test RBAC evaluation with fetched rules

## See also

- [`miren debug`](./debug.md)
