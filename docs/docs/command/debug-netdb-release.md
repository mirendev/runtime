---
title: "miren debug netdb release"
sidebar_label: "debug netdb release"
description: "Manually release IP leases"
---

# miren debug netdb release

Manually release IP leases

## Usage

```bash
miren debug netdb release [flags]
```

## Flags

- `--all, -a` — Release all reserved IPs (use with caution)
- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--force, -f` — Skip confirmation prompt
- `--ip, -i` — Specific IP to release
- `--subnet, -s` — Release all reserved IPs in subnet

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren debug netdb`](./debug-netdb.md)
