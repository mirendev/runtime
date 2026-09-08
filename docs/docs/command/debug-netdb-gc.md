---
title: "miren debug netdb gc"
sidebar_label: "debug netdb gc"
description: "Find and release orphaned IP leases"
---

# miren debug netdb gc

Find and release orphaned IP leases

## Usage

```bash
miren debug netdb gc [flags]
```

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--dry-run, -n` — Show what would be released without making changes
- `--force, -f` — Skip confirmation prompt
- `--subnet, -s` — Only GC IPs in this subnet

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren debug netdb`](./debug-netdb.md)
